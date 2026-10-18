// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// DefaultFabricID is the fabric a controller of the site administers when
// its options name none — matter.js MockSite.addController's admin fabric
// id 1 (mock-site.ts:112).
const DefaultFabricID uint64 = 1

// ControllerOptions configures [Site.AddController].
type ControllerOptions struct {
	// FabricID is the fabric the controller creates; 0 is
	// [DefaultFabricID].
	FabricID uint64
	// NodeID is the controller's node id on the fabric, its root node id;
	// 0 is bridgetest.DefaultControllerNodeID (chip-tool's 112233).
	NodeID uint64
	// VendorID is the fabric's admin vendor id; 0 is 0xFFF1.
	VendorID uint16
	// Label is the fabric label; empty is "fabrictest".
	Label string
	// CATs are the CASE Authenticated Tags of the controller's NOC.
	CATs []uint32
	// Logger receives the controller's logs; nil is the site's logger.
	Logger *slog.Logger
	// Registry, when set, edits the configuration of the client node
	// registry before it is built, on every start of the controller
	// ([Controller.Restart] included): its hooks, AutoStart, the node
	// template. Its Store is the controller's own [client.MemoryStore],
	// kept across restarts; an edit may replace it.
	Registry func(*client.RegistryConfig)
	// Attestation is the commissioner's trust store; nil is
	// attestation.TestStore (the CSA test PAAs, test roots accepted).
	Attestation attestation.PAAStore
	// NoAttestationStore commissions without a trust store — matter.js's
	// controller without a DclCertificateService: every attestation
	// carries the DclServiceUnavailable warning. It overrides Attestation.
	NoAttestationStore bool
}

// Controller is an in-process Matter controller of a site: the module's
// own controller stack (controller/, ADR 0020) wired as an application
// wires it — an exchange manager on the controller's socket, the DNS-SD
// cache on the site's answers, a certificate authority, a fabric, the peer
// set, the subscription registry, the commissioner with its CASE
// responder, and the client node registry — every part on the site's
// clock. matter.js's MockSite.addController builds a ServerNode with the
// controller behavior (mock-site.ts:112); this is the Go equivalent. It is
// safe for concurrent use.
type Controller struct {
	site  *Site
	index int
	opts  ControllerOptions

	listener *udp.Listener
	mgr      *exchange.Manager
	names    *discovery.Names
	ca       *ca.CA
	fabric   *commissioner.Fabric
	resume   *peer.MemoryResumptionStore
	set      *peer.Set
	subs     *interaction.Subscriptions
	com      *commissioner.Commissioner
	reg      *client.Registry

	// caStore keeps the certificate authority and the fabric, resume the
	// CASE resumption records and regStore the client nodes, across a
	// [Controller.Restart] — what matter.js's MockStorageService keeps of
	// a ServerNode stopped and started again.
	caStore  *ca.MemoryStore
	regStore client.Store

	// mu guards the stack's fields against [Controller.Restart] for the
	// site's resolver, which reads them from the devices' goroutines.
	mu     sync.RWMutex
	stop   context.CancelFunc
	served chan struct{}
	once   sync.Once
}

// AddController builds a controller and returns it, closed with the site.
// It commissions nothing until asked ([Controller.Commission]); a
// controller that fails to build fails the test.
func (s *Site) AddController(opts ControllerOptions) *Controller {
	s.tb.Helper()
	if opts.FabricID == 0 {
		opts.FabricID = DefaultFabricID
	}
	if opts.NodeID == 0 {
		opts.NodeID = bridgetest.DefaultControllerNodeID
	}
	if opts.VendorID == 0 {
		opts.VendorID = bridgetest.DefaultAdminVendorID
	}
	if opts.Label == "" {
		opts.Label = "fabrictest"
	}
	if opts.Logger == nil {
		opts.Logger = s.log
	}
	c := &Controller{site: s, index: s.index(), opts: opts, caStore: &ca.MemoryStore{}, resume: &peer.MemoryResumptionStore{}, regStore: &client.MemoryStore{}}
	if err := c.start(); err != nil {
		c.Close()
		s.tb.Fatalf("fabrictest: controller %d: %v", c.index, err)
	}
	s.mu.Lock()
	s.controllers = append(s.controllers, c)
	s.mu.Unlock()
	s.onClose(c.Close)
	return c
}

func (c *Controller) start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	clk, log := c.site.clk, c.opts.Logger
	l, err := c.site.listen(c.index)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	c.listener = l
	if c.mgr, err = exchange.NewManager(l, exchange.Config{Clock: clk, Logger: log}); err != nil {
		return fmt.Errorf("exchange manager: %w", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	c.stop, c.served = stop, make(chan struct{})
	go func() {
		defer close(c.served)
		_ = l.Serve(ctx, c.mgr.HandleDatagram)
	}()
	c.names = discovery.NewNames(newQueryConn(c.site), log)
	if c.ca, err = ca.New(ca.Config{Store: c.caStore, Clock: clk}); err != nil {
		return fmt.Errorf("certificate authority: %w", err)
	}
	if c.fabric, err = commissioner.NewFabric(c.ca, commissioner.FabricConfig{
		FabricID: c.opts.FabricID, RootNodeID: c.opts.NodeID, VendorID: c.opts.VendorID, Label: c.opts.Label, CATs: c.opts.CATs,
		Store: c.caStore,
	}); err != nil {
		return fmt.Errorf("fabric: %w", err)
	}
	if c.set, err = peer.NewSet(peer.Context{
		Exchanges: c.mgr, Names: c.names, Fabrics: c.fabric.Lookup, Resumption: c.resume, Logger: log, Clock: clk,
	}); err != nil {
		return fmt.Errorf("peer set: %w", err)
	}
	c.subs = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: clk, Logger: log})
	// With Fabrics the commissioner runs the CASE responder that adopts
	// the sessions a device opens to the controller into the peer set.
	if c.com, err = commissioner.New(commissioner.Context{
		Exchanges: c.mgr, Peers: c.set, Attestation: c.trustStore(), Logger: log, Clock: clk,
		Subscriptions: c.subs, Fabrics: []*commissioner.Fabric{c.fabric}, Resumption: c.resume,
	}); err != nil {
		return fmt.Errorf("commissioner: %w", err)
	}
	rc := client.RegistryConfig{
		Store: c.regStore, Commissioner: c.com, Peers: c.set, Exchanges: c.mgr, Fabrics: []*commissioner.Fabric{c.fabric},
		Subscriptions: c.subs, Logger: log, Clock: clk, Node: client.Config{Logger: log, Clock: clk},
	}
	if c.opts.Registry != nil {
		c.opts.Registry(&rc)
		c.regStore = rc.Store
	}
	if c.reg, err = client.NewRegistry(rc); err != nil {
		return fmt.Errorf("client registry: %w", err)
	}
	return nil
}

// trustStore is the commissioner's attestation trust store.
func (c *Controller) trustStore() attestation.PAAStore {
	switch {
	case c.opts.NoAttestationStore:
		return nil
	case c.opts.Attestation != nil:
		return c.opts.Attestation
	}
	return attestation.TestStore()
}

// Close stops the controller. It is idempotent.
func (c *Controller) Close() {
	c.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		c.shutdown(ctx)
	})
}

// Restart stops the controller and starts it again on its stores and
// address — matter.js's controller ServerNode stopped and started again
// on its MockStorageService (the "after the controller restarts" cases of
// ClientNodeTest.ts): the certificate authority, the fabric, the CASE
// resumption records and the client node registry's store survive; the
// exchange manager, DNS-SD cache, peer set, subscriptions, commissioner
// and registry are new, the registry restoring the stored nodes. A pair's
// Peer and Interaction belong to the stopped stack; use the registry's
// nodes after a restart. A site on loopback sockets gives the controller a
// new port.
func (c *Controller) Restart(ctx context.Context) error {
	c.shutdown(ctx)
	if err := c.start(); err != nil {
		return fmt.Errorf("fabrictest: restart controller %d: %w", c.index, err)
	}
	return nil
}

// shutdown stops the controller's stack, leaving its stores.
func (c *Controller) shutdown(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	{
		if c.reg != nil {
			_ = c.reg.Close(ctx)
		}
		if c.subs != nil {
			_ = c.subs.Close(ctx)
		}
		if c.com != nil {
			c.com.Close()
		}
		if c.set != nil {
			c.set.Close(ctx)
		}
		if c.mgr != nil {
			c.mgr.Close()
		}
		if c.stop != nil {
			c.stop()
			<-c.served
		}
		if c.listener != nil {
			_ = c.listener.Close()
		}
		if c.names != nil {
			_ = c.names.Close()
		}
	}
}

// Index is the controller's host index on the site, from 1.
func (c *Controller) Index() int { return c.index }

// Addr is the controller's UDP address.
func (c *Controller) Addr() *net.UDPAddr { return c.listener.LocalAddr() }

// Exchanges is the controller's exchange manager.
func (c *Controller) Exchanges() *exchange.Manager {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mgr
}

// Names is the controller's DNS-SD cache, answered by the site.
func (c *Controller) Names() *discovery.Names {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.names
}

// CA is the controller's certificate authority.
func (c *Controller) CA() *ca.CA {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ca
}

// Fabric is the controller's fabric.
func (c *Controller) Fabric() *commissioner.Fabric {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fabric
}

// Peers is the controller's peer set.
func (c *Controller) Peers() *peer.Set {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.set
}

// Resumption is the peer set's CASE resumption store.
func (c *Controller) Resumption() *peer.MemoryResumptionStore {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resume
}

// Subscriptions is the controller's subscription registry.
func (c *Controller) Subscriptions() *interaction.Subscriptions {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subs
}

// Commissioner is the controller's commissioner.
func (c *Controller) Commissioner() *commissioner.Commissioner {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.com
}

// Registry is the controller's client node registry — matter.js's Peers,
// the controller ServerNode's container of client nodes.
func (c *Controller) Registry() *client.Registry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.reg
}

// answers reports whether the controller is node nodeID of the fabric
// with compressedFabricID.
func (c *Controller) answers(compressedFabricID [8]byte, nodeID uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fabric != nil && c.fabric.Identity.CompressedFabricID == compressedFabricID && c.opts.NodeID == nodeID
}

// CommissionOptions configures [Controller.Commission].
type CommissionOptions struct {
	// NodeID is the node id the device is assigned; 0 lets the
	// commissioner draw a random one no peer of the fabric has.
	NodeID uint64
	// Edit, when set, edits the commissioner's options before the flow
	// runs: an attestation policy, a regulatory location, a timeout.
	Edit func(*commissioner.Options)
}

// Commission commissions d onto the controller's fabric — the commissioner's
// on-network flow over d's address, PASE through CommissioningComplete
// over CASE — and returns the pair, whose peer holds the CASE session.
// With a fake site clock it runs under [Site.Run].
func (c *Controller) Commission(ctx context.Context, d *Device, opts CommissionOptions) (*Pair, error) {
	o := commissioner.Options{
		Fabric: c.fabric, Passcode: d.Passcode(), NodeID: opts.NodeID, Addresses: []*net.UDPAddr{d.Addr()},
	}
	if opts.Edit != nil {
		opts.Edit(&o)
	}
	var res commissioner.Result
	err := c.site.Run(ctx, func(ctx context.Context) error {
		var err error
		res, err = c.com.Commission(ctx, o)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("fabrictest: commission device %d: %w", d.index, err)
	}
	p := c.set.Get(res.Address)
	if p == nil {
		return nil, errors.New("fabrictest: the commissioned device has no peer")
	}
	return &Pair{Controller: c, Device: d, Address: res.Address, Peer: p, Interaction: c.com.Interaction(p)}, nil
}

// Pair is a controller and a device of one site: commissioned
// ([Site.CommissionedPair], [Controller.Commission]) or not
// ([Site.UncommissionedPair]).
type Pair struct {
	Controller *Controller
	Device     *Device
	// Address is the device's peer address on the controller's fabric;
	// zero before commissioning.
	Address handshake.PeerAddress
	// Peer is the controller's peer of the device, holding the CASE
	// session; nil before commissioning.
	Peer *peer.Peer
	// Interaction is the controller's interaction with the device over
	// Peer; nil before commissioning. A pair of [Controller.PASE] speaks
	// over PASESession instead.
	Interaction *interaction.Interaction
	// PASESession is the PASE session of a pair of [Controller.PASE]; nil
	// otherwise.
	PASESession *exchange.Session
}

// Commissioned reports whether the pair's device is commissioned.
func (p *Pair) Commissioned() bool { return p.Peer != nil }

// Node is the controller's client node of the device — the registry's
// node for its peer address, created bound to the peer, neither started
// nor subscribed (client.Registry.ForAddress).
func (p *Pair) Node() (*client.Node, error) {
	if !p.Commissioned() {
		return nil, errors.New("fabrictest: the pair is not commissioned")
	}
	return p.Controller.Registry().ForAddress(p.Address, "")
}

// PairOptions configures [Site.CommissionedPair] and
// [Site.UncommissionedPair] — matter.js MockSite.PairOptions
// (mock-site.ts:244).
type PairOptions struct {
	// Device is the device's topology.
	Device Topology
	// Controller configures the controller.
	Controller ControllerOptions
	// Commission configures the commissioning of a commissioned pair.
	Commission CommissionOptions
}

// pairTimeout bounds the commissioning of [Site.CommissionedPair] —
// MockSite.addCommissionedPair's 90 s (mock-site.ts:156), on the wall clock.
const pairTimeout = 90 * time.Second

// UncommissionedPair adds a controller and a device without commissioning
// them — MockSite.addUncommissionedPair (mock-site.ts:144).
func (s *Site) UncommissionedPair(opts PairOptions) *Pair {
	s.tb.Helper()
	c := s.AddController(opts.Controller)
	d := s.AddDevice(opts.Device)
	return &Pair{Controller: c, Device: d}
}

// CommissionedPair adds a controller and a device and commissions the
// device onto the controller's fabric — MockSite.addCommissionedPair
// (mock-site.ts:156). A failed commissioning fails the test.
func (s *Site) CommissionedPair(opts PairOptions) *Pair {
	s.tb.Helper()
	u := s.UncommissionedPair(opts)
	ctx, cancel := context.WithTimeout(context.Background(), pairTimeout)
	defer cancel()
	p, err := u.Controller.Commission(ctx, u.Device, opts.Commission)
	if err != nil {
		s.tb.Fatal(err)
	}
	return p
}

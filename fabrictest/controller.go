// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/internal/inflight"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/tcp"
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
	// Networks overrides the network profiles' templates (matter.js
	// `network: { profiles }`, NetworkServer.ts:248 at 862ad75f); nil keeps
	// the defaults.
	Networks *peer.ProfilesConfig
	// OwnNetworkProfile is the profile of our own network, whose margin
	// pads every exchange's sends (ownNetworkProfileId,
	// NetworkServer.ts:242); "" is "fast".
	OwnNetworkProfile string
	// TCP is the controller's TCP roles (controller.TCPConfig). TCP runs
	// over kernel sockets: it needs a site with Options.Loopback, as a
	// device with TCP does. The fabrictest Controller dials (Outgoing);
	// Incoming is controller.New's ([Site.ControllerConfig]).
	TCP controller.TCPConfig
	// TransportPreference is each peer's preferred transport
	// (controller.Config.TransportPreference).
	TransportPreference exchange.Transport
	// OTA turns on controller.New's OTA push ([Site.ControllerConfig]);
	// the own node runs on the site unless OTA.StartNode is set. The
	// fabrictest Controller ignores it.
	OTA *controller.OTAConfig
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
	// group counts the work in hand of the controller's stack
	// (internal/inflight), kept across restarts.
	group *inflight.Group

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
	c.group = s.newGroup("controller " + strconv.Itoa(c.index))
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
	st, err := c.build()
	if err != nil {
		// What the failed build opened is closed again, the listener
		// among it: a host whose start failed holds no address, so the
		// next start (a Restart, a test's retry) can listen.
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		st.close(ctx)
		return err
	}
	c.mu.Lock()
	c.listener, c.mgr, c.stop, c.served, c.names = st.listener, st.mgr, st.stop, st.served, st.names
	c.ca, c.fabric, c.set, c.subs, c.com, c.reg = st.ca, st.fabric, st.set, st.subs, st.com, st.reg
	c.mu.Unlock()
	return nil
}

// dialTCP is the controller's TCP dialer over kernel sockets, each
// connection's messages handed to mgr (controller/tcp.go's).
func dialTCP(mgr *exchange.Manager, log *slog.Logger) peer.DialTCP {
	d := &tcp.Dialer{Logger: log}
	return func(ctx context.Context, addr *net.UDPAddr) (exchange.Channel, error) {
		conn, err := d.Dial(ctx, addr.String(), func(conn *tcp.Conn, msg []byte, _ func()) { mgr.HandleChannelMessage(conn, msg) })
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
}

// stack is one run of a controller's stack: what [Controller.start] builds
// and [Controller.shutdown] closes.
type stack struct {
	listener *udp.Listener
	mgr      *exchange.Manager
	stop     context.CancelFunc
	served   chan struct{}
	names    *discovery.Names
	ca       *ca.CA
	fabric   *commissioner.Fabric
	set      *peer.Set
	subs     *interaction.Subscriptions
	com      *commissioner.Commissioner
	reg      *client.Registry
}

// build builds a stack on the controller's stores and address, holding no
// lock: the stack is the controller's only once start stores it. On an
// error the stack returned holds what was built before it, for the caller
// to close.
func (c *Controller) build() (*stack, error) {
	clk, log := c.site.hostClock(c.group), c.opts.Logger
	st := &stack{}
	l, err := c.site.listen(c.index, c.group)
	if err != nil {
		return st, fmt.Errorf("listen: %w", err)
	}
	st.listener = l
	networks := peer.NewNetworkProfiles(clk, nil, log)
	if c.opts.Networks != nil {
		networks.SetDefaults(*c.opts.Networks)
	}
	own, err := networks.Get(cmp.Or(c.opts.OwnNetworkProfile, peer.NetworkFast))
	if err != nil {
		return st, err
	}
	xcfg := exchange.Config{Clock: clk, Logger: log, LocalAdditionalMrpDelay: own.AdditionalMrpDelay}
	if c.opts.TCP.Outgoing {
		p := exchange.DefaultLocalParameters()
		p.SupportedTransports |= sessionparams.TransportTCPClient
		p.MaxTCPMessageSize = sessionparams.DefaultMaxTCPMessageSize
		xcfg.LocalParameters = &p
	}
	if st.mgr, err = exchange.NewManager(l, xcfg); err != nil {
		return st, fmt.Errorf("exchange manager: %w", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	served, mgr := make(chan struct{}), st.mgr
	st.stop, st.served = stop, served
	go func() {
		defer close(served)
		_ = l.Serve(ctx, mgr.HandleDatagram)
	}()
	st.names = discovery.NewNames(newQueryConn(c.site), log)
	if st.ca, err = ca.New(ca.Config{Store: c.caStore, Clock: clk}); err != nil {
		return st, fmt.Errorf("certificate authority: %w", err)
	}
	if st.fabric, err = commissioner.NewFabric(st.ca, commissioner.FabricConfig{
		FabricID: c.opts.FabricID, RootNodeID: c.opts.NodeID, VendorID: c.opts.VendorID, Label: c.opts.Label, CATs: c.opts.CATs,
		Store: c.caStore,
	}); err != nil {
		return st, fmt.Errorf("fabric: %w", err)
	}
	pctx := peer.Context{
		Exchanges: st.mgr, Names: st.names, Fabrics: st.fabric.Lookup, Resumption: c.resume, Logger: log, Clock: clk,
		Networks: networks, TransportPreference: c.opts.TransportPreference,
	}
	if c.opts.TCP.Outgoing {
		pctx.DialTCP = dialTCP(mgr, log)
	}
	if st.set, err = peer.NewSet(pctx); err != nil {
		return st, fmt.Errorf("peer set: %w", err)
	}
	st.subs = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: clk, Logger: log})
	// With Fabrics the commissioner runs the CASE responder that adopts
	// the sessions a device opens to the controller into the peer set.
	if st.com, err = commissioner.New(commissioner.Context{
		Exchanges: st.mgr, Peers: st.set, Attestation: c.trustStore(), Logger: log, Clock: clk,
		Subscriptions: st.subs, Fabrics: []*commissioner.Fabric{st.fabric}, Resumption: c.resume,
	}); err != nil {
		return st, fmt.Errorf("commissioner: %w", err)
	}
	rc := client.RegistryConfig{
		Store: c.regStore, Commissioner: st.com, Peers: st.set, Exchanges: st.mgr, Fabrics: []*commissioner.Fabric{st.fabric},
		Subscriptions: st.subs, Logger: log, Clock: clk, Node: client.Config{Logger: log, Clock: clk},
	}
	if c.opts.Registry != nil {
		c.opts.Registry(&rc)
		c.regStore = rc.Store
	}
	if st.reg, err = client.NewRegistry(rc); err != nil {
		return st, fmt.Errorf("client registry: %w", err)
	}
	return st, nil
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
		// The stack's close waits for work that needs the clock: a CASE
		// attempt in flight awaits the acknowledgement of its last
		// StatusReport, which the device sends standalone after its ACK
		// timeout. With a fake clock the close runs under Site.Run, as
		// Restart's does, so that timer fires instead of the close
		// waiting on a clock nobody moves.
		_ = c.site.Run(ctx, func(ctx context.Context) error {
			c.shutdown(ctx)
			return nil
		})
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
	// The stack's close waits for timers of its own (a subscription's
	// last exchange); with a fake clock it runs under Site.Run.
	if err := c.site.Run(ctx, func(ctx context.Context) error {
		c.shutdown(ctx)
		return nil
	}); err != nil {
		return fmt.Errorf("fabrictest: restart controller %d: %w", c.index, err)
	}
	//nolint:contextcheck // the new stack's services run under contexts of their own, as at AddController
	if err := c.start(); err != nil {
		return fmt.Errorf("fabrictest: restart controller %d: %w", c.index, err)
	}
	return nil
}

// shutdown stops the controller's stack, leaving its stores. The fields
// are read under the lock and closed outside it, so the site's resolver
// is not held up while the stack winds down.
func (c *Controller) shutdown(ctx context.Context) {
	c.mu.RLock()
	st := &stack{
		listener: c.listener, mgr: c.mgr, stop: c.stop, served: c.served, names: c.names,
		set: c.set, subs: c.subs, com: c.com, reg: c.reg,
	}
	c.mu.RUnlock()
	st.close(ctx)
}

// close stops what of the stack was built, the last built first. The
// listener's Close returns once its address is free again.
func (st *stack) close(ctx context.Context) {
	if st.reg != nil {
		_ = st.reg.Close(ctx)
	}
	if st.subs != nil {
		_ = st.subs.Close(ctx)
	}
	if st.com != nil {
		st.com.Close()
	}
	if st.set != nil {
		st.set.Close(ctx)
	}
	if st.mgr != nil {
		_ = st.mgr.Shutdown(ctx)
	}
	if st.stop != nil {
		st.stop()
		<-st.served
	}
	if st.listener != nil {
		_ = st.listener.Close()
	}
	if st.names != nil {
		_ = st.names.Close()
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
	Address PeerAddress
	// Peer is the controller's peer of the device, holding the CASE
	// session; nil before commissioning.
	Peer *Peer
	// Interaction is the controller's interaction with the device over
	// Peer; nil before commissioning. A pair of [Controller.PASE] speaks
	// over PASESession instead.
	Interaction *Interaction
	// PASESession is the PASE session of a pair of [Controller.PASE]; nil
	// otherwise.
	PASESession *Session
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

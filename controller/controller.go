// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package controller is the controller role's composition root: one call,
// [New], builds what a controller program otherwise wires from nine
// packages — the database schemas, the certificate authority, our fabric,
// the UDP socket and its exchanges, the DNS-SD browser, the peers, the
// client subscriptions, the commissioner and the registry of client
// nodes — in the order the reference controller builds them, and
// [Controller.Close] tears them down in reverse. [Controller.Pair]
// commissions a node by its QR or manual pairing code; the nodes are
// controller/client's ([client.Node]: Read, Subscribe, Write, Invoke, the
// typed reads of the generated <cluster>def.StateFrom).
//
// It mirrors matter.js's CommissioningController
// (packages/matter.js/src/CommissioningController.ts at 862ad75f): one
// object owns one fabric and the nodes paired into it (:312), start brings
// the stack up and connects the commissioned nodes (:847-866),
// commissionNode discovers a node by its setup identifier and commissions
// it with its passcode (:462-500), close tears it down (:803). matter.js
// marks that class as the legacy surface of its controller, whose new
// form is a ServerNode with the controller behaviour; the Go root keeps
// the role's parts reachable ([Controller.Registry],
// [Controller.Commissioner], [Controller.Fabric]) as the seams
// docs/guides/advanced-seams.md lists, ADR 0026 principle 2 (one
// composition root per role).
package controller

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// Errors of [New] and [Controller.Pair].
var (
	// ErrConfiguration: a required [Config] field is missing.
	ErrConfiguration = errors.New("controller: configuration")
	// ErrNoDevice: no commissionable device answered for the code before
	// the pairing's deadline.
	ErrNoDevice = errors.New("controller: no commissionable device answered")
)

// DefaultVendorID is the vendor ID of our fabric when [Config] sets none:
// 0xFFF1, one of the test vendor IDs (0xFFF1–0xFFF4).
const DefaultVendorID uint16 = 0xFFF1

// DefaultPairTimeout bounds [Controller.Pair] when its context has no
// deadline.
const DefaultPairTimeout = time.Minute

// Config configures a [Controller].
type Config struct {
	// DB is the database the controller keeps its fabric, its CA and the
	// paired nodes in (required). The host opens it with the SQLite
	// driver of its choice; New applies the schemas (store.Apply,
	// sqlstore.Apply). The controller does not close it.
	DB *sql.DB
	// Attestation is the trust store of the Product Attestation
	// Authorities a node's attestation is checked against (required) —
	// attestation.TestStore() for development devices, a DCL-backed
	// store (controller/dcl) in production. There is no default: a
	// controller that trusts the test PAAs by accident commissions
	// counterfeit devices.
	Attestation attestation.PAAStore
	// Listen is the UDP address to listen on; "" is "[::]:0".
	Listen string
	// VendorID is our fabric's vendor ID on its first run; 0 is
	// [DefaultVendorID]. A loaded fabric keeps its own.
	VendorID uint16
	// Label is our fabric's label on its first run.
	Label string
	// Logger receives the stack's logs; nil is slog.Default().
	Logger *slog.Logger
	// Node configures every client node of the registry (client.Config;
	// its Logger defaults to Logger, its Clock to Clock).
	Node client.Config
	// HooksFor, when set, returns the hooks of the client node with local
	// id ("peer1", …) in place of Node.Hooks, so a host tells its nodes'
	// reports, events and subscription signals apart
	// (client.RegistryConfig.HooksFor) — matter.js gives each paired node
	// its own events (packages/matter.js/src/device/PairedNode.ts:276-282
	// at 862ad75f, attributeChanged and eventTriggered among them).
	HooksFor func(id string) client.Hooks
	// ManualStart leaves the commissioned nodes stopped when New returns;
	// false starts each, as matter.js's start connects them unless
	// autoConnect is false (CommissioningController.ts:864-866). A
	// program that only pairs, or reads once, sets it.
	ManualStart bool
	// Names opens the DNS-SD browser; nil is discovery.Open. A test
	// passes one over its own socket.
	Names func(*slog.Logger) (*discovery.Names, error)
	// Clock is the time source of the whole stack — the exchanges' and
	// MRP's timers, the certificate authority's issuing instant, the
	// peers' back-off, the subscriptions' liveness, the registry's
	// expiry; nil is the system clock. A test passes its fake clock
	// (fabrictest.Site.ControllerConfig).
	Clock clock.Clock
	// Listener, when set, is the UDP socket the controller serves on in
	// place of opening Listen: New serves its datagrams and Close closes
	// it. A test passes one on its own network
	// (fabrictest.Site.ControllerConfig); a program leaves it nil.
	Listener *udp.Listener
}

// Controller is a running controller: one fabric, the nodes paired into
// it, and the stack under them. Its methods are safe for concurrent use.
type Controller struct {
	db     *sql.DB
	log    *slog.Logger
	clock  clock.Clock
	fabric *commissioner.Fabric
	mgr    *exchange.Manager
	names  *discovery.Names
	peers  *peer.Set
	subs   *interaction.Subscriptions
	com    *commissioner.Commissioner
	reg    *client.Registry
	stop   func()
}

// New builds the controller over cfg.DB: the fabric is created on the
// first run (a random fabric ID and node ID, saved) and loaded
// afterwards; then the socket, DNS-SD, the peers, the commissioner and
// the registry, which loads the nodes paired before. A failure tears down
// what was built.
func New(ctx context.Context, cfg Config) (_ *Controller, err error) {
	switch {
	case cfg.DB == nil:
		return nil, fmt.Errorf("%w: DB is required", ErrConfiguration)
	case cfg.Attestation == nil:
		return nil, fmt.Errorf("%w: Attestation is required (attestation.TestStore() for development devices)", ErrConfiguration)
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	listen := cfg.Listen
	if listen == "" {
		listen = "[::]:0"
	}
	vendor := cfg.VendorID
	if vendor == 0 {
		vendor = DefaultVendorID
	}
	openNames := cfg.Names
	if openNames == nil {
		openNames = discovery.Open
	}
	clk := clock.Or(cfg.Clock)
	c := &Controller{db: cfg.DB, log: log, clock: clk, stop: func() {}}
	defer func() {
		if err != nil {
			_ = c.Close(context.WithoutCancel(ctx))
		}
	}()
	if err := errors.Join(store.Apply(ctx, cfg.DB), sqlstore.Apply(ctx, cfg.DB)); err != nil {
		return nil, err
	}
	keep := ca.SettingStore{Settings: store.New(cfg.DB)} // "controller.ca" and "controller.ca.fabric"
	authority, err := ca.New(ca.Config{Store: keep, Clock: clk})
	if err != nil {
		return nil, err
	}
	fcfg := commissioner.FabricConfig{VendorID: vendor, Label: cfg.Label, Store: keep, FabricID: max(random(), 1), RootNodeID: random()%0xFFFF_FFEF_FFFF_FFFF + 1}
	if _, found, err := keep.LoadFabric(); err != nil { //nolint:contextcheck // the settings store's Load takes no context
		return nil, err
	} else if found {
		fcfg.FabricID, fcfg.RootNodeID = 0, 0 // the stored record decides
	}
	if c.fabric, err = commissioner.NewFabric(authority, fcfg); err != nil {
		return nil, err
	}
	if err := c.openNetwork(ctx, listen, cfg.Listener, openNames); err != nil {
		return nil, err
	}
	resumption := &peer.MemoryResumptionStore{}
	fabrics := []*commissioner.Fabric{c.fabric}
	if c.peers, err = peer.NewSet(peer.Context{Exchanges: c.mgr, Names: c.names, Fabrics: c.fabric.Lookup, Resumption: resumption, Logger: log, Clock: clk}); err != nil {
		return nil, err
	}
	c.subs = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Logger: log, Clock: clk})
	//nolint:contextcheck // the constructors take no context; the peers they adopt resolve on their own
	if c.com, err = commissioner.New(commissioner.Context{
		Exchanges: c.mgr, Peers: c.peers, Attestation: cfg.Attestation, Logger: log, Clock: clk,
		Subscriptions: c.subs, Fabrics: fabrics, Resumption: resumption,
	}); err != nil {
		return nil, err
	}
	node := cfg.Node
	if node.Logger == nil {
		node.Logger = log
	}
	if node.Clock == nil {
		node.Clock = clk
	}
	//nolint:contextcheck // as commissioner.New
	if c.reg, err = client.NewRegistry(client.RegistryConfig{
		Store: sqlstore.New(cfg.DB), Commissioner: c.com, Peers: c.peers, Exchanges: c.mgr,
		Fabrics: fabrics, Subscriptions: c.subs, Logger: log, Clock: clk, Node: node, HooksFor: cfg.HooksFor, AutoStart: !cfg.ManualStart,
	}); err != nil {
		return nil, err
	}
	if !cfg.ManualStart {
		if err := c.reg.Start(ctx); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// openNetwork opens the socket (or takes the one [Config].Listener
// gives), the exchanges over it and DNS-SD: the steps of [New] between
// the fabric and the peers.
func (c *Controller) openNetwork(ctx context.Context, listen string, l *udp.Listener, openNames func(*slog.Logger) (*discovery.Names, error)) error {
	log := c.log
	if l == nil {
		var err error
		if l, err = udp.New(udp.Config{LocalAddr: listen}); err != nil {
			return err
		}
	}
	var err error
	if c.mgr, err = exchange.NewManager(l, exchange.Config{Logger: log, Clock: c.clock}); err != nil {
		_ = l.Close()
		return err
	}
	serveCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	served := make(chan struct{})
	handle := c.mgr.HandleDatagram // bound here: Close clears c.mgr while Serve runs
	go func() { defer close(served); _ = l.Serve(serveCtx, handle) }()
	c.stop = func() { cancel(); <-served; _ = l.Close() }
	if c.names, err = openNames(log); err != nil {
		return err
	}
	return nil
}

// Close tears the stack down in the reverse order of [New]: the
// registry (its nodes and their subscriptions), the client
// subscriptions, the commissioner, the peers, the exchanges, the socket,
// DNS-SD. The database stays open. It is idempotent.
func (c *Controller) Close(ctx context.Context) error {
	var errs []error
	if c.reg != nil {
		errs = append(errs, c.reg.Close(ctx))
		c.reg = nil
	}
	if c.subs != nil {
		errs = append(errs, c.subs.Close(ctx))
		c.subs = nil
	}
	if c.com != nil {
		c.com.Close()
		c.com = nil
	}
	if c.peers != nil {
		c.peers.Close(ctx)
		c.peers = nil
	}
	if c.mgr != nil {
		c.mgr.Close()
		c.mgr = nil
	}
	c.stop()
	c.stop = func() {}
	if c.names != nil {
		errs = append(errs, c.names.Close())
		c.names = nil
	}
	return errors.Join(errs...)
}

// Registry is the registry of the paired nodes: Commissioned lists them,
// Get finds one by id. A seam of the role (docs/guides/advanced-seams.md).
func (c *Controller) Registry() *client.Registry { return c.reg }

// Nodes are the commissioned nodes, in the registry's order.
func (c *Controller) Nodes() []*client.Node { return c.reg.Commissioned() }

// Fabric is our fabric: its ID, our node ID, the root of trust. A seam of
// the role.
func (c *Controller) Fabric() *commissioner.Fabric { return c.fabric }

// Commissioner is the commissioner the registry pairs through. A seam of
// the role.
func (c *Controller) Commissioner() *commissioner.Commissioner { return c.com }

// PairOptions configure [Controller.Pair]; the zero value pairs with the
// defaults.
type PairOptions struct {
	// AutoSubscribe, when non-nil, overrides the registry's default
	// subscription of the new node (client.CommissionOptions.AutoSubscribe);
	// nil keeps it, as a resident program wants.
	AutoSubscribe *bool
	// OnAttestationFailure decides a node whose attestation fails; the zero
	// value rejects it (attestation.Reject()).
	OnAttestationFailure attestation.Policy
	// Edit adjusts the commissioner's options last (a seam).
	Edit func(*commissioner.Options)
}

// Pair finds the commissionable device a QR code ("MT:…") or a manual
// pairing code names, over DNS-SD by its discriminator, and commissions
// it into our fabric with the code's passcode: PASE, attestation against
// [Config].Attestation, the operational certificate,
// CommissioningComplete. A code [ParsePairingCode] refuses is
// [ErrPairingCode]; a ctx without a deadline is bounded by
// [DefaultPairTimeout]; a code no device answers before it is
// [ErrNoDevice]. Mirrors CommissioningController.commissionNode
// (CommissioningController.ts:462-500 at 862ad75f).
func (c *Controller) Pair(ctx context.Context, code string, opts PairOptions) (*client.Node, error) {
	pc, err := ParsePairingCode(code)
	if err != nil {
		return nil, err
	}
	passcode, id := pc.Passcode, pc.Identifier()
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultPairTimeout)
		defer cancel()
	}
	sc := discovery.NewScanner(c.names)
	defer sc.Close()
	fctx, found := context.WithCancel(ctx)
	var dev *discovery.Device
	_, _ = sc.FindCommissionable(fctx, id, func(d discovery.Device) {
		if dev == nil && len(d.Addresses) > 0 {
			dev = &d
			found()
		}
	})
	found()
	if dev == nil {
		return nil, ErrNoDevice
	}
	n, err := c.reg.ForDevice(ctx, *dev)
	if err != nil {
		return nil, err
	}
	o := commissioner.Options{Passcode: passcode, OnAttestationFailure: opts.OnAttestationFailure}
	if o.OnAttestationFailure.Decide == nil && o.OnAttestationFailure.OnFailure == nil {
		o.OnAttestationFailure = attestation.Reject()
	}
	if opts.Edit != nil {
		opts.Edit(&o)
	}
	if err := c.reg.Commission(ctx, n, client.CommissionOptions{Options: o, AutoSubscribe: opts.AutoSubscribe}); err != nil {
		return n, err
	}
	return n, nil
}

func random() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint64(b[:])
}

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
	"cmp"
	"context"
	"crypto"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/mdns"
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
	// it, as a New that fails does. A test passes one on its own network
	// (fabrictest.Site.ControllerConfig); a program leaves it nil.
	Listener *udp.Listener
	// CAKey, when set, is the root key of our certificate authority, held
	// outside the database: a P-256 crypto.Signer over a key file
	// (controller/ca/keyfile), an HSM or a KMS (ca.Config.RootSigner). A
	// new CA issues its root certificate for it; a stored CA must have
	// been made under it, and a stored root private key is then dropped
	// from the database. nil keeps the root key in the database.
	// matter.js has no such seam: its CertificateAuthority keeps the key
	// pair in its storage (packages/protocol/src/certificate/
	// CertificateAuthority.ts:53, :169 at 862ad75f).
	CAKey crypto.Signer
	// Advertise, when set, publishes our commissioner record
	// (`_matterd._udp`) on the controller's UDP port until Close
	// ([Advertise]); nil publishes none, as matter.js advertises a
	// commissioner only when asked.
	Advertise *Advertise
	// Networks overrides the network profiles' templates field-wise — the
	// throttles and MRP margins the peers' connections and exchanges use
	// (client/peer NetworkProfiles; matter.js NetworkServer.State.profiles,
	// packages/node/src/behavior/system/network/NetworkServer.ts:162-183,
	// :248 at 862ad75f). nil keeps the defaults.
	Networks *peer.ProfilesConfig
	// OwnNetworkProfile is the profile of our own network, whose margin
	// pads every exchange's sends: matter.js ownNetworkProfileId
	// (NetworkServer.ts:238-242, "fast" by default, "thread" on a Thread
	// node; applied at ServerNetworkRuntime.ts:405-406). "" is "fast"; a
	// name no profile has is [ErrConfiguration].
	OwnNetworkProfile string
	// TCP selects the controller's TCP roles ([TCPConfig]); the zero value
	// is TCP off, as matter.js's (NetworkServer.ts:211-217).
	TCP TCPConfig
	// TransportPreference is the transport each peer prefers for its
	// connections: exchange.TransportTCP is a soft hint — a connection
	// falls back to UDP when the peer does not serve TCP — and needs
	// TCP.Outgoing; anything else is UDP (NetworkServer.ts:219-222,
	// NetworkClient.ts:73-79 at 862ad75f).
	TransportPreference exchange.Transport
	// OTA, when set, turns on the OTA push: the update manager and the
	// own node its provider runs on ([OTAConfig]); nil runs neither.
	OTA *OTAConfig
	// Advertiser publishes the Advertise record; nil is
	// mdns.NewZeroconf(), which Close closes. A test passes
	// mdns.NewNoop().
	Advertiser mdns.Advertiser
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
	udp    *udp.Listener
	stop   func()
	// stopTCP closes the TCP listener (tcp.go); tcpAddr is its address.
	stopTCP func()
	tcpAddr *net.TCPAddr
	// unadvertise withdraws the commissioner record (advertise.go).
	unadvertise func(context.Context) error

	// The OTA push (ota.go): the update manager and the reboot armer, the
	// own node with its node id and address; registry is reg for the
	// node hooks, which run while New builds it.
	ota       atomic.Pointer[softwareupdate.Manager]
	registry  atomic.Pointer[client.Registry]
	armer     *peer.RebootResubscribeArmer
	ownNode   OwnNode
	ownNodeID uint64
	ownAddr   *net.UDPAddr
}

// New builds the controller over cfg.DB: the fabric is created on the
// first run (a random fabric ID and node ID, saved) and loaded
// afterwards; then the socket, DNS-SD, the peers, the commissioner and
// the registry, which loads the nodes paired before. A failure tears down
// what was built.
func New(ctx context.Context, cfg Config) (_ *Controller, err error) {
	defer func() {
		if err != nil && cfg.Listener != nil {
			// A failed New closes the socket it was given, as Close would.
			_ = cfg.Listener.Close()
		}
	}()
	switch {
	case cfg.DB == nil:
		return nil, fmt.Errorf("%w: DB is required", ErrConfiguration)
	case cfg.Attestation == nil:
		return nil, fmt.Errorf("%w: Attestation is required (attestation.TestStore() for development devices)", ErrConfiguration)
	case cfg.OTA != nil && cfg.OTA.StartNode == nil:
		return nil, fmt.Errorf("%w: OTA.StartNode is required (node/controllernode.Start)", ErrConfiguration)
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
	authority, err := ca.New(ca.Config{Store: keep, Clock: clk, RootSigner: cfg.CAKey})
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
	networks, err := c.openTransports(ctx, cfg, listen, openNames, clk, log)
	if err != nil {
		return nil, err
	}
	resumption := &peer.MemoryResumptionStore{}
	fabrics := []*commissioner.Fabric{c.fabric}
	pctx := peer.Context{
		Exchanges: c.mgr, Names: c.names, Fabrics: c.fabric.Lookup, Resumption: resumption, Logger: log, Clock: clk, Networks: networks,
		TransportPreference: cfg.TransportPreference, SessionAdded: c.sessionAdded,
	}
	if cfg.TCP.Outgoing {
		pctx.DialTCP = c.dialTCP()
	}
	if c.peers, err = peer.NewSet(pctx); err != nil {
		return nil, err
	}
	if cfg.OTA != nil {
		c.armer = peer.NewRebootResubscribeArmer(c.armerContext())
	}
	c.subs = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Logger: log, Clock: clk, ReportStarted: c.reportStarted})
	//nolint:contextcheck // the constructors take no context; the peers they adopt resolve on their own
	if c.com, err = commissioner.New(commissioner.Context{
		Exchanges: c.mgr, Peers: c.peers, Attestation: cfg.Attestation, Logger: log, Clock: clk,
		Subscriptions: c.subs, Fabrics: fabrics, Resumption: resumption,
	}); err != nil {
		return nil, err
	}
	if err := c.openRegistry(ctx, cfg, fabrics, clk, log); err != nil {
		return nil, err
	}
	if !cfg.ManualStart {
		if err := c.reg.Start(ctx); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// openRegistry builds the node registry over the commissioner and, with
// OTA, starts the OTA push: the steps of [New] after the commissioner.
func (c *Controller) openRegistry(ctx context.Context, cfg Config, fabrics []*commissioner.Fabric, clk clock.Clock, log *slog.Logger) error {
	nodeCfg := cfg.Node
	if nodeCfg.Logger == nil {
		nodeCfg.Logger = log
	}
	if nodeCfg.Clock == nil {
		nodeCfg.Clock = clk
	}
	hooksFor := cfg.HooksFor
	if cfg.OTA != nil {
		hooksFor = c.otaHooks(cfg.HooksFor) //nolint:contextcheck // node hooks carry no context; the manager's work runs on its own lifetime
	}
	var err error
	//nolint:contextcheck // as commissioner.New
	if c.reg, err = client.NewRegistry(client.RegistryConfig{
		Store: sqlstore.New(cfg.DB), Commissioner: c.com, Peers: c.peers, Exchanges: c.mgr,
		Fabrics: fabrics, Subscriptions: c.subs, Logger: log, Clock: clk, Node: nodeCfg, HooksFor: hooksFor, AutoStart: !cfg.ManualStart,
	}); err != nil {
		return err
	}
	c.registry.Store(c.reg)
	if cfg.OTA != nil {
		return c.startOTA(ctx, cfg)
	}
	return nil
}

// openTransports builds the network profiles and opens the network over
// the own profile's margin, the TCP listener with TCP.Incoming and the
// commissioner advertisement with Advertise: the steps of [New] between
// the fabric and the peers.
func (c *Controller) openTransports(ctx context.Context, cfg Config, listen string, openNames func(*slog.Logger) (*discovery.Names, error), clk clock.Clock, log *slog.Logger) (*peer.NetworkProfiles, error) {
	networks := peer.NewNetworkProfiles(clk, nil, log)
	if cfg.Networks != nil {
		networks.SetDefaults(*cfg.Networks)
	}
	own, err := networks.Get(cmp.Or(cfg.OwnNetworkProfile, peer.NetworkFast))
	if err != nil {
		return nil, fmt.Errorf("%w: OwnNetworkProfile: %w", ErrConfiguration, err)
	}
	if err := c.openNetwork(ctx, listen, cfg.Listener, openNames, exchange.Config{
		Logger: log, Clock: clk, LocalAdditionalMrpDelay: own.AdditionalMrpDelay, LocalParameters: localParameters(cfg.TCP),
	}); err != nil {
		return nil, err
	}
	if cfg.TCP.Incoming {
		if err := c.listenTCP(ctx); err != nil {
			return nil, err
		}
	}
	if cfg.Advertise != nil {
		if err := c.advertise(ctx, *cfg.Advertise, cfg.Advertiser, c.fabric.RootVendorID); err != nil {
			return nil, err
		}
	}
	return networks, nil
}

// openNetwork opens the socket (or takes the one [Config].Listener
// gives), the exchanges over it and DNS-SD: the steps of [New] between
// the fabric and the peers.
func (c *Controller) openNetwork(ctx context.Context, listen string, l *udp.Listener, openNames func(*slog.Logger) (*discovery.Names, error), xcfg exchange.Config) error {
	log := c.log
	if l == nil {
		var err error
		if l, err = udp.New(udp.Config{LocalAddr: listen}); err != nil {
			return err
		}
	}
	c.udp = l
	var err error
	if c.mgr, err = exchange.NewManager(l, xcfg); err != nil {
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

// Close tears the stack down in the reverse order of [New]: the update
// manager and the own node, the registry (its nodes and their
// subscriptions), the client
// subscriptions, the commissioner, the peers, the commissioner record,
// the exchanges, the socket, DNS-SD. The database stays open. It is
// idempotent.
func (c *Controller) Close(ctx context.Context) error {
	var errs []error
	if m := c.ota.Swap(nil); m != nil {
		m.Close()
	}
	if c.ownNode != nil {
		errs = append(errs, c.ownNode.Stop(ctx))
		c.ownNode = nil
	}
	if c.armer != nil {
		c.armer.Close()
	}
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
	if c.unadvertise != nil {
		errs = append(errs, c.unadvertise(ctx))
		c.unadvertise = nil
	}
	if c.stopTCP != nil {
		c.stopTCP()
		c.stopTCP = nil
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

// Peers is the peer set the registry and the commissioner reach nodes
// through: a program that talks to a node it did not pair through the
// registry (its own node on a known address, say) adds it here
// (peer.Set.AddKnownPeer) and reaches it with
// Commissioner().Interaction. A seam of the role — matter.js's node
// peers (packages/node/src/node/client/Peers.ts:93 at 862ad75f).
func (c *Controller) Peers() *peer.Set { return c.peers }

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
	// BLE, when set, is the Bluetooth LE central Pair finds the device
	// through, by its advertisement in place of DNS-SD, and commissions it
	// over BTP: PASE over BLE, then the network steps put the device on
	// WiFi or Thread, where CASE finds it over DNS-SD (ADR 0024). A device
	// on BLE has no network yet, so one of WiFi and Thread is required
	// ([ErrConfiguration] otherwise). The BlueZ central is ble/bluez's.
	BLE ble.Central
	// Address, when set, is the device's UDP address: Pair dials it
	// without DNS-SD — no advertisement is needed — as a client.Device
	// holding that address alone (no instance id, no TXT record, so the
	// registry never matches it to a node it knows). The code still
	// supplies the passcode; its discriminator is not checked. Mirrors
	// matter.js MatterController.commission, which commissions a
	// discovery's knownAddress through Peers.forDescriptor({addresses})
	// without a scan (packages/matter.js/src/MatterController.ts:539-566 at
	// 862ad75f; the option is CommissioningController.ts:289). Address
	// and BLE together are [ErrConfiguration].
	Address *net.UDPAddr
	// WiFi and Thread are the operational network the commissioning
	// provisions on a device that advertises Wi-Fi or Thread
	// (commissioner.Options.WiFiNetwork, ThreadNetwork); nil provisions
	// none.
	WiFi   *commissioner.WiFiNetwork
	Thread *commissioner.ThreadNetwork
	// Progress, when set, is called on Pair's goroutine as each step of
	// the pairing starts and ends ([PairingStep]): the search for the
	// device (skipped with Address), PASE, then the commissioning steps
	// under matter.js's names. Pair waits for it, so it must not block.
	// It is the commissioner's Options.Progress, which Edit may replace.
	Progress func(PairingStep)
	// Edit adjusts the commissioner's options last (a seam).
	Edit func(*commissioner.Options)
}

// PairingStep is one report of [PairOptions].Progress: the step's
// number, sub-step and name — matter.js's step names verbatim
// (packages/protocol/src/peer/ControllerCommissioningFlow.ts:536-656 at
// 862ad75f), so a UI can show them, and "Discovery" 2.1 and "PASE" 2.2,
// this module's names for the steps before matter.js's flow — its
// phase, and the error of a failed step (commissioner.Step).
type PairingStep = commissioner.Step

// PairingPhase is a [PairingStep]'s phase: started, then succeeded,
// failed or skipped (commissioner.StepPhase).
type PairingPhase = commissioner.StepPhase

// The phases of a [PairingStep].
const (
	PairingStarted   = commissioner.StepStarted
	PairingSucceeded = commissioner.StepSucceeded
	PairingFailed    = commissioner.StepFailed
	PairingSkipped   = commissioner.StepSkipped
)

// Pair finds the commissionable device a QR code ("MT:…") or a manual
// pairing code names, over DNS-SD by its discriminator, and commissions
// it into our fabric with the code's passcode: PASE, attestation against
// [Config].Attestation, the operational certificate,
// CommissioningComplete. A code [ParsePairingCode] refuses is
// [ErrPairingCode]; a ctx without a deadline is bounded by
// [DefaultPairTimeout]; a code no device answers before it is
// [ErrNoDevice]. With [PairOptions].BLE the device is found over BLE and
// commissioned over BTP; with [PairOptions].Address it is dialled at that
// address without discovery. Mirrors CommissioningController.commissionNode
// (CommissioningController.ts:462-500 at 862ad75f), whose discovery takes
// BLE when the options name a BLE capability (discoveryCapabilities,
// :280-283).
func (c *Controller) Pair(ctx context.Context, code string, opts PairOptions) (*client.Node, error) {
	pc, err := ParsePairingCode(code)
	if err != nil {
		return nil, err
	}
	if opts.BLE != nil && opts.Address != nil {
		return nil, fmt.Errorf("%w: PairOptions.BLE and Address name two ways to reach the device", ErrConfiguration)
	}
	var known *discovery.Device
	if opts.Address != nil {
		d, err := DeviceAt(opts.Address)
		if err != nil {
			return nil, err
		}
		known = &d
	}
	if opts.BLE != nil && opts.WiFi == nil && opts.Thread == nil {
		return nil, fmt.Errorf("%w: pairing over BLE needs PairOptions.WiFi or Thread: a device on BLE has no network yet", ErrConfiguration)
	}
	passcode, id := pc.Passcode, pc.Identifier()
	identity := discovery.Identity{VendorID: pc.VendorID, ProductID: pc.ProductID}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultPairTimeout)
		defer cancel()
	}
	var dev *discovery.Device
	var channel commissioner.Channel
	if known != nil {
		commissioner.ReportDiscovery(opts.Progress, PairingSkipped, nil)
		dev = known
	} else {
		commissioner.ReportDiscovery(opts.Progress, PairingStarted, nil)
		if opts.BLE != nil {
			d, ch, err := c.dialBLE(ctx, opts.BLE, id, identity)
			if err != nil {
				commissioner.ReportDiscovery(opts.Progress, PairingFailed, err)
				return nil, err
			}
			defer func() { _ = ch.Close() }()
			dev, channel = &d, ch
		} else if dev = c.findCommissionable(ctx, id, identity); dev == nil {
			commissioner.ReportDiscovery(opts.Progress, PairingFailed, ErrNoDevice)
			return nil, ErrNoDevice
		}
		commissioner.ReportDiscovery(opts.Progress, PairingSucceeded, nil)
	}
	n, err := c.reg.ForDevice(ctx, *dev)
	if err != nil {
		return nil, err
	}
	o := commissioner.Options{
		Passcode: passcode, OnAttestationFailure: opts.OnAttestationFailure,
		Channel: channel, WiFiNetwork: opts.WiFi, ThreadNetwork: opts.Thread,
		Progress: opts.Progress,
	}
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

// DeviceAt is the client.Device of a device known by its address alone,
// without a DNS-SD answer: no instance id, no TXT record, addr as its one
// address. It is a supported input of client.Registry.ForDevice — which
// matches it to no known node and creates one, as matter.js's
// Peers.forDescriptor does for a descriptor of addresses only
// (RemoteDescriptor.is matches nothing on an absent identifier,
// packages/node/src/behavior/system/commissioning/RemoteDescriptor.ts:37-50
// at 862ad75f) — and what [PairOptions].Address pairs. An address without
// an IP or a port is [ErrConfiguration].
func DeviceAt(addr *net.UDPAddr) (client.Device, error) {
	if addr == nil {
		return client.Device{}, fmt.Errorf("%w: no address", ErrConfiguration)
	}
	ap := addr.AddrPort()
	if !ap.Addr().IsValid() || ap.Port() == 0 {
		return client.Device{}, fmt.Errorf("%w: address %v has no IP or no port", ErrConfiguration, addr)
	}
	ip := ap.Addr()
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	return client.Device{Addresses: []discovery.Address{{IP: ip, Port: ap.Port()}}}, nil
}

// findCommissionable is the first commissionable device id selects that
// DNS-SD reports with an address before ctx ends, passing over one whose
// VP record contradicts the code's vendor or product
// (CommissioningDiscovery.ts:62-90 at 862ad75f); nil when none does.
func (c *Controller) findCommissionable(ctx context.Context, id discovery.Identifier, identity discovery.Identity) *discovery.Device {
	sc := discovery.NewScanner(c.names)
	defer sc.Close()
	fctx, found := context.WithCancel(ctx)
	defer found()
	var dev *discovery.Device
	_, _ = sc.FindCommissionable(fctx, id, func(d discovery.Device) {
		if dev == nil && len(d.Addresses) > 0 && c.namesThisDevice(identity, d) {
			dev = &d
			found()
		}
	})
	return dev
}

// dialBLE scans central until the first device id selects advertises —
// the long discriminator of a QR code, the short one of a manual code, as
// over DNS-SD — and opens the BTP channel to it (ble.Dial): matter.js's
// BleScanner.findCommissionableDevices and the BLE channel of its
// commissioning (packages/protocol/src/common/BleScanner.ts:543-575 at
// 862ad75f). The scan runs until ctx ends, at most [DefaultPairTimeout] on
// the controller's clock; none found is [ErrNoDevice].
func (c *Controller) dialBLE(ctx context.Context, central ble.Central, id discovery.Identifier, identity discovery.Identity) (discovery.Device, *ble.Channel, error) {
	sc := ble.NewScanner(central, ble.ScannerOptions{Clock: c.clock, Logger: c.log})
	defer func() { _ = sc.Close(context.WithoutCancel(ctx)) }()
	// ctx's deadline ends the scan first; the timeout bounds it on the
	// controller's clock otherwise.
	devs, err := sc.Find(ctx, id, DefaultPairTimeout, false)
	if err != nil {
		return discovery.Device{}, nil, err
	}
	devs = slices.DeleteFunc(devs, func(d ble.Device) bool { return !c.namesThisDevice(identity, d.Commissionable()) })
	if len(devs) == 0 {
		return discovery.Device{}, nil, fmt.Errorf("%w over BLE", ErrNoDevice)
	}
	ch, err := ble.Dial(ctx, central, devs[0].Peripheral, ble.DialOptions{Clock: c.clock, Logger: c.log})
	if err != nil {
		return discovery.Device{}, nil, err
	}
	return devs[0].Commissionable(), ch, nil
}

// namesThisDevice reports whether d may be the device the code names:
// one whose advertisement positively names another vendor or product is
// passed over — CommissioningDiscovery.#namesThisDevice (:79-90).
func (c *Controller) namesThisDevice(identity discovery.Identity, d discovery.Device) bool {
	m := discovery.MismatchOf(identity, d.AdvertisedIdentity())
	if m == nil {
		return true
	}
	// reason is the facet, offered what the device advertises, expected
	// what the code names.
	c.log.Info("matter.controller.pair.passing_over", slog.String("instance", d.InstanceID), slog.String("reason", m.Facet),
		slog.Int("offered", int(m.Advertised)), slog.Int("expected", int(m.Payload)))
	return false
}

func random() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint64(b[:])
}

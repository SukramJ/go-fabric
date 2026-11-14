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
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/dcl"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
	"github.com/SukramJ/go-fabric/diag"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/thread/brdiscovery"
	"github.com/SukramJ/go-fabric/transport/message"
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

// errClosed is the error of a method called on a controller after or
// during [Controller.Close]: [ErrConfiguration], which SetGroupKey and
// Discover returned for it first.
var errClosed = fmt.Errorf("%w: the controller is closed", ErrConfiguration)

// DefaultVendorID is the vendor ID of our fabric when [Config] sets none:
// 0xFFF1, one of the test vendor IDs (0xFFF1–0xFFF4).
const DefaultVendorID uint16 = 0xFFF1

// DefaultPairTimeout bounds the search of [Controller.Pair] for the
// device when its context has no deadline: a code no device answers
// within it is [ErrNoDevice]. It does not bound the commissioning of a
// device that was found — that has the commissioner's own budgets
// (commissioner.DefaultPASETimeout, commissioner.DefaultCASEConnectionTimeout
// and the fail-safe), as matter.js's has: its commissionNode sets no
// overall deadline (CommissioningController.ts:462-500 at 862ad75f), only
// the discovery's timeout (CommissioningDiscovery.ts:40-45).
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
	// Scanners are told to forget a device once it is commissioned — a
	// BLE scanner (controller/ble.Scanner) still holding its commissioning
	// advertisement, say: matter.js's ScannerSet, which the commissioner
	// walks after the commissioning steps succeeded
	// (packages/protocol/src/peer/ControllerCommissioner.ts:321-341,
	// :721-723 at 862ad75f). nil is none.
	Scanners []commissioner.CommissionedScanner
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
	// KeepOnboardingCodes keeps the QR or manual pairing code of every
	// device [Controller.Pair] commissions with the node
	// (client.Node.OnboardingCode), so that [Controller.Inventory] can
	// show it again when the paper is lost. It is kept when the
	// commissioning succeeded — also when the node's start then failed —
	// and the device advertised Basic commissioning mode: not for an
	// Enhanced Commissioning Window, whose code ends with it, not for a
	// pairing by PairOptions.Address, which sees no advertisement, and
	// not when PairOptions.Edit replaced the passcode. Off by default: the code is a
	// secret — with it and a factory reset anyone can commission the
	// device — and the database then holds one per node.
	KeepOnboardingCodes bool
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
	// Sessions sets the session intervals the controller reports in its
	// handshakes (PBKDFParamRequest / Sigma1 SessionParameters) —
	// matter.js's sessions.intervals fed into the SessionManager
	// (ServerNetworkRuntime.ts:354-362 at 862ad75f), the controller-side
	// counterpart of node.Config.Sessions. nil is the Matter defaults
	// (500 ms / 300 ms / 4000 ms); an interval outside whole milliseconds
	// from 0 to the field's bound is [ErrConfiguration].
	Sessions *SessionIntervals
	// TransportPreference is the transport each peer prefers for its
	// connections: exchange.TransportTCP is a soft hint — a connection
	// falls back to UDP when the peer does not serve TCP — and needs
	// TCP.Outgoing; anything else is UDP (NetworkServer.ts:219-222,
	// NetworkClient.ts:73-79 at 862ad75f).
	TransportPreference exchange.Transport
	// PeerTiming sets the connection timing of the controller's peer set,
	// every parameter [client.Timing] names: the connection timeout, the
	// address stagger, the delays after an error, the kick and probe
	// cooldowns, the address-change delays and ProvenAddressRetention. It
	// overrides client/peer's DefaultTiming field-wise, a cooldown group
	// member by member: a zero field keeps the default; nil is the
	// defaults. A negative ProvenAddressRetention retains nothing.
	PeerTiming *client.Timing
	// OTA, when set, turns on the OTA push: the update manager and the
	// OTA provider on the controller's endpoint tree ([OTAConfig],
	// [OTAProviderEndpoint]); nil runs neither.
	OTA *OTAConfig
	// GroupTransport is the transport group messages leave on; nil is the
	// controller's own UDP socket, as matter.js's node sends from its own
	// interface. A test hands a redirecting transport here.
	GroupTransport groups.MulticastTransport
	// BorderRouters configures the passive Thread border-router registry
	// the controller keeps on its DNS-SD cache ([Controller.BorderRouters],
	// thread/brdiscovery); the zero value keeps one, as matterjs-server's
	// controller always does.
	BorderRouters BorderRouterConfig
	// Camera, when set, turns on the controller side of Matter cameras
	// ([CameraConfig]): the controller serves a Camera Controller endpoint
	// with a WebRtcTransportRequestor under its own identity and
	// publishes its operational record; nil serves none.
	Camera *CameraConfig
	// Advertiser publishes the Advertise record; nil is
	// mdns.NewResponder(), which Close closes. A test passes
	// mdns.NewNoop().
	Advertiser mdns.Advertiser
	// DCL configures the controller's ledger services ([DCLConfig]); the
	// zero value is the production ledger, online.
	DCL DCLConfig
	// Diagnostics sizes the controller's diagnostic trace
	// ([Controller.DiagnosticEvents]); the zero value keeps
	// diag.DefaultDiagnosticsCapacity events.
	Diagnostics diag.Diagnostics
	// Metrics receives the controller's named counters and gauges
	// (controller/metrics.go: sessions.open, mrp.retransmits,
	// pair.failures, case.unresponsive, reachability.probes,
	// icd.checkins_missed); nil records nothing. node/metrics/expvar
	// implements it.
	Metrics contract.Metrics
}

// Controller is a running controller: one fabric, the nodes paired into
// it, and the stack under them. Its methods are safe for concurrent use,
// [Controller.Close] included: once Close has begun, a method that
// returns an error returns one wrapping [ErrConfiguration], and one that
// does not returns its zero result — no component, an empty list, an
// unsubscribe that does nothing. Fabric, Clock, TCP, Addr and TCPAddr,
// fixed at New, keep answering, and Erase is the reset that runs after a
// Close.
type Controller struct {
	db  *sql.DB
	log *slog.Logger
	// keepCodes is Config.KeepOnboardingCodes.
	keepCodes bool
	clock     clock.Clock
	fabric    *commissioner.Fabric
	mgr       *exchange.Manager
	names     *discovery.Names
	brs       *brdiscovery.Registry
	peers     *peer.Set
	subs      *interaction.Subscriptions
	com       *commissioner.Commissioner
	reg       *client.Registry
	// stopping is the registry a Close left with nodes still stopping —
	// report jobs and the store's final flush queued behind a hook that
	// outlasted Close's ctx ([client.Node.Stop]). Erase waits for it
	// before it deletes a row (review II, R2-P1-ctl-02).
	stopping *client.Registry
	udp      *udp.Listener
	stop     func()
	// stopTCP closes the TCP listener (tcp.go); tcpAddr is its address;
	// tcp is the configuration they follow.
	stopTCP func()
	tcpAddr *net.TCPAddr
	tcp     TCPConfig
	// unadvertise withdraws the commissioner record (advertise.go).
	unadvertise func(context.Context) error
	// adv is the advertiser of the controller's records (the commissioner
	// record, the operational record of a served tree); advOwn says Close
	// closes it.
	adv    mdns.Advertiser
	advOwn bool
	// served is the controller's own endpoint tree (serve.go), nil
	// without one.
	served *served

	// dcl is the configuration of the ledger services, vendorInfo the
	// vendor information service built on first use (vendorinfo.go).
	dcl        DCLConfig
	vendorMu   sync.Mutex
	vendorInfo *dcl.VendorInfoService

	// The OTA push (ota.go): the update manager and the reboot armer;
	// registry is reg for the node hooks, which run while New builds it.
	ota      atomic.Pointer[softwareupdate.Manager]
	registry atomic.Pointer[client.Registry]
	armer    *peer.RebootResubscribeArmer
	armerObs atomic.Pointer[armerObserver] // a test seam, nil in production (ota.go)
	// groups is the controller's group sender (groups.go).
	groups *groups.Manager
	// otaProvider is the OTA provider on the controller's identity
	// (ota_provider.go), nil without Config.OTA.
	otaProvider *otaProvider

	// The diagnostics (diagnostics.go): the trace, the metrics port, the
	// peer set the observers count sessions over (kept past Close, which
	// clears peers), the nodes whose ICD Check-Ins are watched and the
	// stop of the discovery watch.
	diagRing *diagevent.Ring
	metrics  contract.Metrics
	peerSet  atomic.Pointer[peer.Set]
	// closeMu is the controller's one closed gate: Close sets closed
	// under it first, and every public method reads closed — and the
	// fields Close clears (reg, peers, com, names, adv, groups,
	// otaProvider) — under it, so a method either saw the controller
	// open and read them before Close began or returns closed. closeDone
	// is closed when the Close that set closed returns; a second Close
	// waits for it (review II, R2-P2-stop-01).
	closeMu      sync.RWMutex
	closed       bool
	closeDone    chan struct{}
	icdMu        sync.Mutex
	icdWatched   map[*client.Node][]func() // guarded by icdMu
	icdClosed    bool                      // guarded by icdMu: Close stopped every watch
	stopDiscover func()
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
	c := &Controller{db: cfg.DB, log: log, clock: clk, stop: func() {}, dcl: cfg.DCL, tcp: cfg.TCP, metrics: cfg.Metrics, diagRing: newTrace(cfg.Diagnostics, clk), keepCodes: cfg.KeepOnboardingCodes}
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
	// The operational key is rotated on every start, as matter.js's
	// controller does for the fabric it keeps itself (FabricAuthority
	// defaultFabric with rotateNoc, packages/matter.js/src/MatterController.ts:402-415
	// at 862ad75f).
	if c.fabric, err = commissioner.NewFabricAuthority(authority, log).DefaultFabric(fcfg, true); err != nil {
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
		TransportPreference: cfg.TransportPreference, SessionAdded: c.sessionAdded, PeerAdded: c.peerAdded,
		PeerRemoved: c.peerRemoved, Timing: cfg.PeerTiming,
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
		Subscriptions: c.subs, Fabrics: fabrics, Resumption: resumption, Scanners: cfg.Scanners,
	}); err != nil {
		return nil, err
	}
	if err := c.openRegistry(cfg, fabrics, clk, log); err != nil { //nolint:contextcheck // the registry's hooks run on its own lifetime, as otaHooks notes
		return nil, err
	}
	if err := c.serveAndStart(ctx, cfg, log); err != nil {
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
func (c *Controller) openRegistry(cfg Config, fabrics []*commissioner.Fabric, clk clock.Clock, log *slog.Logger) error {
	nodeCfg := cfg.Node
	if nodeCfg.Logger == nil {
		nodeCfg.Logger = log
	}
	if nodeCfg.Clock == nil {
		nodeCfg.Clock = clk
	}
	hooksFor := cfg.HooksFor
	if hooksFor == nil {
		// The registry takes Node.Hooks without HooksFor; the chains below
		// replace HooksFor, so they start from them.
		node := nodeCfg.Hooks
		hooksFor = func(string) client.Hooks { return node }
	}
	if cfg.OTA != nil {
		hooksFor = c.otaHooks(hooksFor) //nolint:contextcheck // node hooks carry no context; the manager's work runs on its own lifetime
	}
	hooksFor = c.diagHooks(hooksFor)
	var err error
	//nolint:contextcheck // as commissioner.New
	if c.reg, err = client.NewRegistry(client.RegistryConfig{
		Store: sqlstore.New(cfg.DB), Commissioner: c.com, Peers: c.peers, Exchanges: c.mgr,
		Fabrics: fabrics, Subscriptions: c.subs, Logger: log, Clock: clk, Node: nodeCfg, HooksFor: hooksFor, AutoStart: !cfg.ManualStart,
	}); err != nil {
		return err
	}
	c.registry.Store(c.reg)
	c.startTrace()
	return nil
}

// serveAndStart composes what the controller serves under its own
// identity over the registry — the group sender, the OTA provider, the
// tree they and the camera controller sit on — then starts the manager
// that waits for the tree.
func (c *Controller) serveAndStart(ctx context.Context, cfg Config, log *slog.Logger) error {
	if err := c.openGroups(ctx, cfg, log); err != nil {
		return err
	}
	if cfg.OTA != nil {
		if err := c.openOTA(ctx, cfg); err != nil {
			return err
		}
	}
	if err := c.serveEndpoints(ctx, cfg, log); err != nil {
		return err
	}
	if cfg.OTA != nil {
		return c.startOTA(ctx, cfg)
	}
	// A database that ran the own node is migrated whether or not the
	// controller serves OTA (review II, R2-P1-ctl-03).
	return c.retireOwnNode(ctx, store.New(cfg.DB), c.log)
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
	if err := cfg.Sessions.validate(); err != nil {
		return nil, err
	}
	if err := c.openNetwork(ctx, listen, cfg.Listener, openNames, exchange.Config{
		Logger: log, Clock: clk, LocalAdditionalMrpDelay: own.AdditionalMrpDelay, LocalParameters: localParameters(cfg.TCP, cfg.Sessions),
		Retransmitted: c.retransmitted,
	}); err != nil {
		return nil, err
	}
	if !cfg.BorderRouters.Disabled {
		c.brs = brdiscovery.New(c.names, brdiscovery.Config{Clock: clk, Logger: log})
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
	// A secure session's datagrams are handled in arrival order
	// (message.SessionOrderKey), as the node's are: matter.js runs one
	// unawaited chain per datagram (ExchangeManager.ts:661 at db021da0) and
	// its runtime finishes the earlier datagram's non-I/O continuation
	// before the next I/O callback (a runtime property); chip dispatches
	// run-to-completion on its one thread (ExchangeMgr.cpp:295-302 at
	// 6170af84). So a request on a later exchange cannot overtake the
	// acknowledgement that finished a BDX transfer. HandleDatagram never
	// waits for a later datagram of its own session — protocol handlers run
	// on goroutines of their own — so the turn is released on return.
	go func() {
		defer close(served)
		_ = l.ServeOrdered(serveCtx, message.SessionOrderKey, func(buf []byte, src *net.UDPAddr, _ func()) { handle(buf, src) })
	}()
	c.stop = func() { cancel(); <-served; _ = l.Close() }
	if c.names, err = openNames(log); err != nil {
		return err
	}
	return nil
}

// Close tears the stack down in the reverse order of [New], once no
// request for the endpoints the controller serves is dispatched any more
// and the one in flight has returned: the update manager and the OTA
// provider, the registry (its nodes and their subscriptions), the client
// subscriptions, the commissioner, the peers, the commissioner record,
// the exchanges, the socket, DNS-SD. The database stays open. It is
// idempotent: a second Close waits for the first to return, within ctx
// (ctx's error when ctx ends first), and returns nil.
func (c *Controller) Close(ctx context.Context) error {
	done, first := c.beginClose()
	if !first {
		return awaitClose(ctx, done)
	}
	defer close(done)
	var errs []error
	if c.subs != nil {
		// Before a served component stops: a QueryImage that arrived after
		// the OTA provider stopped would be dispatched to it, and the
		// update manager behind it. Blocking new activity closes the
		// request unanswered and waits for one in flight. Its error, ctx's,
		// is the one subs.Close below returns.
		_ = c.subs.BlockNewActivity(ctx)
	}
	c.unwatchAllICD()
	if c.stopDiscover != nil {
		c.stopDiscover()
		c.stopDiscover = nil
	}
	c.closeVendorInfo()
	if m := c.ota.Swap(nil); m != nil {
		m.Close()
	}
	if c.groups != nil {
		c.groups.SetTransport(nil)
		c.groups = nil
	}
	c.closeOTAProvider()
	if c.armer != nil {
		c.armer.Close()
	}
	if c.reg != nil {
		if err := c.reg.Close(ctx); err != nil {
			errs = append(errs, err)
			c.stopping = c.reg
		}
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
	if c.served != nil && c.served.withdraw != nil {
		errs = append(errs, c.served.withdraw(ctx))
		c.served.withdraw = nil
	}
	if c.unadvertise != nil {
		errs = append(errs, c.unadvertise(ctx))
		c.unadvertise = nil
	}
	if c.adv != nil {
		if c.advOwn {
			errs = append(errs, c.adv.Close())
		}
		c.adv = nil
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
	if c.brs != nil {
		c.brs.Close()
	}
	if c.names != nil {
		errs = append(errs, c.names.Close())
		c.names = nil
	}
	// A closed peer's sessions leave without SessionDeleted (its context
	// ends first): the gauge reads none once the stack is down.
	c.gauge(MetricSessionsOpen, 0)
	return errors.Join(errs...)
}

// beginClose sets closed under the gate. first is true for the Close
// that set it, which closes done when it returns; a later Close waits on
// done.
func (c *Controller) beginClose() (done chan struct{}, first bool) {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return c.closeDone, false
	}
	c.closed = true
	c.closeDone = make(chan struct{})
	return c.closeDone, true
}

// awaitClose is a second Close: nil once the first has returned, ctx's
// error when ctx ends first.
func awaitClose(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// closeOTAProvider stops the OTA provider, its BDX handler unregistered
// first, so no BDX exchange is handed to the responder that closes.
func (c *Controller) closeOTAProvider() {
	if c.otaProvider == nil {
		return
	}
	if c.mgr != nil {
		c.mgr.SetProtocolHandler(bdx.ProtocolID, nil)
	}
	c.otaProvider.stop()
	c.otaProvider.restore()
	c.otaProvider.bdx.Close()
	c.otaProvider = nil
}

// BorderRouterConfig configures the controller's border-router registry
// ([Config].BorderRouters).
type BorderRouterConfig struct {
	// Disabled leaves the controller without a registry:
	// [Controller.BorderRouters] is nil.
	Disabled bool
}

// BorderRouters is the passive registry of the Thread border routers the
// controller's DNS-SD cache sees (`_meshcop`, `_trel`), which
// controller/topology classifies Thread neighbours by — matterjs-server's
// controller keeps one always (NetworkTopologyService.ts:88-89 of
// matterjs-server at 0bd06f2). Nil when [BorderRouterConfig].Disabled;
// the registry's methods are safe on nil (no entries, no events). Nil
// once Close has begun.
func (c *Controller) BorderRouters() *brdiscovery.Registry {
	if _, ok := c.open(); !ok {
		return nil
	}
	return c.brs
}

// Registry is the registry of the paired nodes: Commissioned lists them,
// Get finds one by id. A seam of the role (docs/guides/advanced-seams.md).
// Nil once Close has begun.
func (c *Controller) Registry() *client.Registry {
	st, _ := c.open()
	return st.reg
}

// Nodes are the commissioned nodes, in the registry's order; none once
// Close has begun.
func (c *Controller) Nodes() []*client.Node {
	st, ok := c.open()
	if !ok {
		return nil
	}
	return st.reg.Commissioned()
}

// stack is what Close tears down, as a method read it under closeMu
// while the controller was open.
type stack struct {
	reg    *client.Registry
	peers  *peer.Set
	com    *commissioner.Commissioner
	names  *discovery.Names
	groups *groups.Manager
	bdx    *bdx.Responder
}

// open is the stack of an open controller, read under the closed gate;
// false, and the zero stack, once Close has begun. A method that uses the
// stack after open returned runs against components that Close may be
// closing: each refuses work once closed.
func (c *Controller) open() (stack, bool) {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()
	if c.closed {
		return stack{}, false
	}
	st := stack{reg: c.reg, peers: c.peers, com: c.com, names: c.names, groups: c.groups}
	if c.otaProvider != nil {
		st.bdx = c.otaProvider.bdx
	}
	return st, true
}

// Fabric is our fabric: its ID, our node ID, the root of trust. A seam of
// the role.
func (c *Controller) Fabric() *commissioner.Fabric { return c.fabric }

// Peers is the peer set the registry and the commissioner reach nodes
// through: a program that talks to a node it did not pair through the
// registry (its own node on a known address, say) adds it here
// (peer.Set.AddKnownPeer) and reaches it with
// Commissioner().Interaction. A seam of the role — matter.js's node
// peers (packages/node/src/node/client/Peers.ts:93 at 862ad75f).
// Nil once Close has begun.
func (c *Controller) Peers() *peer.Set {
	st, _ := c.open()
	return st.peers
}

// Clock is the time source of the controller's stack ([Config.Clock], the
// system clock without one), for a service composed over the controller
// that paces on the same clock.
func (c *Controller) Clock() clock.Clock { return c.clock }

// TCP is the controller's TCP configuration ([Config.TCP]): whether it
// accepts and whether it opens TCP connections.
func (c *Controller) TCP() TCPConfig { return c.tcp }

// Addr is the controller's UDP address: where its sessions, its
// commissioner record and, with a served tree, its operational record
// point.
func (c *Controller) Addr() *net.UDPAddr { return c.udp.LocalAddr() }

// Commissioner is the commissioner the registry pairs through. A seam of
// the role. Nil once Close has begun.
func (c *Controller) Commissioner() *commissioner.Commissioner {
	st, _ := c.open()
	return st.com
}

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
	// TermsAndConditions is the user's acknowledgement of the device's
	// terms and conditions (commissioner.Options.TermsAndConditions): the
	// commissioning sends it with SetTCAcknowledgements before
	// CommissioningComplete. A device that requires terms this does not
	// acknowledge — nil, or a Version below the device's
	// TCMinRequiredVersion — fails Pair early, before AddNOC, with a
	// *commissioner.TermsRequiredError naming the version; a caller shows
	// the terms and pairs again with the acknowledgement. nil acknowledges
	// nothing, as a device without terms needs.
	TermsAndConditions *commissioner.TermsAndConditionsAcknowledgement
	// Progress, when set, is called on Pair's goroutine as each step of
	// the pairing starts and ends ([PairingStep]): the search for the
	// device (skipped with Address), PASE, the commissioning steps under
	// matter.js's names, then the node's start, "Subscribe" 21.1. A
	// failed step is the last report. Pair waits for it, so it must not
	// block.
	// It is the commissioner's Options.Progress, which Edit may replace.
	Progress func(PairingStep)
	// Edit adjusts the commissioner's options last (a seam).
	Edit func(*commissioner.Options)

	// onboardingCode is the code Pair was called with, set when
	// Config.KeepOnboardingCodes is on.
	onboardingCode string
}

// PairingStep is one report of [PairOptions].Progress: the step's
// number, sub-step and name — matter.js's step names verbatim
// (packages/protocol/src/peer/ControllerCommissioningFlow.ts:536-656 at
// 862ad75f), so a UI can show them, "Discovery" 2.1 and "PASE" 2.2,
// this module's names for the steps before matter.js's flow, and
// "Subscribe" 21.1, its name for the node's start after it (the initial
// read and the default subscription) — its
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
// [ErrPairingCode]; with a ctx without a deadline the search for the
// device ends after [DefaultPairTimeout], and a code no device answers
// before it is [ErrNoDevice]; the commissioning of a found device is
// bounded by ctx and the commissioner's own budgets, not by that
// timeout. With [PairOptions].BLE the device is found over BLE and
// commissioned over BTP; with [PairOptions].Address it is dialled at that
// address without discovery. Mirrors CommissioningController.commissionNode
// (CommissioningController.ts:462-500 at 862ad75f), whose discovery takes
// BLE when the options name a BLE capability (discoveryCapabilities,
// :280-283).
//
// The pairing enters the diagnostic trace ([Controller.DiagnosticEvents]):
// its start and each failed step (a warning: one discovery transport of a
// race may fail while another wins), recorded through the Progress hook
// before opts.Progress runs (an opts.Edit that replaces the commissioner's
// Progress replaces that recording too), then the paired node, or — when
// Pair returns an error — the failed pairing as an error, counted once as
// pair.failures ([Config].Metrics).
func (c *Controller) Pair(ctx context.Context, code string, opts PairOptions) (*client.Node, error) {
	tr := &pairTrace{}
	n, err := c.pair(ctx, code, opts, tr)
	switch {
	case err != nil:
		c.recordPairFailed(tr, err)
	case n != nil:
		c.recordPaired(n)
	}
	return n, err
}

// pair is [Controller.Pair] without the trace's final record.
func (c *Controller) pair(ctx context.Context, code string, opts PairOptions, tr *pairTrace) (*client.Node, error) {
	pc, err := ParsePairingCode(code)
	if err != nil {
		return nil, err
	}
	st, ok := c.open()
	if !ok {
		return nil, errClosed
	}
	opts.Progress = c.tracePairing(pc, tr, opts.Progress)
	if c.keepCodes {
		opts.onboardingCode = code
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
	passcode := pc.Passcode
	identity := discovery.Identity{VendorID: pc.VendorID, ProductID: pc.ProductID}
	if known == nil {
		// Without a deadline of the caller's the search alone is bounded:
		// a deadline around the whole pairing would cut the commissioning
		// of a device that was found — over BLE onto Thread the step-18
		// reconnect alone may take minutes.
		var searchTimeout time.Duration
		if _, ok := ctx.Deadline(); !ok {
			searchTimeout = DefaultPairTimeout
		}
		return c.pairDiscovered(ctx, st, pc, identity, opts, searchTimeout)
	}
	commissioner.ReportDiscovery(opts.Progress, PairingSkipped, nil)
	n, err := st.reg.ForDevice(ctx, *known, "")
	if err != nil {
		return nil, err
	}
	o := c.pairOptions(passcode, nil, opts, opts.Progress)
	if err := st.reg.Commission(ctx, n, client.CommissionOptions{Options: o, AutoSubscribe: opts.AutoSubscribe, OnboardingCode: opts.keptCode(o, passcode)}); err != nil {
		return n, err
	}
	return n, nil
}

// keptCode is the code to keep for a commissioning with the options o:
// none when Edit replaced the passcode the code carries.
func (opts PairOptions) keptCode(o commissioner.Options, passcode uint32) string {
	if o.Passcode != passcode {
		return ""
	}
	return opts.onboardingCode
}

// pairOptions are the commissioner's options of a Pair: the passcode,
// the channel (nil over IP), the network, the terms acknowledgement,
// progress and the attestation policy (Reject by default), then
// opts.Edit.
func (c *Controller) pairOptions(passcode uint32, channel commissioner.Channel, opts PairOptions, progress func(PairingStep)) commissioner.Options {
	o := commissioner.Options{
		Passcode: passcode, OnAttestationFailure: opts.OnAttestationFailure,
		Channel: channel, WiFiNetwork: opts.WiFi, ThreadNetwork: opts.Thread,
		TermsAndConditions: opts.TermsAndConditions, Progress: progress,
	}
	if o.OnAttestationFailure.Decide == nil && o.OnAttestationFailure.OnFailure == nil {
		o.OnAttestationFailure = attestation.Reject()
	}
	if opts.Edit != nil {
		opts.Edit(&o)
	}
	return o
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

func random() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint64(b[:])
}

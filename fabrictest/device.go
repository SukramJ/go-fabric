// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"sync"

	_ "modernc.org/sqlite" // the device's SQLite stores with Options.StoreOnDisk (node.Store.DB)

	"thingfabric.dev/go-fabric/bridge"
	"thingfabric.dev/go-fabric/client/discovery"
	"thingfabric.dev/go-fabric/controller/ble/bletest"
	epmem "thingfabric.dev/go-fabric/endpoint/memstore"
	"thingfabric.dev/go-fabric/internal/bridgeseam"
	"thingfabric.dev/go-fabric/internal/inflight"
	"thingfabric.dev/go-fabric/internal/nodeseam"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
	"thingfabric.dev/go-fabric/transport/udp"
)

// Defaults of a device's commissioning configuration — the passcode and
// discriminator chip-tool's examples and the module's own tests pair with.
const (
	DefaultPasscode      uint32 = 20202021
	DefaultDiscriminator uint16 = 3840
	// DefaultPBKDFIterations is the SPAKE2+ iteration count of a device:
	// spake2's minimum, so a test does not spend its time in PBKDF2.
	DefaultPBKDFIterations = 1000
)

// defaultSalt is the SPAKE2+ salt of a device.
var defaultSalt = []byte("SPAKE2P Key Salt")

// Topology is what [Site.AddDevice] builds: the endpoints the device
// serves and any change to the node's configuration.
type Topology struct {
	// Bridged are the endpoints the node's Aggregator serves, added in
	// order before the node starts (node.Aggregator.Add) — the device
	// endpoints of matter.js's MockSite.addDevice. An endpoint's cluster
	// servers are the ones the test holds, so it changes them locally
	// between interactions ([Device.Act]); a restarted device serves the
	// same servers again.
	Bridged []node.Endpoint
	// Snapshotter, when set, is the host topology the node serves in place
	// of its Aggregator (node.Config.Snapshotter): a host test hands its own
	// assembler here — endpointtest.NewEmptySnapshotter is the empty one.
	// Bridged must then be empty.
	Snapshotter bridge.Snapshotter
	// Configure, when set, edits the node's configuration after the site
	// filled it in and before node.New, on every start: a passcode, an
	// advertiser of the test's own, the network listen address on loopback,
	// an OTA requestor. Changing Clock or Store breaks the site's promises.
	Configure func(cfg *node.Config)
	// Prepare, when set, runs with the node after node.New and before its
	// Start, on every start: what a host wires on the node before it comes
	// online — an observer of its bindings (node.Node.Bindings) that must
	// see what the start resolves. The counterpart of a matter.js test
	// acting on a MockServerNode created with `online: false` before
	// node.start().
	Prepare func(n *node.Node)
	// BLE gives the device a Bluetooth radio of the in-memory BLE model
	// (node.Network.GATT): while uncommissioned it is commissionable over
	// BLE through its own bridge, as a device with a radio is, and its QR
	// code says so. [Device.BLE] is the radio as a central reaches it.
	BLE bool
}

// Device is an in-process Matter node of a site — a node.Node with its
// own store, a host address on the site's network and a DNS-SD record set
// the site answers queries from. It mirrors what matter.js's
// MockServerNode exposes to a test (packages/node/src/testing/mock-server-node.ts):
// the node, its store, its endpoints, local access to its servers
// ([Device.Act]) and a restart on the same storage
// (MockSite.storageFor, mock-site.ts:212). It is safe for concurrent use.
type Device struct {
	site  *Site
	index int
	topo  Topology
	// base is the program's own configuration of [Site.AddNode]; nil for
	// [Site.AddDevice], whose configuration the site builds.
	base *node.Config
	// store is the device's persistence, kept across restarts: in memory
	// (store/memstore, endpoint/memstore), or with Options.StoreOnDisk a
	// SQLite file the node opens the stores on.
	store node.Store

	// actMu serialises Act with Start, Stop and Restart.
	actMu sync.Mutex

	// mu guards addr, adv, cfg, n, names and running.
	mu      sync.Mutex
	n       *node.Node
	adv     mdns.Advertiser
	addr    *net.UDPAddr
	cfg     node.Config
	running bool
	// names is the client stack's DNS-SD cache, answered by the site;
	// built at the first start with a non-nil node.Config.Client, kept across
	// restarts and closed with the site.
	names *discovery.Names
	// group counts the work in hand of every node the device runs
	// (internal/inflight), kept across restarts.
	group *inflight.Group
	// ble is the device's BLE peripheral with Topology.BLE, built at the
	// first start and kept across restarts; nil without.
	ble *bletest.Peripheral
}

// clientPort is the port of a device's client socket on its host address
// of the in-memory network: the node's client stack initiates from a
// socket of its own (node/client.go), apart from the server socket on
// udp.MatterPort.
const clientPort = udp.MatterPort + 1

// AddDevice builds a device serving topo, starts it and returns it. The
// device is uncommissioned, its commissioning window open; it stops when
// the site closes. A device that fails to build fails the test.
func (s *Site) AddDevice(topo Topology) *Device {
	s.tb.Helper()
	if topo.Snapshotter != nil && len(topo.Bridged) > 0 {
		s.tb.Fatal("fabrictest: Topology.Bridged with a Snapshotter: the node has no Aggregator to add them to")
	}
	return s.addDevice(topo, nil)
}

// AddNode builds a device from a program's own node configuration — the
// one its daemon composes for node.New — starts it and returns it, so the
// test runs the program's composition on the site. The site puts its own
// parts in: its network (the node's listen address and, with
// a non-nil cfg.Client, its client socket and DNS-SD cache), its clock with
// the host's in-flight counters, an advertiser whose records the site
// answers DNS-SD with, the site's logger when cfg has none, and a store of
// its own when cfg.Store states none (the program's store, a Path or a
// DB, stays the program's). TCP is off on the site. The device serves cfg.Snapshotter or the node's Aggregator, empty
// until the test adds to it ([Device.Act]); a restart builds the node from
// cfg again. matter.js's MockSite.addNode builds a plain ServerNode from the
// test's own options in the site's environment (mock-site.ts:59-106 at
// b25ed960). A node that fails to build fails the test.
func (s *Site) AddNode(cfg node.Config) *Device {
	s.tb.Helper()
	return s.addDevice(Topology{}, &cfg)
}

func (s *Site) addDevice(topo Topology, base *node.Config) *Device {
	s.tb.Helper()
	d := &Device{site: s, index: s.index(), topo: topo, base: base}
	d.group = s.newGroup("device " + strconv.Itoa(d.index))
	var db *sql.DB
	if base != nil && (base.Store.Path != "" || base.Store.DB != nil || base.Store.Credentials != nil || base.Store.Endpoints != nil) {
		d.store = base.Store
	} else if s.opts.StoreOnDisk {
		path := filepath.Join(s.dir, "device"+strconv.Itoa(d.index)+".db")
		var err error
		if db, err = store.OpenSQLite(path); err != nil {
			s.tb.Fatalf("fabrictest: device store: %v", err)
		}
		d.store = node.Store{DB: db}
	} else {
		d.store = node.Store{Credentials: memstore.New(), Endpoints: epmem.New()}
	}
	s.mu.Lock()
	s.devices = append(s.devices, d)
	s.mu.Unlock()
	s.onClose(func() {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		s.logTeardown(fmt.Sprintf("device %d: stop", d.index), d.Stop(ctx))
		if db != nil {
			s.logTeardown(fmt.Sprintf("device %d: close the store", d.index), db.Close())
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		s.tb.Fatalf("fabrictest: device %d: %v", d.index, err)
	}
	return d
}

// Index is the device's host index on the site, from 1.
func (d *Device) Index() int { return d.index }

// Start builds the node again on the device's store and starts it. A
// running device is an error.
func (d *Device) Start(ctx context.Context) error {
	d.actMu.Lock()
	defer d.actMu.Unlock()
	return d.startLocked(ctx)
}

// startLocked is Start's work; d.actMu is held.
func (d *Device) startLocked(ctx context.Context) error {
	d.mu.Lock()
	running := d.running
	d.mu.Unlock()
	if running {
		return errors.New("fabrictest: the device is running")
	}
	// Building the node is the device's work in hand until its own start
	// counts itself: a restart under an outer Site.Run must not let the
	// clock run — the controllers' subscription timeouts with it — while
	// node.New assembles the endpoints (the TestLiveSustainedSurvivesRestart
	// flake, brief CG §3). It is a node.build, which holdBudget does not
	// end: the build waits on no clock and under -race on a loaded machine
	// takes longer than a second.
	d.group.Enter(inflight.NodeBuild)
	building := true
	defer func() {
		if building {
			d.group.Leave(inflight.NodeBuild)
		}
	}()
	cfg := d.config()
	n, err := node.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("node: %w", err)
	}
	// operational is the bridge's socket on the in-memory network, which
	// the node's group messages leave through once it started.
	var operational *udp.Listener
	// A step that fails from here stops the node it built — its client
	// socket among what node.New opened — and closes the bridge's socket,
	// so the next Start finds the site's addresses free (R2-P6-err-2510).
	started := false
	defer func() {
		if started {
			return
		}
		// Under the site's pacing and bounded, as Stop and the site's
		// closer stop a node: a Start Site.Run gave up on may still be in
		// flight, and the node's Stop waits for it and runs the started
		// node's timers on the site's clock — unpaced and unbounded it
		// would never return (review II, R2-C2-test-2501). The node.build
		// count ends first, or it would hold the clock without a budget.
		if building {
			building = false
			d.group.Leave(inflight.NodeBuild)
		}
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
		defer cancel()
		d.site.logTeardown(fmt.Sprintf("device %d: stop after a failed start", d.index), d.site.Run(stopCtx, n.Stop))
		if operational != nil {
			_ = operational.Close()
		}
	}()
	if nw := d.site.net; nw != nil {
		addr := hostAddr(d.index)
		if !bridgeseam.UseListener(n.Bridge(), func() (*udp.Listener, error) {
			l, err := nw.listen(addr, d.group)
			operational = l
			return l, err
		}) {
			return errors.New("fabrictest: the in-memory network carries UDP only (node.Network.TCP.Incoming is set)")
		}
	}
	n.Bridge().AttachOperationalResolver(siteResolver{d.site})
	d.sitePeerClient(n)
	if agg := n.Aggregator(); agg != nil {
		for _, ep := range d.topo.Bridged {
			if err := agg.Add(ctx, ep); err != nil {
				return fmt.Errorf("aggregator: %w", err)
			}
		}
	}
	if d.topo.Prepare != nil {
		d.topo.Prepare(n)
	}
	// Under Site.Run: the node's start re-establishes its former
	// subscriptions and waits for each peer's StatusResponse on a timer
	// of the site's clock (bridge.ReestablishFormerSubscriptions), and a
	// fake clock moves only while the site paces it — a start called
	// directly would wait forever on a response that never comes.
	if err := d.site.Run(ctx, func(ctx context.Context) error {
		// The node's Start counts its own work from here.
		building = false
		d.group.Leave(inflight.NodeBuild)
		return n.Start(ctx)
	}); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if operational != nil {
		// The node's group sender opened a kernel socket of its own
		// (node/groupsend.go); on the in-memory network its group messages
		// leave through the bridge's socket instead (multicast.go).
		n.Groups().SetTransport(operational)
	}
	addr, err := net.ResolveUDPAddr("udp", n.Bridge().LocalAddr())
	if err != nil {
		return fmt.Errorf("address: %w", err)
	}
	started = true
	d.mu.Lock()
	d.n, d.adv, d.addr, d.cfg, d.running = n, cfg.Advertiser, addr, cfg, true
	d.mu.Unlock()
	return nil
}

// config is the node's configuration: the site's defaults, then
// Topology.Configure; for [Site.AddNode] the program's configuration with
// the site's parts put in.
func (d *Device) config() node.Config {
	if d.base != nil {
		return d.programConfig()
	}
	i := strconv.Itoa(d.index)
	cfg := node.Config{
		Network: node.Network{Listen: "[::1]:0"},
		Commissioning: node.Commissioning{
			Passcode: DefaultPasscode, Discriminator: DefaultDiscriminator,
			PBKDFIterations: DefaultPBKDFIterations, Salt: defaultSalt,
		},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric", ProductName: "fabrictest device", NodeLabel: "device" + i, SerialNumber: "FT-" + i,
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:       d.store,
		Advertiser:  mdns.NewNoop(),
		Snapshotter: d.topo.Snapshotter,
		Logger:      d.site.log,
		Clock:       d.site.hostClock(d.group),
	}
	if d.topo.Configure != nil {
		d.topo.Configure(&cfg)
	}
	if cfg.Advertiser == nil {
		cfg.Advertiser = mdns.NewNoop()
	}
	if cfg.Client != nil {
		d.siteClient(&cfg)
	}
	if d.topo.BLE {
		cfg.Network.GATT = d.blePeripheral(cfg).Radio()
	}
	return cfg
}

// programConfig is [Site.AddNode]'s configuration: the program's, with
// the site's network, clock, advertiser, logger and store put in.
func (d *Device) programConfig() node.Config {
	cfg := *d.base
	cfg.Store = d.store
	cfg.Network.Listen = "[::1]:0"
	cfg.Network.TCP = node.TCPConfig{}
	// The site's advertiser replaces the program's own, which its MDNS
	// configured (node.Config.Advertiser excludes it).
	cfg.Network.MDNS = node.MDNS{}
	cfg.Advertiser = mdns.NewNoop()
	if cfg.Logger == nil {
		cfg.Logger = d.site.log
	}
	cfg.Clock = d.site.hostClock(d.group)
	if cfg.Client != nil {
		c := *cfg.Client
		c.Names, c.Listen = nil, ""
		cfg.Client = &c
		d.siteClient(&cfg)
	}
	return cfg
}

// siteClient puts the node's client stack onto the site: its DNS-SD
// cache asks the site (as a Controller's does, mdns.go), and on the
// in-memory network its socket is the device's host address on
// clientPort (on loopback a real socket on [::1]). A Names the topology
// set stays the topology's.
func (d *Device) siteClient(cfg *node.Config) {
	// The site's settings go into a copy: the topology's ClientConfig
	// stays as it set it.
	c := *cfg.Client
	cfg.Client = &c
	if cfg.Client.Names == nil {
		d.mu.Lock()
		if d.names == nil {
			d.names = discovery.NewNames(newQueryConn(d.site), d.site.log)
			names := d.names
			d.site.onClose(func() { d.site.logTeardown("close the DNS-SD cache", names.Close()) })
		}
		cfg.Client.Names = d.names
		d.mu.Unlock()
	}
	nw := d.site.net
	if nw == nil {
		if cfg.Client.Listen == "" {
			cfg.Client.Listen = "[::1]:0"
		}
		return
	}
	addr := hostAddr(d.index)
	addr.Port = clientPort
	nodeseam.UseClientListener(cfg, func() (*udp.Listener, error) { return nw.listen(addr, d.group) })
}

// sitePeerClient puts the client stack a node without a configured one
// builds for its peer path (docs/adr/0032) onto the site, as siteClient
// does for a configured one: its DNS-SD cache asks the site, its socket is
// the device's client address on the in-memory network or a real socket on
// [::1] on loopback.
func (d *Device) sitePeerClient(n *node.Node) {
	d.mu.Lock()
	if d.names == nil {
		d.names = discovery.NewNames(newQueryConn(d.site), d.site.log)
		names := d.names
		d.site.onClose(func() { d.site.logTeardown("close the DNS-SD cache", names.Close()) })
	}
	names := d.names
	d.mu.Unlock()
	nw := d.site.net
	if nw == nil {
		nodeseam.UsePeerClient(n, names, "[::1]:0", nil)
		return
	}
	addr := hostAddr(d.index)
	addr.Port = clientPort
	nodeseam.UsePeerClient(n, names, "", func() (*udp.Listener, error) { return nw.listen(addr, d.group) })
}

// Stop stops the node; its store and address stay the device's. A
// stopped device is not an error.
func (d *Device) Stop(ctx context.Context) error {
	d.actMu.Lock()
	defer d.actMu.Unlock()
	d.mu.Lock()
	n, running := d.n, d.running
	d.running = false
	d.mu.Unlock()
	if !running {
		return nil
	}
	if err := d.stopNode(ctx, n); err != nil {
		// The node is not stopped (node.Stop: the next Stop finishes the
		// work): the device stays running, so the next Stop and the
		// site's closer still reach it (review II, R2-C2-test-2502).
		d.mu.Lock()
		d.running = true
		d.mu.Unlock()
		return err
	}
	return nil
}

// stopNode is Stop's work on n; d.actMu is held.
func (d *Device) stopNode(ctx context.Context, n *node.Node) error {
	if err := d.site.Run(ctx, n.Stop); err != nil { // the stop's own timers on the site's clock, as Start
		return err
	}
	// The stop's CloseSession farewells are in the network or in the
	// controllers' hands when it returns: Stop returns once they handled
	// them, so a request a test sends next does not go out on a session
	// its controller is about to learn the device ended (the
	// TestHostStatementName flake, brief CG §0.4). The clock stands
	// meanwhile: a Restart under an outer Site.Run would otherwise let
	// the controllers' reconnection timers run while the device is down.
	return d.site.drain(ctx, d.group)
}

// FactoryReset factory-resets the device's node ([node.Node.FactoryReset]:
// its fabrics removed, the node stopped, its store and endpoint records
// erased) and, when the device was running, starts a new node on the
// erased store and the same address — matter.js ServerNode.erase, which
// comes back online when the node was (ServerNode.ts:159-200). A Stop
// called meanwhile waits for it and takes the device offline after it. A
// device that is stopped has its store erased and stays stopped.
func (d *Device) FactoryReset(ctx context.Context) error {
	d.actMu.Lock()
	defer d.actMu.Unlock()
	d.mu.Lock()
	n, running := d.n, d.running
	d.running = false
	d.mu.Unlock()
	if !running {
		if n == nil {
			return errors.New("fabrictest: the device has no node")
		}
		return n.FactoryReset(ctx)
	}
	if err := d.site.Run(ctx, n.FactoryReset); err != nil {
		return err
	}
	if err := d.site.drain(ctx, d.group); err != nil {
		return err
	}
	return d.startLocked(ctx)
}

// Restart stops the node and starts a new one on the same store and
// address, serving the same topology: what survives is what the node
// persisted — fabrics, ACL, subscriptions, endpoint numbers.
func (d *Device) Restart(ctx context.Context) error {
	if err := d.Stop(ctx); err != nil {
		return fmt.Errorf("fabrictest: stop: %w", err)
	}
	return d.Start(ctx)
}

// Running reports whether the device's node runs: false after
// [Device.Stop], or after a [Device.FactoryReset] of a stopped device.
func (d *Device) Running() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

// Node is the running node; after a [Device.Restart] it is the new one.
func (d *Device) Node() *node.Node {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.n
}

// AwaitCASESession waits, bounded by ctx and on the site's clock
// ([Site.Eventually]), until the device's node lists an established CASE
// session with the node peerNodeID on its fabric fabricIndex — after a
// peer connected to the device directly, before the test reads the
// device's session state or acts on the session from the device's side.
// The device completes its side of CASE after it sent the status report
// that ends the handshake and lets the peer go on, and lists no session
// still establishing; [Controller.Commission] waits the same way.
func (d *Device) AwaitCASESession(ctx context.Context, fabricIndex uint8, peerNodeID uint64) error {
	return d.site.Eventually(ctx, fmt.Sprintf("device %d's CASE session with node %#x on fabric %d", d.index, peerNodeID, fabricIndex), func() bool {
		return d.listsCASESession(func(fabric uint8, peer, _ uint64) bool {
			return fabric == fabricIndex && peer == peerNodeID
		})
	})
}

// listsCASESession reports whether the device's node lists an
// established CASE session match accepts, by its fabric index, peer node
// id and local node id. Sessions omits a session still establishing
// (operational.Manager.Sessions).
func (d *Device) listsCASESession(match func(fabric uint8, peer, local uint64) bool) bool {
	n := d.Node()
	if n == nil {
		return false
	}
	for _, s := range n.Sessions().Sessions() {
		if !s.IsPASE && match(s.FabricIndex, s.PeerNodeID, s.LocalNodeID) {
			return true
		}
	}
	return false
}

// Addr is the device's UDP address: its host address on the in-memory
// network, its socket's on loopback.
func (d *Device) Addr() *net.UDPAddr {
	d.mu.Lock()
	defer d.mu.Unlock()
	return cloneAddr(d.addr)
}

// Passcode is the device's setup passcode.
func (d *Device) Passcode() uint32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg.Commissioning.Passcode
}

// Discriminator is the device's commissioning discriminator.
func (d *Device) Discriminator() uint16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg.Commissioning.Discriminator
}

// EndpointOf is the number of the bridged endpoint whose StableKey renders
// as key; false when the node serves none.
func (d *Device) EndpointOf(key string) (uint16, bool) {
	t := d.Node().Bridge().Topology()
	if t == nil {
		return 0, false
	}
	for _, ep := range t.Bridged() {
		if ep.SourceKey != nil && ep.SourceKey.String() == key {
			return ep.ID, true
		}
	}
	return 0, false
}

// Act runs fn with the running node for local access to its servers —
// the local access matter.js's MockServerNode gives a test
// (mock-server-node.ts): an attribute fn sets on a server is reported to
// subscribers as a host's change is. Act does not
// overlap Start, Stop, Restart or FactoryReset, and returns fn's error; a
// device that is not running is refused without fn (R2-P6-conc-2520).
// fn must not call Start, Stop, Restart, FactoryReset, OpenWindow or Act
// of the same device: each waits for Act to return.
func (d *Device) Act(fn func(n *node.Node) error) error {
	d.actMu.Lock()
	defer d.actMu.Unlock()
	d.mu.Lock()
	n, running := d.n, d.running
	d.mu.Unlock()
	if n == nil || !running {
		return errors.New("fabrictest: the device is not running")
	}
	return fn(n)
}

// advertised is what the site's DNS-SD answers for the device: its
// advertiser, its IP and its host name.
func (d *Device) advertised() (mdns.Advertiser, net.IP, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.running || d.addr == nil {
		return nil, nil, ""
	}
	return d.adv, d.addr.IP, hostName(d.index)
}

// siteResolver is every device's operational resolver: a controller of the
// site by its fabric and node id. A device reaches a controller through it
// when it re-establishes a subscription (bridge.OperationalResolver).
type siteResolver struct{ s *Site }

func (r siteResolver) ResolveOperational(ctx context.Context, compressedFabricID [8]byte, nodeID uint64) ([]*net.UDPAddr, error) {
	for _, c := range r.s.Controllers() {
		if c.answers(compressedFabricID, nodeID) {
			return []*net.UDPAddr{c.Addr()}, nil
		}
	}
	for _, a := range r.s.controllerAdvertisers() {
		if addr := a.answersOperational(compressedFabricID, nodeID); addr != nil {
			return []*net.UDPAddr{addr}, nil
		}
	}
	// A device of the site, as its operational record names it: a node's
	// initiator (an OTA requestor reaching its provider) resolves another
	// node of its fabric.
	for _, d := range r.s.Devices() {
		if d.answers(ctx, compressedFabricID, nodeID) {
			return []*net.UDPAddr{d.Addr()}, nil
		}
	}
	return nil, fmt.Errorf("fabrictest: no node %016X-%016X on the site", compressedFabricID, nodeID)
}

// answers reports whether the running device is node nodeID of the fabric
// with compressedFabricID.
func (d *Device) answers(ctx context.Context, compressedFabricID [8]byte, nodeID uint64) bool {
	d.mu.Lock()
	n, running := d.n, d.running
	d.mu.Unlock()
	if !running || n == nil {
		return false
	}
	fabrics, err := n.FabricInfos(ctx)
	if err != nil {
		return false
	}
	for _, f := range fabrics {
		if f.CompressedFabricID == compressedFabricID && f.NodeID == nodeID {
			return true
		}
	}
	return false
}

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

	_ "modernc.org/sqlite" // the device's SQLite stores (node.Store.DB)

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/internal/bridgeseam"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/udp"
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
	// of its Aggregator (node.Config.Topology): a host test hands its own
	// assembler here — endpointtest.NewEmptySnapshotter is the empty one.
	// Bridged must then be empty.
	Snapshotter bridge.Snapshotter
	// Configure, when set, edits the node's configuration after the site
	// filled it in and before node.New, on every start: a passcode, an
	// advertiser of the test's own, the network listen address on loopback,
	// an OTA requestor. Changing Clock or Store breaks the site's promises.
	Configure func(cfg *node.Config)
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
	db    *sql.DB

	// actMu serialises Act with Start, Stop and Restart.
	actMu sync.Mutex

	mu      sync.Mutex
	n       *node.Node
	adv     mdns.Advertiser
	addr    *net.UDPAddr
	cfg     node.Config
	running bool
}

// AddDevice builds a device serving topo, starts it and returns it. The
// device is uncommissioned, its commissioning window open; it stops when
// the site closes. A device that fails to build fails the test.
func (s *Site) AddDevice(topo Topology) *Device {
	s.tb.Helper()
	if topo.Snapshotter != nil && len(topo.Bridged) > 0 {
		s.tb.Fatal("fabrictest: Topology.Bridged with a Snapshotter: the node has no Aggregator to add them to")
	}
	d := &Device{site: s, index: s.index(), topo: topo}
	path := filepath.Join(s.tb.TempDir(), "device"+strconv.Itoa(d.index)+".db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		s.tb.Fatalf("fabrictest: device store: %v", err)
	}
	d.db = db
	s.mu.Lock()
	s.devices = append(s.devices, d)
	s.mu.Unlock()
	s.onClose(func() {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		_ = d.Stop(ctx)
		_ = db.Close()
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
	d.mu.Lock()
	running := d.running
	d.mu.Unlock()
	if running {
		return errors.New("fabrictest: the device is running")
	}
	cfg := d.config()
	n, err := node.New(cfg)
	if err != nil {
		return fmt.Errorf("node: %w", err)
	}
	if nw := d.site.net; nw != nil {
		addr := hostAddr(d.index)
		if !bridgeseam.UseListener(n.Bridge(), func() (*udp.Listener, error) { return nw.listen(addr) }) {
			return errors.New("fabrictest: the in-memory network carries UDP only (node.Network.TCP.Incoming is set)")
		}
	}
	n.Bridge().AttachOperationalResolver(siteResolver{d.site})
	if agg := n.Aggregator(); agg != nil {
		for _, ep := range d.topo.Bridged {
			if err := agg.Add(ctx, ep); err != nil {
				return fmt.Errorf("aggregator: %w", err)
			}
		}
	}
	if err := n.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	addr, err := net.ResolveUDPAddr("udp", n.Bridge().LocalAddr())
	if err != nil {
		_ = n.Stop(ctx)
		return fmt.Errorf("address: %w", err)
	}
	d.mu.Lock()
	d.n, d.adv, d.addr, d.cfg, d.running = n, cfg.Advertiser, addr, cfg, true
	d.mu.Unlock()
	return nil
}

// config is the node's configuration: the site's defaults, then
// Topology.Configure.
func (d *Device) config() node.Config {
	i := strconv.Itoa(d.index)
	cfg := node.Config{
		Network: node.Network{Listen: "[::1]:0", DisableReannounce: true},
		Commissioning: node.Commissioning{
			Passcode: DefaultPasscode, Discriminator: DefaultDiscriminator,
			PBKDFIterations: DefaultPBKDFIterations, Salt: defaultSalt,
		},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric", ProductName: "fabrictest device", NodeLabel: "device" + i, SerialNumber: "FT-" + i,
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:      node.Store{DB: d.db},
		Advertiser: mdns.NewNoop(),
		Topology:   d.topo.Snapshotter,
		Logger:     d.site.log,
		Clock:      d.site.clk,
	}
	if d.topo.Configure != nil {
		d.topo.Configure(&cfg)
	}
	if cfg.Advertiser == nil {
		cfg.Advertiser = mdns.NewNoop()
	}
	return cfg
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
	return n.Stop(ctx)
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

// Node is the running node; after a [Device.Restart] it is the new one.
func (d *Device) Node() *node.Node {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.n
}

// Bridge is the node's bridge.
func (d *Device) Bridge() *bridge.Bridge { return d.Node().Bridge() }

// Store is the node's credential store: fabrics, ACL, groups,
// subscriptions and settings.
func (d *Device) Store() *store.Store { return d.Node().Store() }

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

// Advertiser is the node's DNS-SD advertiser — the records the site
// answers queries with.
func (d *Device) Advertiser() mdns.Advertiser {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.adv
}

// Endpoints are the endpoints the node serves: the root, the Aggregator
// and the bridged ones.
func (d *Device) Endpoints() []*endpoint.Endpoint {
	if t := d.Bridge().Topology(); t != nil {
		return t.Endpoints
	}
	return nil
}

// EndpointOf is the number of the bridged endpoint whose StableKey renders
// as key; false when the node serves none.
func (d *Device) EndpointOf(key string) (uint16, bool) {
	t := d.Bridge().Topology()
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
// matter.js Node.act, which MockServerNode's tests use to change a
// behavior's state in place (mock-server-node.ts): an attribute fn sets on
// a server is reported to subscribers as a host's change is. Act does not
// overlap Start, Stop or Restart, and returns fn's error.
func (d *Device) Act(fn func(n *node.Node) error) error {
	d.actMu.Lock()
	defer d.actMu.Unlock()
	n := d.Node()
	if n == nil {
		return errors.New("fabrictest: the device never started")
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

func (r siteResolver) ResolveOperational(_ context.Context, compressedFabricID [8]byte, nodeID uint64) ([]*net.UDPAddr, error) {
	for _, c := range r.s.Controllers() {
		if c.answers(compressedFabricID, nodeID) {
			return []*net.UDPAddr{c.Addr()}, nil
		}
	}
	return nil, fmt.Errorf("fabrictest: no controller %016X-%016X on the site", compressedFabricID, nodeID)
}

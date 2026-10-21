// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/netip"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// liveRegistry is the AK harness without its commissioning: a node.Node
// with one plug, and the controller — exchange manager, DNS-SD cache with
// the node's operational record injected, one fabric labelled "live",
// peer set, subscription registry, commissioner — whose client nodes a
// Registry on a SQLite store holds.
type liveRegistry struct {
	lv     *live
	db     *sql.DB
	store  *sqlstore.Store
	mgr    *exchange.Manager
	set    *peer.Set
	com    *commissioner.Commissioner
	fabric *commissioner.Fabric
	subs   *interaction.Subscriptions
	clock  *fakeClock
	hooks  *liveRegHooks
}

// liveRegHooks records what the registry's nodes report.
type liveRegHooks struct {
	mu      sync.Mutex
	alive   int
	startUp int
	changed chan struct{}
}

func (h *liveRegHooks) hooksFor(string) client.Hooks {
	return client.Hooks{
		SubscriptionAlive: func() {
			h.mu.Lock()
			h.alive++
			h.mu.Unlock()
		},
		Event: func(_ uint16, cluster, event uint32, _ interaction.EventData, _ any) {
			if cluster == basicinfo.ClusterID && event == basicinfo.EventStartUp {
				h.mu.Lock()
				h.startUp++
				h.mu.Unlock()
			}
		},
		Changed: func() {
			select {
			case h.changed <- struct{}{}:
			default:
			}
		},
	}
}

func (h *liveRegHooks) count(f *int) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return *f
}

func newLiveRegistryHarness(t *testing.T) *liveRegistry {
	t.Helper()
	lr := &liveRegistry{lv: &live{dbPath: filepath.Join(t.TempDir(), "node.db")}, clock: newFakeClock(), hooks: &liveRegHooks{changed: make(chan struct{}, 1024)}}
	lv := lr.lv
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	lv.addr = l.LocalAddr()
	lv.node, lv.srv = liveNodeAt(t, lv.dbPath, func() *net.UDPAddr { return lv.addr })
	lr.mgr, err = exchange.NewManager(l, exchange.Config{Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, stop := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(serveCtx, lr.mgr.HandleDatagram)
	}()
	conn := &fakeMDNS{out: make(chan mdns.QueryResponse, 64)}
	names := discovery.NewNames(conn, discardLog())
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	lr.fabric, err = commissioner.NewFabric(authority, commissioner.FabricConfig{FabricID: liveFabricID, RootNodeID: liveController, VendorID: 0xFFF1, Label: "live"})
	if err != nil {
		t.Fatal(err)
	}
	resumption := &peer.MemoryResumptionStore{}
	lr.set, err = peer.NewSet(peer.Context{Exchanges: lr.mgr, Names: names, Fabrics: lr.fabric.Lookup, Resumption: resumption, Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	lr.subs = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Logger: discardLog()})
	lv.reg = lr.subs
	lr.com, err = commissioner.New(commissioner.Context{
		Exchanges: lr.mgr, Peers: lr.set, Attestation: attestation.TestStore(), Logger: discardLog(),
		Subscriptions: lr.subs, Fabrics: []*commissioner.Fabric{lr.fabric}, Resumption: resumption,
	})
	if err != nil {
		t.Fatal(err)
	}
	lr.db, err = sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "controller.db")+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlstore.Apply(context.Background(), lr.db); err != nil {
		t.Fatal(err)
	}
	lr.store = sqlstore.New(lr.db)
	t.Cleanup(func() {
		_ = lr.subs.Close(context.Background())
		lr.com.Close()
		lr.set.Close(context.Background())
		lr.mgr.Close()
		stop()
		<-served
		_ = names.Close()
		_ = lr.db.Close()
	})
	nodeAddr, err := net.ResolveUDPAddr("udp", lv.node.Bridge().LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	op := discovery.OperationalQName(lr.fabric.Identity.CompressedFabricID, liveNode)
	hdr := func(name string, typ uint16) dns.RR_Header {
		return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: typ, Class: dns.ClassINET | 0x8000, Ttl: 120}
	}
	msg := new(dns.Msg)
	msg.Response = true
	msg.Answer = []dns.RR{
		&dns.SRV{Hdr: hdr(op, dns.TypeSRV), Target: "live-node.local.", Port: uint16(nodeAddr.Port)}, //nolint:gosec // a test port
		&dns.TXT{Hdr: hdr(op, dns.TypeTXT), Txt: []string{"SII=500", "SAI=300"}},
		&dns.A{Hdr: hdr("live-node.local", dns.TypeA), A: net.ParseIP("127.0.0.1").To4()},
	}
	b, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	conn.out <- mdns.QueryResponse{Msg: b}
	for _, ep := range lv.node.Bridge().Topology().Bridged() {
		if ep.SourceKey != nil && ep.SourceKey.String() == "plug:1" {
			lv.plugEP = ep.ID
		}
	}
	if lv.plugEP == 0 {
		t.Fatal("the plug has no endpoint")
	}
	return lr
}

func (lr *liveRegistry) registry(t *testing.T) *client.Registry {
	t.Helper()
	r, err := client.NewRegistry(client.RegistryConfig{
		Store: lr.store, Commissioner: lr.com, Peers: lr.set, Exchanges: lr.mgr, Fabrics: []*commissioner.Fabric{lr.fabric},
		Subscriptions: lr.subs, AutoStart: true, Logger: discardLog(), Clock: lr.clock, HooksFor: lr.hooks.hooksFor,
		Node: client.Config{Logger: discardLog(), Rand: func() float64 { return 0 }},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

// ownFabricLabel reads OperationalCredentials.Fabrics through the node
// (fabric-filtered: our entry) and returns its label.
func ownFabricLabel(ctx context.Context, t *testing.T, n *client.Node) string {
	t.Helper()
	rep, err := n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{
		Attributes: []im.ConcreteAttributePath{attrPath(0, opCreds, opCredsFabrics)},
	}, IncludeKnownVersions: true})
	if err != nil {
		t.Fatalf("reading Fabrics: %v", err)
	}
	v, ok := rep.Value(0, opCreds, opCredsFabrics)
	if !ok || len(v.Children) != 1 {
		t.Fatalf("Fabrics %+v", v)
	}
	for _, f := range v.Children {
		for _, c := range f.Children {
			if c.El.Tag.Number == 5 {
				s, _ := spec.DecodeString(c)
				return s
			}
		}
	}
	t.Fatal("no label")
	return ""
}

func (lr *liveRegistry) waitAlive(t *testing.T, after int) {
	t.Helper()
	waitUntil60(t, "a SubscriptionAlive", func() bool { return lr.hooks.count(&lr.hooks.alive) > after })
}

func waitUntil60(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func onOffOf(n *client.Node, ep uint16) any {
	e := n.Endpoint(ep)
	if e == nil {
		return nil
	}
	c := e.Cluster(onoff.ClusterID)
	if c == nil {
		return nil
	}
	v, _ := c.Decoded(onoff.AttrOnOff)
	return v
}

// TestLiveRegistry: the registry over a commissioned node.Node, on a
// SQLite store — a device found and commissioned through the registry
// (Peers.forDescriptor, CommissioningClient.commission), the fabric label
// reconciled once per start (NetworkClient.#reconcileFabricLabel), the
// registry restored from the database and auto-started (Peers.initialize,
// #nodeOnline), the device's restart answered by the startUp reaction
// (Peers.#onStartUp), the node decommissioned (ClientNode.decommission),
// and a commissionable node culled (#cullExpiredNodesAndAddresses).
func TestLiveRegistry(t *testing.T) {
	t.Parallel()
	lr := newLiveRegistryHarness(t)
	lv := lr.lv
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	r := lr.registry(t)
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// 1. Found and commissioned.
	nodeAddr := netip.MustParseAddrPort(lv.node.Bridge().LocalAddr())
	n, err := r.ForDevice(ctx, discovery.Device{InstanceID: "live", Addresses: []discovery.Address{{IP: nodeAddr.Addr(), Port: nodeAddr.Port()}}})
	if err != nil {
		t.Fatal(err)
	}
	if rec := n.Record(); n.ID() != "peer1" || n.IsCommissioned() || len(rec.Addresses) != 1 || rec.DiscoveredAt.IsZero() {
		t.Fatalf("found %s %+v", n.ID(), rec)
	}
	if err := r.Commission(ctx, n, client.CommissionOptions{Options: commissioner.Options{
		Passcode: testPasscode, NodeID: liveNode, OnAttestationFailure: attestation.Accept(),
	}}); err != nil {
		t.Fatalf("Commission: %v", err)
	}
	addr := handshake.PeerAddress{FabricIndex: lr.fabric.FabricIndex, NodeID: liveNode}
	rec := n.Record()
	if rec.PeerAddress == nil || *rec.PeerAddress != addr || rec.CommissionedAt.IsZero() || rec.FabricIndexOnPeer == 0 || len(rec.Addresses) != 1 {
		t.Fatalf("record after commissioning %+v", rec)
	}
	waitUntil60(t, "Connected", func() bool { return n.ConnectionState() == client.Connected })
	if !n.Seeded() || !slices.Equal(ids(r.Commissioned()), []string{"peer1"}) {
		t.Fatal("not seeded, or not the commissioned node")
	}
	if got, _ := lr.store.NodeIDs(); !slices.Equal(got, []string{"peer1"}) {
		t.Fatalf("stored %v", got)
	}
	t.Logf("record: fabric index on peer %d, addresses %v", rec.FabricIndexOnPeer, rec.Addresses)

	// 2. The fabric label: the commissioner set "live"; after the first
	// SubscriptionAlive there is nothing to reconcile. Renamed, and the node
	// restarted: the next SubscriptionAlive pushes "renamed".
	lr.waitAlive(t, 0)
	client.WaitRegistry(r)
	if got := ownFabricLabel(ctx, t, n); got != "live" {
		t.Fatalf("the device's label %q", got)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	lr.fabric.Label = "renamed"
	alive := lr.hooks.count(&lr.hooks.alive)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	lr.waitAlive(t, alive)
	client.WaitRegistry(r)
	if got := ownFabricLabel(ctx, t, n); got != "renamed" {
		t.Fatalf("the device's label after the reconciliation %q", got)
	}
	fabrics, err := lv.node.FabricInfos(ctx)
	if err != nil || len(fabrics) != 1 || fabrics[0].Label != "renamed" {
		t.Fatalf("the device's store: %+v %v", fabrics, err)
	}

	// 3. Restored from the database: bound to its peer, its image from the
	// cache; AutoStart resumes the subscription.
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	r2 := lr.registry(t)
	n2 := r2.Get("peer1")
	if n2 == nil || n2 == n || !n2.IsCommissioned() || n2.Record().FabricIndexOnPeer != rec.FabricIndexOnPeer {
		t.Fatal("peer1 not restored")
	}
	p := lr.set.Get(addr)
	if p == nil || p.Descriptor().OperationalAddress == nil || p.Descriptor().OperationalAddress.String() != n2.Record().Addresses[0].String() {
		t.Fatal("the restored node's peer")
	}
	if len(n2.Endpoints()) < 3 || onOffOf(n2, lv.plugEP) == nil || n2.SubscriptionActive() {
		t.Fatalf("the restored image %v", tree(n2))
	}
	if err := r2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitUntil60(t, "the resumed subscription", n2.SubscriptionActive)
	lv.srv.Set(true)
	waitUntil60(t, "OnOff true", func() bool { return onOffOf(n2, lv.plugEP) == true })

	// 4. The device restarts: its StartUp arrives over the subscription it
	// resumes, and the sessions from before the restart are given up.
	before := lr.mgr.SessionsFor(addr)
	startUps := lr.hooks.count(&lr.hooks.startUp)
	if err := lv.node.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	for _, s := range before {
		t.Logf("after the device's stop: session lost %v closed %v", s.IsPeerLost(), s.IsClosed())
	}
	lv.node, lv.srv = liveNodeAt(t, lv.dbPath, func() *net.UDPAddr { return lv.addr })
	waitUntil60(t, "the StartUp event", func() bool { return lr.hooks.count(&lr.hooks.startUp) > startUps })
	client.WaitRegistry(r2)
	after := lr.mgr.SessionsFor(addr)
	for _, s := range before {
		if slices.Contains(after, s) {
			t.Fatalf("a session from before the restart survived: %d before, %d after", len(before), len(after))
		}
	}
	t.Logf("sessions: %d before the restart, %d after", len(before), len(after))
	lv.srv.Set(false)
	waitUntil60(t, "OnOff false after the restart", func() bool { return onOffOf(n2, lv.plugEP) == false })

	// 5. Decommissioned: the device's fabric is gone, so are the node, its
	// stored data and its protocol peer.
	if err := r2.Decommission(ctx, n2); err != nil {
		t.Fatalf("Decommission: %v", err)
	}
	if fabrics, err := lv.node.FabricInfos(ctx); err != nil || len(fabrics) != 0 {
		t.Fatalf("the device's fabrics after the decommission %+v %v", fabrics, err)
	}
	if len(r2.Commissioned()) != 0 || r2.Size() != 0 || lr.set.Get(addr) != nil {
		t.Fatal("the node or its peer outlived the decommission")
	}
	if got, _ := lr.store.NodeIDs(); len(got) != 0 {
		t.Fatalf("stored %v", got)
	}

	// 6. A commissionable node the scanner does not see again is culled
	// once its lifespan ends: DefaultTTL, the Go scanner carrying no TTL.
	gone, err := r2.ForDevice(ctx, discovery.Device{InstanceID: "gone", Addresses: []discovery.Address{{IP: netip.MustParseAddr("127.0.0.1"), Port: 9}}})
	if err != nil || gone.ID() != "peer2" {
		t.Fatalf("ForDevice %v %v", gone, err)
	}
	lr.clock.Advance(client.DefaultTTL - client.ExpirationInterval)
	if r2.Get("peer2") == nil {
		t.Fatal("culled before its lifespan ended")
	}
	lr.clock.Advance(2 * client.ExpirationInterval)
	if r2.Get("peer2") != nil || r2.Size() != 0 {
		t.Fatal("not culled")
	}
}

func ids(nodes []*client.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.ID())
	}
	return out
}

// The registry's live harness: a node.Node with one bridged plug in
// process on a loopback socket and the system clock, and the controller
// parts the harness wires around it. (TestLiveClientNode runs on a
// fabrictest pair instead, live_test.go.)

const (
	liveFabricID     uint64 = 0x0000_0000_0000_0F0D
	liveController   uint64 = 112233
	liveNode         uint64 = 0x0000_0000_0000_4323
	liveDiscriminate uint16 = 3840
	testPasscode     uint32 = 20202021
)

// plug is a bridged OnOffPlugInUnit holding an onoff.Server.
type plug struct{ srv *onoff.Server }

func (p *plug) Spec() endpoint.Spec {
	return endpoint.Spec{
		StableKey: endpoint.StringKey("plug:1"), DeviceAddress: "plug:1", ChannelAddress: "plug:1:0",
		DeviceType: onoff.DeviceTypeOnOffPlugInUnit, FriendlyName: "plug", Source: p,
	}
}

func (p *plug) MatterDeviceType() uint16 { return onoff.DeviceTypeOnOffPlugInUnit }

func (p *plug) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{p.srv}
}

// fakeMDNS is an mDNS socket that sends nowhere and receives what the test
// injects.
type fakeMDNS struct{ out chan mdns.QueryResponse }

func (f *fakeMDNS) Send([]byte) error                    { return nil }
func (f *fakeMDNS) Responses() <-chan mdns.QueryResponse { return f.out }
func (f *fakeMDNS) Close() error                         { return nil }

type resolverFunc func(ctx context.Context, cfid [8]byte, nodeID uint64) ([]*net.UDPAddr, error)

func (f resolverFunc) ResolveOperational(ctx context.Context, cfid [8]byte, nodeID uint64) ([]*net.UDPAddr, error) {
	return f(ctx, cfid, nodeID)
}

func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

// liveNodeAt builds a node with one plug on the database at path, the
// controller's address as its operational resolver's answer.
func liveNodeAt(t *testing.T, path string, controllerAddr func() *net.UDPAddr) (*node.Node, *onoff.Server) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := node.New(node.Config{
		Network:            node.Network{Listen: "127.0.0.1:0", PreferIPv4: true},
		Commissioning:      node.Commissioning{Passcode: testPasscode, Discriminator: liveDiscriminate, PBKDFIterations: 1000, Salt: []byte("SPAKE2P Key Salt")},
		ProductDescription: node.ProductDescription{VendorID: 0xFFF1, ProductID: 0x8001},
		BasicInformation: node.BasicInformation{
			VendorName: "go-fabric test", ProductName: "client live test", NodeLabel: "live node", SerialNumber: "CL-1",
			HardwareVersion: 1, HardwareVersionString: "1", SoftwareVersion: 1, SoftwareVersionString: "1",
		},
		Store:      node.Store{DB: db},
		Advertiser: mdns.NewNoop(),
		Logger:     discardLog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Aggregator().Add(context.Background(), &plug{srv: srv}); err != nil {
		t.Fatal(err)
	}
	n.Bridge().AttachOperationalResolver(resolverFunc(func(context.Context, [8]byte, uint64) ([]*net.UDPAddr, error) {
		return []*net.UDPAddr{controllerAddr()}, nil
	}))
	if err := n.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	return n, srv
}

// live is the registry harness's node: one plug, on a loopback socket.
type live struct {
	node   *node.Node
	srv    *onoff.Server
	dbPath string
	addr   *net.UDPAddr
	reg    *interaction.Subscriptions
	plugEP uint16
}

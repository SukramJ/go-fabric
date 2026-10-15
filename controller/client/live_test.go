// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// The live harness follows controller/interaction/live_test.go: a node.Node
// with one bridged plug in process, commissioned by the module's own
// commissioner, whose context carries the subscription registry and the
// CASE responder.

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

// live is a node commissioned by the module's own commissioner.
type live struct {
	node   *node.Node
	srv    *onoff.Server
	dbPath string
	addr   *net.UDPAddr
	reg    *interaction.Subscriptions
	peer   *peer.Peer
	c      *interaction.Interaction
	plugEP uint16
}

func newLive(t *testing.T) *live {
	t.Helper()
	lv := &live{dbPath: filepath.Join(t.TempDir(), "node.db")}
	l, err := udp.New(udp.Config{LocalAddr: "127.0.0.1:0", PreferIPv4: true})
	if err != nil {
		t.Fatal(err)
	}
	lv.addr = l.LocalAddr()
	lv.node, lv.srv = liveNodeAt(t, lv.dbPath, func() *net.UDPAddr { return lv.addr })
	mgr, err := exchange.NewManager(l, exchange.Config{Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, stop := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = l.Serve(serveCtx, mgr.HandleDatagram)
	}()
	conn := &fakeMDNS{out: make(chan mdns.QueryResponse, 64)}
	names := discovery.NewNames(conn, discardLog())
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := commissioner.NewFabric(authority, commissioner.FabricConfig{FabricID: liveFabricID, RootNodeID: liveController, VendorID: 0xFFF1, Label: "live"})
	if err != nil {
		t.Fatal(err)
	}
	resumption := &peer.MemoryResumptionStore{}
	set, err := peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: fabric.Lookup, Resumption: resumption, Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	lv.reg = interaction.NewSubscriptions(interaction.SubscriptionsConfig{Logger: discardLog()})
	com, err := commissioner.New(commissioner.Context{
		Exchanges: mgr, Peers: set, Attestation: attestation.TestStore(), Logger: discardLog(),
		Subscriptions: lv.reg, Fabrics: []*commissioner.Fabric{fabric}, Resumption: resumption,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = lv.reg.Close(context.Background())
		com.Close()
		set.Close(context.Background())
		mgr.Close()
		stop()
		<-served
		_ = names.Close()
	})
	nodeAddr, err := net.ResolveUDPAddr("udp", lv.node.Bridge().LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	op := discovery.OperationalQName(fabric.Identity.CompressedFabricID, liveNode)
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := com.Commission(ctx, commissioner.Options{
		Fabric: fabric, Passcode: testPasscode, NodeID: liveNode, Addresses: []*net.UDPAddr{nodeAddr}, OnAttestationFailure: attestation.Accept(),
	})
	if err != nil {
		t.Fatalf("Commission: %v", err)
	}
	lv.peer = set.Get(res.Address)
	lv.c = com.Interaction(lv.peer)
	for _, ep := range lv.node.Bridge().Topology().Bridged() {
		if ep.SourceKey != nil && ep.SourceKey.String() == "plug:1" {
			lv.plugEP = ep.ID
		}
	}
	if lv.plugEP == 0 {
		t.Fatal("the plug has no endpoint")
	}
	return lv
}

// liveHooks records what a live client node reports.
type liveHooks struct {
	mu        sync.Mutex
	installed []uint16
	deleted   []string
	changed   chan struct{}
	state     chan client.ConnectionState
	events    int
}

func newLiveHooks() *liveHooks {
	return &liveHooks{changed: make(chan struct{}, 1024), state: make(chan client.ConnectionState, 64)}
}

func (h *liveHooks) hooks() client.Hooks {
	return client.Hooks{
		EndpointInstalled: func(ep *client.Endpoint) {
			h.mu.Lock()
			h.installed = append(h.installed, ep.Number())
			h.mu.Unlock()
		},
		ClusterDeleted: func(ep *client.Endpoint, c *client.Cluster) {
			h.mu.Lock()
			h.deleted = append(h.deleted, c.Name())
			h.mu.Unlock()
		},
		Changed: func() {
			select {
			case h.changed <- struct{}{}:
			default:
			}
		},
		ConnectionStateChanged: func(s client.ConnectionState) { h.state <- s },
		Event: func(uint16, uint32, uint32, interaction.EventData, any) {
			h.mu.Lock()
			h.events++
			h.mu.Unlock()
		},
	}
}

func (h *liveHooks) waitState(t *testing.T, want client.ConnectionState) {
	t.Helper()
	deadline := time.After(60 * time.Second)
	for {
		select {
		case s := <-h.state:
			if s == want {
				return
			}
		case <-deadline:
			t.Fatalf("no transition to %v", want)
		}
	}
}

// waitFor polls cond on every Changed, at most 60 s.
func (h *liveHooks) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(60 * time.Second)
	for !cond() {
		select {
		case <-h.changed:
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// tree describes the image: number, device type, owner and cluster count
// per endpoint.
func tree(n *client.Node) []string {
	eps := n.Endpoints()
	out := make([]string, 0, len(eps))
	for _, ep := range eps {
		owner := "-"
		if o := ep.Owner(); o != nil {
			owner = strconv.Itoa(int(o.Number()))
		}
		out = append(out, fmt.Sprintf("ep%d type %#x owner %s clusters %d", ep.Number(), ep.DeviceType(), owner, len(ep.Clusters())))
	}
	return out
}

// TestLiveClientNode: the client node over a commissioned node.Node — the
// initial read of a newly commissioned node and the default subscription
// (NetworkClient.ts:113-196 at 0ceb202d) install the tree the node's
// Descriptors describe (ClientStructure.ts:851-1009); a toggle on the node
// reaches the image through the subscription; the node restarted on its
// database delivers again; a lost subscription is re-established with the
// refreshed versions and event minimum (ClientNodeInteraction.ts:148-155);
// the plug removed from the node is erased from the image (:971-1008); a
// second client node on the store loads the tree without a read
// (:183-232).
func TestLiveClientNode(t *testing.T) {
	t.Parallel()
	lv := newLive(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	st := &client.MemoryStore{}
	h := newLiveHooks()
	n, err := client.New(client.Config{
		ID: "peer1", Peer: lv.peer, Interaction: lv.c, Store: st, AutoSubscribe: true, NewlyCommissioned: true,
		Hooks: h.hooks(), Logger: discardLog(), Rand: func() float64 { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []interaction.SubscribeRequest
	client.ObserveRequests(n, func(r interaction.SubscribeRequest) {
		mu.Lock()
		requests = append(requests, r)
		mu.Unlock()
	})
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	h.waitState(t, client.Connected)
	if !n.Seeded() || !n.SubscriptionActive() {
		t.Fatal("not seeded or not subscribed")
	}

	// The tree: the root keeps the RootNode type with an unknown revision
	// (ClientStructure.ts:876-892); the aggregator under it; the plug under
	// the aggregator.
	root := n.Endpoint(0)
	if root == nil || root.DeviceType() != 0x0016 || root.Name() != "RootNode" {
		t.Fatalf("root %+v", tree(n))
	}
	if _, ok := root.DeviceRevision(); ok {
		t.Fatal("the root took a revision from its DeviceTypeList")
	}
	plugEP := n.Endpoint(lv.plugEP)
	if plugEP == nil || plugEP.DeviceType() != uint32(onoff.DeviceTypeOnOffPlugInUnit) || plugEP.Name() != "OnOffPlugInUnit" {
		t.Fatalf("plug endpoint %v", tree(n))
	}
	agg := plugEP.Owner()
	if agg == nil || agg.DeviceType() != 0x000E || agg.Name() != "Aggregator" || agg.Owner() != root {
		t.Fatalf("aggregator %v", tree(n))
	}
	c := plugEP.Cluster(onoff.ClusterID)
	if c == nil || c.Definition() == nil || !c.Supports(onoff.AttrOnOff) || !c.SupportsCommand(onoff.CmdToggle) {
		t.Fatal("the plug's OnOff cluster")
	}
	if v, err := c.Decoded(onoff.AttrOnOff); err != nil || v != false {
		t.Fatalf("OnOff %v %v", v, err)
	}
	// The client view: the generated device type, and the image judged
	// by the server-side validator.
	if d := plugEP.Device(); d == nil || d.Name != "OnOffPlugInUnit" {
		t.Fatalf("the plug's Device() %v", d)
	}
	if r := plugEP.Conformance(); r.Status != client.ConformanceClean {
		t.Fatalf("the plug's conformance %v: %v, unjudged %v", r.Status, r.Violations, r.Unjudged)
	}
	rev, revOK := plugEP.DeviceRevision()
	t.Logf("tree after the initial read: %v (plug revision %d %v)", tree(n), rev, revOK)
	var named []string
	for _, ep := range n.Endpoints() {
		for _, cl := range ep.Clusters() {
			if cl.Definition() == nil && !slices.Contains(named, cl.Name()) {
				named = append(named, cl.Name())
			}
		}
	}
	sort.Strings(named)
	t.Logf("named clusters met (no generated definition): %v", named)
	h.mu.Lock()
	t.Logf("events delivered: %d", h.events)
	h.mu.Unlock()
	p := n.Properties()
	floor, ceiling := n.IntervalBounds(nil, nil)
	t.Logf("properties: %+v; bounds %v %v", p, floor, ceiling)

	// The first subscription request carried the versions of the initial
	// read and the event minimum.
	mu.Lock()
	first := requests[0]
	mu.Unlock()
	if len(first.DataVersionFilters) == 0 || len(first.EventFilters) != 1 || first.EventFilters[0].EventMin == 0 {
		t.Fatalf("the default subscription's request: %d filters, events %+v", len(first.DataVersionFilters), first.EventFilters)
	}

	// A toggle on the node reaches the image.
	lv.srv.Set(true)
	h.waitFor(t, "OnOff true", func() bool { v, _ := c.Decoded(onoff.AttrOnOff); return v == true })

	// The node restarts on its database: the subscription delivers again.
	if err := lv.node.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	lv.node, lv.srv = liveNodeAt(t, lv.dbPath, func() *net.UDPAddr { return lv.addr })
	h.waitFor(t, "the subscription after the restart", n.SubscriptionActive)
	lv.srv.Set(false)
	h.waitFor(t, "OnOff false after the restart", func() bool {
		cl := n.Endpoint(lv.plugEP).Cluster(onoff.ClusterID)
		v, _ := cl.Decoded(onoff.AttrOnOff)
		return v == false
	})
	mu.Lock()
	afterRestart := len(requests)
	mu.Unlock()
	t.Logf("subscription requests after the node's restart: %d", afterRestart)

	// The underlying subscription is lost: the replacement carries the
	// refreshed versions and the event minimum.
	lv.reg.CloseForPeer(lv.reg.Active()[0].Peer())
	deadline := time.Now().Add(60 * time.Second)
	for {
		mu.Lock()
		got := len(requests)
		mu.Unlock()
		if got > afterRestart {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no re-subscription")
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	refreshed := requests[len(requests)-1]
	mu.Unlock()
	if len(refreshed.DataVersionFilters) == 0 || len(refreshed.EventFilters) != 1 || refreshed.EventFilters[0].EventMin == 0 {
		t.Fatalf("the refreshed request: %d filters, events %+v", len(refreshed.DataVersionFilters), refreshed.EventFilters)
	}
	h.waitFor(t, "the re-established subscription", n.SubscriptionActive)

	// The plug leaves the node: erased from the image.
	h.mu.Lock()
	h.installed, h.deleted = nil, nil
	h.mu.Unlock()
	if err := lv.node.Aggregator().Remove(ctx, endpoint.StringKey("plug:1")); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the plug's erasure", func() bool { return n.Endpoint(lv.plugEP) == nil })
	h.mu.Lock()
	installed, deleted := h.installed, h.deleted
	h.mu.Unlock()
	if len(installed) != 0 {
		t.Fatalf("endpoints installed on the removal: %v", installed)
	}
	t.Logf("cluster deletions on the removal: %v; tree %v", deleted, tree(n))
	want := tree(n)

	// A second node on the store loads the tree from the cache.
	if err := n.Close(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := client.New(client.Config{ID: "peer1", Peer: lv.peer, Interaction: lv.c, Store: st, Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	if got := tree(second); !slices.Equal(got, want) {
		t.Fatalf("reloaded tree %v, want %v", got, want)
	}
	if second.SubscriptionActive() || second.ConnectionState() != client.Disconnected {
		t.Fatal("the reloaded node is online")
	}
}

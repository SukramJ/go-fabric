// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/mdns"
)

// The registry harness: a controller with an exchange manager on a socket
// that sends nowhere, a DNS-SD cache that hears nothing, one fabric, a
// peer set and a commissioner — enough to restore, bind and delete nodes
// offline.

type silentConn struct{}

func (silentConn) Send(*net.UDPAddr, []byte) error { return nil }

type silentMDNS struct{ out chan mdns.QueryResponse }

func (silentMDNS) Send([]byte) error                      { return nil }
func (m silentMDNS) Responses() <-chan mdns.QueryResponse { return m.out }
func (silentMDNS) Close() error                           { return nil }

type regHarness struct {
	mgr    *exchange.Manager
	set    *peer.Set
	com    *commissioner.Commissioner
	fabric *commissioner.Fabric
	subs   *interaction.Subscriptions
	clock  *manualClock
	store  *MemoryStore
	logs   *logRecorder
}

const regFabricID uint64 = 0x0F0D

func newRegHarness(t *testing.T) *regHarness {
	t.Helper()
	h := &regHarness{clock: &manualClock{now: time.UnixMilli(1_900_000_000_000)}, store: &MemoryStore{}, logs: &logRecorder{}}
	mgr, err := exchange.NewManager(silentConn{}, exchange.Config{Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	h.mgr = mgr
	names := discovery.NewNames(silentMDNS{out: make(chan mdns.QueryResponse)}, slog.New(slog.DiscardHandler))
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	h.fabric, err = commissioner.NewFabric(authority, commissioner.FabricConfig{FabricID: regFabricID, RootNodeID: 112233, VendorID: 0xFFF1, Label: "ours"})
	if err != nil {
		t.Fatal(err)
	}
	h.set, err = peer.NewSet(peer.Context{Exchanges: mgr, Names: names, Fabrics: h.fabric.Lookup, Resumption: &peer.MemoryResumptionStore{}, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	h.subs = interaction.NewSubscriptions(interaction.SubscriptionsConfig{})
	h.com, err = commissioner.New(commissioner.Context{Exchanges: mgr, Peers: h.set, Subscriptions: h.subs, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = h.subs.Close(context.Background())
		h.com.Close()
		h.set.Close(context.Background())
		mgr.Close()
		_ = names.Close()
	})
	return h
}

func (h *regHarness) config() RegistryConfig {
	return RegistryConfig{
		Store: h.store, Commissioner: h.com, Peers: h.set, Exchanges: h.mgr, Fabrics: []*commissioner.Fabric{h.fabric},
		Subscriptions: h.subs, Logger: slog.New(h.logs), Clock: h.clock, Node: Config{Clock: h.clock},
	}
}

func (h *regHarness) registry(t *testing.T, mutate func(*RegistryConfig)) *Registry {
	t.Helper()
	cfg := h.config()
	if mutate != nil {
		mutate(&cfg)
	}
	r, err := NewRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

// seed stores a node's record and settings.
func (h *regHarness) seed(t *testing.T, id string, rec Record, s settings) {
	t.Helper()
	values, err := rec.encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.Save(id, 0, recordCluster, values, nil); err != nil {
		t.Fatal(err)
	}
	sv, err := s.encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.Save(id, 0, settingsCluster, sv, nil); err != nil {
		t.Fatal(err)
	}
}

func commissionedRecord(nodeID uint64) Record {
	return Record{
		PeerAddress:           &handshake.PeerAddress{FabricIndex: 1, NodeID: nodeID},
		CaseAuthenticatedTags: []uint32{0x0002_0001},
		Addresses:             []NetworkAddress{{IP: netip.MustParseAddr("fd00::10"), Port: 5540}},
		DiscoveredAt:          time.UnixMilli(1_800_000_000_000),
		CommissionedAt:        time.UnixMilli(1_800_000_001_000),
		FabricIndexOnPeer:     2,
	}
}

func ids(nodes []*Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.ID())
	}
	return out
}

// TestRegistryConfig: the dependencies are required.
func TestRegistryConfig(t *testing.T) {
	t.Parallel()
	h := newRegHarness(t)
	for name, mutate := range map[string]func(*RegistryConfig){
		"commissioner": func(c *RegistryConfig) { c.Commissioner = nil },
		"peers":        func(c *RegistryConfig) { c.Peers = nil },
		"exchanges":    func(c *RegistryConfig) { c.Exchanges = nil },
		"fabrics":      func(c *RegistryConfig) { c.Fabrics = nil },
		"nil fabric":   func(c *RegistryConfig) { c.Fabrics = []*commissioner.Fabric{nil} },
	} {
		cfg := h.config()
		mutate(&cfg)
		if _, err := NewRegistry(cfg); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Defaults: a memory store, the system clock, a discarding logger.
	r, err := NewRegistry(RegistryConfig{Commissioner: h.com, Peers: h.set, Exchanges: h.mgr, Fabrics: []*commissioner.Fabric{h.fabric}})
	if err != nil || r.Size() != 0 {
		t.Fatalf("%v %d", err, r.Size())
	}
	_ = r.Close(t.Context())
	failing := h.config()
	failing.Store = &idsFailingStore{}
	if _, err := NewRegistry(failing); err == nil {
		t.Fatal("a store that cannot list its nodes")
	}
}

type idsFailingStore struct{ MemoryStore }

func (*idsFailingStore) NodeIDs() ([]string, error) { return nil, errors.New("down") }

// TestRegistryRestore: Peers.initialize — every stored node is restored in
// id order, a commissioned one bound to its protocol peer from its record
// (CommissioningClient.ts:139-142, :699-706); a node that cannot load is
// logged and skipped; the id counter passes every stored peerN.
func TestRegistryRestore(t *testing.T) {
	t.Parallel()
	h := newRegHarness(t)
	h.seed(t, "peer1", commissionedRecord(0x10), settings{})
	h.seed(t, "peer3", Record{DeviceIdentifier: "CAFE", DiscoveredAt: time.UnixMilli(1_800_000_000_000)}, settings{})
	h.seed(t, "peer12x", Record{}, settings{}) // the leading digits count: next id past 12
	h.seed(t, "kitchen", Record{}, settings{})
	foreign := commissionedRecord(0x11)
	foreign.PeerAddress.FabricIndex = 9
	h.seed(t, "foreign", foreign, settings{})
	_ = h.store.Save("broken", 0, recordCluster, map[uint32][]byte{recHostname: {0xFF}}, nil)

	r := h.registry(t, nil)
	if got := ids(r.Nodes()); !slices.Equal(got, []string{"kitchen", "peer1", "peer12x", "peer3"}) {
		t.Fatalf("restored %v", got)
	}
	if !h.logs.has("matter.controller.client.registry.load_failed") {
		t.Fatal("no load_failed")
	}
	addr := handshake.PeerAddress{FabricIndex: 1, NodeID: 0x10}
	p := h.set.Get(addr)
	n := r.Get("peer1")
	if p == nil || n.peer() != p || r.GetByAddress(addr) != n || r.GetByAddress(handshake.PeerAddress{FabricIndex: 1, NodeID: 1}) != nil {
		t.Fatal("peer1 is not bound to its peer")
	}
	d := p.Descriptor()
	if d.OperationalAddress.String() != "[fd00::10]:5540" || !slices.Equal(d.CaseAuthenticatedTags, []uint32{0x0002_0001}) {
		t.Fatalf("the peer's descriptor %+v", d)
	}
	if got := ids(r.Commissioned()); !slices.Equal(got, []string{"peer1"}) {
		t.Fatalf("commissioned %v", got)
	}
	if r.Get("peer3").peer() != nil || r.Get("nope") != nil || r.Size() != 4 {
		t.Fatal("peer3 is bound")
	}
	if _, err := r.Get("peer3").Read(t.Context(), ReadRequest{}); !errors.Is(err, ErrNotCommissioned) {
		t.Fatalf("an unbound node read: %v", err)
	}
	// ForAddress allocates past peer12.
	n13, err := r.ForAddress(handshake.PeerAddress{FabricIndex: 1, NodeID: 0x20}, "")
	if err != nil || n13.ID() != "peer13" || !n13.IsCommissioned() || n13.peer() == nil || n13.Record().DiscoveredAt.IsZero() {
		t.Fatalf("ForAddress %v %v", n13, err)
	}
	if again, err := r.ForAddress(handshake.PeerAddress{FabricIndex: 1, NodeID: 0x20}, ""); err != nil || again != n13 {
		t.Fatal("ForAddress created a second node")
	}
	if _, err := r.ForAddress(handshake.PeerAddress{FabricIndex: 4, NodeID: 0x20}, ""); !errors.Is(err, ErrForeignFabric) {
		t.Fatalf("a foreign fabric: %v", err)
	}
	if _, err := r.ForAddress(handshake.PeerAddress{FabricIndex: 1, NodeID: 0x21}, "kitchen"); !errors.Is(err, ErrConfig) {
		t.Fatalf("a taken id: %v", err)
	}
	if own, err := r.ForAddress(handshake.PeerAddress{FabricIndex: 1, NodeID: 0x22}, "hall"); err != nil || own.ID() != "hall" {
		t.Fatalf("an own id: %v", err)
	}
	// A restored record keeps its discovery time; a new node takes now.
	if !r.Get("peer3").Record().DiscoveredAt.Equal(time.UnixMilli(1_800_000_000_000)) || !r.Get("kitchen").Record().DiscoveredAt.Equal(h.clock.Now()) {
		t.Fatal("discovery times")
	}
}

// TestRegistryAutoStart: Peers.#nodeOnline starts the commissioned,
// enabled nodes only; #nodeOffline stops them.
func TestRegistryAutoStart(t *testing.T) {
	t.Parallel()
	h := newRegHarness(t)
	h.seed(t, "peer1", commissionedRecord(0x10), settings{})
	h.seed(t, "peer2", commissionedRecord(0x11), settings{disabled: true})
	h.seed(t, "peer3", Record{}, settings{})
	r := h.registry(t, func(c *RegistryConfig) { c.AutoStart = true })
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !r.Get("peer1").isStarted() || r.Get("peer2").isStarted() || r.Get("peer3").isStarted() {
		t.Fatal("the wrong nodes started")
	}
	if err := r.Stop(t.Context()); err != nil || r.Get("peer1").isStarted() {
		t.Fatalf("Stop: %v", err)
	}
	// Without AutoStart nothing starts.
	manual := newRegHarness(t)
	manual.seed(t, "peer1", commissionedRecord(0x10), settings{})
	mr := manual.registry(t, nil)
	_ = mr.Start(t.Context())
	if mr.Get("peer1").isStarted() {
		t.Fatal("started without AutoStart")
	}
	// A node that fails to start is logged; the others start.
	failing := newRegHarness(t)
	failing.seed(t, "peer1", commissionedRecord(0x10), settings{})
	failing.seed(t, "peer2", commissionedRecord(0x11), settings{})
	fr := failing.registry(t, func(c *RegistryConfig) { c.AutoStart = true })
	_ = fr.Get("peer1").Close(t.Context())
	_ = fr.Start(t.Context())
	if !failing.logs.has("matter.controller.client.registry.start_failed") || !fr.Get("peer2").isStarted() {
		t.Fatal("a failing start stopped the loop")
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatal("Close is not idempotent")
	}
	if err := r.Start(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Close: %v", err)
	}
	if _, err := r.ForAddress(handshake.PeerAddress{FabricIndex: 1, NodeID: 0x30}, ""); !errors.Is(err, ErrClosed) {
		t.Fatalf("ForAddress after Close: %v", err)
	}
	// The store keeps the nodes across Close.
	if ids, _ := h.store.NodeIDs(); len(ids) != 3 {
		t.Fatalf("stored %v", ids)
	}
}

// TestRegistryErase: Peers.erase deletes every node — store, registry and
// protocol peer; a node that cannot be deleted is closed and dropped.
func TestRegistryErase(t *testing.T) {
	t.Parallel()
	h := newRegHarness(t)
	h.seed(t, "peer1", commissionedRecord(0x10), settings{})
	h.seed(t, "peer2", Record{}, settings{})
	r := h.registry(t, nil)
	r.Erase(t.Context())
	if r.Size() != 0 || h.set.Get(handshake.PeerAddress{FabricIndex: 1, NodeID: 0x10}) != nil {
		t.Fatal("Erase left nodes")
	}
	if ids, _ := h.store.NodeIDs(); len(ids) != 0 {
		t.Fatalf("stored %v", ids)
	}
}

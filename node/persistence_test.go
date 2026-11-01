// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

// The exported endpoint-level persistence (issue #117): a host with its
// own assembler persists bridged NodeLabels and scene tables through it,
// across a restart, and the override replaces the facade's own.

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

const (
	clusterBridgedBasicInformation uint32 = 0x0039
	attrBridgedNodeLabel           uint32 = 0x0005
)

func putLabel(s string) func(e *tlv.Encoder, tag tlv.Tag) {
	return func(e *tlv.Encoder, tag tlv.Tag) { e.PutUTF8(tag, s) }
}

// hostNode is a node with Config.Topology over a host assembler built on
// node.NewEndpointPersistence before node.New, its one plug's Spec
// NodeLabel restored from the persistence as the facade's Aggregator does.
func hostNode(t *testing.T, db *sql.DB) (*node.Node, node.EndpointPersistence) {
	t.Helper()
	ctx := context.Background()
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := sqlitestore.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.DiscardHandler)
	ep := node.NewEndpointPersistence(store.New(db), logger)
	cfg := minimalConfig(db)
	asm, err := endpoint.New(sqlitestore.New(db), endpoint.Config{
		VendorID: cfg.ProductDescription.VendorID, ProductID: cfg.ProductDescription.ProductID,
		OnNodeLabelWritten: ep.OnNodeLabelWritten,
		Scenes:             ep,
	}, logger)
	if err != nil {
		t.Fatal(err)
	}
	p := newPlug(t, "plug:1")
	cfg.Topology = func(ctx context.Context) (*endpoint.Topology, error) {
		spec := p.Spec()
		if label := ep.NodeLabel(ctx, spec.StableKey); label != "" {
			spec.NodeLabel = label
		}
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "host", Endpoints: []endpoint.Spec{spec}}})
	}
	return newNode(t, cfg), ep
}

// TestEndpointPersistenceForHostTopology: a NodeLabel a controller writes
// on a host-assembled endpoint and a scene table saved through the
// exported persistence are read back by a new node over the same store,
// through the host's persistence and through Node.Persistence alike.
func TestEndpointPersistenceForHostTopology(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	key := endpoint.StringKey("plug:1")

	n1, ep1 := hostNode(t, openDB(t, path))
	s := paseSession(t, n1)
	id, ok := endpointOf(t, n1, "plug:1")
	if !ok {
		t.Fatal("host endpoint missing")
	}
	if st, err := s.Write(ctx, attr(id, clusterBridgedBasicInformation, attrBridgedNodeLabel), putLabel("Kitchen")); err != nil || st != im.StatusSuccess {
		t.Fatalf("bridged NodeLabel write = %s, %v", st, err)
	}
	ep1.SaveScenes(key, []byte("scene-table"))
	if err := n1.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	n2, ep2 := hostNode(t, openDB(t, path))
	if err := n2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	id2, _ := endpointOf(t, n2, "plug:1")
	if got := n2.Bridge().Topology().FindByID(id2).NodeLabel(); got != "Kitchen" {
		t.Errorf("NodeLabel after the restart %q, want Kitchen", got)
	}
	for name, p := range map[string]node.EndpointPersistence{"host": ep2, "node": n2.Persistence()} {
		if got := p.NodeLabel(ctx, key); got != "Kitchen" {
			t.Errorf("%s NodeLabel %q", name, got)
		}
		if got := string(p.LoadScenes(key)); got != "scene-table" {
			t.Errorf("%s scene table %q", name, got)
		}
	}
	// The keys stay the facade's (persist.go).
	for k, want := range map[string]string{"bridged_node_label.plug:1": "Kitchen", "scenes.plug:1": "scene-table"} {
		if v, ok, err := n2.Store().GetSetting(ctx, k); err != nil || !ok || v != want {
			t.Errorf("setting %s = %q (%v, %v), want %q", k, v, ok, err, want)
		}
	}
}

// fakePersistence records what the facade hands it.
type fakePersistence struct {
	mu     sync.Mutex
	labels map[string]string
	scenes map[string][]byte
}

func (f *fakePersistence) LoadScenes(key endpoint.SourceKey) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scenes[key.String()]
}

func (f *fakePersistence) SaveScenes(key endpoint.SourceKey, table []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scenes[key.String()] = table
}

func (f *fakePersistence) OnNodeLabelWritten(key endpoint.SourceKey, label string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labels[key.String()] = label
}

func (f *fakePersistence) NodeLabel(_ context.Context, key endpoint.SourceKey) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.labels[key.String()]
}

// TestEndpointPersistenceOverride: Config.EndpointPersistence is what the
// facade's own assembler and Aggregator use — the label it holds is
// restored, a controller's write reaches it, the store stays untouched —
// and what Node.Persistence returns.
func TestEndpointPersistenceOverride(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake := &fakePersistence{labels: map[string]string{"plug:1": "Hall"}, scenes: map[string][]byte{}}
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.EndpointPersistence = fake
	n := newNode(t, cfg)
	if n.Persistence() != node.EndpointPersistence(fake) {
		t.Error("Persistence is not the override")
	}
	if err := n.Aggregator().Add(ctx, newPlug(t, "plug:1")); err != nil {
		t.Fatal(err)
	}
	s := paseSession(t, n)
	id, _ := endpointOf(t, n, "plug:1")
	if got := n.Bridge().Topology().FindByID(id).NodeLabel(); got != "Hall" {
		t.Errorf("restored NodeLabel %q, want the override's Hall", got)
	}
	if st, err := s.Write(ctx, attr(id, clusterBridgedBasicInformation, attrBridgedNodeLabel), putLabel("Office")); err != nil || st != im.StatusSuccess {
		t.Fatalf("bridged NodeLabel write = %s, %v", st, err)
	}
	if got := fake.NodeLabel(ctx, endpoint.StringKey("plug:1")); got != "Office" {
		t.Errorf("override saw label %q, want Office", got)
	}
	if _, ok, _ := n.Store().GetSetting(ctx, "bridged_node_label.plug:1"); ok {
		t.Error("the facade's store was written beside the override")
	}
}

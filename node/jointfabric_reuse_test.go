// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/cluster/jointfabric"
	jfdsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// nullSync is a DatastoreSync the test never calls.
type nullSync struct{ jfdsdef.DatastoreSync }

// TestReusedFabricIndexDoesNotInheritTheAnchor (Fable RECORD-13): a
// datastore that still records a removed fabric's index as its anchor —
// here re-recorded after the removal, as a record an absent endpoint kept
// would be — does not hand the Joint Fabric's registry to the next fabric
// of that index: the node drops every FabricScopedState server's state
// for the index (the datastore's included) before it announces the new
// fabric (R2-P5-port-13's path).
func TestReusedFabricIndexDoesNotInheritTheAnchor(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "node.db")
	db := openDB(t, path)
	n := newNode(t, minimalConfig(db))
	dev, err := device.NewJointFabricAdministrator(device.Options{StableKey: device.StringKey("jf"), Name: "JF"},
		jfdsdef.Declare{Sync: nullSync{}})
	if err != nil {
		t.Fatal(err)
	}
	ds, ok := dev.JointFabricDatastore().Underlying().(*jointfabric.Datastore)
	if !ok {
		t.Fatalf("the datastore server is %T", dev.JointFabricDatastore().Underlying())
	}
	if err := n.Aggregator().Add(ctx, dev); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	if _, err := c.Commission(ctx); err != nil {
		t.Fatalf("Commission: %v", err)
	}
	fi := c.FabricIndex()
	if err := n.RemoveFabric(ctx, fi); err != nil {
		t.Fatalf("RemoveFabric: %v", err)
	}
	// The stale record of the removed fabric.
	if err := ds.SetAnchorFabricIndex(ctx, fi); err != nil {
		t.Fatal(err)
	}
	if err := ds.AddPendingNode(ctx, 9, "of the removed fabric"); err != nil {
		t.Fatal(err)
	}
	if err := store.New(db).SetMetadataCounter(ctx, "next_fabric_index", uint64(fi)); err != nil {
		t.Fatal(err)
	}
	var (
		mu      sync.Mutex
		anchors []uint8
		nodes   []int
	)
	n.Observe(node.Observers{OnFabricAdded: func(node.FabricInfo) {
		mu.Lock()
		anchors = append(anchors, ds.AnchorFabricIndex())
		nodes = append(nodes, len(ds.NodeList()))
		mu.Unlock()
	}})
	waitLive(t, "the window to reopen", func() bool { return n.Window().Open })
	opts := commissionerOptions()
	opts.FabricID, opts.Seed = testFabricID+1, 9
	again := bridgetest.Dial(t, n.Bridge().LocalAddr(), opts)
	if _, err := again.Commission(ctx); err != nil {
		t.Fatalf("Commission again: %v", err)
	}
	if again.FabricIndex() != fi {
		t.Fatalf("the second fabric took index %d, want the reused %d", again.FabricIndex(), fi)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(anchors) != 1 || anchors[0] != 0 || nodes[0] != 0 {
		t.Fatalf("at the reused index's announcement: anchor %v, nodes %v; want the removed fabric's registry gone", anchors, nodes)
	}
}

// TestNodeAttachesTheKitDatastore (Fable RECORD-19): the node hands the
// kit's JointFabricDatastore its fabric table (Datastore.AttachNode): once
// the anchor is recorded, the anchor fabric reads the tables and another
// fabric reads them withheld. Without the attach every peer is refused.
func TestNodeAttachesTheKitDatastore(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	n := newNode(t, minimalConfig(db))
	dev, err := device.NewJointFabricAdministrator(device.Options{StableKey: device.StringKey("jf"), Name: "JF"},
		jfdsdef.Declare{Sync: nullSync{}})
	if err != nil {
		t.Fatal(err)
	}
	ds, ok := dev.JointFabricDatastore().Underlying().(*jointfabric.Datastore)
	if !ok {
		t.Fatalf("the datastore server is %T", dev.JointFabricDatastore().Underlying())
	}
	if err := n.Aggregator().Add(ctx, dev); err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	if _, err := c.Commission(ctx); err != nil {
		t.Fatalf("Commission: %v", err)
	}
	fi := c.FabricIndex()
	if err := ds.SetAnchorFabricIndex(ctx, fi); err != nil {
		t.Fatal(err)
	}
	if err := ds.SetFriendlyName(ctx, "jf"); err != nil {
		t.Fatal(err)
	}
	read := func(fabric uint8) any {
		v, _ := ds.MatterReadFiltered(im.WithSubject(im.WithFabricFilter(ctx, true, fabric), 0x77, nil), jfdsdef.AttrFriendlyName)
		return v
	}
	if v := read(fi); v != "jf" {
		t.Fatalf("the anchor fabric reads FriendlyName %q; the node did not attach its fabric table", v)
	}
	if v := read(fi + 1); v != "" {
		t.Fatalf("another fabric reads FriendlyName %q", v)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
)

// TestOwedFabricRemovalReachesObserveAsync (review II, R2-P6-err-2402):
// the removal a start owes for an uncommitted AddNOC reaches an
// ObserveAsync hook registered before Start, as it reaches an Observe
// hook — the delivery acknowledges the event in the store, so a hook that
// misses it never hears it (Observers' at-least-once promise).
func TestOwedFabricRemovalReachesObserveAsync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	seeded := seedPendingAdd(t, path)
	n := newNode(t, minimalConfig(openDB(t, path)))
	var mu sync.Mutex
	var syncHeard, asyncHeard []uint8
	n.Observe(node.Observers{OnFabricRemoved: func(info node.FabricInfo) {
		mu.Lock()
		syncHeard = append(syncHeard, info.FabricIndex)
		mu.Unlock()
	}})
	n.ObserveAsync(node.Observers{OnFabricRemoved: func(info node.FabricInfo) {
		mu.Lock()
		asyncHeard = append(asyncHeard, info.FabricIndex)
		mu.Unlock()
	}})
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := n.Stop(context.Background()); err != nil { // drains the callback queue
		t.Fatalf("Stop: %v", err)
	}
	owed, err := store.New(openDB(t, path)).OwedFabricEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []uint8{seeded.FabricIndex}
	if !slices.Equal(syncHeard, want) || !slices.Equal(asyncHeard, want) {
		t.Errorf("OnFabricRemoved: Observe heard %v, ObserveAsync heard %v; want %v both (owed after Start: %d)", syncHeard, asyncHeard, want, len(owed))
	}
}

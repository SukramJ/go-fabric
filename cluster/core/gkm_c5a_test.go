// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/groups"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// holdTableStoreA1 is a memstore seen by GroupKeyManagement whose next
// ListGroupTable, once armed, reads its answer and then holds the caller
// until release closes.
type holdTableStoreA1 struct {
	*memstore.Store
	hold             atomic.Bool
	entered, release chan struct{}
}

func (s *holdTableStoreA1) ListGroupTable(ctx context.Context, f uint8) ([]store.GroupTableEntry, error) {
	rows, err := s.Store.ListGroupTable(ctx, f)
	if s.hold.CompareAndSwap(true, false) {
		close(s.entered)
		<-s.release
	}
	return rows, err
}

// TestGroupTableMarkFollowsTheLastChangeA1 (R5-conc-5002): two group table
// changes of one fabric at once — a removal that empties the table, held
// by the store between its read and its mark, and an addition after it —
// leave the fabric marked as holding GroupTable entries, which the store
// holds: the removal's stale read does not clear the addition's mark, so
// the fabric's later removal is reported. Not parallel:
// WaitParkedOnMutexA1 reads every goroutine's stack.
func TestGroupTableMarkFollowsTheLastChangeA1(t *testing.T) {
	ctx := context.Background()
	const f uint8 = 3
	st := memstore.New()
	if _, err := st.AddFabric(ctx, store.FabricRecord{FabricIndex: f, FabricID: 0x0FA3, CompressedID: [8]byte{f, 2, 3, 4, 5, 6, 7, 8}}); err != nil {
		t.Fatal(err)
	}
	mgr, err := groups.NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	hs := &holdTableStoreA1{Store: st, entered: make(chan struct{}), release: make(chan struct{})}
	g, err := core.NewGroupKeyManagement(hs, core.GroupKeyMgmtConfig{Groups: mgr, MaxGroupsPerFabric: 4, MaxGroupKeysPerFabric: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.AddEndpointForGroup(ctx, f, 0x202, 1, "g"); err != nil {
		t.Fatal(err)
	}

	hs.hold.Store(true)
	removed := make(chan struct{})
	go func() {
		defer close(removed)
		if _, err := mgr.RemoveEndpoint(ctx, f, 1, 0x202, false); err != nil {
			t.Error(err)
		}
	}()
	<-hs.entered // the removal read an empty table
	added := make(chan struct{})
	go func() {
		defer close(added)
		if err := mgr.AddEndpointForGroup(ctx, f, 0x203, 1, "h"); err != nil {
			t.Error(err)
		}
	}()
	core.WaitParkedOnMutexA1(t, added, "markTableHeld")
	close(hs.release)
	<-removed
	<-added

	rows, err := st.ListGroupTable(ctx, f)
	if err != nil || len(rows) != 1 {
		t.Fatalf("the store holds %v, %v; want the added group", rows, err)
	}
	if held := g.MatterHeldFabrics(); !slices.Contains(held, f) {
		t.Errorf("MatterHeldFabrics = %v, want %d: the removal's stale read cleared the addition's mark", held, f)
	}
}

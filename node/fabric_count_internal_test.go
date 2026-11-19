// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// listCountingStore counts the fabric listings of the store under it.
type listCountingStore struct {
	store.Port
	lists atomic.Int32
}

func (s *listCountingStore) ListFabrics(ctx context.Context) ([]store.FabricRecord, error) {
	s.lists.Add(1)
	return s.Port.ListFabrics(ctx)
}

// TestFabricCountIsCachedUntilAFabricChanges (review II, R2-P7-req-05,
// owner decisions 275, 280): the counter the node shares with the bridge,
// the window and AdministratorCommissioning lists the store once for many
// calls, and again after a fabric is installed and after one is removed.
func TestFabricCountIsCachedUntilAFabricChanges(t *testing.T) {
	ctx := context.Background()
	st := &listCountingStore{Port: memstore.New()}
	cfg := mdnsTestConfig(t)
	cfg.Store.Credentials = st
	n := newMDNSTestNode(t, cfg)
	count := n.window.fabricCount

	want := func(when string, fabrics int, lists int32) {
		t.Helper()
		before := st.lists.Load()
		for range 100 {
			got, err := count(ctx)
			if err != nil || got != fabrics {
				t.Fatalf("%s: count = %d, %v; want %d", when, got, err, fabrics)
			}
		}
		if got := st.lists.Load() - before; got != lists {
			t.Fatalf("%s: 100 counts listed the store %d times, want %d", when, got, lists)
		}
	}
	want("first", 0, 1)
	want("kept", 0, 0)

	idx, err := st.AddFabric(ctx, store.FabricRecord{FabricID: 1, NodeID: 2, Pending: store.FabricCommitted})
	if err != nil {
		t.Fatal(err)
	}
	n.fabricInstalled(ctx, idx, 1, 2, nil) // the identity load fails; the count is dropped first
	want("after the install", 1, 1)

	if err := n.RemoveFabric(ctx, idx); err != nil {
		t.Fatal(err)
	}
	want("after the removal", 0, 1)
}

// TestFabricCountCacheKeepsNothingUncertain: a listing taken while a
// fail-safe is armed — an AddNOC's pending row may go without a hook — or
// one an invalidate overtook is not kept.
func TestFabricCountCacheKeepsNothingUncertain(t *testing.T) {
	t.Parallel()
	var lists atomic.Int32
	var armed atomic.Bool
	var c *fabricCountCache
	invalidateWhileListing := false
	c = &fabricCountCache{
		list: func(context.Context) (int, error) {
			lists.Add(1)
			if invalidateWhileListing {
				c.invalidate()
			}
			return 1, nil
		},
		armed: armed.Load,
	}
	ctx := context.Background()
	armed.Store(true)
	_, _ = c.count(ctx)
	_, _ = c.count(ctx)
	if got := lists.Load(); got != 2 {
		t.Fatalf("armed fail-safe: %d listings for 2 counts, want 2", got)
	}
	armed.Store(false)
	invalidateWhileListing = true
	_, _ = c.count(ctx)
	invalidateWhileListing = false
	_, _ = c.count(ctx)
	if got := lists.Load(); got != 4 {
		t.Fatalf("an invalidate during the listing: %d listings, want 4 (the overtaken one is not kept)", got)
	}
	_, _ = c.count(ctx)
	if got := lists.Load(); got != 4 {
		t.Fatalf("%d listings, want the count kept at 4", got)
	}
	var nilCache *fabricCountCache
	nilCache.invalidate() // nil-safe
}

// TestFabricCountCacheConcurrent runs counts and invalidates together for
// the race detector; every count is the store's.
func TestFabricCountCacheConcurrent(t *testing.T) {
	t.Parallel()
	c := &fabricCountCache{list: func(context.Context) (int, error) { return 3, nil }}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				if n, err := c.count(context.Background()); err != nil || n != 3 {
					t.Errorf("count = %d, %v", n, err)
					return
				}
			}
		})
		wg.Go(func() {
			for range 200 {
				c.invalidate()
			}
		})
	}
	wg.Wait()
}

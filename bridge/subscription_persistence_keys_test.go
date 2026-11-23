// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/store/memstore"
)

// loadCountingStore counts the loads of every row's payload.
type loadCountingStore struct {
	*memstore.Store
	loads atomic.Int32
}

func (s *loadCountingStore) LoadServerSubscriptions(ctx context.Context) ([][]byte, error) {
	s.loads.Add(1)
	return s.Store.LoadServerSubscriptions(ctx)
}

// TestSaveBoundsTheFabricWithoutLoadingRows: once a run began on its
// store, a save keeps its fabric's rows at the quota from the keys of the
// rows the bridge itself saved — no row's payload is loaded or decoded per
// save — and the rows kept are those the load-based bound keeps; the
// re-establishment's order check needs no load either while the order
// holds (review II, R2-C2-bound-0501).
func TestSaveBoundsTheFabricWithoutLoadingRows(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	m := subscription.NewManager(subscription.Config{MaxSubscriptionsPerFabric: 3}, nil, nil)
	b.AttachSubscriptionManager(m)
	st := &loadCountingStore{Store: memstore.New()}
	b.AttachSubscriptionStore(st)
	b.beginSubscriptionRun()
	st.loads.Store(0)

	save := func() uint32 {
		id := liveSubscription(t, b, m, 0xA1, true)
		b.persistSubscription(context.Background(), id)
		return id
	}
	old := []uint32{save(), save(), save()}
	// End them without terminating them: their rows stay, as a session
	// loss leaves them.
	for _, id := range old {
		if err := m.Release(id); err != nil {
			t.Fatal(err)
		}
	}
	b.restoreCreationOrder(context.Background(), nil)
	n1, n2 := save(), save()
	if n := st.loads.Load(); n != 0 {
		t.Errorf("five saves and the order check loaded every row %d times, want 0", n)
	}
	if got, want := persistedIDs(t, st.Store), []uint32{old[2], n1, n2}; !slices.Equal(got, want) {
		t.Fatalf("rows after the cap = %#x, want %#x", got, want)
	}
}

// failingSaveStore refuses saves while fail is set.
type failingSaveStore struct {
	loadCountingStore
	fail atomic.Bool
}

func (s *failingSaveStore) SaveServerSubscription(ctx context.Context, id uint32, fabric uint8, peer uint64, payload []byte) error {
	if s.fail.Load() {
		return errors.New("disk full")
	}
	return s.Store.SaveServerSubscription(ctx, id, fabric, peer, payload)
}

// TestFailedStoreWriteFallsBackToLoading: after a store write fails the
// bridge's keys may not say what the store holds, so the next bound loads
// the rows again (review II, R2-C2-bound-0501).
func TestFailedStoreWriteFallsBackToLoading(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	m := subscription.NewManager(subscription.Config{MaxSubscriptionsPerFabric: 3}, nil, nil)
	b.AttachSubscriptionManager(m)
	st := &failingSaveStore{loadCountingStore: loadCountingStore{Store: memstore.New()}}
	b.AttachSubscriptionStore(st)
	b.beginSubscriptionRun()
	st.fail.Store(true)
	b.persistSubscription(context.Background(), liveSubscription(t, b, m, 0xA1, true))
	st.fail.Store(false)
	st.loads.Store(0)
	b.persistSubscription(context.Background(), liveSubscription(t, b, m, 0xA1, true))
	if st.loads.Load() == 0 {
		t.Error("the save after a failed one bounded the fabric from keys that may be wrong")
	}
}

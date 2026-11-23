// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	otadef "thingfabric.dev/go-fabric/cluster/spec/otasoftwareupdaterequestor"
)

// firstSaveBlocks holds the first state save until released and counts
// the saves that reached it.
type firstSaveBlocks struct {
	*memOtaStore
	entered chan struct{}
	release chan struct{}
	saves   atomic.Int32
}

func (s *firstSaveBlocks) SaveOTARequestorState(ctx context.Context, b []byte) error {
	if s.saves.Add(1) == 1 {
		close(s.entered)
		<-s.release
	}
	return s.memOtaStore.SaveOTARequestorState(ctx, b)
}

// TestOTARequestorPersistsInSnapshotOrder (R2-C2-store-1203): two
// persists store in the order of their snapshots — a save of an older
// snapshot held up in the store does not land after a newer one, so the
// state stored last carries the update in progress the newer snapshot
// holds.
func TestOTARequestorPersistsInSnapshotOrder(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var st *firstSaveBlocks
		h := newOtaHarness(t, func(c *OTARequestorConfig) {
			st = &firstSaveBlocks{memOtaStore: c.Store.(*memOtaStore), entered: make(chan struct{}), release: make(chan struct{})} //nolint:forcetypeassert // test
			c.Store = st
		})
		ctx := context.Background()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); h.r.persist(ctx) }() // the older snapshot: nothing in progress
		<-st.entered

		loc := otadef.ProviderLocation{FabricIndex: 1, ProviderNodeId: testProvider}
		h.r.mu.Lock()
		h.r.inProgress = &updateInProgress{NewSoftwareVersion: 2, Location: &loc, UpdateToken: []byte("token-12345678")}
		h.r.mu.Unlock()
		wg.Add(1)
		go func() { defer wg.Done(); h.r.persist(ctx) }() // the newer snapshot

		// Unserialised, the newer save reaches the store and returns;
		// serialised, it waits for the older one. Either way it is
		// durably blocked or done here, and the older one is released.
		synctest.Wait()
		close(st.release)
		wg.Wait()

		b, ok, err := st.LoadOTARequestorState(ctx)
		if err != nil || !ok {
			t.Fatalf("stored state %v %v", ok, err)
		}
		var got persistedState
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if got.UpdateInProgressDetails == nil {
			t.Error("the state stored last is the older snapshot: no update in progress")
		}
	})
}

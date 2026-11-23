// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
)

// ackCountingStore counts the acknowledgements of owed fabric events.
type ackCountingStore struct {
	store.Port
	acks atomic.Int32
}

func (s *ackCountingStore) AckOwedFabricEvent(ctx context.Context, id uint64) error {
	s.acks.Add(1)
	return s.Port.AckOwedFabricEvent(ctx, id)
}

// TestOwedFabricRemovalAckedAfterTheQueuedHookRan (review II,
// R4-err-4751, decision 600): the removal a start owes is acknowledged
// only once an ObserveAsync hook ran, not when it was queued — while the
// hook has not run the event stays owed, so a process that ends then
// delivers it at its next start.
func TestOwedFabricRemovalAckedAfterTheQueuedHookRan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.db")
	seeded := seedPendingAdd(t, path)
	db := openDB(t, path)
	cfg := minimalConfig(db)
	counting := &ackCountingStore{Port: store.New(db)}
	cfg.Store.Credentials = counting
	n := newNode(t, cfg)
	entered, release := make(chan node.FabricInfo, 1), make(chan struct{})
	n.ObserveAsync(node.Observers{OnFabricRemoved: func(info node.FabricInfo) {
		entered <- info
		<-release
	}})
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if info := <-entered; info.FabricIndex != seeded.FabricIndex {
		t.Fatalf("OnFabricRemoved for fabric %d; want %d", info.FabricIndex, seeded.FabricIndex)
	}
	if got := counting.acks.Load(); got != 0 {
		close(release)
		t.Fatalf("%d acknowledgements while the queued hook has not returned; want none", got)
	}
	if owed, err := store.New(openDB(t, path)).OwedFabricEvents(context.Background()); err != nil || len(owed) != 1 {
		close(release)
		t.Fatalf("owed events while the hook runs: %+v, %v; want the removal still owed", owed, err)
	}
	close(release)
	if err := n.Stop(context.Background()); err != nil { // drains the callback queue
		t.Fatalf("Stop: %v", err)
	}
	if got := counting.acks.Load(); got != 1 {
		t.Fatalf("%d acknowledgements once the hook ran; want 1", got)
	}
	if owed, err := store.New(openDB(t, path)).OwedFabricEvents(context.Background()); err != nil || len(owed) != 0 {
		t.Fatalf("owed events after the hook ran: %+v, %v; want none", owed, err)
	}
}

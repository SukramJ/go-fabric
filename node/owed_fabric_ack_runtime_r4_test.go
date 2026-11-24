// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/internal/serialqueue"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
)

// startWithAnnouncedPendingAdd starts a node over a store holding one
// fabric the host was told of, then marks that fabric's row as an AddNOC
// no CommissioningComplete committed — the state a run-time fail-safe
// expiry rolls back (the node announces an AddNOC's fabric before the
// commit, observedFabricInstalled). register runs before Start.
func startWithAnnouncedPendingAdd(t *testing.T, register func(*node.Node)) (n *node.Node, counting *ackCountingStore, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "node.db")
	seeded := seedPendingAdd(t, path)
	db := openDB(t, path)
	if _, err := db.Exec(`UPDATE matter_fabrics SET pending = ? WHERE fabric_index = ?`, store.FabricCommitted, seeded.FabricIndex); err != nil {
		t.Fatal(err)
	}
	cfg := minimalConfig(db)
	counting = &ackCountingStore{Port: store.New(db)}
	cfg.Store.Credentials = counting
	n = newNode(t, cfg)
	register(n)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := db.Exec(`UPDATE matter_fabrics SET pending = ? WHERE fabric_index = ?`, store.FabricPendingAdd, seeded.FabricIndex); err != nil {
		t.Fatal(err)
	}
	return n, counting, path
}

// TestRunTimeRollbackAckedAfterTheQueuedHookRan (review II,
// R4-err-4751b, decisions 616, 622): the removal a run-time fail-safe
// expiry owes is acknowledged only once the node's ObserveAsync hook ran,
// not when the hook was queued — while it has not run the event stays
// owed, so a process that ends then delivers it at its next start.
func TestRunTimeRollbackAckedAfterTheQueuedHookRan(t *testing.T) {
	entered, release := make(chan node.FabricInfo, 1), make(chan struct{})
	n, counting, path := startWithAnnouncedPendingAdd(t, func(n *node.Node) {
		n.ObserveAsync(node.Observers{OnFabricRemoved: func(info node.FabricInfo) {
			entered <- info
			<-release
		}})
	})
	n.Root().OperationalCredentials.OnFailSafeExpiry(context.Background(), 0)
	<-entered
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

// TestRunTimeRollbackSurvivesAFullCallbackQueue (review II, R4-err-4751b,
// decisions 615, 622): the run-time rollback's ObserveAsync hooks and its
// acknowledgement are exempt from the callback queue's drop rule, as the
// owed events' at start — a queue that overflows while the first hook
// runs drops other callbacks, but the second hook runs and the event is
// acknowledged after it.
func TestRunTimeRollbackSurvivesAFullCallbackQueue(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var second atomic.Int32
	var counting *ackCountingStore
	n, counting, path := startWithAnnouncedPendingAdd(t, func(n *node.Node) {
		n.ObserveAsync(node.Observers{OnFabricRemoved: func(node.FabricInfo) {
			close(entered)
			<-release
		}})
		n.ObserveAsync(node.Observers{OnFabricRemoved: func(node.FabricInfo) {
			if got := counting.acks.Load(); got != 0 {
				t.Errorf("%d acknowledgements before the second hook ran; want none", got)
			}
			second.Add(1)
		}})
	})
	n.Root().OperationalCredentials.OnFailSafeExpiry(context.Background(), 0)
	<-entered
	node.FloodCallbacks(n, serialqueue.Bound+10)
	close(release)
	if err := n.Stop(context.Background()); err != nil { // drains the callback queue
		t.Fatalf("Stop: %v", err)
	}
	if got := node.CallbacksDropped(n); got == 0 {
		t.Fatal("the flood dropped nothing; the test does not fill the queue")
	}
	if got := second.Load(); got != 1 {
		t.Fatalf("the second hook ran %d times; want once, in this process", got)
	}
	if got := counting.acks.Load(); got != 1 {
		t.Fatalf("%d acknowledgements after the hooks ran; want 1", got)
	}
	if owed, err := store.New(openDB(t, path)).OwedFabricEvents(context.Background()); err != nil || len(owed) != 0 {
		t.Fatalf("owed events after the hooks ran: %+v, %v; want none", owed, err)
	}
}

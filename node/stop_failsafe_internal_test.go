// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
)

// TestStopWaitsForTheFailSafeRollback pins review II R2-P2-stop-06: a
// fail-safe whose expiry fired before Stop runs its rollback on the
// watcher's goroutine; Stop does not return over it — it waits for the
// rollback, bounded by its ctx, and says so when the ctx ends first —
// so no rollback writes into a store Stop closed or a host already
// erased.
func TestStopWaitsForTheFailSafeRollback(t *testing.T) {
	t.Parallel()
	n, fake, _ := commissionedKeeperNode(t, mdns.NewNoop())
	gc := n.root.GeneralCommissioning
	entered := make(chan struct{})
	gate := make(chan struct{})
	var running atomic.Bool
	gc.SetOnFailSafeExpired(func(context.Context, uint8) {
		running.Store(true)
		close(entered)
		<-gate
		running.Store(false)
	})
	defer close(gate)
	if err := gc.ArmFailSafeFor(t.Context(), 1, 0); err != nil {
		t.Fatal(err)
	}
	fake.Advance(2 * time.Second)
	<-entered

	// The test's own bound on Stop: the rollback never ends while Stop
	// runs, so a Stop that waits for it gives up at this deadline.
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err := n.Stop(ctx)
	if err == nil && running.Load() {
		t.Fatal("Stop returned nil while the fail-safe's rollback still ran")
	}
	if err == nil {
		t.Fatal("Stop returned nil, yet the rollback it would have waited for never ended")
	}
}

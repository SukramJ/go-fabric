// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"testing"
	"time"
)

// TestCommissioningAttemptCapThroughFabricRemoved drives the twentieth
// failed commissioning attempt through the node's fabric removal: a
// fail-safe armed in the node's own window for fabric 1 expires inside
// Node.fabricRemoved (GeneralCommissioning.FabricRemoved), its end reaches
// the bridge's session closer, and the cap closes the window from there —
// the window's transition hooks (the node's commissioningHost.transition)
// running inside the fabric removal. Run under -race; a lock inversion or
// a re-entry would deadlock and the context bounds it (pin lift b25ed960,
// unit P3-C, Fable post-read F5).
func TestCommissioningAttemptCapThroughFabricRemoved(t *testing.T) {
	t.Parallel()
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
	n := newMDNSTestNode(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	win := n.br.CommissioningWindow()
	if win == nil || !win.IsOwnWindow() {
		t.Fatal("precondition: the uncommissioned node's own window is open")
	}
	gc := n.root.GeneralCommissioning
	const attemptsBeforeCap = 19 // bridge's paseMaxErrors - 1
	for range attemptsBeforeCap {
		if err := gc.ArmFailSafeFor(ctx, 60, 0); err != nil {
			t.Fatal(err)
		}
		gc.ExpireFailSafe(ctx)
	}
	if !win.IsOpen() {
		t.Fatalf("window closed after %d failed attempts", attemptsBeforeCap)
	}
	// The twentieth: a fail-safe the commissioner's AddNOC re-stamped for
	// fabric 1, ended by that fabric's removal.
	if err := gc.ArmFailSafeFor(ctx, 60, 1); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		n.fabricRemoved(ctx, 1)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("Node.fabricRemoved did not return: the cap's window close deadlocked inside the fabric removal")
	}
	if gc.FailSafeArmed() {
		t.Fatal("precondition: the fabric removal expired the fail-safe")
	}
	if win.IsOpen() {
		t.Fatal("window still open after the twentieth failed attempt, reached through the fabric removal")
	}
}

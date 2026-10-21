// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// TestStopLeavesNoTimerArmed pins ADR 0026 P5 for timers: once Stop
// returned, nothing the node started is armed on its clock — the
// uncommissioned node's own commissioning window (48 h) and the bridge's
// session-close budget included.
func TestStopLeavesNoTimerArmed(t *testing.T) {
	ctx := context.Background()
	fake := clocktest.New(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "n.db")))
	cfg.Clock = fake
	n := newNode(t, cfg)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !n.Bridge().CommissioningWindow().IsOpen() {
		t.Fatal("the uncommissioned node opened no window")
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if armed := fake.Armed(); len(armed) != 0 {
		t.Fatalf("armed after Stop: %v", armed)
	}
}

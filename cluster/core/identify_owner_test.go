// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestIdentify_CloseWaitsForTheCountdown pins Identify as the owner of
// its countdown goroutine (G1-14, ADR 0026 P5): Close returns after the
// goroutine did — its ticker is stopped, nothing stays armed on the
// clock.
func TestIdentify_CloseWaitsForTheCountdown(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))
	id := core.NewIdentify()
	id.SetClock(clk)
	if _, err := id.MatterInvoke(context.Background(), 0x00, uint16(600)); err != nil {
		t.Fatalf("MatterInvoke(Identify): %v", err)
	}
	for range 10_000 { // the countdown arms its ticker on its own goroutine
		if clk.Pending() > 0 {
			break
		}
		clk.Yield()
	}
	if clk.Pending() != 1 {
		t.Fatalf("%d timers armed, want the countdown's ticker", clk.Pending())
	}
	id.Close()
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d timers armed after Close, want 0 — the countdown still runs", n)
	}
}

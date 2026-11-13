// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestGencomm_ReArmKeepsOneExpiryWatcher pins the fail-safe's expiry
// watcher (review II, W2-conc-15): one per armed window, replaced on a
// re-arm and stopped on a disarm. Before, every arm and re-arm started a
// goroutine with a timer of its own that slept to its own deadline — a
// commissioner that re-arms every few seconds left one parked per
// re-arm. The fake clock counts the armed timers; the expiry fires once,
// at the last deadline.
func TestGencomm_ReArmKeepsOneExpiryWatcher(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	gc := newGencomm(t, core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoor,
		FailSafeMaxSeconds: 900,
		Clock:              clk,
	})
	var fired atomic.Int32
	gc.SetOnFailSafeExpired(func(context.Context, uint8) { fired.Add(1) })

	arm := func(seconds uint16) {
		t.Helper()
		resp, err := gc.MatterInvoke(context.Background(), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: seconds})
		if err != nil {
			t.Fatalf("ArmFailSafe: %v", err)
		}
		if r := resp.(core.ArmFailSafeResponse); r.ErrorCode != core.CommissioningErrorOK {
			t.Fatalf("ArmFailSafe ErrorCode = %d, want OK", r.ErrorCode)
		}
	}
	const rearms = 100
	for range rearms {
		arm(60)
		clk.Advance(time.Second)
	}
	// Whatever a watcher goroutine still has to do is runnable.
	for range 50 {
		clk.Yield()
	}
	if n := clk.Pending(); n != 1 {
		t.Fatalf("%d expiry timers armed after %d re-arms, want 1", n, rearms)
	}
	if n := fired.Load(); n != 0 {
		t.Fatalf("the expiry fired %d time(s) while the window was re-armed", n)
	}

	// The last arm was one second ago: 58 s on the window stands, 60 s on
	// it has expired.
	clk.Advance(58 * time.Second)
	clk.Yield()
	if n := fired.Load(); n != 0 || !gc.FailSafeArmed() {
		t.Fatalf("one second before the last deadline: fired %d, armed %v — want 0, true", n, gc.FailSafeArmed())
	}
	clk.Advance(2 * time.Second)
	for range 50 {
		if fired.Load() != 0 {
			break
		}
		clk.Yield()
	}
	clk.Advance(time.Hour)
	clk.Yield()
	if n := fired.Load(); n != 1 {
		t.Fatalf("the expiry fired %d time(s), want once", n)
	}
	if n := clk.Pending(); n != 0 {
		t.Errorf("%d timers armed after the expiry, want 0", n)
	}

	// A disarm stops the watcher of the window it ends.
	arm(60)
	arm(0)
	if n := clk.Pending(); n != 0 {
		t.Errorf("%d timers armed after a disarm, want 0", n)
	}
}

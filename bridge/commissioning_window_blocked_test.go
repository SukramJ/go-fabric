// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// TestCommissioningWindow_BlockEndsACommissioningAfterTheWindowClosed is
// R2-P3-pase-04 (R2-P2-rr-dev-08): once the host blocks commissioning, no
// commissioning goes on — the PASE sessions close and the fail-safe armed
// over PASE expires — whether or not a window is still open: a window's
// timer ends the window, not the PASE session a commissioner holds or its
// fail-safe.
func TestCommissioningWindow_BlockEndsACommissioningAfterTheWindowClosed(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	w, gc := windowOverGC(t)
	w.SetClock(clk)
	w.SetFailSafeExpirer(gc)
	closer := &fakePaseSessionCloser{}
	w.SetPaseSessionCloser(closer)
	ctx := context.Background()
	if err := w.OpenWindow(ctx, wire.OpenWindowParams{CommissioningTimeoutSeconds: 180}); err != nil {
		t.Fatal(err)
	}
	if resp, err := gc.MatterInvoke(im.WithAuthModePASE(ctx), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 900}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe over PASE: %+v, %v", resp, err)
	}
	clk.Advance(181 * time.Second)
	if w.IsOpen() {
		t.Fatal("the 180 s window is open 181 s later")
	}
	closes := closer.calls.Load()

	w.SetBlocked(ctx, errors.New("attestation unavailable"))
	if closer.calls.Load() == closes {
		t.Error("the host's block left the PASE sessions open")
	}
	if gc.FailSafeArmed() {
		t.Error("the host's block left the fail-safe armed over PASE")
	}
}

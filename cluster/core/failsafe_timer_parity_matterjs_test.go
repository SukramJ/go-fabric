// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

// A port of matter.js packages/protocol/test/common/FailsafeTimerTest.ts at
// 862ad75f: the fail-safe's expiry on a re-arm with 0, on the cumulative
// limit and its absence after completion.
//
// matter.js tests the FailsafeTimer class; the Go fail-safe lives in the
// General Commissioning server, so the cases drive it through ArmFailSafe
// and CommissioningComplete and observe OnFailSafeExpired. The server floors
// the window and the cumulative limit at 900 s, so the matter.js
// millisecond vectors are scaled where a case needs a limit below that.

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// failsafeTimerFixture is a General Commissioning server on a fake clock
// whose expiry hook reports on a channel.
type failsafeTimerFixture struct {
	clk     *clocktest.Fake
	gc      *core.GeneralCommissioning
	expired chan uint8
}

func newFailsafeTimerFixture(t *testing.T, maxSeconds, cumulativeSeconds uint16) *failsafeTimerFixture {
	t.Helper()
	f := &failsafeTimerFixture{
		clk:     clocktest.New(time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)),
		expired: make(chan uint8, 4),
	}
	f.gc = newGencomm(t, core.GeneralCommissioningConfig{
		LocationCapability:           core.RegulatoryIndoor,
		FailSafeMaxSeconds:           maxSeconds,
		CumulativeFailSafeMaxSeconds: cumulativeSeconds,
		Clock:                        f.clk,
		OnFailSafeExpired:            func(_ context.Context, idx uint8) { f.expired <- idx },
	})
	return f
}

// arm sends ArmFailSafe on ctx and waits until the expiry watcher it
// starts has armed its timer, so a following Advance reaches it.
func (f *failsafeTimerFixture) arm(t *testing.T, ctx context.Context, seconds uint16, watchers int) {
	t.Helper()
	resp, err := f.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: seconds})
	if err != nil {
		t.Fatalf("ArmFailSafe(%d): %v", seconds, err)
	}
	if code := resp.(core.ArmFailSafeResponse).ErrorCode; code != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe(%d) ErrorCode = %d, want OK", seconds, code)
	}
	for range 1000 {
		if f.clk.Pending() >= watchers {
			return
		}
		f.clk.Yield()
	}
	t.Fatalf("expiry watcher not armed: %d pending, want %d", f.clk.Pending(), watchers)
}

func (f *failsafeTimerFixture) wantNotExpired(t *testing.T) {
	t.Helper()
	f.clk.Yield()
	select {
	case <-f.expired:
		t.Fatal("expiry callback called")
	default:
	}
}

func (f *failsafeTimerFixture) wantExpired(t *testing.T) {
	t.Helper()
	select {
	case <-f.expired:
	case <-time.After(5 * time.Second): // a bound on a missing callback, not a wait for time
		t.Fatal("expiry callback not called")
	}
}

func TestFailsafeTimerVerifyExpiryHandling(t *testing.T) {
	t.Parallel()

	t.Run("Expiry callback is called directly when failsafe expires after being rearmed (with 0)", func(t *testing.T) {
		t.Parallel()
		// FailsafeTimerTest.ts:45-56: armed for 3 s, re-armed with 0 after 1.5 s.
		f := newFailsafeTimerFixture(t, 900, 900)
		ctx := context.Background()
		f.arm(t, ctx, 3, 1)
		f.clk.Advance(1500 * time.Millisecond)
		f.wantNotExpired(t)
		resp, err := f.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 0})
		if err != nil {
			t.Fatalf("ArmFailSafe(0): %v", err)
		}
		if code := resp.(core.ArmFailSafeResponse).ErrorCode; code != core.CommissioningErrorOK {
			t.Fatalf("ArmFailSafe(0) ErrorCode = %d, want OK", code)
		}
		// Directly: the callback ran before the invoke returned.
		select {
		case <-f.expired:
		default:
			t.Fatal("expiry callback not called by the re-arm with 0")
		}
	})

	t.Run("Expiry callback is called when max cumulative failsafe expires", func(t *testing.T) {
		t.Parallel()
		// FailsafeTimerTest.ts:58-70 (3 s window, 2 s cumulative; re-arm for
		// 1 s at 1.5 s; expired 0.5 s later) scaled by 450 so the cumulative
		// limit is the server's floor of 900 s: the re-arm at 675 s for 450 s
		// would run to 1125 s, the cumulative limit expires it at 900 s. The
		// first window is the 900 s ceiling, as 1350 s is above it.
		f := newFailsafeTimerFixture(t, 900, 900)
		ctx := context.Background()
		f.arm(t, ctx, 900, 1)
		f.clk.Advance(675 * time.Second)
		f.wantNotExpired(t)
		f.arm(t, ctx, 450, 2)
		f.wantNotExpired(t)
		f.clk.Advance(225*time.Second - time.Nanosecond)
		f.wantNotExpired(t)
		f.clk.Advance(time.Nanosecond)
		f.wantExpired(t)
	})

	t.Run("Expiry callback is not called when failsafe was completed", func(t *testing.T) {
		t.Parallel()
		// FailsafeTimerTest.ts:72-84: completed 1.5 s into a 3 s window; the
		// expiry 1.5 s later does not call back. Completion is
		// CommissioningComplete over the CASE session of the arming fabric.
		f := newFailsafeTimerFixture(t, 900, 900)
		ctx := im.WithFabricFilter(context.Background(), false, 1)
		f.arm(t, ctx, 3, 1)
		f.clk.Advance(1500 * time.Millisecond)
		f.wantNotExpired(t)
		resp, err := f.gc.MatterInvoke(ctx, 0x04, nil)
		if err != nil {
			t.Fatalf("CommissioningComplete: %v", err)
		}
		if code := resp.(core.CommissioningCompleteResponse).ErrorCode; code != core.CommissioningErrorOK {
			t.Fatalf("CommissioningComplete ErrorCode = %d, want OK", code)
		}
		f.wantNotExpired(t)
		f.clk.Advance(1500 * time.Millisecond)
		f.wantNotExpired(t)
	})
}

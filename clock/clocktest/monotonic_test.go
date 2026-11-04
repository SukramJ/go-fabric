// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clocktest_test

import (
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

var stepStart = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// StepWall moves Now alone: the monotonic reading and the armed timers
// stay (MockTime.stepWallClock, packages/testing/src/mocks/time.ts:472-478
// at 862ad75f).
func TestStepWallMovesNowAndNotTheMonotonicReading(t *testing.T) {
	t.Parallel()
	f := clocktest.New(stepStart)
	var fired atomic.Int32
	f.AfterFunc(time.Minute, func() { fired.Add(1) })
	f.Advance(10 * time.Second)
	if got := f.Monotonic(); got != 10*time.Second {
		t.Fatalf("Monotonic after Advance(10s) = %v", got)
	}

	f.StepWall(-time.Hour)
	if got, want := f.Now(), stepStart.Add(10*time.Second-time.Hour); !got.Equal(want) {
		t.Fatalf("Now after StepWall(-1h) = %v, want %v", got, want)
	}
	if got := f.Monotonic(); got != 10*time.Second {
		t.Fatalf("Monotonic after StepWall = %v, want 10s", got)
	}
	if armed := f.Armed(); !slices.Equal(armed, []time.Duration{50 * time.Second}) {
		t.Fatalf("Armed after StepWall = %v, want [50s]", armed)
	}
	if n := fired.Load(); n != 0 {
		t.Fatalf("timer fired %d times on a wall step", n)
	}

	f.StepWall(2 * time.Hour)
	if got, want := f.Now(), stepStart.Add(10*time.Second+time.Hour); !got.Equal(want) {
		t.Fatalf("Now after StepWall(+2h) = %v, want %v", got, want)
	}
	f.Advance(50 * time.Second)
	if n := fired.Load(); n != 1 {
		t.Fatalf("timer fired %d times after the advance, want 1", n)
	}
}

func TestArmedOrdersByDeadlineThenByArming(t *testing.T) {
	t.Parallel()
	f := clocktest.New(stepStart)
	f.NewTimer(2 * time.Second)
	f.NewTimer(time.Second)
	f.NewTimer(time.Second)
	if got, want := f.Armed(), []time.Duration{time.Second, time.Second, 2 * time.Second}; !slices.Equal(got, want) {
		t.Fatalf("Armed = %v, want %v", got, want)
	}
}

func TestTickerRefusesANonPositiveInterval(t *testing.T) {
	t.Parallel()
	f := clocktest.New(stepStart)
	mustPanic(t, "NewTicker(0)", func() { f.NewTicker(0) })
	tk := f.NewTicker(time.Second)
	defer tk.Stop()
	mustPanic(t, "Ticker.Reset(0)", func() { tk.Reset(0) })
}

func mustPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("%s did not panic", what)
		}
	}()
	fn()
}

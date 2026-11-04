// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clock_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// wallOnly is a Clock without a Monotonic reading: MonotonicOf falls back
// to its wall reading since the Unix epoch.
type wallOnly struct{ at time.Time }

func (w wallOnly) Now() time.Time { return w.at }
func (wallOnly) AfterFunc(d time.Duration, f func()) clock.Timer {
	return clock.System{}.AfterFunc(d, f)
}

func (wallOnly) NewTimer(d time.Duration) clock.Timer { return clock.System{}.NewTimer(d) }

func (wallOnly) NewTicker(d time.Duration) clock.Ticker { return clock.System{}.NewTicker(d) }

func TestSystemMonotonicAdvancesAndIsTheNilClocks(t *testing.T) {
	t.Parallel()
	first := clock.System{}.Monotonic()
	if first < 0 {
		t.Fatalf("System.Monotonic() = %v, want a non-negative reading", first)
	}
	if viaNil := clock.MonotonicOf(nil); viaNil < first {
		t.Fatalf("MonotonicOf(nil) = %v, before the earlier System reading %v", viaNil, first)
	}
	time.Sleep(time.Millisecond)
	if later := (clock.System{}).Monotonic(); later <= first {
		t.Fatalf("System.Monotonic() = %v after a sleep, not after %v", later, first)
	}
	wall := clock.WallOf(nil, clock.System{}.Monotonic())
	if d := time.Since(wall); d < 0 || d > time.Minute {
		t.Fatalf("WallOf(nil, now) = %v, %v from the system clock", wall, d)
	}
}

func TestMonotonicOfFallsBackToTheWallReading(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	c := wallOnly{at: at}
	if got, want := clock.MonotonicOf(c), at.Sub(time.Unix(0, 0)); got != want {
		t.Fatalf("MonotonicOf(wall-only clock) = %v, want %v", got, want)
	}
	// WallOf inverts MonotonicOf: a reading one hour before the current
	// one is the wall time one hour ago.
	mono := clock.MonotonicOf(c) - time.Hour
	if got, want := clock.WallOf(c, mono), at.Add(-time.Hour); !got.Equal(want) {
		t.Fatalf("WallOf(wall-only clock, now-1h) = %v, want %v", got, want)
	}
}

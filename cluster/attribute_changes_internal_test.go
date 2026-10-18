// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cluster

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// TestQuieterStaleFlushDoesNothing: a deferred report whose timer was
// replaced, or whose held change already went out, reports nothing when
// it runs late — the race a wall-clock timer has when its callback is due
// as the timer stops, which the test clock (a stopped timer never fires)
// cannot produce, so the late run is made by hand.
func TestQuieterStaleFlushDoesNothing(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1000, 0))
	reports := 0
	q := &Quieter{Report: func() { reports++ }, Clock: clk}
	q.Changed(false, false) // reports at once
	clk.Advance(100 * time.Millisecond)
	q.Changed(false, false) // held, timer armed
	q.mu.Lock()
	gen := q.gen
	q.mu.Unlock()

	q.flush(gen - 1) // a replaced timer
	if reports != 1 {
		t.Fatalf("a replaced timer reported: %d", reports)
	}
	q.mu.Lock()
	q.pending = false // the held change went out another way
	q.mu.Unlock()
	q.flush(gen)
	if reports != 1 {
		t.Fatalf("a timer with nothing held reported: %d", reports)
	}
}

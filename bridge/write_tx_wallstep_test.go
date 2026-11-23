// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// steppedClock is a clock whose wall reading the test steps while its
// monotonic reading only advances: what clock.System gives across an NTP
// step.
type steppedClock struct {
	clock.System
	mu   sync.Mutex
	wall time.Time
	mono time.Duration
}

func (c *steppedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wall
}

func (c *steppedClock) Monotonic() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mono
}

// advance moves the monotonic reading by elapsed and the wall reading by
// elapsed+step.
func (c *steppedClock) advance(elapsed, step time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mono += elapsed
	c.wall = c.wall.Add(elapsed + step)
}

// TestChunkedWriteIdleIsMonotonic: the idle time of a chunked write is
// measured on the monotonic reading. A forward wall step of more than
// [writeTxTTL] between two chunks does not sweep the write's own
// transaction, and after a backward step an abandoned write is swept once
// writeTxTTL of monotonic time passed (review II, R2-C2-fsm-0350).
func TestChunkedWriteIdleIsMonotonic(t *testing.T) {
	t.Parallel()
	c := &steppedClock{wall: time.Unix(1_800_000_000, 0), mono: time.Hour}
	b := &Bridge{clock: c}
	key := mrp.ExchangeKey{SessionID: 7, ExchangeID: 9}
	first := b.chunkWriteTransaction(key, true)
	if first == nil {
		t.Fatal("no transaction for the first chunk")
	}
	c.advance(20*time.Millisecond, 2*time.Minute)
	if !b.writeContinues(key) {
		t.Error("after a forward wall step the write does not continue")
	}
	if got := b.chunkWriteTransaction(key, true); got != first {
		t.Error("after a forward wall step the next chunk got a fresh transaction")
	}

	abandoned := mrp.ExchangeKey{SessionID: 8, ExchangeID: 1}
	if b.chunkWriteTransaction(abandoned, true) == nil {
		t.Fatal("no transaction for the abandoned write")
	}
	c.advance(writeTxTTL+time.Second, -time.Hour)
	b.chunkWriteTransaction(mrp.ExchangeKey{SessionID: 9, ExchangeID: 1}, false).Release()
	if n := b.routing.openWriteTxs(8); n != 0 {
		t.Errorf("after a backward wall step and writeTxTTL of idle, session 8 keeps %d chunked writes", n)
	}
}

// TestTimedSweepIntervalIsMonotonic: the sweep of timed deadlines is
// amortised on the monotonic reading; a backward wall step does not
// postpone it (review II, R2-C2-fsm-0350).
func TestTimedSweepIntervalIsMonotonic(t *testing.T) {
	t.Parallel()
	c := &steppedClock{wall: time.Unix(1_800_000_000, 0), mono: time.Hour}
	b := &Bridge{clock: c}
	b.sweepTimedDeadlines()
	b.routing.timedDeadlines.Store(timedKey{sessionID: 1, exchangeID: 1}, c.Now().Add(-2*time.Hour))
	c.advance(timedSweepInterval+time.Second, -time.Hour)
	b.sweepTimedDeadlines()
	n := 0
	b.routing.timedDeadlines.Range(func(any, any) bool { n++; return true })
	if n != 0 {
		t.Errorf("after a backward wall step and a sweep interval, %d elapsed deadlines remain", n)
	}
}

// monoOf is the monotonic reading [clock.MonotonicOf] gives for t on a
// clock without one: its offset from the Unix epoch.
func monoOf(t time.Time) time.Duration { return t.Sub(time.Unix(0, 0)) }

// TestNoWallClockIntervalsInBridge is the standing guard of
// R2-C2-fsm-0350's class: an interval kept as Unix nanoseconds and
// compared with time.Unix(0, …) loses the monotonic reading, so a
// wall-clock step moves it. The package's intervals use
// [clock.MonotonicOf] or a time.Time that keeps its reading.
func TestNoWallClockIntervalsInBridge(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{".UnixNano()", "time.Unix(0,"} {
			if strings.Contains(string(src), bad) {
				t.Errorf("%s uses %s: measure the interval on clock.MonotonicOf", f, bad)
			}
		}
	}
}

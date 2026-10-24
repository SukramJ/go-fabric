// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
)

var start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func ended(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// TestWithTimeoutFiresOnAdvance: the deadline is the fake's, not the
// machine's — it holds however long the test waits, and ends the context
// with DeadlineExceeded once the fake passes it.
func TestWithTimeoutFiresOnAdvance(t *testing.T) {
	t.Parallel()
	f := clocktest.New(start)
	ctx, cancel := clock.WithTimeout(context.Background(), f, 5*time.Second)
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Error("a fake-clock deadline reported a wall-clock Deadline")
	}
	time.Sleep(20 * time.Millisecond)
	f.Advance(4 * time.Second)
	if ended(ctx) {
		t.Fatal("ended before the fake passed the deadline")
	}
	f.Advance(time.Second)
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) || !errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
		t.Fatalf("Err %v, Cause %v; want DeadlineExceeded", ctx.Err(), context.Cause(ctx))
	}
	child, stop := context.WithCancel(ctx)
	defer stop()
	if !errors.Is(context.Cause(child), context.DeadlineExceeded) {
		t.Fatalf("a derived context's Cause %v", context.Cause(child))
	}
}

// TestWithTimeoutCancelStopsTheTimer: cancel ends the context with
// Canceled and disarms the fake's timer, so the fake's next deadline is
// not a finished operation's.
func TestWithTimeoutCancelStopsTheTimer(t *testing.T) {
	t.Parallel()
	f := clocktest.New(start)
	ctx, cancel := clock.WithTimeout(context.Background(), f, time.Minute)
	if f.Pending() != 1 {
		t.Fatalf("pending %d, want the deadline's timer", f.Pending())
	}
	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("Err %v, want Canceled", ctx.Err())
	}
	if f.Pending() != 0 {
		t.Fatalf("pending %d after cancel", f.Pending())
	}
	cancel() // idempotent
}

// TestWithTimeoutParentEnds: the parent's end ends the context with the
// parent's cause and disarms the timer.
func TestWithTimeoutParentEnds(t *testing.T) {
	t.Parallel()
	f := clocktest.New(start)
	parent, stop := context.WithCancel(context.Background())
	ctx, cancel := clock.WithTimeout(parent, f, time.Minute)
	defer cancel()
	stop()
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("Err %v, want the parent's Canceled", ctx.Err())
	}
	deadline := time.Now().Add(10 * time.Second)
	for f.Pending() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the timer outlived the parent")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestWithTimeoutNonPositive: a deadline already passed ends the
// context at once, as context.WithTimeout does.
func TestWithTimeoutNonPositive(t *testing.T) {
	t.Parallel()
	f := clocktest.New(start)
	ctx, cancel := clock.WithTimeout(context.Background(), f, 0)
	defer cancel()
	if !ended(ctx) || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("Err %v, want DeadlineExceeded at once", ctx.Err())
	}
}

// TestWithDeadline: the instant is the fake's.
func TestWithDeadline(t *testing.T) {
	t.Parallel()
	f := clocktest.New(start)
	ctx, cancel := clock.WithDeadline(context.Background(), f, start.Add(time.Second))
	defer cancel()
	if ended(ctx) {
		t.Fatal("ended at once: the instant was read on the wall clock")
	}
	f.Advance(time.Second)
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("Err %v", ctx.Err())
	}
}

// TestWithTimeoutSystem: on nil and System it is the standard library's —
// a wall-clock Deadline, DeadlineExceeded on expiry.
func TestWithTimeoutSystem(t *testing.T) {
	t.Parallel()
	for _, c := range []clock.Clock{nil, clock.System{}} {
		ctx, cancel := clock.WithTimeout(context.Background(), c, time.Millisecond)
		if _, ok := ctx.Deadline(); !ok {
			t.Errorf("%T: no Deadline", c)
		}
		<-ctx.Done()
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Errorf("%T: Err %v", c, ctx.Err())
		}
		cancel()
		dctx, dcancel := clock.WithDeadline(context.Background(), c, time.Now().Add(time.Hour))
		if d, ok := dctx.Deadline(); !ok || time.Until(d) < 59*time.Minute {
			t.Errorf("%T: WithDeadline %v %v", c, d, ok)
		}
		dcancel()
	}
}

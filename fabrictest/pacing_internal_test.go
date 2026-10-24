// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/internal/inflight"
)

// recordingTB is a testing.TB that keeps what Logf logs.
type recordingTB struct {
	testing.TB
	logs atomic.Int64
}

func (r *recordingTB) Logf(string, ...any) { r.logs.Add(1) }

func pacingCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// TestStep: Step fires the timers due within its step, and only after the
// site is quiet; without a fake clock it fails.
func TestStep(t *testing.T) {
	t.Parallel()
	site := NewSite(t, Options{Logger: slog.New(slog.DiscardHandler)})
	var fired atomic.Int64
	site.Fake().AfterFunc(time.Second, func() { fired.Add(1) })
	site.Fake().AfterFunc(3*time.Second, func() { fired.Add(1) })
	if err := site.Step(pacingCtx(t), 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if fired.Load() != 1 {
		t.Fatalf("fired %d timers in a 2 s step, want 1", fired.Load())
	}
	wall := NewSite(t, Options{Logger: slog.New(slog.DiscardHandler), Clock: clock.System{}})
	if err := wall.Step(pacingCtx(t), time.Second); err == nil {
		t.Fatal("Step on a wall-clock site did not fail")
	}
}

// TestCountedWorkHoldsTheClock: work a host counts holds Settle until it
// is finished; work that stands with nothing moving holds it for
// holdBudget, is logged and no longer holds it, until more is counted.
func TestCountedWorkHoldsTheClock(t *testing.T) {
	t.Parallel()
	rec := &recordingTB{TB: t}
	site := NewSite(rec, Options{Logger: slog.New(slog.DiscardHandler)})
	g := site.newGroup("device 9")
	if inflight.Of(site.hostClock(g)) != g {
		t.Fatal("a host's clock does not carry its group")
	}
	g.Enter(inflight.UDPDatagrams)
	go func() {
		time.Sleep(50 * time.Millisecond)
		g.Leave(inflight.UDPDatagrams)
	}()
	start := time.Now()
	if err := site.Settle(pacingCtx(t)); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("Settle returned while the work was in hand")
	}
	if rec.logs.Load() != 0 {
		t.Fatal("finished work was logged as abandoned")
	}

	g.Enter(inflight.DeviceCallbacks)
	start = time.Now()
	if err := site.Settle(pacingCtx(t)); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < holdBudget || rec.logs.Load() != 1 {
		t.Fatalf("stuck work: Settle after %v, %d logs; want the hold budget and one log", time.Since(start), rec.logs.Load())
	}
	start = time.Now()
	if err := site.Settle(pacingCtx(t)); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) >= holdBudget {
		t.Fatal("abandoned work held the clock again")
	}
	if !strings.Contains(site.inflightString(), "device 9") {
		t.Fatalf("in flight %q does not name the host", site.inflightString())
	}
	g.Leave(inflight.DeviceCallbacks)
	if site.inflightString() != "" {
		t.Fatalf("in flight %q after the work ended", site.inflightString())
	}
}

// TestWaitNamesTheWorkInHand: a wait that runs out names what the hosts
// still hold.
func TestWaitNamesTheWorkInHand(t *testing.T) {
	t.Parallel()
	site := NewSite(t, Options{Logger: slog.New(slog.DiscardHandler), Wait: 20 * time.Millisecond})
	g := site.newGroup("controller 3")
	g.Enter(inflight.ExchangeDelivered)
	defer g.Leave(inflight.ExchangeDelivered)
	err := site.Settle(context.Background())
	if !errors.Is(err, ErrWaited) || !strings.Contains(err.Error(), "controller 3") {
		t.Fatalf("Settle: %v, want ErrWaited naming controller 3", err)
	}
}

// TestDrainHoldsTheClock: drain counts a node.stop in its own group while
// it waits — an outer wait sees it as work — and returns once the other
// work is done.
func TestDrainHoldsTheClock(t *testing.T) {
	t.Parallel()
	site := NewSite(t, Options{Logger: slog.New(slog.DiscardHandler)})
	own, other := site.newGroup("device 1"), site.newGroup("controller 2")
	other.Enter(inflight.UDPDatagrams)
	done := make(chan error, 1)
	go func() { done <- site.drain(pacingCtx(t), own) }()
	deadline := time.Now().Add(10 * time.Second)
	for own.Snapshot()[inflight.NodeStop] != 1 {
		if time.Now().After(deadline) {
			t.Fatal("drain counted nothing in its own group")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("drain returned with work in hand: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	other.Leave(inflight.UDPDatagrams)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if own.Busy() {
		t.Fatal("drain left its count behind")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := site.drain(ctx, own); !errors.Is(err, context.Canceled) {
		t.Fatalf("drain on a cancelled context: %v", err)
	}
	short := NewSite(t, Options{Logger: slog.New(slog.DiscardHandler), Wait: time.Millisecond})
	if err := short.drain(context.Background(), short.newGroup("x")); err != nil && !errors.Is(err, ErrWaited) {
		t.Fatalf("drain with a short wait: %v", err)
	}
}

// TestBuildHoldsTheClockPastTheBudget: a node.build holds Settle until it
// ends, past holdBudget, and is not abandoned — a node's construction
// waits on no clock and under -race on a loaded machine outlasts the
// budget (the TestLiveSustainedSurvivesRestart failure, brief CG).
func TestBuildHoldsTheClockPastTheBudget(t *testing.T) {
	t.Parallel()
	rec := &recordingTB{TB: t}
	site := NewSite(rec, Options{Logger: slog.New(slog.DiscardHandler)})
	g := site.newGroup("device 4")
	g.Enter(inflight.NodeBuild)
	var ended atomic.Bool
	go func() {
		time.Sleep(holdBudget + 200*time.Millisecond)
		ended.Store(true)
		g.Leave(inflight.NodeBuild)
	}()
	if err := site.Settle(pacingCtx(t)); err != nil {
		t.Fatal(err)
	}
	if !ended.Load() || rec.logs.Load() != 0 {
		t.Fatalf("Settle returned with the build in hand (ended %v, %d logs)", ended.Load(), rec.logs.Load())
	}
	own := site.newGroup("device 5")
	g.Enter(inflight.NodeBuild)
	ended.Store(false)
	go func() {
		time.Sleep(holdBudget + 200*time.Millisecond)
		ended.Store(true)
		g.Leave(inflight.NodeBuild)
	}()
	if err := site.drain(pacingCtx(t), own); err != nil {
		t.Fatal(err)
	}
	if !ended.Load() {
		t.Fatal("drain returned with another host's build in hand")
	}
}

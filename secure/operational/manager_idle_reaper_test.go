// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

// White-box tests for the StartReaper background sweep — the production
// activation path (daemon wiring calls StartReaper with
// [SessionIdleTimeout] / a 60 s poll; the bridge's receive and seal
// chokepoints refresh MarkActiveRx / MarkActiveTx). Mirrors matter.js
// packages/protocol/src/session/Session.ts activity timestamps; the
// graceful farewell semantics of the sweep itself are pinned in
// manager_close_parity_test.go.
//
// The reaper ticks on a clocktest.Fake: a test moves the clock poll by
// poll until the effect it waits for (a session's close hook) arrived, so
// no assertion depends on how fast the machine is. A negative assertion
// (a session survives) is read after a control session — idle, opened
// beside it — was reaped: the sweep that reaped the control saw the other
// session too.

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

const (
	reaperTestIdle = 30 * time.Second
	reaperTestPoll = 10 * time.Second
	// reaperTestSteps bounds a wait: each step is one poll of fake time
	// and a Fake.Yield, so the bound is wall time only when the reaper
	// goroutine does not run at all.
	reaperTestSteps = 10_000
)

// newReaperManager is a manager on a fake clock with its close hooks
// recorded and its reaper running; the reaper stops with the test.
func newReaperManager(t *testing.T) (*Manager, *clocktest.Fake, *closeRecorder) {
	t.Helper()
	clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
	m := NewManager(newMinimalFakeStore())
	m.SetClock(clk)
	rec := &closeRecorder{}
	rec.wire(m)
	m.StartReaper(context.Background(), reaperTestIdle, reaperTestPoll)
	t.Cleanup(m.StopReaper)
	return m, clk, rec
}

// openReaperSession opens a PASE session on m for localID.
func openReaperSession(t *testing.T, m *Manager, localID uint64) *Entry {
	t.Helper()
	e, err := m.OpenFromPase(localID, 0xA11, 0x2222, []byte("shared-secret"))
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	return e
}

// advanceUntilClosed moves the clock poll by poll — running each before
// a step — until the close hook fired for id.
func advanceUntilClosed(t *testing.T, clk *clocktest.Fake, rec *closeRecorder, id uint16, each func()) {
	t.Helper()
	for range reaperTestSteps {
		if _, closed, _, _ := rec.snapshot(); slices.Contains(closed, id) {
			return
		}
		if each != nil {
			each()
		}
		clk.Advance(reaperTestPoll)
		clk.Yield()
	}
	t.Fatalf("session %d was not reaped within %d polls", id, reaperTestSteps)
}

// TestStartReaper_EvictsIdleSessionGracefully verifies the end-to-end
// background path: a session whose last activity falls behind the idle
// TTL is evicted by the periodic sweep, with the graceful CloseSession
// notifier and the close cascade firing exactly as for a direct
// reapIdle call.
func TestStartReaper_EvictsIdleSessionGracefully(t *testing.T) {
	t.Parallel()
	m, clk, rec := newReaperManager(t)
	entry := openReaperSession(t, m, 0xB0B)
	entry.MarkActiveRx() // traffic once, then silence

	advanceUntilClosed(t, clk, rec, entry.SessionID, nil)
	if n := m.Active(); n != 0 {
		t.Fatalf("Active() = %d, want 0 — reaper did not evict the idle session", n)
	}
	// The graceful notifier runs before the close cascade (reapIdle), so
	// both are recorded once the close hook fired.
	graceful, closed, _, _ := rec.snapshot()
	if len(graceful) != 1 || graceful[0] != entry.SessionID {
		t.Errorf("graceful notifier calls = %v, want [%d]", graceful, entry.SessionID)
	}
	if len(closed) != 1 || closed[0] != entry.SessionID {
		t.Errorf("onSessionClose calls = %v, want [%d]", closed, entry.SessionID)
	}
	if clk.Now().Before(time.UnixMilli(1_700_000_000_000).Add(reaperTestIdle)) {
		t.Errorf("reaped at %v, before the idle timeout", clk.Now())
	}
}

// TestStartReaper_ActiveSessionSurvivesSweeps verifies the keep-alive
// contract the production TTL depends on: a session whose peer keeps
// sending (subscription heartbeat acks → MarkActiveRx) survives many
// sweep intervals, and only becomes reap-eligible once the traffic
// stops. Concurrent marking against the running sweep also exercises
// the Entry/Manager locking under -race.
func TestStartReaper_ActiveSessionSurvivesSweeps(t *testing.T) {
	t.Parallel()
	m, clk, rec := newReaperManager(t)
	active := openReaperSession(t, m, 0xB0B)
	active.MarkActiveRx()

	// Three controls in a row, each idle from its open: every one is
	// reaped while the active session, marked every poll, stays.
	for i := range 3 {
		control := openReaperSession(t, m, 0xC00+uint64(i))
		control.MarkActiveRx()
		advanceUntilClosed(t, clk, rec, control.SessionID, active.MarkActiveRx)
		if _, closed, _, _ := rec.snapshot(); slices.Contains(closed, active.SessionID) {
			t.Fatalf("round %d: the reaper evicted the active session", i)
		}
	}
	if n := m.Active(); n != 1 {
		t.Fatalf("Active() = %d during live traffic, want 1", n)
	}

	// Silence: the session must now age out.
	advanceUntilClosed(t, clk, rec, active.SessionID, nil)
	if n := m.Active(); n != 0 {
		t.Fatalf("Active() = %d after traffic stopped, want 0", n)
	}
}

// TestReapIdleTakesANeverUsedSessionAfterTheTimeout pins the idle clock
// of a session that never carried a message (review II, W1-sec-06): it
// starts at the session's creation, so the session survives every sweep
// inside one idle timeout — commissioning may still be in flight — and is
// reaped by the first sweep past it, with the close cascade of any other
// idle session. Before, a zero last-activity time exempted the session
// for good.
func TestReapIdleTakesANeverUsedSessionAfterTheTimeout(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
	m := NewManager(newMinimalFakeStore())
	m.SetClock(clk)
	rec := &closeRecorder{}
	rec.wire(m)
	never := openReaperSession(t, m, 0xB0B)
	if la := never.LastActivity(); !la.IsZero() {
		t.Fatalf("LastActivity() = %v on a session that never carried a message, want zero", la)
	}

	clk.Advance(reaperTestIdle - time.Second)
	m.reapIdle(reaperTestIdle)
	if n := m.Active(); n != 1 {
		t.Fatalf("Active() = %d one second before the idle timeout, want 1 — a young never-used session must stay", n)
	}

	clk.Advance(2 * time.Second)
	m.reapIdle(reaperTestIdle)
	if n := m.Active(); n != 0 {
		t.Fatalf("Active() = %d one idle timeout after creation, want 0 — a never-used session must be reaped", n)
	}
	graceful, closed, _, _ := rec.snapshot()
	if !slices.Contains(graceful, never.SessionID) || !slices.Contains(closed, never.SessionID) {
		t.Errorf("close hooks (graceful %v / closed %v) did not fire for session %d", graceful, closed, never.SessionID)
	}
}

// TestStopReaperWaitsForTheReaper pins the reaper's owner (G1-14, ADR
// 0026 principle 5): StopReaper returns once the goroutine returned —
// its deferred ticker Stop has run, so nothing is armed on the clock —
// is idempotent, and a second StartReaper while one runs starts none.
func TestStopReaperWaitsForTheReaper(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
	m := NewManager(newMinimalFakeStore())
	m.SetClock(clk)
	m.StopReaper() // safe without a reaper

	m.StartReaper(context.Background(), reaperTestIdle, reaperTestPoll)
	m.StartReaper(context.Background(), reaperTestIdle, reaperTestPoll)
	if n := clk.Pending(); n != 1 {
		t.Fatalf("%d tickers armed after two StartReaper calls, want 1", n)
	}
	m.StopReaper()
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d tickers armed after StopReaper, want 0 — the reaper goroutine still runs", n)
	}
	m.StopReaper()

	// The parent context's end stops it too; StopReaper then only waits.
	ctx, cancel := context.WithCancel(context.Background())
	m.StartReaper(ctx, reaperTestIdle, reaperTestPoll)
	cancel()
	m.StopReaper()
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d tickers armed after the parent's cancel and StopReaper, want 0", n)
	}
}

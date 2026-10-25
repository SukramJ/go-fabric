// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/protocol/test/icd/IcdPeerWakefulnessTest.ts at
// 862ad75f: a LIT peer's awake and available state on a fake clock
// (MockTime.advance is Fake.Advance), subscriptions held, the default wait,
// the operating-mode flips, suspend and close. matter.js's observables are
// the On… registrations; `awake.value` is Awake. The two "clock" cases need
// MockTime.stepWallClock, which clocktest.Fake has not (its Set moves the one
// reading it has, and on the system clock a time.Time carries the monotonic
// reading a wall step does not move) — skipped with that reason.
package icd_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/icd"
)

var wakeTimings = icd.Timings{ActiveModeThreshold: 4 * time.Second, IdleModeDuration: 30 * time.Second}

// checkInDue is the next Check-In's deadline after activity with
// wakeTimings (IcdPeerWakefulnessTest.ts:17-18).
const checkInDue = 4*time.Second + 30*time.Second + icd.CheckInMargin

type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) inc() { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// litPeer is IcdPeerWakefulnessTest.ts:23-32.
func litPeer() (*icd.PeerWakefulness, *clocktest.Fake, *counter) {
	fake := clocktest.New(modeEpoch)
	w := icd.NewPeerWakefulness(fake)
	w.SetTimings(wakeTimings)
	w.SetRequiresAwait(true)
	missed := &counter{}
	w.OnCheckInMissed(missed.inc)
	return w, fake, missed
}

func wantDueIn(t *testing.T, w *icd.PeerWakefulness, fake *clocktest.Fake, d time.Duration) {
	t.Helper()
	if due, ok := w.NextCheckInDue(); !ok || !due.Equal(fake.Now().Add(d)) {
		t.Fatalf("nextCheckInDue %v (%v), want now + %v", due, ok, d)
	}
}

func wantNoDue(t *testing.T, w *icd.PeerWakefulness) {
	t.Helper()
	if due, ok := w.NextCheckInDue(); ok {
		t.Fatalf("nextCheckInDue %v", due)
	}
}

func TestIcdPeerWakefulness(t *testing.T) {
	t.Parallel()
	t.Run("is always awake and available while the peer needs no awaiting", func(t *testing.T) {
		t.Parallel()
		fake := clocktest.New(modeEpoch)
		w := icd.NewPeerWakefulness(fake)
		w.SetTimings(wakeTimings)
		missed := &counter{}
		w.OnCheckInMissed(missed.inc)
		w.NoteActive()
		fake.Advance(checkInDue + time.Millisecond)
		if !w.Awake() || !w.Available() || missed.get() != 0 {
			t.Fatalf("awake %v available %v missed %d", w.Awake(), w.Available(), missed.get())
		}
		wantNoDue(t, w)
	})
	t.Run("starts asleep and unavailable for a LIT peer without activity", func(t *testing.T) {
		t.Parallel()
		w, _, _ := litPeer()
		if w.Awake() || w.Available() {
			t.Fatal("awake or available")
		}
		wantNoDue(t, w)
	})
	t.Run("is awake for the Active Mode and available until the next Check-In is due", func(t *testing.T) {
		t.Parallel()
		w, fake, missed := litPeer()
		w.NoteActive()
		if !w.Awake() {
			t.Fatal("asleep")
		}
		wantDueIn(t, w, fake, checkInDue)
		fake.Advance(wakeTimings.ActiveModeThreshold + time.Millisecond)
		if w.Awake() || !w.Available() {
			t.Fatalf("awake %v available %v", w.Awake(), w.Available())
		}
		fake.Advance(checkInDue - wakeTimings.ActiveModeThreshold)
		if w.Available() || missed.get() != 1 {
			t.Fatalf("available %v missed %d", w.Available(), missed.get())
		}
		wantNoDue(t, w)
	})
	t.Run("applies the threshold a Check-In carries", func(t *testing.T) {
		t.Parallel()
		w, fake, _ := litPeer()
		twenty := 20 * time.Second
		w.NoteCheckIn(&twenty)
		fake.Advance(19 * time.Second)
		if !w.Awake() {
			t.Fatal("asleep")
		}
	})
	t.Run("stays awake for a StayActive promise and available for an idle period after it", func(t *testing.T) {
		t.Parallel()
		w, fake, _ := litPeer()
		w.NoteActive()
		w.NoteStayActive(60 * time.Second)
		fake.Advance(59 * time.Second)
		if !w.Awake() {
			t.Fatal("asleep")
		}
		wantDueIn(t, w, fake, time.Second+wakeTimings.IdleModeDuration+icd.CheckInMargin)
	})
	t.Run("applies new timings at once", func(t *testing.T) {
		t.Parallel()
		w, fake, missed := litPeer()
		w.NoteActive()
		fake.Advance(20 * time.Second)
		tm := wakeTimings
		tm.IdleModeDuration = time.Second // due at 4 s + 1 s + 10 s, already passed
		w.SetTimings(tm)
		if w.Available() || missed.get() != 1 {
			t.Fatalf("available %v missed %d", w.Available(), missed.get())
		}
	})
	t.Run("clock", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{
			"keeps its deadline across a forward wall-clock step",
			"ends Active Mode on time across a backward wall-clock step",
		} {
			t.Run(name, func(t *testing.T) {
				t.Skip("MockTime.stepWallClock: clocktest.Fake has one reading, and the system clock's time.Time carries the monotonic reading a wall-clock step does not move")
			})
		}
	})
	t.Run("subscription", func(t *testing.T) {
		t.Parallel()
		t.Run("keeps the peer available without a deadline while a subscription is held", func(t *testing.T) {
			t.Parallel()
			w, fake, missed := litPeer()
			release := w.HoldSubscription()
			defer release()
			w.NoteActive()
			wantNoDue(t, w)
			fake.Advance(5 * time.Hour)
			if !w.Available() || missed.get() != 0 {
				t.Fatalf("available %v missed %d", w.Available(), missed.get())
			}
		})
		t.Run("keeps the peer available while any of several subscriptions is held", func(t *testing.T) {
			t.Parallel()
			w, fake, missed := litPeer()
			remaining := w.HoldSubscription()
			defer remaining()
			released := w.HoldSubscription()
			w.NoteActive()
			released()
			fake.Advance(5 * time.Hour)
			if !w.Available() || missed.get() != 0 {
				t.Fatalf("available %v missed %d", w.Available(), missed.get())
			}
		})
		t.Run("expects the next Check-In again once the last subscription is released", func(t *testing.T) {
			t.Parallel()
			w, fake, _ := litPeer()
			release := w.HoldSubscription()
			w.NoteActive()
			release()
			wantDueIn(t, w, fake, checkInDue)
		})
		t.Run("reports a missed Check-In when the released subscription leaves it overdue", func(t *testing.T) {
			t.Parallel()
			w, fake, missed := litPeer()
			release := w.HoldSubscription()
			w.NoteActive()
			fake.Advance(time.Hour)
			if missed.get() != 0 {
				t.Fatal("missed while subscribed")
			}
			release()
			if w.Available() || missed.get() != 1 {
				t.Fatalf("available %v missed %d", w.Available(), missed.get())
			}
		})
	})
	t.Run("nextCheckInWithin", func(t *testing.T) {
		t.Parallel()
		idle := wakeTimings.IdleModeDuration + icd.CheckInMargin
		t.Run("is the idle window without a deadline", func(t *testing.T) {
			t.Parallel()
			w, _, _ := litPeer()
			if got := w.NextCheckInWithin(); got != idle {
				t.Fatal(got)
			}
		})
		t.Run("is the idle window when the deadline is closer", func(t *testing.T) {
			t.Parallel()
			w, fake, _ := litPeer()
			w.NoteActive()
			fake.Advance(10 * time.Second)
			if got := w.NextCheckInWithin(); got != idle {
				t.Fatal(got)
			}
		})
		t.Run("is the time to the deadline when that is longer", func(t *testing.T) {
			t.Parallel()
			w, _, _ := litPeer()
			w.NoteActive()
			w.NoteStayActive(60 * time.Second)
			if got := w.NextCheckInWithin(); got != 60*time.Second+idle {
				t.Fatal(got)
			}
		})
	})
	t.Run("operating mode", func(t *testing.T) {
		t.Parallel()
		t.Run("forces awake and available when the peer stops requiring await", func(t *testing.T) {
			t.Parallel()
			w, fake, missed := litPeer()
			w.NoteActive()
			w.SetRequiresAwait(false)
			if !w.Awake() || !w.Available() {
				t.Fatal("not forced")
			}
			fake.Advance(checkInDue + time.Millisecond)
			if missed.get() != 0 {
				t.Fatal("missed")
			}
		})
		t.Run("resumes from the observed activity when the peer requires await again", func(t *testing.T) {
			t.Parallel()
			w, _, _ := litPeer()
			w.NoteActive()
			w.SetRequiresAwait(false)
			w.SetRequiresAwait(true)
			if !w.Awake() {
				t.Fatal("asleep")
			}
		})
		t.Run("emits operatingModeChanged on each requiresAwait flip, not on a no-op set", func(t *testing.T) {
			t.Parallel()
			w := icd.NewPeerWakefulness(clocktest.New(modeEpoch))
			var mu sync.Mutex
			var seen []bool
			w.OnOperatingModeChanged(func(v bool) { mu.Lock(); seen = append(seen, v); mu.Unlock() })
			edge := w.ModeSignal()
			w.SetRequiresAwait(false)
			w.SetRequiresAwait(true)
			w.SetRequiresAwait(true)
			w.SetRequiresAwait(false)
			mu.Lock()
			defer mu.Unlock()
			if !slices.Equal(seen, []bool{true, false}) {
				t.Fatal(seen)
			}
			select {
			case <-edge:
			default:
				t.Fatal("the mode signal stayed open")
			}
		})
		t.Run("does not report a missed Check-In when awaiting starts with an overdue deadline", func(t *testing.T) {
			t.Parallel()
			fake := clocktest.New(modeEpoch)
			w := icd.NewPeerWakefulness(fake)
			w.SetTimings(wakeTimings)
			missed := &counter{}
			w.OnCheckInMissed(missed.inc)
			w.NoteActive()
			fake.Advance(checkInDue + time.Millisecond)
			w.SetRequiresAwait(true)
			if w.Available() || missed.get() != 0 {
				t.Fatalf("available %v missed %d", w.Available(), missed.get())
			}
		})
		t.Run("does not report a missed Check-In when the peer starts requiring await", func(t *testing.T) {
			t.Parallel()
			w, _, missed := litPeer()
			if w.Available() || missed.get() != 0 {
				t.Fatalf("available %v missed %d", w.Available(), missed.get())
			}
		})
	})
	t.Run("suspend", func(t *testing.T) {
		t.Parallel()
		t.Run("releases parked consumers and stops reporting missed Check-Ins", func(t *testing.T) {
			t.Parallel()
			w, fake, missed := litPeer()
			w.NoteActive()
			fake.Advance(wakeTimings.ActiveModeThreshold + time.Millisecond)
			if w.Awake() {
				t.Fatal("awake")
			}
			_, parked := w.AwakeSignal()
			w.Suspend()
			<-parked
			if !w.Awake() || !w.Available() {
				t.Fatal("not released")
			}
			wantNoDue(t, w)
			fake.Advance(checkInDue)
			if missed.get() != 0 {
				t.Fatal("missed")
			}
		})
		t.Run("keeps the timers stopped when activity is observed while suspended", func(t *testing.T) {
			t.Parallel()
			w, fake, missed := litPeer()
			w.Suspend()
			w.NoteActive()
			fake.Advance(checkInDue + time.Millisecond)
			if missed.get() != 0 || !w.Awake() {
				t.Fatalf("missed %d awake %v", missed.get(), w.Awake())
			}
		})
		t.Run("does not signal a mode change", func(t *testing.T) {
			t.Parallel()
			w, _, _ := litPeer()
			changes := &counter{}
			w.OnOperatingModeChanged(func(bool) { changes.inc() })
			w.Suspend()
			if changes.get() != 0 {
				t.Fatal("mode change")
			}
		})
		t.Run("does not report a missed Check-In when resuming with an overdue deadline", func(t *testing.T) {
			t.Parallel()
			w, fake, missed := litPeer()
			w.NoteActive()
			w.Suspend()
			fake.Advance(checkInDue + time.Millisecond)
			w.Resume()
			if w.Available() || missed.get() != 0 {
				t.Fatalf("available %v missed %d", w.Available(), missed.get())
			}
		})
		t.Run("resumes from the observed activity", func(t *testing.T) {
			t.Parallel()
			w, _, _ := litPeer()
			w.NoteActive()
			due, _ := w.NextCheckInDue()
			w.Suspend()
			w.Resume()
			if got, ok := w.NextCheckInDue(); !ok || !got.Equal(due) {
				t.Fatalf("due %v, want %v", got, due)
			}
		})
	})
	t.Run("close", func(t *testing.T) {
		t.Parallel()
		t.Run("keeps the timers stopped when a subscription is released afterwards", func(t *testing.T) {
			t.Parallel()
			w, fake, missed := litPeer()
			release := w.HoldSubscription()
			w.NoteActive()
			w.Close()
			release()
			w.Resume()
			fake.Advance(time.Hour)
			if missed.get() != 0 {
				t.Fatal("missed")
			}
			wantNoDue(t, w)
		})
		t.Run("does not report a missed Check-In", func(t *testing.T) {
			t.Parallel()
			w, fake, missed := litPeer()
			w.NoteActive()
			w.Close()
			fake.Advance(checkInDue + time.Millisecond)
			if missed.get() != 0 {
				t.Fatal("missed")
			}
		})
		t.Run("releases a consumer parked on the awake edge", func(t *testing.T) {
			t.Parallel()
			w, _, _ := litPeer()
			released := &counter{}
			w.OnAwakeChanged(func(awake bool) {
				if awake {
					released.inc()
				}
			})
			w.Close()
			if released.get() == 0 || !w.Awake() {
				t.Fatal("not released")
			}
		})
	})
}

// TestAwaitAwake holds IcdPeerWakefulness.ts:56-66 on the fake clock: an
// awake peer at once, a sleeping one on its wake, the timeout and the
// abort.
func TestAwaitAwake(t *testing.T) {
	t.Parallel()
	w, fake, _ := litPeer()
	done := make(chan icd.AwaitResult, 1)
	go func() { done <- w.AwaitAwake(t.Context(), time.Minute) }()
	for fake.Pending() < 1 {
		time.Sleep(time.Millisecond)
	}
	w.NoteActive()
	if r := <-done; r != icd.AwaitAwakeResult {
		t.Fatalf("wake: %v", r)
	}
	if r := w.AwaitAwake(t.Context(), time.Minute); r != icd.AwaitAwakeResult {
		t.Fatalf("awake: %v", r)
	}
	w2, fake2, _ := litPeer()
	go func() { done <- w2.AwaitAwake(t.Context(), time.Minute) }()
	for fake2.Pending() < 1 {
		time.Sleep(time.Millisecond)
	}
	fake2.Advance(time.Minute)
	if r := <-done; r != icd.AwaitTimeout {
		t.Fatalf("timeout: %v", r)
	}
	ctx, cancel := contextWithCancel(t)
	cancel()
	if r := w2.AwaitAwake(ctx, time.Minute); r != icd.AwaitAborted {
		t.Fatalf("abort: %v", r)
	}
}

func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(t.Context())
}

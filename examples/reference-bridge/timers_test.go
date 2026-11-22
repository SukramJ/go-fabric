// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"testing"
	"time"

	ccldef "thingfabric.dev/go-fabric/cluster/spec/closurecontrol"
	valvedef "thingfabric.dev/go-fabric/cluster/spec/valveconfigurationandcontrol"
)

// heldTimers is a latestTimer schedule that never fires by itself: the
// test fires an arming's callback — the newest, or an earlier one, which
// stands for a callback that fired before a re-arm stopped it and waited
// for the device's lock meanwhile.
type heldTimers struct{ fns []func() }

func (h *heldTimers) after(_ time.Duration, f func()) func() bool {
	h.fns = append(h.fns, f)
	return func() bool { return true }
}

func (h *heldTimers) fire(t *testing.T, i int) {
	t.Helper()
	if i >= len(h.fns) {
		t.Fatalf("arming %d of %d", i, len(h.fns))
	}
	h.fns[i]()
}

// TestLatestTimerRunsOnlyTheLatestArming is the class guard of
// R2-P6-conc-2607 / -2651: a re-arm or a stop makes an earlier arming's
// callback stale, and the latest one is taken once.
func TestLatestTimerRunsOnlyTheLatestArming(t *testing.T) {
	var h heldTimers
	var lt latestTimer
	lt.after = h.after
	var took []uint64
	fn := func(gen uint64) {
		if lt.takeLocked(gen) {
			took = append(took, gen)
		}
	}
	lt.armLocked(time.Second, fn)
	lt.armLocked(time.Second, fn)
	h.fire(t, 0)
	if len(took) != 0 || !lt.armedLocked() {
		t.Fatalf("the stale arming ran: %v, armed %v", took, lt.armedLocked())
	}
	h.fire(t, 1)
	h.fire(t, 1)
	if len(took) != 1 || lt.armedLocked() {
		t.Fatalf("the latest arming: took %v, armed %v", took, lt.armedLocked())
	}
	lt.armLocked(time.Second, fn)
	lt.stopLocked()
	h.fire(t, 2)
	if len(took) != 1 {
		t.Fatalf("a stopped arming ran: %v", took)
	}
}

// TestValveStaleArrivalLeavesTheNewTravel holds R2-P6-conc-2607 on the
// valve: an Open's arrival that fires after a Close re-armed the travel
// leaves the valve Transitioning towards Closed; the Close's own arrival
// closes it.
func TestValveStaleArrivalLeavesTheNewTravel(t *testing.T) {
	var arrive, closer heldTimers
	v := newDemoValve("valve", 60)
	v.arrive.after, v.closer.after = arrive.after, closer.after
	ctx := context.Background()
	if err := v.Open(ctx, valvedef.OpenRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(ctx); err != nil {
		t.Fatal(err)
	}
	arrive.fire(t, 0)
	if st, _ := v.CurrentState(); st != valvedef.ValveStateTransitioning {
		t.Fatalf("after the Open's stale arrival: %v, want Transitioning", st)
	}
	if len(closer.fns) > 0 {
		closer.fire(t, 0) // the Open's timed close, stopped by Close
	}
	arrive.fire(t, 1)
	if st, _ := v.CurrentState(); st != valvedef.ValveStateClosed {
		t.Fatalf("after the Close's arrival: %v, want Closed", st)
	}
}

// TestGarageStaleCallbacksLeaveTheNewAction holds R2-P6-conc-2651 (a): a
// travel's or calibration's callback that fires after a newer MoveTo,
// Calibrate or Stop replaced it reports nothing and leaves the newer
// timer in place, where Stop still stops it. Stale callbacks return before
// they reach the server, so the garage needs none.
func TestGarageStaleCallbacksLeaveTheNewAction(t *testing.T) {
	var travel, calibration heldTimers
	g := newDemoGarage("garage")
	g.travel.after, g.calibration.after = travel.after, calibration.after
	ctx := context.Background()
	if err := g.MoveTo(ctx, ccldef.TargetPositionMoveToFullyOpen); err != nil {
		t.Fatal(err)
	}
	if err := g.MoveTo(ctx, ccldef.TargetPositionMoveToFullyClosed); err != nil {
		t.Fatal(err)
	}
	travel.fire(t, 0)
	g.mu.Lock()
	armed := g.travel.armedLocked()
	g.mu.Unlock()
	if !armed {
		t.Fatal("the first travel's stale arrival dropped the second travel's timer")
	}
	if err := g.Calibrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := g.Calibrate(ctx); err != nil {
		t.Fatal(err)
	}
	calibration.fire(t, 0)
	g.mu.Lock()
	armed = g.calibration.armedLocked()
	g.mu.Unlock()
	if !armed {
		t.Fatal("the first calibration's stale end dropped the second's timer")
	}
	g.mu.Lock()
	g.travel.stopLocked()
	g.calibration.stopLocked()
	g.mu.Unlock()
	travel.fire(t, 1)
	calibration.fire(t, 1)
}

// TestPanelStaleTickStartsNoSecondChain holds R2-P6-conc-2651 (b): a tick
// of the chain a newer start replaced does nothing — it neither touches
// the server nor arms a second chain.
func TestPanelStaleTickStartsNoSecondChain(t *testing.T) {
	var motion heldTimers
	p := newDemoPanel("panel")
	p.motion.after = motion.after
	p.start(panelStepValue)
	p.start(panelPositionStep)
	motion.fire(t, 0)
	if len(motion.fns) != 2 {
		t.Fatalf("%d armings, want 2: the stale tick armed a chain", len(motion.fns))
	}
	p.mu.Lock()
	armed := p.motion.armedLocked()
	p.mu.Unlock()
	if !armed {
		t.Fatal("the stale tick consumed the new chain's arming")
	}
}

// TestWaterHeaterStaleBoostExpiryLeavesTheNewBoost holds R2-P6-conc-2651
// (d): the expiry of a boost a newer Boost re-armed does not end the new
// boost.
func TestWaterHeaterStaleBoostExpiryLeavesTheNewBoost(t *testing.T) {
	var ends heldTimers
	w := newDemoWaterHeater("heater")
	w.boostEnds.after = ends.after
	w.mu.Lock()
	w.boosting = true
	w.boostEnds.armLocked(time.Minute, w.boostExpired)
	w.boostEnds.armLocked(time.Minute, w.boostExpired)
	w.mu.Unlock()
	ends.fire(t, 0)
	w.mu.Lock()
	boosting := w.boosting
	w.mu.Unlock()
	if !boosting {
		t.Fatal("the first boost's stale expiry ended the second boost")
	}
}

// TestWasherStaleCountdownTickWritesNothing holds R2-P6-conc-2651 (c): a
// countdown tick that fired before a Stop and runs after it writes no
// CountdownTime over the Stop's null.
func TestWasherStaleCountdownTickWritesNothing(t *testing.T) {
	w := newDemoWasher("washer")
	if _, err := w.device(bridged("washer", w.name)); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	w.remaining = 30
	w.mu.Unlock()
	w.startCountdown()
	w.mu.Lock()
	stop := w.ticking
	w.mu.Unlock()
	w.stopCountdown()
	if err := w.setCountdown(nil); err != nil {
		t.Fatal(err)
	}
	if w.countdownTick(stop) {
		t.Error("the stopped countdown's tick ran")
	}
	if v := w.ops.Get().CountdownTime; v != nil {
		t.Errorf("CountdownTime %v after Stop, want null", *v)
	}
}

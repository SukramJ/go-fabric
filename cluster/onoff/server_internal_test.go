// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package onoff

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
)

// raceClock is the test clock, a [clocktest.Fake], that also keeps the
// last callback armed so the test can run it by hand after its timer has
// been stopped — the race a wall-clock timer has when the callback is
// already due as the timer stops, which the Fake (a stopped timer never
// fires) cannot produce on its own.
type raceClock struct {
	*clocktest.Fake
	f func()
}

func newRaceClock() *raceClock { return &raceClock{Fake: clocktest.New(time.Unix(0, 0))} }

func (c *raceClock) AfterFunc(d time.Duration, f func()) clock.Timer {
	c.f = f
	return c.Fake.AfterFunc(d, f)
}

// TestTickHoldsAt0xFFFF reaches the hold branches of both ticks
// (OnOffServer.ts:240-244, :313-317), which a write to 0xFFFF pre-empts
// by stopping the timer at once (:71-84): a tick that still finds the
// hold value stops its timer and changes nothing.
func TestTickHoldsAt0xFFFF(t *testing.T) {
	t.Parallel()
	s, err := New(Config{Features: FeatureLT, TimeSource: clocktest.New(time.Unix(0, 0)), Initial: map[uint32]any{
		AttrOnOff: true, AttrOnTime: uint16(hold), AttrOffWaitTime: uint16(hold),
	}})
	if err != nil {
		t.Fatal(err)
	}
	s.op.Lock()
	s.timedOn.running, s.delayedOff.running = true, true
	s.timedOnTick()
	s.delayedOffTick()
	running := s.timedOn.running || s.delayedOff.running
	st := s.load()
	s.op.Unlock()
	if running {
		t.Error("a tick at 0xFFFF left its timer running")
	}
	if st.onTime != hold || st.offWaitTime != hold || !st.onOff {
		t.Errorf("state moved: %+v", st)
	}
}

// TestStaleTickDoesNothing: a callback that runs after its timer stopped
// changes nothing.
func TestStaleTickDoesNothing(t *testing.T) {
	t.Parallel()
	rc := newRaceClock()
	s, err := New(Config{Features: FeatureLT, TimeSource: rc})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MatterInvoke(t.Context(), CmdOnWithTimedOff, timedOffReq(50)); err != nil {
		t.Fatal(err)
	}
	stale := rc.f
	s.MatterQuiesce()
	stale()
	if v, _ := s.MatterRead(AttrOnTime); v != uint16(50) {
		t.Errorf("OnTime %v after a stale tick", v)
	}
}

func timedOffReq(onTime uint16) any {
	return onoffdef.OnWithTimedOffRequest{OnTime: onTime}
}

// TestDelayedSceneApplyStaleRun: a delayed scene apply whose timer was
// already due as a change of OnOff dropped it (OnOffServer.ts:62,
// :291-297) changes nothing when it runs.
func TestDelayedSceneApplyStaleRun(t *testing.T) {
	t.Parallel()
	rc := newRaceClock()
	s, err := New(Config{Features: FeatureLT, TimeSource: rc})
	if err != nil {
		t.Fatal(err)
	}
	s.MatterApplySceneValues(t.Context(), map[uint32]uint64{AttrOnOff: 1}, 500)
	due := rc.f
	s.Set(true) // drops the pending value
	s.Set(false)
	due()
	if s.OnOff() {
		t.Fatal("a dropped scene apply turned the server on")
	}
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package transition_test

import (
	"errors"
	"math"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/transition"
)

// attr is one transitionable attribute the tests own, with a log of what
// the engine applied and reported.
type attr struct {
	mu       sync.Mutex
	start    time.Time
	values   map[string]float64
	known    map[string]bool
	applied  []sample
	reports  []int // RemainingTime at each RemainingTimeChanged
	settled  []string
	applyErr error
	engine   *transition.Engine
}

type sample struct {
	ms    int64
	name  string
	value float64
}

func newAttr(manage bool, props map[string]transition.Property, values map[string]float64) *attr {
	a := &attr{start: time.Now(), values: values, known: map[string]bool{}}
	for n := range values {
		a.known[n] = true
	}
	a.engine = transition.New(transition.Config{
		Manage:     manage,
		Properties: props,
		Read: func(name string) (float64, bool) {
			a.mu.Lock()
			defer a.mu.Unlock()
			return a.values[name], a.known[name]
		},
		Apply: func(changes []transition.Change) error {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.applyErr != nil {
				return a.applyErr
			}
			for _, c := range changes {
				a.values[c.Name], a.known[c.Name] = c.Value, true
				a.applied = append(a.applied, sample{time.Since(a.start).Milliseconds(), c.Name, c.Value})
			}
			return nil
		},
		RemainingTimeChanged: func() {
			rt := a.engine.RemainingTime()
			a.mu.Lock()
			a.reports = append(a.reports, rt)
			a.mu.Unlock()
		},
		Settled: func(name string) {
			a.mu.Lock()
			a.settled = append(a.settled, name)
			a.mu.Unlock()
		},
	})
	return a
}

func (a *attr) value(name string) float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.values[name]
}

func (a *attr) reported() []int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.reports)
}

func (a *attr) appliedAt(ms int64) (float64, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.applied {
		if s.ms == ms {
			return s.value, true
		}
	}
	return 0, false
}

// sleep advances the bubble's clock and lets every timer that came due run.
func sleep(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

var level = map[string]transition.Property{"level": {Min: 1, Max: 254}}

// TestStepCadenceRoundingAndFinish ports matter.js
// packages/node/test/behaviors/level-control/LevelControlServerTest.ts
// "transitions to off with correct events": CurrentLevel 128 to 1 over
// 4 s. Steps every 100 ms; the first accounts for 200 ms (prevStepAt is
// seeded one interval back), so the transition ends at 3.9 s; values are
// rounded half up; RemainingTime reports 40 at the start and 0 at the end.
func TestStepCadenceRoundingAndFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newAttr(true, level, map[string]float64{"level": 128})
		rate := (1.0 - 128.0) / 40 * 10
		if err := a.engine.Start(transition.Transition{Name: "level", Rate: rate, Target: 1}); err != nil {
			t.Fatal(err)
		}
		if got := a.engine.RemainingTime(); got != 40 {
			t.Fatalf("RemainingTime at the start = %d, want 40", got)
		}
		sleep(50 * time.Millisecond)
		if v := a.value("level"); v != 128 {
			t.Fatalf("value before the first step = %v, want 128", v)
		}
		sleep(50 * time.Millisecond)
		// 128 - 31.75/s * 0.2 s = 121.65
		if v := a.value("level"); v != 122 {
			t.Fatalf("value at the first step = %v, want 122", v)
		}
		sleep(900 * time.Millisecond)
		// The step at 1 s accounts for 1.1 s: 128 - 34.925 = 93.075.
		if v, ok := a.appliedAt(1000); !ok || v != 93 {
			t.Errorf("value at 1 s = %v (%v), want 93", v, ok)
		}
		sleep(2900 * time.Millisecond)
		// matter.js reports 96 / 65 / 33 at each second: the values of the
		// steps 100 ms earlier, which its quieter report carries.
		for ms, want := range map[int64]float64{900: 96, 1900: 65, 2900: 33} {
			if v, ok := a.appliedAt(ms); !ok || v != want {
				t.Errorf("value at %d ms = %v (%v), want %v (matter.js event)", ms, v, ok, want)
			}
		}
		if v, ok := a.appliedAt(3900); !ok || v != 1 {
			t.Errorf("value at 3.9 s = %v (%v), want the target 1", v, ok)
		}
		if a.engine.Active("level") || a.engine.RemainingTime() != 0 {
			t.Error("the transition is still running after it reached its target")
		}
		if got := a.reported(); !slices.Equal(got, []int{40, 0}) {
			t.Errorf("RemainingTime reports %v, want [40 0]", got)
		}
		if !slices.Equal(a.settled, []string{"level"}) {
			t.Errorf("settled %v, want [level]", a.settled)
		}
		// Nothing runs afterwards.
		n := len(a.applied)
		sleep(time.Hour)
		if len(a.applied) != n {
			t.Error("the engine applied a value after its transition ended")
		}
	})
}

// TestRemainingTimeReportsOnCommandChanges ports matter.js
// LevelControlServerTest.ts "emits RemainingTime with command changes":
// a second command whose remaining time differs from the reported one by
// one second or less is not reported, so 1→254 in 15 s twice reports
// [150 0].
func TestRemainingTimeReportsOnCommandChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newAttr(true, level, map[string]float64{"level": 1})
		moveTo := func() {
			rate := (254 - a.value("level")) / 150 * 10
			if err := a.engine.Start(transition.Transition{Name: "level", Rate: rate, Target: 254}); err != nil {
				t.Fatal(err)
			}
		}
		moveTo()
		sleep(5 * time.Second)
		moveTo()
		sleep(20 * time.Second)
		if got := a.reported(); !slices.Equal(got, []int{150, 0}) {
			t.Errorf("RemainingTime reports %v, want [150 0]", got)
		}
		if v := a.value("level"); v != 254 {
			t.Errorf("level = %v, want 254", v)
		}
	})
}

// TestRemainingTimeReportsTCLVL23 is the RemainingTime half of
// TC-LVL-2.3 (src/python_testing/TC_LVL_2_3.py steps 16-23): 10 s, a new
// 15 s command after 5 s, then the end — three reports, ~100, ~150, 0.
func TestRemainingTimeReportsTCLVL23(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newAttr(true, level, map[string]float64{"level": 254})
		start := func(tenths float64) {
			rate := (1 - a.value("level")) / tenths * 10
			if err := a.engine.Start(transition.Transition{Name: "level", Rate: rate, Target: 1}); err != nil {
				t.Fatal(err)
			}
		}
		start(100)
		sleep(5 * time.Second)
		start(150)
		sleep(20 * time.Second)
		got := a.reported()
		if len(got) != 3 || got[0] < 95 || got[0] > 100 || got[1] < 145 || got[1] > 150 || got[2] != 0 {
			t.Errorf("RemainingTime reports %v, want [~100 ~150 0]", got)
		}
	})
}

// TestShortTransitionIsNotReported: "For commands with a transition time
// … less than 1 second, changes to this attribute SHALL NOT be reported"
// (matter.js Transitions.start, suppressReportingRemainingTimeOnFinish).
func TestShortTransitionIsNotReported(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newAttr(true, level, map[string]float64{"level": 100})
		if err := a.engine.Start(transition.Transition{Name: "level", Rate: 200, Target: 200}); err != nil {
			t.Fatal(err)
		}
		if got := a.engine.RemainingTime(); got != 5 {
			t.Fatalf("RemainingTime = %d, want 5", got)
		}
		sleep(time.Second)
		if v := a.value("level"); v != 200 {
			t.Fatalf("level = %v, want 200", v)
		}
		if got := a.reported(); len(got) != 0 {
			t.Errorf("RemainingTime reports %v, want none", got)
		}
		if !slices.Equal(a.settled, []string{"level"}) {
			t.Errorf("settled %v: the quieter report is still flushed at the end", a.settled)
		}
	})
}

// TestCancelReportsZero ports matter.js
// packages/node/test/behaviors/level-control/RemainingTimeTest.ts "reads
// zero and reports it when a stop ends the transition" and "reports
// nothing for a stop with no transition underway".
func TestCancelReportsZero(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newAttr(true, level, map[string]float64{"level": 1})
		a.engine.CancelAll()
		if got := a.reported(); len(got) != 0 {
			t.Fatalf("a stop with nothing running reported %v", got)
		}
		if err := a.engine.Start(transition.Transition{Name: "level", Rate: 253.0 / 15, Target: 254}); err != nil {
			t.Fatal(err)
		}
		sleep(time.Second)
		a.engine.CancelAll()
		if got := a.reported(); !slices.Equal(got, []int{150, 0}) {
			t.Errorf("RemainingTime reports %v, want [150 0]", got)
		}
		at := a.value("level")
		if at <= 1 || at >= 254 {
			t.Errorf("level %v: a stop holds the value it had reached", at)
		}
		sleep(time.Minute)
		if a.value("level") != at || a.engine.RemainingTime() != 0 {
			t.Error("the transition went on after the stop")
		}
		if !slices.Equal(a.settled, []string{"level"}) {
			t.Errorf("settled %v, want [level]: a stop flushes the quieter report", a.settled)
		}
	})
}

// TestCancelOneOfTwo: ending the longer of two transitions shortens the
// remaining time, which is reported when it changes by more than a second
// (matter.js Transitions.cancel with other transitions ongoing).
func TestCancelOneOfTwo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		props := map[string]transition.Property{"x": {Max: 1000}, "y": {Max: 1000}}
		a := newAttr(true, props, map[string]float64{"x": 0, "y": 0})
		_ = a.engine.Start(transition.Transition{Name: "x", Rate: 10, Target: 100}) // 10 s
		_ = a.engine.Start(transition.Transition{Name: "y", Rate: 10, Target: 300}) // 30 s
		a.engine.Cancel("y")
		if got := a.reported(); !slices.Equal(got, []int{100, 300, 100}) {
			t.Errorf("RemainingTime reports %v, want [100 300 100]", got)
		}
		sleep(20 * time.Second)
		if got := a.reported(); !slices.Equal(got, []int{100, 300, 100, 0}) {
			t.Errorf("RemainingTime reports %v, want [100 300 100 0]", got)
		}
	})
}

// TestImmediate: a rate of 0, NaN or ±Inf (a TransitionTime of 0) and an
// engine that does not manage transitions apply the target at once,
// cropped to the bounds, without a RemainingTime report (matter.js
// Transitions.start, immediate branch).
func TestImmediate(t *testing.T) {
	for name, tc := range map[string]struct {
		manage bool
		rate   float64
		target float64
		want   float64
	}{
		"zero rate":           {true, 0, 60, 60},
		"NaN rate (0/0)":      {true, math.NaN(), 60, 60},
		"infinite rate (x/0)": {true, math.Inf(-1), 60, 60},
		"unmanaged":           {false, 5, 60, 60},
		"cropped to max":      {true, 0, 300, 254},
		"cropped to min":      {true, math.Inf(1), -5, 1},
		"Move up, no rate":    {true, 0, math.Inf(1), 254},
		"Move down, no rate":  {true, 0, math.Inf(-1), 1},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a := newAttr(tc.manage, level, map[string]float64{"level": 100})
				if err := a.engine.Start(transition.Transition{Name: "level", Rate: tc.rate, Target: tc.target}); err != nil {
					t.Fatal(err)
				}
				if v := a.value("level"); v != tc.want {
					t.Errorf("level = %v, want %v", v, tc.want)
				}
				if a.engine.Active("level") || a.engine.RemainingTime() != 0 || len(a.reported()) != 0 {
					t.Error("an immediate transition left something running or reported")
				}
			})
		})
	}
}

// TestBoundsAndNoTarget: a bounded property moves to the bound in the
// direction of travel when it has no target or its target lies beyond the
// bound, and a transition already at that bound does not start (matter.js
// #determineTargetValue and start).
func TestBoundsAndNoTarget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newAttr(true, level, map[string]float64{"level": 250})
		if err := a.engine.Start(transition.Transition{Name: "level", Rate: 10, Target: math.Inf(1)}); err != nil {
			t.Fatal(err)
		}
		if got := a.engine.RemainingTime(); got != 4 {
			t.Errorf("RemainingTime towards +Inf = %d, want 4 (to 254)", got)
		}
		sleep(time.Second)
		if v := a.value("level"); v != 254 || a.engine.Active("level") {
			t.Fatalf("level = %v, want 254 and done", v)
		}
		if err := a.engine.Start(transition.Transition{Name: "level", Rate: 10, NoTarget: true}); err != nil {
			t.Fatal(err)
		}
		if a.engine.Active("level") {
			t.Error("a move up started at the maximum")
		}
		if err := a.engine.Start(transition.Transition{Name: "level", Rate: -100, Target: -40}); err != nil {
			t.Fatal(err)
		}
		sleep(5 * time.Second)
		if v := a.value("level"); v != 1 {
			t.Errorf("level = %v, want the minimum 1", v)
		}
	})
}

// TestMaximumTransitionTime: TransitionTime 0xFFFE (the largest a
// MoveToLevel carries, 6553.4 s) runs gradually and reports it.
func TestMaximumTransitionTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newAttr(true, level, map[string]float64{"level": 1})
		if err := a.engine.Start(transition.Transition{Name: "level", Rate: 253.0 / 0xFFFE * 10, Target: 254}); err != nil {
			t.Fatal(err)
		}
		if got := a.reported(); !slices.Equal(got, []int{0xFFFE}) {
			t.Fatalf("RemainingTime reports %v, want [65534]", got)
		}
		sleep(10 * time.Second)
		if got := a.engine.RemainingTime(); got < 0xFFFE-101 || got > 0xFFFE-99 {
			t.Errorf("RemainingTime after 10 s = %d, want about %d", got, 0xFFFE-100)
		}
		a.engine.StopAll()
	})
}

// TestReplace: a new transition of the same property replaces the running
// one silently; one timer drives both properties of an engine.
func TestReplace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newAttr(true, level, map[string]float64{"level": 1})
		_ = a.engine.Start(transition.Transition{Name: "level", Rate: 10, Target: 254})
		sleep(time.Second)
		_ = a.engine.Start(transition.Transition{Name: "level", Rate: -10, Target: 1})
		sleep(1500 * time.Millisecond)
		if v := a.value("level"); v != 1 || a.engine.Active("level") {
			t.Errorf("level = %v, want the replacing transition's target 1", v)
		}
	})
}

// TestStopAllLeavesNoTimer: an engine whose transitions were stopped owns
// no timer — nothing is applied however long the clock runs.
func TestStopAllLeavesNoTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		props := map[string]transition.Property{"a": {Max: 100, Cyclic: true}, "b": {Max: 100}}
		a := newAttr(true, props, map[string]float64{"a": 0, "b": 0})
		_ = a.engine.Start(transition.Transition{Name: "a", Rate: 1, NoTarget: true}) // endless
		_ = a.engine.Start(transition.Transition{Name: "b", Rate: 1, Target: 100})
		sleep(time.Second)
		a.engine.StopAll()
		a.mu.Lock()
		n := len(a.applied)
		a.mu.Unlock()
		sleep(24 * time.Hour)
		a.mu.Lock()
		defer a.mu.Unlock()
		if len(a.applied) != n {
			t.Errorf("%d values applied after StopAll", len(a.applied)-n)
		}
		if len(a.reports) != 1 || len(a.settled) != 0 {
			t.Errorf("StopAll reported %v / settled %v; it ends without a report", a.reports, a.settled)
		}
	})
}

// TestApplyErrorAborts: a host that refuses a step ends every transition,
// as matter.js aborts on an unhandled error during a step.
func TestApplyErrorAborts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newAttr(true, level, map[string]float64{"level": 1})
		_ = a.engine.Start(transition.Transition{Name: "level", Rate: 10, Target: 254})
		sleep(time.Second)
		a.mu.Lock()
		a.applyErr = errors.New("device gone")
		a.mu.Unlock()
		sleep(time.Second)
		if a.engine.Active("level") {
			t.Error("the transition survived a failed step")
		}
		if err := a.engine.Start(transition.Transition{Name: "level", Target: 50}); err == nil {
			t.Error("an immediate transition swallowed Apply's error")
		}
	})
}

// TestHueDistanceByDirection pins matter.js ColorControlServer
// #getHueDistanceByDirection for the four directions.
func TestHueDistanceByDirection(t *testing.T) {
	const maxHue = 254
	for _, tc := range []struct {
		current, target float64
		dir             transition.HueDirection
		want            float64
	}{
		{128, 129, transition.HueUp, 1},
		{128, 129, transition.HueDown, -253},
		{128, 129, transition.HueShortest, 1},
		{128, 129, transition.HueLongest, -253},
		{200, 10, transition.HueUp, 65},
		{200, 10, transition.HueDown, -189},
		{200, 10, transition.HueShortest, 65},
		{200, 10, transition.HueLongest, -189},
		{10, 200, transition.HueShortest, -64},
		{10, 200, transition.HueLongest, 190},
		{50, 50, transition.HueUp, 255}, // an equal hue is a whole turn
		{50, 50, 9, 0},                  // unknown direction
	} {
		if got := transition.HueDistance(tc.current, tc.target, tc.dir, maxHue); got != tc.want {
			t.Errorf("HueDistance(%v, %v, %v) = %v, want %v", tc.current, tc.target, tc.dir, got, tc.want)
		}
	}
}

// TestCyclicHueDownwards ports matter.js
// packages/node/test/behaviors/color-control/ColorControlServerTest.ts
// "transitions cyclic hue downwards with correct events": hue 128 to 129
// downwards in 6 s wraps through 0, covers 253 units and ends on 129.
// matter.js's quieter events carry 86, 44, 1, 213, 171 at each second
// (the values of the steps 100 ms earlier, on its mock clock); the values
// here are within one unit of them.
func TestCyclicHueDownwards(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		props := map[string]transition.Property{"hue": {Min: 0, Max: 254, Cyclic: true}}
		a := newAttr(true, props, map[string]float64{"hue": 128})
		distance := func(c, tg float64) float64 { return transition.HueDistance(c, tg, transition.HueDown, 254) }
		rate := distance(128, 129) / 60 * 10
		if err := a.engine.Start(transition.Transition{Name: "hue", Rate: rate, Target: 129, CyclicDistance: distance}); err != nil {
			t.Fatal(err)
		}
		if got := a.reported(); !slices.Equal(got, []int{60}) {
			t.Fatalf("RemainingTime reports %v, want [60]", got)
		}
		sleep(6 * time.Second)
		for ms, want := range map[int64]float64{900: 86, 1900: 44, 2900: 1, 3900: 213, 4900: 171} {
			v, ok := a.appliedAt(ms)
			if !ok || math.Abs(v-want) > 1 {
				t.Errorf("hue at %d ms = %v (%v), want %v ± 1", ms, v, ok, want)
			}
		}
		if v, ok := a.appliedAt(5900); !ok || v != 129 {
			t.Errorf("hue at 5.9 s = %v (%v), want the target 129", v, ok)
		}
		if a.engine.Active("hue") {
			t.Error("the cyclic transition did not end")
		}
		if got := a.reported(); !slices.Equal(got, []int{60, 0}) {
			t.Errorf("RemainingTime reports %v, want [60 0]", got)
		}
	})
}

// TestCyclicEndless: a cyclic property without a target moves until it is
// stopped, wrapping, and reports no remaining time (a color loop).
func TestCyclicEndless(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		props := map[string]transition.Property{"hue": {Min: 0, Max: 0xFFFF, Cyclic: true}}
		a := newAttr(true, props, map[string]float64{"hue": 0xFF00})
		_ = a.engine.Start(transition.Transition{Name: "hue", Rate: 0xFFFF / 25.0, NoTarget: true})
		if a.engine.RemainingTime() != 0 {
			t.Error("an endless transition reports a remaining time")
		}
		sleep(time.Minute)
		if !a.engine.Active("hue") {
			t.Fatal("the endless transition ended by itself")
		}
		a.mu.Lock()
		wrapped := false
		for i := 1; i < len(a.applied); i++ {
			wrapped = wrapped || a.applied[i].value < a.applied[i-1].value
		}
		a.mu.Unlock()
		if !wrapped {
			t.Error("the value never wrapped")
		}
		a.engine.StopAll()
	})
}

// TestDirectionalDistance pins matter.js
// Transitions.calculateCyclicDistance, the default cyclic distance.
func TestDirectionalDistance(t *testing.T) {
	for _, tc := range []struct{ current, target, rate, want float64 }{
		{10, 20, 1, 10},
		{20, 10, 1, (100 - 20) + (100 - 10)},
		{20, 10, -1, 10},
		{10, 20, -1, 10 + 20},
	} {
		if got := transition.DirectionalDistance(tc.current, tc.target, tc.rate, 0, 100); got != tc.want {
			t.Errorf("DirectionalDistance(%v, %v, %v) = %v, want %v", tc.current, tc.target, tc.rate, got, tc.want)
		}
	}
}

// TestConcurrentCommands drives an engine from several goroutines while
// it steps; run with -race.
func TestConcurrentCommands(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := newAttr(true, level, map[string]float64{"level": 1})
		var wg sync.WaitGroup
		for i := range 4 {
			wg.Go(func() {
				for j := range 20 {
					switch (i + j) % 4 {
					case 0:
						_ = a.engine.Start(transition.Transition{Name: "level", Rate: 50, Target: 254})
					case 1:
						_ = a.engine.Start(transition.Transition{Name: "level", Rate: -50, Target: 1})
					case 2:
						a.engine.CancelAll()
					default:
						_ = a.engine.RemainingTime()
					}
					time.Sleep(70 * time.Millisecond)
				}
			})
		}
		wg.Wait()
		a.engine.StopAll()
	})
}

// TestStopFinishAndEdgeCases covers the remaining entry points: Stop ends
// a transition silently, Finish / FinishAll end it as completed (matter.js
// Transitions.stop / finish), an unknown current value or a value already
// at its target starts nothing, and an engine without a RemainingTime
// report still steps.
func TestStopFinishAndEdgeCases(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		props := map[string]transition.Property{"x": {Max: 100}, "y": {Max: 100}, "h": {Max: 99, Cyclic: true}}
		a := newAttr(true, props, map[string]float64{"x": 0, "y": 0, "h": 90})

		_ = a.engine.Start(transition.Transition{Name: "x", Rate: 1, Target: 50})
		a.engine.Stop("x")
		if a.engine.Active("x") || len(a.settled) != 0 {
			t.Error("Stop reported or left the transition running")
		}

		_ = a.engine.Start(transition.Transition{Name: "x", Rate: 1, Target: 50})
		_ = a.engine.Start(transition.Transition{Name: "y", Rate: 1, Target: 60})
		a.engine.Finish("x")
		if a.engine.Active("x") || !a.engine.Active("y") {
			t.Error("Finish(x) did not end x alone")
		}
		a.engine.FinishAll()
		if a.engine.Active("y") || !slices.Equal(a.settled, []string{"x", "y"}) {
			t.Errorf("FinishAll left y running or settled %v", a.settled)
		}
		if got := a.reported(); !slices.Equal(got, []int{500, 600, 0}) {
			t.Errorf("RemainingTime reports %v, want [500 600 0]", got)
		}

		// The default cyclic distance: 90 up to 10 runs |90-99| + |99-10|
		// = 98 units (matter.js calculateCyclicDistance), 9.8 s at 10/s.
		_ = a.engine.Start(transition.Transition{Name: "h", Rate: 10, Target: 10})
		if got := a.engine.RemainingTime(); got != 98 {
			t.Errorf("cyclic RemainingTime = %d, want 98", got)
		}
		sleep(10 * time.Second)
		if v := a.value("h"); v != 10 {
			t.Errorf("h = %v, want 10", v)
		}

		// Already at the target; and a null value cannot step.
		_ = a.engine.Start(transition.Transition{Name: "h", Rate: 10, Target: 10})
		a.mu.Lock()
		a.known["x"] = false
		a.mu.Unlock()
		_ = a.engine.Start(transition.Transition{Name: "x", Rate: 1, Target: 50})
		if a.engine.Active("h") || a.engine.Active("x") {
			t.Error("a transition started at its target, or from a null value")
		}
		// A null value is still set by an immediate transition.
		_ = a.engine.Start(transition.Transition{Name: "x", Target: 7})
		if v := a.value("x"); v != 7 {
			t.Errorf("x = %v, want 7", v)
		}

		quiet := transition.New(transition.Config{
			Manage:     true,
			Properties: map[string]transition.Property{"z": {Max: 10}},
			Read:       func(string) (float64, bool) { return 0, true },
			Apply:      func([]transition.Change) error { return nil },
		})
		_ = quiet.Start(transition.Transition{Name: "z", Rate: 10, Target: 10})
		quiet.CancelAll()
		_ = quiet.Start(transition.Transition{Name: "z", Rate: 10, NoTarget: true})
		sleep(2 * time.Second)
		quiet.StopAll()
	})
}

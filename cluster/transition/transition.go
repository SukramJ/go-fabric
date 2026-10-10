// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package transition is the module's port of matter.js's attribute
// transition engine (packages/node/src/behavior/Transitions.ts): it moves
// one or more numeric attributes of one cluster instance towards a target
// over time, in steps, and computes the RemainingTime the LevelControl and
// ColorControl clusters report while it does.
//
// A cluster server owns one [Engine] and drives it from its commands; the
// engine never touches an attribute itself. It reads the current value
// through [Config.Read] and hands every value it steps to through
// [Config.Apply], so the server decides where a value goes — to a host
// device that cannot ramp by itself, to its own state, or both.
//
// What follows matter.js exactly:
//
//   - a transition whose rate is 0, not a number or infinite applies its
//     target at once, cropped to the property's bounds, and so does every
//     transition when [Config.Manage] is false (matter.js
//     manageTransitions);
//   - a gradual transition steps every [Config.StepInterval] (100 ms by
//     default), carries its value as a float and applies it rounded half
//     up, the way JavaScript's Math.round does; the first step accounts for
//     one interval more than has passed (matter.js seeds prevStepAt one
//     interval in the past), so a transition ends one interval early;
//   - a cyclic property (hue) wraps between its bounds and ends once the
//     distance its direction implies has been covered; a bounded one ends
//     at its target, or at the bound in the direction of travel when it
//     has none or the target lies beyond the bound;
//   - RemainingTime is the longest remaining transition, in tenths of a
//     second, and it is reported by the rules of #updateRemainingTime: a
//     command reports a remaining time of at least one second that differs
//     by more than one second from the last reported one, a transition
//     shorter than one second is never reported, and the end of every
//     transition — completed ([Engine.Finish]) or ended by a command
//     ([Engine.Cancel]) — reports 0 when anything else was reported;
//   - the end of a property's transition flushes that property's quieter
//     report ([Config.Settled]): matter.js QuietObservable.emitNow.
//
// Two deliberate differences, both because the engine does not observe the
// attribute between its own steps (matter.js instruments `$Changed`):
// a step whose value already equals the target ends the transition even
// when the attribute did not change, and a value changed by someone else
// is not noticed until the next step overwrites it.
//
// Concurrency: an idle engine owns no goroutine and no timer. A running
// transition owns one [time.AfterFunc] timer, re-armed per step and
// stopped when the last transition ends, so everything runs on the clock
// [testing/synctest] controls. Engine methods are safe for concurrent
// use; the callbacks run with the engine's operation lock held and must
// not call back into Start, Stop, Finish or Cancel — [Engine.RemainingTime]
// and [Engine.Active] are safe from them.
package transition

import (
	"math"
	"sync"
	"time"
)

// DefaultStepInterval is the time between two steps of a gradual
// transition: matter.js Transitions.DEFAULT_STEP_INTERVAL, the
// LevelControl / ColorControl transitionStepInterval default.
const DefaultStepInterval = 100 * time.Millisecond

// ExternalTimeUnit is the unit RemainingTime is reported in, a tenth of a
// second: matter.js Transitions.DEFAULT_EXTERNAL_TIME_UNIT.
const ExternalTimeUnit = 100 * time.Millisecond

// reportFloor is the remaining time, in tenths, a command must exceed to
// be reported, and the change it must exceed (matter.js
// #updateRemainingTime and start: "less than 1 second … SHALL NOT be
// reported").
const reportFloor = 10

// Property bounds one transitionable attribute (matter.js
// Transitions.PropertyConfiguration).
type Property struct {
	// Min and Max bound the value. Every property the Matter clusters
	// transition has both.
	Min, Max float64
	// Cyclic wraps the value from Max to Min and back (hue).
	Cyclic bool
}

// Change is one value [Config.Apply] receives: the property and its new,
// rounded value.
type Change struct {
	Name  string
	Value float64
}

// Config is an engine's configuration (matter.js
// Transitions.Configuration).
type Config struct {
	// Manage makes transitions gradual. Without it every transition
	// applies its target at once (matter.js manageTransitions).
	Manage bool
	// StepInterval is the time between steps; zero is
	// [DefaultStepInterval].
	StepInterval time.Duration
	// Properties names the transitionable properties.
	Properties map[string]Property
	// Read returns a property's current value; ok=false means it is
	// unknown (a null attribute).
	Read func(name string) (value float64, ok bool)
	// Apply writes the values of one step, or of an immediate transition.
	// An error ends every transition of the engine without a report, as
	// matter.js aborts on an unhandled error during a step.
	Apply func(changes []Change) error
	// RemainingTimeChanged reports that RemainingTime is to be reported
	// (matter.js remainingTimeEvent). Nil disables RemainingTime
	// reporting.
	RemainingTimeChanged func()
	// Settled reports that a property's transition ended, at its target
	// or by a command: its quieter report is due now (matter.js
	// quiet.emitNow). Optional.
	Settled func(name string)
}

// Transition is one transition request (matter.js
// Transitions.Transition).
type Transition struct {
	// Name is the property.
	Name string
	// Rate is the change per second, signed. 0, NaN and ±Inf apply the
	// target at once.
	Rate float64
	// RateFrom computes Rate from the property's current value, read
	// inside [Engine.Start] under the engine's lock, and replaces Rate
	// when set. matter.js reads the current value and starts the
	// transition in one synchronous handler (moveToColorTemperatureLogic,
	// moveToSaturationLogic, LevelControl moveToLevelLogic), so a running
	// step can never land between the two; a Go caller that reads the
	// value itself and then calls Start races the step goroutine, and a
	// step landing in between yields a rate from the old value and a
	// RemainingTime from the new one (one tenth of a second short).
	// Optional; a property whose value is unknown keeps Rate.
	RateFrom func(current float64) float64
	// Target is the value the transition ends at. ±Inf is allowed and
	// means the bound in that direction (LevelControl Move).
	Target float64
	// NoTarget leaves Target unset: a bounded property then moves to the
	// bound in the direction of Rate, a cyclic one moves until stopped.
	NoTarget bool
	// CyclicDistance overrides the distance a cyclic transition covers
	// from current to target (hue direction rules). Nil uses
	// [DirectionalDistance].
	CyclicDistance func(current, target float64) float64
	// OnStep runs before every step's value is applied, with that value
	// (matter.js onStep). Optional.
	OnStep func(value float64)
}

// state is one running transition (matter.js Transitions.PropertyState).
type state struct {
	t           Transition
	current     float64
	changePerMs float64
	prevStepAt  time.Time
	distance    float64
	hasDistance bool
	// quietFinish suppresses the RemainingTime report at the end of a
	// transition shorter than one second.
	quietFinish bool
}

// Engine runs the transitions of one cluster instance.
type Engine struct {
	cfg Config

	// op serialises the operations, callbacks included.
	op sync.Mutex

	// mu guards everything below; it is never held across a callback.
	mu            sync.Mutex
	order         []string // running properties, in start order
	states        map[string]*state
	timer         *time.Timer
	gen           uint64 // identifies the armed timer
	nextTick      time.Time
	prevPublished int
}

// New returns an idle engine.
func New(cfg Config) *Engine {
	if cfg.StepInterval <= 0 {
		cfg.StepInterval = DefaultStepInterval
	}
	return &Engine{cfg: cfg, states: map[string]*state{}}
}

// Active reports whether name is transitioning (matter.js stateOf).
func (e *Engine) Active(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.states[name]
	return ok
}

// immediate reports whether a rate applies its target at once (matter.js
// `!changePerS || !Number.isFinite(changePerS)`).
func immediate(rate float64) bool {
	return rate == 0 || math.IsNaN(rate) || math.IsInf(rate, 0)
}

// Start begins a transition of t.Name, replacing any it has (matter.js
// Transitions.start). An immediate transition is applied before Start
// returns, and Apply's error is returned.
func (e *Engine) Start(t Transition) error {
	e.op.Lock()
	defer e.op.Unlock()
	e.stopLocked(t.Name)

	current, known := e.cfg.Read(t.Name)
	if t.RateFrom != nil && known {
		t.Rate = t.RateFrom(current)
	}
	if known && !t.NoTarget && current == t.Target {
		return nil
	}
	prop := e.cfg.Properties[t.Name]
	if !e.cfg.Manage || immediate(t.Rate) {
		if t.NoTarget {
			return nil
		}
		target := min(max(t.Target, prop.Min), prop.Max)
		if known && jsRound(target) == current {
			return nil
		}
		return e.cfg.Apply([]Change{{Name: t.Name, Value: jsRound(target)}})
	}
	if !known {
		// matter.js steps a value that is not a number into a step error,
		// which ends the transition at its first step.
		return nil
	}

	st := &state{t: t, current: current, changePerMs: t.Rate / 1000}
	if prop.Cyclic {
		if !t.NoTarget {
			distance := t.CyclicDistance
			if distance == nil {
				distance = func(c, tg float64) float64 { return DirectionalDistance(c, tg, t.Rate, prop.Min, prop.Max) }
			}
			st.distance, st.hasDistance = math.Abs(distance(current, t.Target)), true
			if st.distance == 0 {
				return nil
			}
		}
	} else if (t.Rate < 0 && current == prop.Min) || (t.Rate > 0 && current == prop.Max) {
		return nil
	}

	now := time.Now()
	st.prevStepAt = now.Add(-e.cfg.StepInterval)
	e.mu.Lock()
	e.states[t.Name] = st
	e.order = append(e.order, t.Name)
	if e.timer == nil {
		e.armLocked(now.Add(e.cfg.StepInterval))
	}
	e.mu.Unlock()

	if e.cfg.RemainingTimeChanged == nil {
		return nil
	}
	remaining := e.RemainingTime()
	if remaining < reportFloor {
		e.mu.Lock()
		st.quietFinish = true
		e.mu.Unlock()
		return nil
	}
	e.updateRemainingTime(remaining, true)
	return nil
}

// DirectionalDistance is matter.js Transitions.calculateCyclicDistance:
// the distance from current to target moving in the direction of rate,
// wrapping through max (upwards) or min (downwards).
func DirectionalDistance(current, target, rate, minValue, maxValue float64) float64 {
	if rate > 0 {
		if current > target {
			return math.Abs(current-maxValue) + math.Abs(maxValue-target)
		}
	} else if current < target {
		return math.Abs(current-minValue) + math.Abs(minValue-target)
	}
	return math.Abs(current - target)
}

// HueDirection is ColorControl's DirectionEnum, the way a hue command
// travels round the colour wheel.
type HueDirection uint8

// DirectionEnum values (color-control.element.ts).
const (
	HueShortest HueDirection = 0
	HueLongest  HueDirection = 1
	HueUp       HueDirection = 2
	HueDown     HueDirection = 3
)

// HueDistance is matter.js ColorControlServer #getHueDistanceByDirection:
// the signed distance from current to target in direction on a wheel whose
// largest value is maxHue (254, or 65535 for the enhanced hue). Upwards
// it wraps past maxHue; an equal hue is a whole turn. The sign is the
// direction of travel, so distance / TransitionTime is the rate. An
// unknown direction, which matter.js treats as an implementation error,
// is no distance at all.
func HueDistance(current, target float64, direction HueDirection, maxHue float64) float64 {
	distance := maxHue + target + 1 - current
	if target > current {
		distance = target - current
	}
	if distance == 0 {
		return 0
	}
	switch direction {
	case HueUp:
		return distance
	case HueDown:
		return -(maxHue - distance)
	case HueShortest:
		if math.Abs(distance) > maxHue/2 {
			return -(maxHue - distance)
		}
		return distance
	case HueLongest:
		if math.Abs(distance) > maxHue/2 {
			return distance
		}
		return -(maxHue - distance)
	}
	return 0
}

// Stop ends the transition of name without a report (matter.js
// Transitions.stop(name)).
func (e *Engine) Stop(name string) {
	e.op.Lock()
	defer e.op.Unlock()
	e.stopLocked(name)
}

// StopAll ends every transition without a report (matter.js
// Transitions.stop()). The engine stays usable; this is also what a
// server runs when it leaves the topology, so no timer outlives it.
func (e *Engine) StopAll() {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	e.order, e.states = nil, map[string]*state{}
	e.disarmLocked()
	e.mu.Unlock()
}

// stopLocked is stop(name). The caller holds op.
func (e *Engine) stopLocked(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.states[name]; !ok {
		return
	}
	delete(e.states, name)
	for i, n := range e.order {
		if n == name {
			e.order = append(e.order[:i:i], e.order[i+1:]...)
			break
		}
	}
	if len(e.states) == 0 {
		e.disarmLocked()
	}
}

// Finish ends the transition of name as completed (matter.js
// Transitions.finish(name)).
func (e *Engine) Finish(name string) {
	e.op.Lock()
	defer e.op.Unlock()
	e.finishLocked([]string{name})
}

// FinishAll ends every transition as completed (matter.js
// Transitions.finish()).
func (e *Engine) FinishAll() {
	e.op.Lock()
	defer e.op.Unlock()
	e.finishLocked(e.names())
}

// finishLocked is matter.js finish. The caller holds op.
func (e *Engine) finishLocked(names []string) {
	report := false
	for _, name := range names {
		e.mu.Lock()
		st, ok := e.states[name]
		e.mu.Unlock()
		if !ok {
			continue
		}
		e.stopLocked(name)
		if e.cfg.Settled != nil {
			e.cfg.Settled(name)
		}
		report = report || !st.quietFinish
	}
	if report && e.idle() {
		e.updateRemainingTime(0, false)
	}
}

// Cancel ends the transition of name at the request of a command, short
// of its target (matter.js Transitions.cancel(name)).
func (e *Engine) Cancel(name string) {
	e.op.Lock()
	defer e.op.Unlock()
	e.cancelLocked([]string{name})
}

// CancelAll ends every transition at the request of a command (matter.js
// Transitions.cancel()): RemainingTime becomes 0 and is reported where
// anything was reported before.
func (e *Engine) CancelAll() {
	e.op.Lock()
	defer e.op.Unlock()
	e.cancelLocked(nil)
}

// cancelLocked is matter.js cancel; nil names every transition. The
// caller holds op.
func (e *Engine) cancelLocked(names []string) {
	previous := e.RemainingTime()
	if names == nil {
		names = e.names()
		e.mu.Lock()
		e.order, e.states = nil, map[string]*state{}
		e.disarmLocked()
		e.mu.Unlock()
	} else {
		for _, name := range names {
			e.stopLocked(name)
		}
	}
	if e.cfg.Settled != nil {
		for _, name := range names {
			e.cfg.Settled(name)
		}
	}
	if !e.idle() {
		// Ending the longest of several transitions shortens the remaining
		// time, which a command may report.
		e.updateRemainingTime(e.RemainingTime(), true)
		return
	}
	if previous > reportFloor {
		e.mu.Lock()
		e.prevPublished = previous
		e.mu.Unlock()
	}
	e.updateRemainingTime(0, false)
}

// updateRemainingTime is matter.js #updateRemainingTime.
func (e *Engine) updateRemainingTime(next int, fromCommand bool) {
	if e.cfg.RemainingTimeChanged == nil {
		return
	}
	e.mu.Lock()
	prev := e.prevPublished
	if prev == 0 && next <= reportFloor {
		e.mu.Unlock()
		return
	}
	if fromCommand && abs(prev-next) <= reportFloor {
		e.mu.Unlock()
		return
	}
	e.prevPublished = next
	e.mu.Unlock()
	e.cfg.RemainingTimeChanged()
}

// RemainingTime is the time left in the longest running transition, in
// tenths of a second, rounded (matter.js Transitions.remainingTime). It is
// 0 when nothing runs and whenever transitions are not managed.
func (e *Engine) RemainingTime() int {
	if !e.cfg.Manage {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	longest := 0.0
	for _, st := range e.states {
		distance, ok := e.distanceLeftLocked(st)
		if !ok {
			continue
		}
		longest = max(longest, math.Abs(distance/st.changePerMs))
	}
	return int(jsRound(longest / float64(ExternalTimeUnit/time.Millisecond)))
}

// distanceLeftLocked is matter.js #determineDistanceLeft.
func (e *Engine) distanceLeftLocked(st *state) (float64, bool) {
	if st.hasDistance {
		return st.distance, true
	}
	target, ok := e.target(st)
	if !ok {
		return 0, false
	}
	return math.Abs(st.current - target), true
}

// target is matter.js #determineTargetValue: a bounded property without a
// target, or with one beyond its bound in the direction of travel, ends at
// that bound.
func (e *Engine) target(st *state) (float64, bool) {
	prop := e.cfg.Properties[st.t.Name]
	target, ok := st.t.Target, !st.t.NoTarget
	if prop.Cyclic {
		return target, ok
	}
	switch {
	case st.changePerMs < 0 && (!ok || target < prop.Min):
		return prop.Min, true
	case st.changePerMs > 0 && (!ok || target > prop.Max):
		return prop.Max, true
	}
	return target, ok
}

// armLocked arms the step timer for at. The caller holds mu.
func (e *Engine) armLocked(at time.Time) {
	e.gen++
	gen := e.gen
	e.nextTick = at
	e.timer = time.AfterFunc(time.Until(at), func() { e.tick(gen) })
}

// disarmLocked stops the step timer. The caller holds mu.
func (e *Engine) disarmLocked() {
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	e.gen++
}

// tick is one step of every running transition (matter.js #step and
// step), followed by the next arming.
func (e *Engine) tick(gen uint64) {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	if gen != e.gen || e.timer == nil {
		e.mu.Unlock()
		return // stopped, or replaced, while this fire waited
	}
	e.timer = nil
	e.mu.Unlock()

	e.step(time.Now())

	e.mu.Lock()
	if len(e.states) > 0 && e.timer == nil {
		next := e.nextTick.Add(e.cfg.StepInterval)
		if now := time.Now(); next.Before(now) {
			next = now
		}
		e.armLocked(next)
	}
	e.mu.Unlock()
}

// step advances every transition to now and applies what changed. The
// caller holds op.
func (e *Engine) step(now time.Time) {
	var ended []string
	for _, name := range e.names() {
		e.mu.Lock()
		st, ok := e.states[name]
		if !ok {
			e.mu.Unlock()
			continue
		}
		next, done, valid := e.advanceLocked(st, now)
		e.mu.Unlock()
		if !valid {
			e.stopLocked(name)
			continue
		}
		if st.t.OnStep != nil {
			st.t.OnStep(next)
		}
		e.mu.Lock()
		st.current, st.prevStepAt = next, now
		e.mu.Unlock()
		if done {
			ended = append(ended, name)
		}
	}

	var changes []Change
	for _, name := range e.names() {
		e.mu.Lock()
		st := e.states[name]
		e.mu.Unlock()
		if st == nil {
			continue
		}
		value := jsRound(st.current)
		if current, ok := e.cfg.Read(name); !ok || current != value {
			changes = append(changes, Change{Name: name, Value: value})
		}
	}
	if len(changes) > 0 {
		if err := e.cfg.Apply(changes); err != nil {
			e.mu.Lock()
			e.order, e.states = nil, map[string]*state{}
			e.disarmLocked()
			e.mu.Unlock()
			return
		}
	}
	// A property ends when the value it applied is its target (matter.js
	// ends it from the attribute's change event).
	for _, c := range changes {
		e.mu.Lock()
		st := e.states[c.Name]
		var target float64
		var ok bool
		if st != nil {
			target, ok = e.target(st)
		}
		e.mu.Unlock()
		if ok && c.Value == target && !contains(ended, c.Name) {
			ended = append(ended, c.Name)
		}
	}
	if len(ended) > 0 {
		e.finishLocked(ended)
	}
}

// advanceLocked computes a transition's next value at now (matter.js
// step). done reports that the value reached the target; valid=false is a
// step error. The caller holds mu.
func (e *Engine) advanceLocked(st *state, now time.Time) (next float64, done, valid bool) {
	prop := e.cfg.Properties[st.t.Name]
	change := st.changePerMs * float64(now.Sub(st.prevStepAt)) / float64(time.Millisecond)
	switch {
	case prop.Cyclic:
		next = addWithOverflow(st.current, change, prop.Min, prop.Max)
	default:
		next = min(max(st.current+change, prop.Min), prop.Max)
	}
	target, hasTarget := e.target(st)
	if prop.Cyclic {
		if st.hasDistance && hasTarget {
			st.distance -= math.Abs(change)
			if st.distance <= 0 {
				next, st.distance, done = target, 0, true
			}
		}
		return next, done, true
	}
	switch {
	case st.changePerMs < 0:
		if hasTarget && next < target {
			next = target
		}
	case st.changePerMs > 0:
		if hasTarget && next > target {
			next = target
		}
	default:
		return 0, false, false // a rate of zero never starts gradually
	}
	if !hasTarget {
		return 0, false, false
	}
	return next, next == target, true
}

// AddWithOverflow is matter.js addValueWithOverflow (@matter/general): value
// plus add, wrapped round [minValue, maxValue] once — what a cyclic step
// (ColorControl StepHue) aims at.
func AddWithOverflow(value, add, minValue, maxValue float64) float64 {
	return addWithOverflow(value, add, minValue, maxValue)
}

// addWithOverflow is matter.js addValueWithOverflow.
func addWithOverflow(value, add, minValue, maxValue float64) float64 {
	v := value + add
	switch {
	case v < minValue:
		return v - minValue + maxValue
	case v > maxValue:
		return v - maxValue + minValue
	}
	return v
}

// names returns the running properties in start order.
func (e *Engine) names() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.order...)
}

// idle reports whether nothing runs.
func (e *Engine) idle() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.states) == 0
}

// jsRound rounds half up, as JavaScript's Math.round does (Go's
// math.Round rounds half away from zero).
func jsRound(v float64) float64 { return math.Floor(v + 0.5) }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func contains(list []string, name string) bool {
	for _, n := range list {
		if n == name {
			return true
		}
	}
	return false
}

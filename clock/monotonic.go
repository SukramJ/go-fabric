// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clock

import "time"

// Monotonic is the optional second reading of a [Clock]: elapsed time that
// only advances, unaffected by a wall-clock step (an NTP correction, a
// manual clock set). Code that measures an interval — a session's
// activity threshold, a peer-loss cutoff, the age of a cache entry — reads
// it through [MonotonicOf] and converts to the wall clock only for
// exposure, the way matter.js keeps its timestamps on Time.nowUs (the
// monotonic reading, packages/general/src/time/Time.ts:53-64 at 862ad75f)
// and reports them through the current wall-clock offset
// (packages/protocol/src/session/Session.ts:154-162).
//
// [System] implements it with the monotonic reading time.Now carries;
// [github.com/SukramJ/go-fabric/clock/clocktest.Fake] implements it with
// its advanced time, which [Fake.StepWall] leaves alone.
type Monotonic interface {
	// Monotonic is the elapsed reading. Only differences of two readings
	// of the same clock mean anything.
	Monotonic() time.Duration
}

// MonotonicOf is c's [Monotonic] reading, or — for a clock without one —
// the wall reading since the Unix epoch, which then follows every step.
func MonotonicOf(c Clock) time.Duration {
	c = Or(c)
	if m, ok := c.(Monotonic); ok {
		return m.Monotonic()
	}
	return c.Now().Sub(time.Unix(0, 0))
}

// WallOf converts a [Monotonic] reading taken from c into the wall-clock
// time c reads now for it — matter.js Session.ts:154-162 (wallClockActivity:
// `timestamp + (Time.nowMs - Time.nowUs)`). Whether a reading was taken
// at all is the caller's to know.
func WallOf(c Clock, mono time.Duration) time.Time {
	c = Or(c)
	return c.Now().Add(mono - MonotonicOf(c))
}

// systemStart anchors [System.Monotonic]: a reading with time.Now's
// monotonic component, so the difference ignores wall-clock steps.
var systemStart = time.Now()

// Monotonic implements [Monotonic] on the monotonic reading time.Now
// carries (time.Time.Sub uses it when both operands have one).
func (System) Monotonic() time.Duration { return time.Now().Sub(systemStart) }

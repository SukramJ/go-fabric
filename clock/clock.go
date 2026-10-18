// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package clock is the module's one time port: the current time, one-shot
// timers and tickers behind an interface, so that a test can drive every
// interval of the stack — MRP retransmission, the acknowledgement pump, the
// subscription reporting loop, commissioning windows, cluster timers — by
// hand instead of waiting for the wall clock.
//
// Every package of the module takes its time from here: nothing outside
// this package calls time.Now, time.AfterFunc, time.NewTimer,
// time.NewTicker, time.Sleep, time.After, time.Tick, time.Since or
// time.Until in production code (clock/noraw_test.go fails the build on
// one). [System] is the standard library and the default of every owner;
// [github.com/SukramJ/go-fabric/clock/clocktest.Fake] is the test clock.
//
// A host injects a clock through the owner's configuration — the
// `Clock clock.Clock` field of [github.com/SukramJ/go-fabric/node.Config],
// [github.com/SukramJ/go-fabric/bridge.Config] and the other owners. A nil
// clock is [System]. A node hands its clock on to the bridge, the store's
// persist loop, OTA and mDNS, so one field moves the whole device side.
//
// [Clock.AfterFunc] runs f in its own goroutine, as time.AfterFunc does;
// f must not assume it runs on the goroutine that armed the timer.
package clock

import "time"

// Clock is a time source: the current time, one-shot timers and tickers.
// Its methods are safe for concurrent use.
type Clock interface {
	// Now is the current time.
	Now() time.Time
	// AfterFunc calls f in its own goroutine once d has elapsed. The
	// returned Timer's C is nil.
	AfterFunc(d time.Duration, f func()) Timer
	// NewTimer sends the current time on the Timer's C once d has elapsed.
	NewTimer(d time.Duration) Timer
	// NewTicker sends the current time on the Ticker's C every d. d must
	// be greater than zero.
	NewTicker(d time.Duration) Ticker
}

// Timer is a one-shot timer a [Clock] started. Its semantics are those of
// time.Timer since Go 1.23: after Stop or Reset returns, no stale value is
// received from C.
type Timer interface {
	// Stop prevents the timer from firing. It reports whether it did:
	// false means the timer had already fired or been stopped.
	Stop() bool
	// Reset re-arms the timer to fire after d. It reports whether the
	// timer had been active.
	Reset(d time.Duration) bool
	// C is the channel a timer from [Clock.NewTimer] delivers on; nil for
	// a timer from [Clock.AfterFunc].
	C() <-chan time.Time
}

// Ticker is a periodic ticker a [Clock] started, with time.Ticker's
// semantics: a slow receiver misses ticks rather than queueing them.
type Ticker interface {
	// Stop turns the ticker off; no more ticks are sent.
	Stop()
	// Reset stops the ticker and restarts it with period d.
	Reset(d time.Duration)
	// C is the channel the ticks are delivered on.
	C() <-chan time.Time
}

// System is the [Clock] of the time package. Its zero value is ready.
type System struct{}

// Now is time.Now.
func (System) Now() time.Time { return time.Now() }

// AfterFunc is time.AfterFunc.
func (System) AfterFunc(d time.Duration, f func()) Timer {
	return systemTimer{time.AfterFunc(d, f)}
}

// NewTimer is time.NewTimer.
func (System) NewTimer(d time.Duration) Timer { return systemTimer{time.NewTimer(d)} }

// NewTicker is time.NewTicker.
func (System) NewTicker(d time.Duration) Ticker { return systemTicker{time.NewTicker(d)} }

type systemTimer struct{ t *time.Timer }

func (s systemTimer) Stop() bool                 { return s.t.Stop() }
func (s systemTimer) Reset(d time.Duration) bool { return s.t.Reset(d) }
func (s systemTimer) C() <-chan time.Time        { return s.t.C }

type systemTicker struct{ t *time.Ticker }

func (s systemTicker) Stop()                 { s.t.Stop() }
func (s systemTicker) Reset(d time.Duration) { s.t.Reset(d) }
func (s systemTicker) C() <-chan time.Time   { return s.t.C }

// Or is c, or [System] when c is nil — the default every owner applies.
func Or(c Clock) Clock {
	if c == nil {
		return System{}
	}
	return c
}

// Sleep blocks on c for d or until done is closed, whichever comes first,
// and reports whether the full d elapsed. A nil done never closes.
func Sleep(c Clock, d time.Duration, done <-chan struct{}) bool {
	t := c.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C():
		return true
	case <-done:
		return false
	}
}

// After is c.NewTimer(d).C(), time.After on c. The timer is not stopped
// when the caller stops waiting; use [Clock.NewTimer] where that matters.
func After(c Clock, d time.Duration) <-chan time.Time { return c.NewTimer(d).C() }

// Since is c.Now().Sub(t), time.Since on c.
func Since(c Clock, t time.Time) time.Duration { return c.Now().Sub(t) }

// Until is t.Sub(c.Now()), time.Until on c.
func Until(c Clock, t time.Time) time.Duration { return t.Sub(c.Now()) }

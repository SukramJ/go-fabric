// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package clocktest is the test clock of the module: [Fake], a
// [clock.Clock] whose time moves only when the test moves it.
//
// It follows matter.js's MockTime (packages/testing/src/mocks/time.ts):
// timers are callbacks at a virtual instant, [Fake.Advance] runs the ones
// due in the interval in time order and awaits each, [Fake.Yield] lets the
// work they started proceed, and [Fake.Resolve] moves time forward until a
// pending operation completes. Go has no microtask queue, so Yield is a
// scheduler hint and a short real pause rather than a guarantee; a test
// that needs one specific goroutine to have run waits for its effect.
package clocktest

import (
	"cmp"
	"context"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// Fake is a [clock.Clock] driven by the test. Its zero value is not
// ready; use [New]. All methods are safe for concurrent use.
type Fake struct {
	// advancing serialises Advance (and Resolve's steps): two goroutines
	// moving one clock would interleave their firing order.
	advancing sync.Mutex

	// mu guards entries, now and seq.
	mu      sync.Mutex
	now     time.Time
	seq     uint64
	entries map[*entry]struct{}
}

var _ clock.Clock = (*Fake)(nil)

// entry is one armed timer or ticker.
type entry struct {
	f      *Fake
	at     time.Time
	seq    uint64 // arming order: ties at one instant fire first-armed first
	armed  bool
	fn     func()         // AfterFunc
	ch     chan time.Time // NewTimer / NewTicker
	period time.Duration  // > 0 for a ticker
}

// New returns a Fake reading start. It arms nothing.
func New(start time.Time) *Fake {
	return &Fake{now: start, entries: make(map[*entry]struct{})}
}

// Now is the fake's current time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Set moves the reading to t, forwards or backwards, without firing
// anything — a wall-clock step. Armed timers keep their deadlines; one
// that is overdue after the step fires on the next [Fake.Advance], which
// may be Advance(0).
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	f.now = t
	f.mu.Unlock()
}

// AfterFunc arms f to run in its own goroutine once the fake has advanced
// by d. A d of zero or less is due at once and runs on the next
// [Fake.Advance], as MockTime's timers do.
func (f *Fake) AfterFunc(d time.Duration, fn func()) clock.Timer {
	e := &entry{f: f, fn: fn}
	f.arm(e, d)
	return (*fakeTimer)(e)
}

// NewTimer arms a timer that sends the fake's time on C once the fake has
// advanced by d.
func (f *Fake) NewTimer(d time.Duration) clock.Timer {
	e := &entry{f: f, ch: make(chan time.Time, 1)}
	f.arm(e, d)
	return (*fakeTimer)(e)
}

// NewTicker arms a ticker that sends the fake's time on C every d of
// advanced time. It panics on a d of zero or less, as time.NewTicker does.
func (f *Fake) NewTicker(d time.Duration) clock.Ticker {
	if d <= 0 {
		panic("clocktest: non-positive interval for NewTicker")
	}
	e := &entry{f: f, ch: make(chan time.Time, 1), period: d}
	f.arm(e, d)
	return (*fakeTicker)(e)
}

func (f *Fake) arm(e *entry, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.armLocked(e, d)
}

func (f *Fake) armLocked(e *entry, d time.Duration) {
	f.seq++
	e.seq, e.at, e.armed = f.seq, f.now.Add(d), true
	f.entries[e] = struct{}{}
}

// disarmLocked reports whether e was armed.
func (f *Fake) disarmLocked(e *entry) bool {
	was := e.armed
	e.armed = false
	delete(f.entries, e)
	return was
}

// Pending is the number of armed timers and running tickers.
func (f *Fake) Pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}

// Next is the deadline of the earliest armed timer or ticker; false when
// nothing is armed.
func (f *Fake) Next() (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.earliestLocked(); e != nil {
		return e.at, true
	}
	return time.Time{}, false
}

// Armed is the time left until each armed timer and ticker fires, the
// earliest first — what a test reads to see which timeouts the code under
// test started (MockTime's timerCountFor, by duration instead of name).
func (f *Fake) Armed() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	es := make([]*entry, 0, len(f.entries))
	for e := range f.entries {
		es = append(es, e)
	}
	slices.SortFunc(es, func(a, b *entry) int {
		if c := a.at.Compare(b.at); c != 0 {
			return c
		}
		return cmp.Compare(a.seq, b.seq)
	})
	out := make([]time.Duration, len(es))
	for i, e := range es {
		out[i] = e.at.Sub(f.now)
	}
	return out
}

func (f *Fake) earliestLocked() *entry {
	var next *entry
	for e := range f.entries {
		if next == nil || e.at.Before(next.at) || (e.at.Equal(next.at) && e.seq < next.seq) {
			next = e
		}
	}
	return next
}

// Advance moves the fake forward by d. Every timer and ticker tick due up
// to the new time fires in time order, the clock reading each one's
// deadline as it fires — a timer a callback arms inside the interval fires
// in the same call. An AfterFunc callback runs in its own goroutine and
// Advance waits for it to return before the next one fires (MockTime
// awaits each); a channel timer or ticker sends without blocking, as the
// standard library does, so a slow receiver misses ticks. Advance returns
// when every due callback has returned, the clock reading the new time.
//
// A callback must not call Advance on the same fake: it would wait for
// itself.
func (f *Fake) Advance(d time.Duration) {
	f.advancing.Lock()
	defer f.advancing.Unlock()

	f.mu.Lock()
	target := f.now.Add(d)
	f.mu.Unlock()
	for {
		f.mu.Lock()
		e := f.earliestLocked()
		if e == nil || e.at.After(target) {
			if f.now.Before(target) {
				f.now = target
			}
			f.mu.Unlock()
			return
		}
		if e.at.After(f.now) {
			f.now = e.at
		}
		at := f.now
		if e.period > 0 {
			f.armLocked(e, e.period) // a ticker re-arms before it ticks (MockTimer)
		} else {
			f.disarmLocked(e)
		}
		fn, ch := e.fn, e.ch
		f.mu.Unlock()

		if fn != nil {
			done := make(chan struct{})
			go func() {
				defer close(done)
				fn()
			}()
			<-done
			continue
		}
		select {
		case ch <- at:
		default:
		}
	}
}

// Yield lets the goroutines the fired callbacks and ticks started run
// before the test continues — MockTime's yield. It is a scheduler hint
// and a short real pause: enough for work that is runnable, no promise
// for work that waits on I/O.
func (f *Fake) Yield() {
	for range 8 {
		runtime.Gosched()
	}
	time.Sleep(time.Millisecond) // the one real pause: clock/ is the port, the guard does not read it
}

// Resolve runs wait in its own goroutine and moves the fake forward,
// deadline by deadline with a [Fake.Yield] before each step, until wait
// returns; its error is Resolve's. When nothing is armed it stops moving
// time and keeps waiting — the work may still arm a timer — until wait
// returns or ctx is done, in which case Resolve returns ctx.Err(). This is
// MockTime's resolve, which moves time until a promise settles.
func (f *Fake) Resolve(ctx context.Context, wait func() error) error {
	done := make(chan error, 1)
	go func() { done <- wait() }()
	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		f.Yield()
		select {
		case err := <-done:
			return err
		default:
		}
		at, ok := f.Next()
		if !ok {
			continue // Yield paced the loop; nothing to advance
		}
		f.Advance(max(at.Sub(f.Now()), 0))
	}
}

// fakeTimer is a one-shot entry seen as a [clock.Timer].
type fakeTimer entry

func (t *fakeTimer) Stop() bool {
	e := (*entry)(t)
	e.f.mu.Lock()
	defer e.f.mu.Unlock()
	was := e.f.disarmLocked(e)
	drain(e.ch)
	return was
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	e := (*entry)(t)
	e.f.mu.Lock()
	defer e.f.mu.Unlock()
	was := e.f.disarmLocked(e)
	drain(e.ch)
	e.f.armLocked(e, d)
	return was
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

// fakeTicker is a periodic entry seen as a [clock.Ticker].
type fakeTicker entry

func (t *fakeTicker) Stop() {
	e := (*entry)(t)
	e.f.mu.Lock()
	defer e.f.mu.Unlock()
	e.f.disarmLocked(e)
	drain(e.ch)
}

func (t *fakeTicker) Reset(d time.Duration) {
	if d <= 0 {
		panic("clocktest: non-positive interval for Ticker.Reset")
	}
	e := (*entry)(t)
	e.f.mu.Lock()
	defer e.f.mu.Unlock()
	e.f.disarmLocked(e)
	drain(e.ch)
	e.period = d
	e.f.armLocked(e, d)
}

func (t *fakeTicker) C() <-chan time.Time { return t.ch }

// drain empties a timer channel so that no stale value is received after
// Stop or Reset — the standard library's guarantee since Go 1.23.
func drain(ch chan time.Time) {
	if ch == nil {
		return
	}
	select {
	case <-ch:
	default:
	}
}

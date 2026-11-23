// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"sync"
	"time"
)

// latestTimer is a device's one pending timer whose callback acts only
// while it is still the latest arming (R2-P6-conc-2607, -2651). Stopping
// a time.Timer does not recall a callback that has already fired and
// waits for the device's lock; without a check that callback would act on
// the state a newer arming set up — complete the new travel at once, end
// the new boost, drop the new timer. Every arming and every stop advance
// the generation under the device's lock; the callback receives its
// arming's generation and, holding the same lock, asks takeLocked first —
// and acts in that critical section: an act after releasing it undoes an
// arming made in between (R2-C2-conc-2602).
// fakeReadings' gen (fleet_energy_market.go) is the same pattern.
//
// The zero value is ready; after arms through time.AfterFunc when nil (a
// test or the camera's clock sets it).
type latestTimer struct {
	after func(d time.Duration, f func()) (stop func() bool)
	gen   uint64
	stop  func() bool
}

// armLocked stops the previous arming and arms fn to run after d with
// this arming's generation. The caller holds the device's lock.
func (t *latestTimer) armLocked(d time.Duration, fn func(gen uint64)) {
	t.stopLocked()
	gen := t.gen
	after := t.after
	if after == nil {
		after = func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }
	}
	t.stop = after(d, func() { fn(gen) })
}

// stopLocked stops the pending arming, if any; a callback of it that
// already fired finds itself stale. The caller holds the device's lock.
func (t *latestTimer) stopLocked() {
	if t.stop != nil {
		t.stop()
		t.stop = nil
	}
	t.gen++
}

// armedLocked reports whether an arming is pending. The caller holds the
// device's lock.
func (t *latestTimer) armedLocked() bool { return t.stop != nil }

// latestLocked reports whether gen is still the latest arming, taken or
// not: no arming and no stop came after it. A callback that took gen and
// released the lock for a while asks it before it acts. The caller holds
// the device's lock.
func (t *latestTimer) latestLocked(gen uint64) bool { return gen == t.gen }

// takeLocked reports whether gen is the pending arming and, if so, marks
// it fired; a stale callback (false) does nothing. The caller holds the
// device's lock.
func (t *latestTimer) takeLocked(gen uint64) bool {
	if t.stop == nil || gen != t.gen {
		return false
	}
	t.stop = nil
	return true
}

// orderedQueue runs posted functions one at a time, in the order they
// were posted, on a goroutine of its own (R2-P6-conc-2608): a caller that
// holds its device's lock posts the event of a transition without calling
// into a server under that lock, and two transitions' events cannot
// overtake each other as two goroutines' could. The zero value is ready.
type orderedQueue struct {
	mu      sync.Mutex
	pending []func()
	running bool
}

// post queues fn; a drain goroutine runs while anything is queued.
func (q *orderedQueue) post(fn func()) {
	q.mu.Lock()
	q.pending = append(q.pending, fn)
	if q.running {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.mu.Unlock()
	go q.drain()
}

func (q *orderedQueue) drain() {
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		fn := q.pending[0]
		q.pending[0] = nil
		q.pending = q.pending[1:]
		q.mu.Unlock()
		fn()
	}
}

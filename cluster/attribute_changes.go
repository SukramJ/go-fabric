// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package cluster

import (
	"slices"
	"sync"
	"time"
)

// AttributeChanges is the listener set behind
// [contract.AttributeChangeNotifier] for a cluster server that keeps its
// own attribute state. Embed it (by value, never copied after first use)
// and call [AttributeChanges.Notify] with the attributes that moved; the
// zero value is ready to use.
type AttributeChanges struct {
	mu        sync.Mutex
	next      int
	listeners map[int]func([]uint32)
}

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (c *AttributeChanges) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listeners == nil {
		c.listeners = map[int]func([]uint32){}
	}
	id := c.next
	c.next++
	c.listeners[id] = cb
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.listeners, id)
			c.mu.Unlock()
		})
	}
}

// Notify tells every listener that attrIDs changed. It calls them outside
// its lock, in registration order; an empty list notifies nobody.
func (c *AttributeChanges) Notify(attrIDs ...uint32) {
	if len(attrIDs) == 0 {
		return
	}
	c.mu.Lock()
	ids := make([]int, 0, len(c.listeners))
	for id := range c.listeners {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	cbs := make([]func([]uint32), 0, len(ids))
	for _, id := range ids {
		cbs = append(cbs, c.listeners[id])
	}
	c.mu.Unlock()
	for _, cb := range cbs {
		cb(slices.Clone(attrIDs))
	}
}

// QuietMinimumInterval is the minimum time between two reports of a
// quieter ("Q") attribute: matter.js QuietObservable.defaults
// minimumEmitInterval, Seconds.one
// (packages/general/src/util/Observable.ts).
const QuietMinimumInterval = time.Second

// Quieter throttles the change reports of one quieter ("Q") attribute the
// way matter.js reports it: a server-side QuietEvent
// (packages/node/src/behavior/Events.ts) whose QuietObservable
// (packages/general/src/util/Observable.ts) emits
//
//   - at once when the value changes from null, or to null from a
//     non-null value (QuietEvent's shouldEmit returns "now");
//   - otherwise at once when the last report is older than
//     [QuietMinimumInterval], and else once, with the latest value, when
//     that interval has passed since the last report.
//
// A change the throttle holds back is still the attribute's value — a
// read sees it — but neither advances the DataVersion nor marks the
// attribute dirty until the deferred report goes out
// (ServerBehaviorBacking reports a quieter property only on its quiet
// emit).
type Quieter struct {
	// Report is called, outside the throttle's lock, when a report goes
	// out.
	Report func()
	// Now and AfterFunc default to time.Now and time.AfterFunc.
	Now       func() time.Time
	AfterFunc func(d time.Duration, f func()) *time.Timer

	mu       sync.Mutex
	lastEmit time.Time
	pending  bool
	timer    *time.Timer
	gen      uint64 // identifies the armed timer; a stale one is ignored
}

// Changed reports one change of the attribute from wasNull to isNull (the
// nullness of the old and new value); the caller has already stored the
// new value and calls this only for an actual change.
func (q *Quieter) Changed(wasNull, isNull bool) {
	q.mu.Lock()
	now := q.now()
	immediate := wasNull || isNull
	if immediate || q.lastEmit.IsZero() || q.lastEmit.Add(QuietMinimumInterval).Before(now) {
		q.emitLocked(now)
		q.mu.Unlock()
		q.Report()
		return
	}
	q.pending = true
	if q.timer == nil {
		after := q.AfterFunc
		if after == nil {
			after = time.AfterFunc
		}
		q.gen++
		gen := q.gen
		q.timer = after(q.lastEmit.Add(QuietMinimumInterval).Sub(now), func() { q.flush(gen) })
	}
	q.mu.Unlock()
}

// EmitNow sends a report held back by the throttle at once, and nothing
// when none is pending — matter.js QuietObservable.emitNow, which a
// transition calls when it ends so the final value is reported without
// waiting out the interval (Transitions.ts finish / cancel).
func (q *Quieter) EmitNow() {
	q.mu.Lock()
	if !q.pending {
		q.mu.Unlock()
		return
	}
	q.emitLocked(q.now())
	q.mu.Unlock()
	q.Report()
}

// flush sends the deferred report of timer gen, if that timer is still
// the armed one and a report is still pending.
func (q *Quieter) flush(gen uint64) {
	q.mu.Lock()
	if gen != q.gen || q.timer == nil {
		q.mu.Unlock()
		return
	}
	q.timer = nil
	if !q.pending {
		q.mu.Unlock()
		return
	}
	q.emitLocked(q.now())
	q.mu.Unlock()
	q.Report()
}

func (q *Quieter) emitLocked(now time.Time) {
	q.lastEmit = now
	q.pending = false
	if q.timer != nil {
		q.timer.Stop()
		q.timer = nil
	}
}

func (q *Quieter) now() time.Time {
	if q.Now != nil {
		return q.Now()
	}
	return time.Now()
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cluster

import (
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/contract"
)

// AttributeChanges is the listener set behind
// [contract.AttributeChangeNotifier] and [contract.AttributeReportNotifier]
// for a cluster server that keeps its own attribute state. Embed it (by
// value, never copied after first use) and call [AttributeChanges.Notify]
// with the attributes that moved; the zero value is ready to use.
//
// A change goes to two listener sets: the change listeners
// (OnAttributeChange, the host's view — matter.js's `$Changed` observers)
// and the report listeners (OnAttributeReport, the wire's — matter.js's
// OnlineEvent.online). [AttributeChanges.NotifyFrom] tells both; a server
// with a quieter ("Q") attribute tells the change listeners of its every
// change with [AttributeChanges.ChangedFrom] and the report listeners
// when its [Quieter] emits, with [AttributeChanges.ReportFrom].
type AttributeChanges struct {
	// mu guards next, listeners and reporters.
	mu        sync.Mutex
	next      int
	listeners map[int]func(contract.Change)
	reporters map[int]func(contract.Change)
}

// OnAttributeChange implements [contract.AttributeChangeNotifier]: cb
// sees every change at once.
func (c *AttributeChanges) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return c.add(false, cb)
}

// OnAttributeReport implements [contract.AttributeReportNotifier]: cb
// sees the changes that go on the wire — a quieter attribute's by its
// throttle.
func (c *AttributeChanges) OnAttributeReport(cb func(change contract.Change)) (unsubscribe func()) {
	return c.add(true, cb)
}

// add registers cb as a report listener when report, as a change
// listener otherwise, and returns its idempotent unsubscribe.
func (c *AttributeChanges) add(report bool, cb func(contract.Change)) (unsubscribe func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listeners == nil {
		c.listeners = map[int]func(contract.Change){}
		c.reporters = map[int]func(contract.Change){}
	}
	id := c.next
	c.next++
	if report {
		c.reporters[id] = cb
	} else {
		c.listeners[id] = cb
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.listeners, id)
			delete(c.reporters, id)
			c.mu.Unlock()
		})
	}
}

// Notify tells every listener that attrIDs changed, without saying where
// the change came from ([contract.OriginUnknown]); [AttributeChanges.NotifyFrom]
// says it.
func (c *AttributeChanges) Notify(attrIDs ...uint32) {
	c.NotifyFrom(contract.OriginUnknown, attrIDs...)
}

// NotifyFrom tells every listener — change and report listeners alike —
// that attrIDs changed and where the change came from
// ([contract.Change]); each listener gets its own copy of the ids. It
// calls them outside its lock, in registration order; an empty list
// notifies nobody.
func (c *AttributeChanges) NotifyFrom(origin contract.Origin, attrIDs ...uint32) {
	c.notify(origin, attrIDs, true, true)
}

// ChangedFrom tells the change listeners alone that attrIDs changed: a
// quieter attribute's change, which the host sees at once while its
// report waits for the throttle ([AttributeChanges.ReportFrom]).
func (c *AttributeChanges) ChangedFrom(origin contract.Origin, attrIDs ...uint32) {
	c.notify(origin, attrIDs, true, false)
}

// ReportFrom tells the report listeners alone that attrIDs changed: the
// emit of a quieter attribute's throttle, whose changes the change
// listeners saw already ([AttributeChanges.ChangedFrom]).
func (c *AttributeChanges) ReportFrom(origin contract.Origin, attrIDs ...uint32) {
	c.notify(origin, attrIDs, false, true)
}

// notify calls the change listeners when changes and the report listeners
// when reports, outside the lock, in registration order.
func (c *AttributeChanges) notify(origin contract.Origin, attrIDs []uint32, changes, reports bool) {
	if len(attrIDs) == 0 {
		return
	}
	c.mu.Lock()
	ids := make([]int, 0, len(c.listeners)+len(c.reporters))
	if changes {
		for id := range c.listeners {
			ids = append(ids, id)
		}
	}
	if reports {
		for id := range c.reporters {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	cbs := make([]func(contract.Change), 0, len(ids))
	for _, id := range ids {
		if cb, ok := c.listeners[id]; ok && changes {
			cbs = append(cbs, cb)
		} else if cb, ok := c.reporters[id]; ok && reports {
			cbs = append(cbs, cb)
		}
	}
	c.mu.Unlock()
	for _, cb := range cbs {
		cb(contract.Change{Attributes: slices.Clone(attrIDs), Origin: origin})
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
// emit). The throttle stands in front of the wire only: the host's
// listeners see every change at once ([AttributeChanges.ChangedFrom]), as
// every observer of a QuietEvent itself does (Events.ts:267-297, the
// QuietObservable is only its `online` view); Report is the wire's
// ([AttributeChanges.ReportFrom]).
//
// A last report later than the clock's now — the clock changed beneath
// the throttle, as a device's does when it is added to a node and its
// servers move from the system clock to the node's — counts as long past:
// the change is reported at once rather than after the distance between
// the two clocks.
type Quieter struct {
	// Report is called, outside the throttle's lock, when a report goes
	// out.
	Report func()
	// Clock is the throttle's time source; nil is [clock.System].
	Clock clock.Clock

	// mu guards gen, lastEmit, pending and timer.
	mu       sync.Mutex
	lastEmit time.Time
	pending  bool
	timer    func() bool // stops the armed deferred report
	gen      uint64      // identifies the armed timer; a stale one is ignored
}

// Changed reports one change of the attribute from wasNull to isNull (the
// nullness of the old and new value); the caller has already stored the
// new value and calls this only for an actual change.
func (q *Quieter) Changed(wasNull, isNull bool) {
	q.mu.Lock()
	now := q.now()
	immediate := wasNull || isNull
	if immediate || q.lastEmit.IsZero() || q.lastEmit.Add(QuietMinimumInterval).Before(now) || now.Before(q.lastEmit) {
		q.emitLocked(now)
		q.mu.Unlock()
		q.Report()
		return
	}
	q.pending = true
	if q.timer == nil {
		q.gen++
		gen := q.gen
		q.timer = q.afterFunc(q.lastEmit.Add(QuietMinimumInterval).Sub(now), func() { q.flush(gen) })
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

// Stop disarms a deferred report and drops the change it held back,
// without reporting it: the throttle's part of a server's
// [contract.ClusterQuiescer]. The throttle stays usable; the next change
// reports as the rules above say.
func (q *Quieter) Stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.gen++
	q.pending = false
	if q.timer != nil {
		q.timer()
		q.timer = nil
	}
}

func (q *Quieter) emitLocked(now time.Time) {
	q.lastEmit = now
	q.pending = false
	if q.timer != nil {
		q.timer()
		q.timer = nil
	}
}

func (q *Quieter) now() time.Time { return clock.Or(q.Clock).Now() }

// afterFunc arms f after d on the throttle's clock and returns its stop.
func (q *Quieter) afterFunc(d time.Duration, f func()) func() bool {
	return clock.Or(q.Clock).AfterFunc(d, f).Stop
}

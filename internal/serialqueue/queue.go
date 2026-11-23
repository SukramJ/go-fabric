// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package serialqueue is the serial callback goroutine of the facade's
// `…Async` callbacks: a device's (device) and the node's own (node's
// OnDiagnosticEventAsync). One type, so both follow one rule.
package serialqueue

import (
	"log/slog"
	"slices"
	"sync"

	"thingfabric.dev/go-fabric/internal/inflight"
)

// Queue is a serial goroutine: the `…Async` callbacks run on it, one
// after the other, in the order they were enqueued. Its owner (ADR 0026
// principle 5) starts it and stops it — waiting until every queued
// callback ran. Enqueue never blocks the goroutine that enqueues; a
// callback handed to a queue that is not running is dropped. The queue
// holds at most [Bound] callbacks that have not started: past it the
// oldest of them is dropped and counted ([Queue.Dropped]) and the first
// drop since the queue last ran empty is logged at warn
// (`matter.callbacks.dropped`); when the queue runs empty again the
// episode's count and the queue's total are logged at warn
// (`matter.callbacks.overflow_ended`), so a host callback slower than the events
// behind it costs those events, not unbounded memory. A callback queued
// with [Queue.EnqueueExempt] is never dropped and not counted against
// [Bound]; its caller bounds how many it queues. A callback
// must not stop its own queue's owner: the stop would wait for the
// callback that called it.
type Queue struct {
	// life serialises start and stop, so a start waits for a stop's
	// drain; it is taken before mu.
	life sync.Mutex
	// mu guards items, running and stopped; cond waits on mu.
	mu    sync.Mutex
	cond  *sync.Cond
	items []item
	// exempt is the number of items queued with EnqueueExempt.
	exempt  int
	dropped uint64 // callbacks dropped at the bound, over the queue's life
	// overflowing is set by a drop and cleared when the queue runs empty:
	// one warning per episode, not one per dropped callback.
	overflowing bool
	// episodeBase is dropped when the current overflow episode began.
	episodeBase uint64
	// log is the queue's logger; nil logs through slog.Default().
	log     *slog.Logger
	running bool
	stopped chan struct{} // closed when the goroutine of this run exits
	// group, when set, is the in-flight group a callback counts in from
	// its Enqueue until it returned (internal/inflight DeviceCallbacks).
	group func() *inflight.Group
}

// item is one queued callback and the group it is counted in; an exempt
// one is never dropped at the bound.
type item struct {
	fn     func()
	g      *inflight.Group
	exempt bool
}

// CountIn makes every callback enqueued from now on count as work of the
// group group returns at its Enqueue, until it returned: a fabrictest
// site does not move its clock while a host callback waits or runs.
// group may return nil (no group: nothing is counted).
func (q *Queue) CountIn(group func() *inflight.Group) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.group = group
}

// Bound is the number of queued callbacks that have not started a queue
// holds; the one running and the exempt ones ([Queue.EnqueueExempt]) are
// not counted.
const Bound = 1024

// New returns a queue that is not running.
func New() *Queue {
	q := &Queue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Enqueue queues fn; a no-op for nil or while the queue does not run.
// Past [Bound] the oldest queued callback that is not exempt is dropped.
func (q *Queue) Enqueue(fn func()) { q.enqueue(fn, false) }

// EnqueueExempt queues fn as [Queue.Enqueue] does, but exempt from the
// drop rule: it is never dropped and does not count against [Bound], so
// it runs in its place in the order while the queue runs (a no-op for nil
// or while the queue does not run). The queue does not bound the exempt
// callbacks; the caller does. The node queues its owed fabric events'
// ObserveAsync hooks and their acknowledgements with it at start — at most
// one call per owed event and async fabric hook plus one acknowledgement
// per owed event, the few rollbacks the store owes (review II,
// R4-err-4751, decision 615).
func (q *Queue) EnqueueExempt(fn func()) { q.enqueue(fn, true) }

func (q *Queue) enqueue(fn func(), exempt bool) {
	if fn == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.running {
		return
	}
	it := item{fn: fn, exempt: exempt}
	if q.group != nil {
		it.g = q.group()
		it.g.Enter(inflight.DeviceCallbacks)
	}
	if !exempt && len(q.items)-q.exempt >= Bound {
		q.dropOldestLocked()
	}
	if exempt {
		q.exempt++
	}
	q.items = append(q.items, it)
	q.cond.Signal()
}

// dropOldestLocked drops the oldest queued callback that is not exempt,
// counts it and logs the first drop of an episode; q.mu is held and at
// least one such callback is queued.
func (q *Queue) dropOldestLocked() {
	i := slices.IndexFunc(q.items, func(it item) bool { return !it.exempt })
	old := q.items[i]
	if i == 0 {
		// The common case, no exempt callback ahead: no copy.
		q.items[0] = item{}
		q.items = q.items[1:]
	} else {
		q.items = slices.Delete(q.items, i, i+1)
	}
	q.dropped++
	old.g.Leave(inflight.DeviceCallbacks)
	if !q.overflowing {
		q.overflowing = true
		q.episodeBase = q.dropped - 1
		q.logger().Warn("matter.callbacks.dropped",
			slog.Int("max", Bound),
			slog.Uint64("dropped", q.dropped))
	}
}

// SetLogger sets the logger of the queue's warnings; nil logs through
// slog.Default().
func (q *Queue) SetLogger(l *slog.Logger) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.log = l
}

func (q *Queue) logger() *slog.Logger {
	if q.log != nil {
		return q.log
	}
	return slog.Default()
}

// Len is the number of queued callbacks that have not started.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Dropped is the number of callbacks dropped at [Bound] since New.
func (q *Queue) Dropped() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dropped
}

// Start runs the queue's goroutine; a no-op while it runs.
func (q *Queue) Start() {
	q.life.Lock()
	defer q.life.Unlock()
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running {
		return
	}
	q.running = true
	q.stopped = make(chan struct{})
	go q.run(q.stopped)
}

// Stop ends the goroutine once it ran every queued callback and waits for
// it; a no-op when it does not run.
func (q *Queue) Stop() {
	q.life.Lock()
	defer q.life.Unlock()
	q.mu.Lock()
	if !q.running {
		q.mu.Unlock()
		return
	}
	q.running = false
	done := q.stopped
	q.cond.Signal()
	q.mu.Unlock()
	<-done
}

func (q *Queue) run(done chan struct{}) {
	defer close(done)
	for {
		q.mu.Lock()
		for len(q.items) == 0 && q.running {
			q.cond.Wait()
		}
		if len(q.items) == 0 {
			q.mu.Unlock()
			return
		}
		var episode, total uint64
		ended := len(q.items) == 1 && q.overflowing
		if len(q.items) == 1 {
			q.overflowing = false
		}
		if ended {
			episode, total = q.dropped-q.episodeBase, q.dropped
		}
		it := q.items[0]
		q.items[0] = item{}
		q.items = q.items[1:]
		if it.exempt {
			q.exempt--
		}
		log := q.logger()
		q.mu.Unlock()
		if ended {
			// What the episode cost, once it is over: the warning at its
			// start cannot say (review II, R2-P6-bound-0209).
			log.Warn("matter.callbacks.overflow_ended",
				slog.Uint64("dropped", episode),
				slog.Uint64("total", total))
		}
		func() {
			defer it.g.Leave(inflight.DeviceCallbacks)
			it.fn()
		}()
	}
}

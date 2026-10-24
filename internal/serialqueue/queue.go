// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package serialqueue is the serial callback goroutine of the facade's
// `…Async` callbacks: a device's (device) and the node's own (node's
// OnDiagnosticEventAsync). One type, so both follow one rule.
package serialqueue

import (
	"sync"

	"github.com/SukramJ/go-fabric/internal/inflight"
)

// Queue is a serial goroutine: the `…Async` callbacks run on it, one
// after the other, in the order they were enqueued. Its owner (ADR 0026
// principle 5) starts it and stops it — waiting until every queued
// callback ran. Enqueue never blocks the goroutine that enqueues; a
// callback handed to a queue that is not running is dropped. A callback
// must not stop its own queue's owner: the stop would wait for the
// callback that called it.
type Queue struct {
	// life serialises start and stop, so a start waits for a stop's
	// drain; it is taken before mu.
	life sync.Mutex
	// mu guards items, running and stopped; cond waits on mu.
	mu      sync.Mutex
	cond    *sync.Cond
	items   []item
	running bool
	stopped chan struct{} // closed when the goroutine of this run exits
	// group, when set, is the in-flight group a callback counts in from
	// its Enqueue until it returned (internal/inflight DeviceCallbacks).
	group func() *inflight.Group
}

// item is one queued callback and the group it is counted in.
type item struct {
	fn func()
	g  *inflight.Group
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

// New returns a queue that is not running.
func New() *Queue {
	q := &Queue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Enqueue queues fn; a no-op for nil or while the queue does not run.
func (q *Queue) Enqueue(fn func()) {
	if fn == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.running {
		return
	}
	it := item{fn: fn}
	if q.group != nil {
		it.g = q.group()
		it.g.Enter(inflight.DeviceCallbacks)
	}
	q.items = append(q.items, it)
	q.cond.Signal()
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
		it := q.items[0]
		q.items[0] = item{}
		q.items = q.items[1:]
		q.mu.Unlock()
		func() {
			defer it.g.Leave(inflight.DeviceCallbacks)
			it.fn()
		}()
	}
}

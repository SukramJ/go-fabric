// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package serialqueue is the serial callback goroutine of the facade's
// `…Async` callbacks: a device's (device) and the node's own (node's
// OnDiagnosticEventAsync). One type, so both follow one rule.
package serialqueue

import "sync"

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
	items   []func()
	running bool
	stopped chan struct{} // closed when the goroutine of this run exits
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
	q.items = append(q.items, fn)
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
		fn := q.items[0]
		q.items[0] = nil
		q.items = q.items[1:]
		q.mu.Unlock()
		fn()
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// queue is a device's serial goroutine: the `…Async` callbacks run on it,
// one after the other, in the order the changes were reported. The device
// owns it (ADR 0026 principle 5): the node starts it when it adds the
// device and stops it — waiting until every queued callback ran — when it
// removes the device or stops. Enqueue never blocks the goroutine that
// reports a change; a callback handed to a queue that is not running is
// dropped. A callback must not remove its own device or stop its node:
// the stop would wait for the callback that called it.
type queue struct {
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

func newQueue() *queue {
	q := &queue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Enqueue implements spec.Queue.
func (q *queue) Enqueue(fn func()) {
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

// start runs the queue's goroutine; a no-op while it runs.
func (q *queue) start() {
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

// stop ends the goroutine once it ran every queued callback and waits for
// it; a no-op when it does not run.
func (q *queue) stop() {
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

func (q *queue) run(done chan struct{}) {
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

// deviceClock is the time source of the servers a device builds: the
// clock the host set ([Options].Clock), else the node's once the device is
// added, else [clock.System]. A timer keeps the clock it was armed on.
type deviceClock struct {
	// mu guards host and node.
	mu   sync.Mutex
	host clock.Clock // Options.Clock
	node clock.Clock
}

func (c *deviceClock) get() clock.Clock {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.host != nil:
		return c.host
	case c.node != nil:
		return c.node
	default:
		return clock.System{}
	}
}

func (c *deviceClock) setNode(n clock.Clock) {
	c.mu.Lock()
	c.node = n
	c.mu.Unlock()
}

// Now implements [clock.Clock].
func (c *deviceClock) Now() time.Time { return c.get().Now() }

// AfterFunc implements [clock.Clock].
func (c *deviceClock) AfterFunc(d time.Duration, f func()) clock.Timer {
	return c.get().AfterFunc(d, f)
}

// NewTimer implements [clock.Clock].
func (c *deviceClock) NewTimer(d time.Duration) clock.Timer { return c.get().NewTimer(d) }

// NewTicker implements [clock.Clock].
func (c *deviceClock) NewTicker(d time.Duration) clock.Ticker { return c.get().NewTicker(d) }

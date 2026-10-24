// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/internal/serialqueue"
)

// queue is a device's serial goroutine (serialqueue.Queue): the `…Async`
// callbacks run on it, one after the other, in the order the changes were
// reported. The device owns it (ADR 0026 principle 5): the node starts it
// when it adds the device and stops it — waiting until every queued
// callback ran — when it removes the device or stops. A callback handed
// to a queue that is not running is dropped. A callback must not remove
// its own device or stop its node: the stop would wait for the callback
// that called it.
type queue = serialqueue.Queue

func newQueue() *queue { return serialqueue.New() }

// deviceClock is the time source of the servers a device builds: the
// node's once the device is added, [clock.System] before. A timer keeps
// the clock it was armed on.
type deviceClock struct {
	// mu guards node.
	mu   sync.Mutex
	node clock.Clock
}

func (c *deviceClock) get() clock.Clock {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.node != nil {
		return c.node
	}
	return clock.System{}
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

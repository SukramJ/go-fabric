// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"errors"
	"time"
)

// ErrClosed is returned by an operation on a [Names] that has been closed.
var ErrClosed = errors.New("discovery: closed")

// clock is the time source of a [Names]. Production uses the wall clock;
// tests substitute a fake that fires timers on demand.
type clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) stopper
}

type stopper interface{ Stop() bool }

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

func (wallClock) AfterFunc(d time.Duration, f func()) stopper { return time.AfterFunc(d, f) }

// loop is the single goroutine that owns a [Names]'s state. It runs
// operations posted by the exported methods, timer expiries and received
// packets, one at a time. After each it drains the microtask queue, then
// the macrotask queue (each macrotask followed by its own microtasks) —
// the ordering matter.js gets from its event loop, and the one its
// coalescing (one query packet per tick, one service notification per
// batch of record changes) relies on.
type loop struct {
	ops     chan task
	closing chan struct{}
	done    chan struct{}
	micro   []func()
	macro   []func()
}

func newLoop() *loop {
	return &loop{
		ops:     make(chan task, 64),
		closing: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// task is one unit of loop work; done, when set, is closed once the task
// and everything it deferred have run.
type task struct {
	fn   func()
	done chan struct{}
}

// post hands fn to the loop without waiting for it to run. It reports
// false once the loop has stopped. Never call it from the loop itself.
func (l *loop) post(fn func()) bool {
	return l.enqueue(task{fn: fn})
}

func (l *loop) enqueue(t task) bool {
	select {
	case <-l.done:
		return false
	default:
	}
	select {
	case l.ops <- t:
		return true
	case <-l.done:
		return false
	}
}

// call runs fn on the loop and waits until it and the micro- and
// macrotasks it deferred have run. Never call it from the loop itself.
func (l *loop) call(fn func()) error {
	ran := make(chan struct{})
	if !l.enqueue(task{fn: fn, done: ran}) {
		return ErrClosed
	}
	select {
	case <-ran:
		return nil
	case <-l.done:
		select {
		case <-ran:
			return nil
		default:
			return ErrClosed
		}
	}
}

// exec runs one loop task and everything it deferred.
func (l *loop) exec(t task) {
	if t.done != nil {
		defer close(t.done)
	}
	t.fn()
	l.drainMicro()
	for len(l.macro) > 0 {
		m := l.macro[0]
		l.macro = l.macro[1:]
		m()
		l.drainMicro()
	}
}

func (l *loop) drainMicro() {
	for len(l.micro) > 0 {
		m := l.micro[0]
		l.micro = l.micro[1:]
		m()
	}
}

// microtask defers fn to run after the current loop task (matter.js: an
// awaited promise continuation).
func (l *loop) microtask(fn func()) { l.micro = append(l.micro, fn) }

// macrotask defers fn to run after the current task and its microtasks
// (matter.js: Time.sleep(…, 0)).
func (l *loop) macrotask(fn func()) { l.macro = append(l.macro, fn) }

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"fmt"
	"log/slog"
	"slices"
	"sync"
)

// listeners are the host callbacks of one node event registered with an
// `On…` method, in registration order. Every node event follows the one
// callback rule (ADR 0025 principle 8): `On…` runs the callback inline on
// the event's goroutine, `On…Async` queues it on the node's callback
// goroutine, and both return the unsubscribe function — as matter.js's
// node events are Observables a handler attaches to with `on` and
// detaches from with `off` (packages/general/src/util/Observable.ts:55,
// :70 at 862ad75f).
type listeners[T any] struct {
	mu   sync.Mutex
	list []*func(T)
}

// add registers fn; the returned unsubscribe is idempotent. A nil fn
// registers nothing.
func (l *listeners[T]) add(fn func(T)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	entry := &fn
	l.mu.Lock()
	l.list = append(l.list, entry)
	l.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.list = slices.DeleteFunc(l.list, func(e *func(T)) bool { return e == entry })
			l.mu.Unlock()
		})
	}
}

// empty reports whether no callback is registered.
func (l *listeners[T]) empty() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.list) == 0
}

// emit runs every registered callback with v, each guarded by
// [contain]: a panic is logged under event and the rest still run.
func (l *listeners[T]) emit(logger *slog.Logger, event string, v T) {
	l.mu.Lock()
	list := slices.Clone(l.list)
	l.mu.Unlock()
	for _, fn := range list {
		contain(logger, event, func() { (*fn)(v) })
	}
}

// contain runs a host callback, logging a panic under event instead of
// letting it unwind the node's goroutine.
func contain(logger *slog.Logger, event string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("node.observer.panic", slog.String("event", event), slog.String("panic", fmt.Sprint(r)))
		}
	}()
	fn()
}

// queued wraps fn to run on the node's callback goroutine, one call after
// the other, while the node runs; Stop returns once every queued call ran.
func (n *Node) queued(event string, fn func()) {
	n.callbacks.Enqueue(func() { contain(n.logger, event, fn) })
}

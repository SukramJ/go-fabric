// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package serialqueue_test

import (
	"bytes"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/internal/inflight"
	"github.com/SukramJ/go-fabric/internal/serialqueue"
)

// TestQueue: a stopped queue drops, a running one runs in order, Stop
// waits for the queued callbacks, and Start and Stop are idempotent.
func TestQueue(t *testing.T) {
	q := serialqueue.New()
	q.Enqueue(func() { t.Error("ran on a stopped queue") })
	q.Enqueue(nil)
	q.Stop() // not running: a no-op
	q.Start()
	q.Start() // running: a no-op
	var got []int
	for i := range 5 {
		q.Enqueue(func() { got = append(got, i) })
	}
	q.Stop()
	if !slices.Equal(got, []int{0, 1, 2, 3, 4}) {
		t.Errorf("ran %v", got)
	}
	q.Start()
	done := make(chan struct{})
	q.Enqueue(func() { close(done) })
	q.Stop()
	<-done
}

// TestQueueCountsIn: with CountIn a callback is work of the group from its
// Enqueue until it returned.
func TestQueueCountsIn(t *testing.T) {
	g := inflight.New("device 1")
	q := serialqueue.New()
	q.CountIn(func() *inflight.Group { return g })
	q.Start()
	release, running := make(chan struct{}), make(chan struct{})
	q.Enqueue(func() {
		close(running)
		<-release
	})
	q.Enqueue(func() {})
	<-running
	if got := g.Snapshot()[inflight.DeviceCallbacks]; got != 2 {
		t.Fatalf("counted %d callbacks, want the running and the queued one", got)
	}
	close(release)
	q.Stop()
	if g.Busy() {
		t.Fatalf("counted %v after the queue drained", g.Snapshot())
	}
}

// TestQueueIsBounded holds R2-P6-bound-0209: a callback slower than the
// events that enqueue behind it does not grow the queue without limit.
// Past serialqueue.Bound queued callbacks the oldest queued one is dropped
// and counted, its in-flight count released; the rest still run in order.
func TestQueueIsBounded(t *testing.T) {
	g := inflight.New("device 1")
	q := serialqueue.New()
	q.CountIn(func() *inflight.Group { return g })
	var logged bytes.Buffer
	q.SetLogger(slog.New(slog.NewTextHandler(&logged, nil)))
	q.Start()
	release, running := make(chan struct{}), make(chan struct{})
	q.Enqueue(func() {
		close(running)
		<-release
	})
	<-running
	const extra = 100
	var ran []int
	for i := range serialqueue.Bound + extra {
		q.Enqueue(func() { ran = append(ran, i) })
	}
	if got := q.Len(); got != serialqueue.Bound {
		t.Fatalf("queued %d callbacks, want the bound %d", got, serialqueue.Bound)
	}
	if got := q.Dropped(); got != extra {
		t.Fatalf("dropped %d, want %d", got, extra)
	}
	if n := strings.Count(logged.String(), "matter.callbacks.dropped"); n != 1 {
		t.Fatalf("logged the drop %d times, want once per episode:\n%s", n, logged.String())
	}
	if got := g.Snapshot()[inflight.DeviceCallbacks]; got != serialqueue.Bound+1 {
		t.Fatalf("counted %d callbacks, want the running one and the %d queued", got, serialqueue.Bound)
	}
	close(release)
	q.Stop()
	if len(ran) != serialqueue.Bound || ran[0] != extra || ran[len(ran)-1] != serialqueue.Bound+extra-1 {
		t.Fatalf("ran %d callbacks from %v to %v, want the newest %d in order", len(ran), ran[0], ran[len(ran)-1], serialqueue.Bound)
	}
	if g.Busy() {
		t.Fatalf("counted %v after the queue drained", g.Snapshot())
	}
}

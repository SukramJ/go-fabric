// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package serialqueue_test

import (
	"bytes"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/internal/inflight"
	"thingfabric.dev/go-fabric/internal/serialqueue"
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

// TestQueueReportsWhatAnOverflowCost: when a queue that dropped callbacks
// runs empty again, it logs how many the overflow episode dropped and how
// many the queue dropped in all, so the host learns the whole count, not
// only that dropping began (review II, R2-P6-bound-0209).
func TestQueueReportsWhatAnOverflowCost(t *testing.T) {
	q := serialqueue.New()
	var logged bytes.Buffer
	q.SetLogger(slog.New(slog.NewTextHandler(&logged, nil)))
	q.Start()
	for _, extra := range []int{7, 3} {
		release, running := make(chan struct{}), make(chan struct{})
		q.Enqueue(func() {
			close(running)
			<-release
		})
		<-running
		for range serialqueue.Bound + extra {
			q.Enqueue(func() {})
		}
		close(release)
		for q.Len() > 0 {
			runtime.Gosched()
		}
		q.Enqueue(func() {}) // runs after the episode's last callback
		for q.Len() > 0 {
			runtime.Gosched()
		}
	}
	q.Stop()
	out := logged.String()
	for _, want := range []string{"dropped=7 total=7", "dropped=3 total=10"} {
		if !strings.Contains(out, "matter.callbacks.overflow_ended") || !strings.Contains(out, want) {
			t.Fatalf("the log does not report an episode with %q:\n%s", want, out)
		}
	}
}

// TestQueueExemptCallbacksAreNeverDropped (review II, R4-err-4751,
// decision 615): a callback queued with EnqueueExempt survives a full
// queue — it is not dropped at the bound, not counted against it, and
// runs in its place in the order, the droppable callbacks around it
// dropped and counted as before.
func TestQueueExemptCallbacksAreNeverDropped(t *testing.T) {
	g := inflight.New("device 1")
	q := serialqueue.New()
	q.CountIn(func() *inflight.Group { return g })
	q.Start()
	release, running := make(chan struct{}), make(chan struct{})
	q.Enqueue(func() {
		close(running)
		<-release
	})
	<-running
	var ran []int
	q.Enqueue(func() { ran = append(ran, -1) }) // the oldest droppable: dropped
	q.EnqueueExempt(func() { ran = append(ran, -2) })
	q.EnqueueExempt(nil) // a no-op
	const extra = 10
	for i := range serialqueue.Bound + extra {
		q.Enqueue(func() { ran = append(ran, i) })
	}
	q.EnqueueExempt(func() { ran = append(ran, -3) })
	if got := q.Len(); got != serialqueue.Bound+2 {
		t.Fatalf("queued %d callbacks, want the bound %d and the 2 exempt", got, serialqueue.Bound)
	}
	if got := q.Dropped(); got != extra+1 {
		t.Fatalf("dropped %d, want %d", got, extra+1)
	}
	close(release)
	q.Stop()
	if len(ran) != serialqueue.Bound+2 || ran[0] != -2 || ran[1] != extra || ran[len(ran)-1] != -3 {
		t.Fatalf("ran %d callbacks, first %v, %v, last %v; want the exempt -2 first, the newest %d droppable from %d, the exempt -3 last",
			len(ran), ran[0], ran[1], ran[len(ran)-1], serialqueue.Bound, extra)
	}
	if g.Busy() {
		t.Fatalf("counted %v after the queue drained", g.Snapshot())
	}
	q.EnqueueExempt(func() { t.Error("an exempt callback ran on a stopped queue") })
}

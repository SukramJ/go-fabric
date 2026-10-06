// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package cluster_test

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
)

var _ contract.AttributeChangeNotifier = (*cluster.AttributeChanges)(nil)

func TestAttributeChangesNotifiesInOrderAndUnsubscribes(t *testing.T) {
	t.Parallel()
	var c cluster.AttributeChanges
	var got []string
	unA := c.OnMatterAttributesChanged(func(ids []uint32) { got = append(got, "a"+string(rune('0'+ids[0]))) })
	c.OnMatterAttributesChanged(func(ids []uint32) {
		ids[0] = 9 // a listener's slice is its own
		got = append(got, "b")
	})
	c.Notify()
	c.Notify(1)
	unA()
	unA()
	c.Notify(2)
	if want := []string{"a1", "b", "b"}; !slices.Equal(got, want) {
		t.Errorf("calls %v, want %v", got, want)
	}
}

// fakeClock drives a Quieter by hand.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	armed []func()
	waits []time.Duration
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) AfterFunc(d time.Duration, fn func()) *time.Timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.armed = append(f.armed, fn)
	f.waits = append(f.waits, d)
	return time.NewTimer(time.Hour)
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *fakeClock) fire(i int) { f.armed[i]() }

// TestQuieterMirrorsMatterJSQuietEvent walks the QuietObservable rules: a
// change from or to null reports at once, the first non-null change
// reports at once, a change within a second of the last report is held
// and reported once with the latest value when the second is up.
func TestQuieterMirrorsMatterJSQuietEvent(t *testing.T) {
	t.Parallel()
	clk := &fakeClock{now: time.Unix(1000, 0)}
	reports := 0
	q := &cluster.Quieter{Report: func() { reports++ }, Now: clk.Now, AfterFunc: clk.AfterFunc}

	q.Changed(true, false) // null → 60: now
	if reports != 1 {
		t.Fatalf("null → value: %d reports, want 1", reports)
	}
	clk.advance(100 * time.Millisecond)
	q.Changed(false, false) // 60 → 59 within the second: held
	q.Changed(false, false) // 59 → 58: still held, one timer
	if reports != 1 || len(clk.armed) != 1 || clk.waits[0] != 900*time.Millisecond {
		t.Fatalf("held changes: %d reports, %d timers (%v)", reports, len(clk.armed), clk.waits)
	}
	clk.advance(900 * time.Millisecond)
	clk.fire(0)
	if reports != 2 {
		t.Fatalf("after the interval: %d reports, want 2", reports)
	}
	clk.fire(0) // a stale timer does nothing
	if reports != 2 {
		t.Fatalf("stale timer reported: %d", reports)
	}
	clk.advance(1001 * time.Millisecond)
	q.Changed(false, false) // more than a second later: now
	if reports != 3 {
		t.Fatalf("after a quiet second: %d reports, want 3", reports)
	}
	clk.advance(10 * time.Millisecond)
	q.Changed(false, false) // held …
	q.Changed(false, true)  // … then to null: now, and the held one is dropped
	if reports != 4 {
		t.Fatalf("value → null: %d reports, want 4", reports)
	}
	clk.fire(1)
	if reports != 4 {
		t.Fatalf("a timer superseded by an immediate report fired: %d", reports)
	}
}

// TestQuieterDefaultsToTheWallClock lets the real timer deliver a held
// report.
func TestQuieterDefaultsToTheWallClock(t *testing.T) {
	t.Parallel()
	done := make(chan struct{}, 2)
	q := &cluster.Quieter{Report: func() { done <- struct{}{} }}
	q.Changed(false, false)
	<-done
	q.Changed(false, false)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the held report never went out")
	}
}

// TestQuieterEmitNow is matter.js QuietObservable.emitNow, which a
// transition calls when it ends: a held report goes out at once (and its
// timer no longer fires), and with nothing held nothing is reported.
func TestQuieterEmitNow(t *testing.T) {
	t.Parallel()
	clk := &fakeClock{now: time.Unix(1000, 0)}
	reports := 0
	q := &cluster.Quieter{Report: func() { reports++ }, Now: clk.Now, AfterFunc: clk.AfterFunc}

	q.EmitNow()
	if reports != 0 {
		t.Fatalf("EmitNow with nothing held: %d reports", reports)
	}
	q.Changed(false, false) // first change: now
	clk.advance(200 * time.Millisecond)
	q.Changed(false, false) // held
	q.EmitNow()
	if reports != 2 {
		t.Fatalf("EmitNow with a held change: %d reports, want 2", reports)
	}
	clk.fire(0)
	q.EmitNow()
	if reports != 2 {
		t.Fatalf("the superseded timer or a second EmitNow reported: %d", reports)
	}
	clk.advance(300 * time.Millisecond)
	q.Changed(false, false) // within a second of the EmitNow report: held
	if reports != 2 {
		t.Fatalf("EmitNow did not count as the last report: %d", reports)
	}
}

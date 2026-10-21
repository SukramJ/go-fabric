// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cluster_test

import (
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
)

var _ contract.AttributeChangeNotifier = (*cluster.AttributeChanges)(nil)

func TestAttributeChangesNotifiesInOrderAndUnsubscribes(t *testing.T) {
	t.Parallel()
	var c cluster.AttributeChanges
	var got []string
	unA := c.OnAttributeChange(func(change contract.Change) {
		ids := change.Attributes
		got = append(got, "a"+string(rune('0'+ids[0])))
	})
	c.OnAttributeChange(func(change contract.Change) {
		ids := change.Attributes
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

// TestQuieterMirrorsMatterJSQuietEvent walks the QuietObservable rules: a
// change from or to null reports at once, the first non-null change
// reports at once, a change within a second of the last report is held
// and reported once with the latest value when the second is up.
func TestQuieterMirrorsMatterJSQuietEvent(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1000, 0))
	reports := 0
	q := &cluster.Quieter{Report: func() { reports++ }, Clock: clk}

	q.Changed(true, false) // null → 60: now
	if reports != 1 {
		t.Fatalf("null → value: %d reports, want 1", reports)
	}
	clk.Advance(100 * time.Millisecond)
	q.Changed(false, false) // 60 → 59 within the second: held
	q.Changed(false, false) // 59 → 58: still held, one timer
	if armed := clk.Armed(); reports != 1 || len(armed) != 1 || armed[0] != 900*time.Millisecond {
		t.Fatalf("held changes: %d reports, timers %v", reports, armed)
	}
	clk.Advance(900 * time.Millisecond)
	if reports != 2 {
		t.Fatalf("after the interval: %d reports, want 2", reports)
	}
	if n := clk.Pending(); n != 0 { // the report went out; nothing is held
		t.Fatalf("%d timers armed after the held report", n)
	}
	clk.Advance(1001 * time.Millisecond)
	q.Changed(false, false) // more than a second later: now
	if reports != 3 {
		t.Fatalf("after a quiet second: %d reports, want 3", reports)
	}
	clk.Advance(10 * time.Millisecond)
	q.Changed(false, false) // held …
	q.Changed(false, true)  // … then to null: now, and the held one is dropped
	if reports != 4 {
		t.Fatalf("value → null: %d reports, want 4", reports)
	}
	clk.Advance(time.Second)
	if reports != 4 || clk.Pending() != 0 {
		t.Fatalf("a timer superseded by an immediate report fired: %d reports, %d armed", reports, clk.Pending())
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
	clk := clocktest.New(time.Unix(1000, 0))
	reports := 0
	q := &cluster.Quieter{Report: func() { reports++ }, Clock: clk}

	q.EmitNow()
	if reports != 0 {
		t.Fatalf("EmitNow with nothing held: %d reports", reports)
	}
	q.Changed(false, false) // first change: now
	clk.Advance(200 * time.Millisecond)
	q.Changed(false, false) // held
	q.EmitNow()
	if reports != 2 {
		t.Fatalf("EmitNow with a held change: %d reports, want 2", reports)
	}
	if n := clk.Pending(); n != 0 {
		t.Fatalf("EmitNow left the held report's timer armed (%d)", n)
	}
	q.EmitNow()
	if reports != 2 {
		t.Fatalf("the superseded timer or a second EmitNow reported: %d", reports)
	}
	clk.Advance(300 * time.Millisecond)
	q.Changed(false, false) // within a second of the EmitNow report: held
	if reports != 2 {
		t.Fatalf("EmitNow did not count as the last report: %d", reports)
	}
}

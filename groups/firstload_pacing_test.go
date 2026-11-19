// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// TestFirstLoadIsPacedOnTheReceivePath: while the store's ListFabrics
// fails and the group fabrics were never loaded, a flood of group
// datagrams costs one store call per interval — the same 1 s doubling
// pacing an owed reload has — each datagram refused with the load's
// error; Load does not wait, and a recovered store loads at the next due
// attempt (review II, R2-P7-sec-04).
func TestFirstLoadIsPacedOnTheReceivePath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	fake := clocktest.New(time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC))
	m.SetClock(fake)
	st.mu.Lock()
	st.failList = true
	st.mu.Unlock()
	op1, sid1 := opKeyOf(t, 0xA1)
	counter := uint32(0)
	lists := func() int {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.listCalls
	}
	flood := func(n int) {
		t.Helper()
		for range n {
			counter++
			if _, err := m.Decode(ctx, sealGroup(t, op1, sid1, 0x0101, 7, counter, true, imPayload([]byte("x")))); err == nil {
				t.Fatal("a group datagram decoded while the first load fails")
			}
		}
	}
	before := lists()
	flood(100)
	if got := lists() - before; got != 1 {
		t.Fatalf("%d ListFabrics calls for 100 datagrams within the first interval, want 1", got)
	}
	for _, step := range []struct {
		advance time.Duration
		want    int
	}{
		{time.Second, 1},     // the 1 s interval passed: one more try, the next in 2 s
		{time.Second, 0},     // 1 s of 2
		{time.Second, 1},     // 2 s passed; next in 4 s
		{3 * time.Second, 0}, // 3 s of 4
	} {
		fake.Advance(step.advance)
		before = lists()
		flood(20)
		if got := lists() - before; got != step.want {
			t.Fatalf("after advancing %v: %d ListFabrics calls for 20 datagrams, want %d", step.advance, got, step.want)
		}
	}
	before = lists()
	if err := m.Load(ctx); err == nil {
		t.Fatal("Load while the store fails: nil error")
	}
	if got := lists() - before; got != 1 {
		t.Fatalf("Load called ListFabrics %d times, want 1 (it does not wait for the interval)", got)
	}

	st.mu.Lock()
	st.failList = false
	st.mu.Unlock()
	before = lists()
	flood(5) // still within the interval Load's failure set: refused, no store call
	if got := lists() - before; got != 0 {
		t.Fatalf("%d ListFabrics calls before the interval passed, want 0", got)
	}
	fake.Advance(time.Minute)
	counter++
	if msg, err := m.Decode(ctx, sealGroup(t, op1, sid1, 0x0101, 7, counter, true, imPayload([]byte("x")))); err != nil || msg.Duplicate {
		t.Fatalf("a group datagram after the store recovered: err %v", err)
	}
	if got := lists() - before; got != 1 {
		t.Fatalf("the recovered load called ListFabrics %d times, want 1", got)
	}
	before = lists()
	counter++
	if _, err := m.Decode(ctx, sealGroup(t, op1, sid1, 0x0101, 7, counter, true, imPayload([]byte("x")))); err != nil {
		t.Fatalf("a second datagram after the load: %v", err)
	}
	if got := lists() - before; got != 0 {
		t.Fatalf("%d ListFabrics calls once loaded, want 0", got)
	}
}

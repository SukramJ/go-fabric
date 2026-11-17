// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/store"
)

var errStoreDown = errors.New("store down")

// twoFabricManager is newTestManager plus fabric 2 (compressed id 0x22…,
// key set 0x20 with epoch key 0xC3… mapped to group 0x0202), both loaded,
// and fabric 2's operational key and session id.
func twoFabricManager(t *testing.T) (m *Manager, st *memStore, op2 []byte, sid2 uint16) {
	t.Helper()
	m, st = newTestManager(t)
	compressed2 := [8]byte{0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22, 0x22}
	st.addFabric(2, 0x0FAC, compressed2)
	st.putKeySet(2, store.GroupKeySet{GroupKeySetID: 0x20, EpochKey0: epochKeyOf(0xC3), EpochStart0: 1})
	st.mapGroup(2, 0x0202, 0x20)
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	op2, err := OperationalKey(epochKeyOf(0xC3), compressed2)
	if err != nil {
		t.Fatal(err)
	}
	if sid2, err = SessionID(op2); err != nil {
		t.Fatal(err)
	}
	return m, st, op2, sid2
}

func parkedFabrics(m *Manager) int {
	m.receptionMu.Lock()
	defer m.receptionMu.Unlock()
	return len(m.parkedReception)
}

// TestForgetOwesTheReloadOfThatFabricOnly: after ForgetFabric only the
// forgotten fabric is reloaded on its next use; while the store fails, the
// other fabrics go on decoding from memory and only the forgotten one is
// absent — the store failure is not widened to every fabric.
func TestForgetOwesTheReloadOfThatFabricOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op1, sid1 := opKeyOf(t, 0xA1)
	m, st, op2, sid2 := twoFabricManager(t)
	st.mu.Lock()
	st.failList, st.failGet = true, errStoreDown
	st.mu.Unlock()

	m.ForgetFabric(1)
	if msg, err := m.Decode(ctx, sealGroup(t, op2, sid2, 0x0202, 9, 1, true, []byte("x"))); err != nil || msg.Duplicate {
		t.Fatalf("a message of fabric 2 while the store fails after ForgetFabric(1): err %v", err)
	}
	if _, err := m.Decode(ctx, sealGroup(t, op1, sid1, 0x0101, 7, 1, true, []byte("x"))); err == nil {
		t.Fatal("fabric 1 decoded while its reload failed")
	}
	if err := m.Load(ctx); !errors.Is(err, errStoreDown) {
		t.Fatalf("Load while the store fails: %v, want the store's error", err)
	}

	st.mu.Lock()
	st.failList, st.failGet = false, nil
	st.mu.Unlock()
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if msg, err := m.Decode(ctx, sealGroup(t, op1, sid1, 0x0101, 7, 2, true, []byte("x"))); err != nil || msg.Duplicate {
		t.Fatalf("fabric 1 after the store recovered: err %v", err)
	}
}

// TestParkedWindowsSurviveASecondForget: forget, a failed load, forget
// again, a load that succeeds — the windows parked by the first forget
// still come back, so a recorded datagram stays a duplicate.
func TestParkedWindowsSurviveASecondForget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op, sid := opKeyOf(t, 0xA1)
	m, st := newTestManager(t)
	recorded := sealGroup(t, op, sid, 0x0101, 7, 100, true, []byte("toggle"))
	if msg, err := m.Decode(ctx, recorded); err != nil || msg.Duplicate {
		t.Fatalf("the first message: err %v", err)
	}
	m.ForgetFabric(1)
	st.mu.Lock()
	st.failGet = errStoreDown
	st.mu.Unlock()
	if err := m.Load(ctx); err == nil {
		t.Fatal("Load succeeded with a failing store")
	}
	m.ForgetFabric(1)
	st.mu.Lock()
	st.failGet = nil
	st.mu.Unlock()
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if msg, err := m.Decode(ctx, recorded); err != nil || !msg.Duplicate {
		t.Fatalf("the recorded datagram after forget, failed load, forget, load: err %v, want a duplicate", err)
	}
}

// TestParkedWindowWinsOverAFreshOne: a decode in flight across the forget
// may create a fresh window for the key before the reload; the parked
// window of a source wins over it (a replay the fresh one took stays
// refused afterwards), and a fresh source the parked set lacks keeps its
// window.
func TestParkedWindowWinsOverAFreshOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op, sid := opKeyOf(t, 0xA1)
	m, _ := newTestManager(t)
	recorded := make([][]byte, 3) // counters 100, 101, 102 of source 7
	for i := range recorded {
		recorded[i] = sealGroup(t, op, sid, 0x0101, 7, uint32(100+i), true, []byte("toggle"))
		if msg, err := m.Decode(ctx, recorded[i]); err != nil || msg.Duplicate {
			t.Fatalf("message %d: err %v", i, err)
		}
	}
	m.ForgetFabric(1)
	// The in-flight decode: a fresh window for source 7 takes the replay
	// of counter 100, one for source 9 takes counter 50.
	if !window(m, op, 7).Accept(100) || !window(m, op, 9).Accept(50) {
		t.Fatal("precondition: fresh windows refused their first counter")
	}
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(recorded); i++ {
		if msg, err := m.Decode(ctx, recorded[i]); err != nil || !msg.Duplicate {
			t.Fatalf("the recorded datagram %d of source 7: err %v, want a duplicate (the parked window wins)", 100+i, err)
		}
	}
	if msg, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, 7, 103, true, []byte("toggle"))); err != nil || msg.Duplicate {
		t.Fatalf("source 7's next counter: err %v", err)
	}
	if window(m, op, 9).Accept(50) {
		t.Fatal("the fresh window of source 9, which the parked set lacks, was dropped")
	}
}

// TestParkedWindowsOfARemovedFabricAreDropped: a fabric whose row is gone
// (RemoveFabric) is never loaded again; its parked windows — and with
// them its operational keys — go at the next attempt to load it.
func TestParkedWindowsOfARemovedFabricAreDropped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op, sid := opKeyOf(t, 0xA1)
	m, st := newTestManager(t)
	if _, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, 7, 100, true, []byte("toggle"))); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	delete(st.fabrics, 1)
	st.mu.Unlock()
	m.ForgetFabric(1)
	if parkedFabrics(m) != 1 {
		t.Fatal("precondition: nothing parked")
	}
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if n := parkedFabrics(m); n != 0 {
		t.Fatalf("%d removed fabrics' windows still parked after the load, want 0", n)
	}
}

// TestOwedReloadIsPacedOnTheReceivePath: while the owed fabric's load
// fails, the receive path tries it at most once per interval — 1 s,
// doubling up to 60 s, reset on success — so a group sender cannot drive
// a store read per datagram; an explicit Load tries at once.
func TestOwedReloadIsPacedOnTheReceivePath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st, op2, sid2 := twoFabricManager(t)
	fake := clocktest.New(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC))
	m.SetClock(fake)
	reads := func() int {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.getCalls
	}
	st.mu.Lock()
	st.failGet = errStoreDown
	st.mu.Unlock()
	m.ForgetFabric(1)
	counter := uint32(0)
	flood := func(n int) {
		for range n {
			counter++
			if _, err := m.Decode(ctx, sealGroup(t, op2, sid2, 0x0202, 9, counter, true, []byte("x"))); err != nil {
				t.Fatal(err)
			}
		}
	}
	before := reads()
	flood(100)
	if got := reads() - before; got != 1 {
		t.Fatalf("%d store reads for 100 datagrams within the first interval, want 1", got)
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
		before = reads()
		flood(20)
		if got := reads() - before; got != step.want {
			t.Fatalf("after advancing %v: %d store reads for 20 datagrams, want %d", step.advance, got, step.want)
		}
	}
	before = reads()
	if err := m.Load(ctx); !errors.Is(err, errStoreDown) {
		t.Fatalf("Load: %v", err)
	}
	if got := reads() - before; got != 1 {
		t.Fatalf("Load read the store %d times, want 1 (it does not wait for the interval)", got)
	}
	st.mu.Lock()
	st.failGet = nil
	st.mu.Unlock()
	fake.Advance(time.Minute)
	flood(1)
	op1, sid1 := opKeyOf(t, 0xA1)
	if _, err := m.Decode(ctx, sealGroup(t, op1, sid1, 0x0101, 7, 1, true, []byte("x"))); err != nil {
		t.Fatalf("fabric 1 after the store recovered: %v", err)
	}
}

// TestLoadReadsAnOwedFabricOnce: Load does not run the receive path's
// paced attempt besides its own — one store read and one backoff step per
// Load (round 6).
func TestLoadReadsAnOwedFabricOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st, _, _ := twoFabricManager(t)
	m.SetClock(clocktest.New(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)))
	st.mu.Lock()
	st.failGet = errStoreDown
	st.mu.Unlock()
	m.ForgetFabric(1)
	st.mu.Lock()
	before := st.getCalls
	st.mu.Unlock()
	if err := m.Load(ctx); !errors.Is(err, errStoreDown) {
		t.Fatalf("Load: %v", err)
	}
	st.mu.Lock()
	reads := st.getCalls - before
	st.mu.Unlock()
	m.mu.RLock()
	backoff := m.owedBackoff
	m.mu.RUnlock()
	if reads != 1 || backoff != owedRetryFirst {
		t.Fatalf("one Load: %d store reads, backoff %v; want 1 and %v", reads, backoff, owedRetryFirst)
	}
}

// TestNewForgetStartsUnpaced: a debt cleared by another route (here a
// Reload after the store recovered) leaves no deadline behind — the next
// forget's first receive-path attempt is not held back by the old
// backoff (round 6).
func TestNewForgetStartsUnpaced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	op1, sid1 := opKeyOf(t, 0xA1)
	m, st, op2, sid2 := twoFabricManager(t)
	m.SetClock(clocktest.New(time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)))
	st.mu.Lock()
	st.failGet = errStoreDown
	st.mu.Unlock()
	m.ForgetFabric(1)
	for c := range uint32(3) { // paced attempts fail: a backoff is set
		if _, err := m.Decode(ctx, sealGroup(t, op2, sid2, 0x0202, 9, c+1, true, []byte("x"))); err != nil {
			t.Fatal(err)
		}
	}
	st.mu.Lock()
	st.failGet = nil
	st.mu.Unlock()
	if err := m.Reload(ctx, 1); err != nil { // the debt is cleared, not through the pacing
		t.Fatal(err)
	}
	m.ForgetFabric(2)
	if msg, err := m.Decode(ctx, sealGroup(t, op2, sid2, 0x0202, 9, 10, true, []byte("x"))); err != nil || msg.Duplicate {
		t.Fatalf("fabric 2 on the first receive after its forget: err %v (held back by an old backoff?)", err)
	}
	if msg, err := m.Decode(ctx, sealGroup(t, op1, sid1, 0x0101, 7, 1, true, []byte("x"))); err != nil || msg.Duplicate {
		t.Fatalf("fabric 1 after its Reload: err %v", err)
	}
}

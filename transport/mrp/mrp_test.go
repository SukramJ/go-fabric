// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mrp

import (
	"sync"
	"testing"
)

// --- Counter ---

// TestCounterMonotonic confirms successive Next() calls return
// strictly increasing values from a deterministic seed.
func TestCounterMonotonic(t *testing.T) {
	c := NewCounterFromSeed(100)
	prev := c.Next()
	for range 10 {
		next := c.Next()
		if next != prev+1 {
			t.Fatalf("got %d after %d, want %d", next, prev, prev+1)
		}
		prev = next
	}
}

// TestCounterWrap covers the 2^32 wrap-around. After wrap the
// successor must be 0, not negative or stuck.
func TestCounterWrap(t *testing.T) {
	c := NewCounterFromSeed(0xFFFFFFFF)
	if c.Next() != 0xFFFFFFFF {
		t.Fatal("first Next did not return seed")
	}
	if c.Next() != 0 {
		t.Fatal("wrap did not produce 0")
	}
}

// TestCounterPeekDoesNotAdvance ensures Peek is non-mutating.
func TestCounterPeekDoesNotAdvance(t *testing.T) {
	c := NewCounterFromSeed(7)
	if c.Peek() != 7 {
		t.Fatal("first Peek did not return seed")
	}
	if c.Peek() != 7 {
		t.Fatal("second Peek mutated the counter")
	}
	c.Next()
	if c.Peek() != 8 {
		t.Fatal("Peek did not reflect the advance")
	}
}

// TestCounterConcurrentAdvance race-tests the atomic increment.
func TestCounterConcurrentAdvance(t *testing.T) {
	c := NewCounterFromSeed(0)
	const N = 1000
	var wg sync.WaitGroup
	wg.Add(N)
	seen := make([]uint32, N)
	for i := range N {
		go func(i int) {
			defer wg.Done()
			seen[i] = c.Next()
		}(i)
	}
	wg.Wait()
	uniq := map[uint32]struct{}{}
	for _, v := range seen {
		uniq[v] = struct{}{}
	}
	if len(uniq) != N {
		t.Fatalf("got %d unique values, want %d", len(uniq), N)
	}
}

// --- Window ---

// TestWindowAcceptsFreshAndRejectsDuplicate covers the basic
// bitmap-tracking surface.
func TestWindowAcceptsFreshAndRejectsDuplicate(t *testing.T) {
	w := NewWindow()
	if !w.Accept(100) {
		t.Fatal("first counter rejected")
	}
	if w.Accept(100) {
		t.Fatal("duplicate accepted")
	}
}

// TestWindowAcceptsAdvancing emulates an in-order counter stream.
func TestWindowAcceptsAdvancing(t *testing.T) {
	w := NewWindow()
	for i := range uint32(10) {
		if !w.Accept(i) {
			t.Fatalf("counter %d rejected", i)
		}
	}
}

// TestWindowAcceptsBackInWindow allows older-but-recent counters
// (out-of-order delivery within the 32-slot window).
func TestWindowAcceptsBackInWindow(t *testing.T) {
	w := NewWindow()
	w.Accept(10)
	if !w.Accept(5) {
		t.Fatal("in-window 5 rejected after seeing 10")
	}
	if w.Accept(5) {
		t.Fatal("duplicate 5 accepted")
	}
}

// TestNoRolloverWindowRejectsStale rejects counters older than 32 slots
// back. Secure variant only — the unsecured window reads the same
// distance as a restarted peer counter and rolls forward instead, see
// TestRolloverWindowRollsBackOnPeerRestart.
func TestNoRolloverWindowRejectsStale(t *testing.T) {
	w := NewWindowNoRollover()
	w.Accept(100)
	if w.Accept(50) {
		t.Fatal("stale 50 accepted (>= windowSize behind 100)")
	}
}

// TestNoRolloverWindowResetsOnBigJump simulates a session re-key where
// the counter trajectory restarts at a much larger value.
func TestNoRolloverWindowResetsOnBigJump(t *testing.T) {
	w := NewWindowNoRollover()
	w.Accept(10)
	if !w.Accept(1000) {
		t.Fatal("big jump rejected")
	}
	// The pre-jump counters fall outside the new window.
	if w.Accept(15) {
		t.Fatal("pre-jump counter accepted post-reset")
	}
}

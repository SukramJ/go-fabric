// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

import (
	"testing"
	"time"
)

// TestAllocateIDAtAFullTableTakesTheOldestPlaceholderWithoutAWalk: at a full
// id space an allocation reclaims the oldest staked id
// (BD-Matter-CASE-IDExhaustionEvictsPlaceholdersOnly) from a queue, without
// walking the 65534-entry table under the manager's write lock (review II,
// R2-P3-lan-dev-02: two walks per allocation, about 0.5 ms each, measured).
func TestAllocateIDAtAFullTableTakesTheOldestPlaceholderWithoutAWalk(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	order := make([]uint16, 0, SessionIDSpace)
	for range SessionIDSpace {
		id, err := m.AllocateID()
		if err != nil {
			t.Fatalf("AllocateID: %v", err)
		}
		order = append(order, id)
	}
	// A released placeholder leaves the queue as it leaves the table.
	m.ReleaseID(order[0])
	if id, err := m.AllocateID(); err != nil || id != order[0] {
		t.Fatalf("AllocateID with one free id = (%d, %v), want (%d, nil)", id, err, order[0])
	}
	order = append(order[1:], order[0])

	start := time.Now()
	for i, want := range order {
		id, err := m.AllocateID()
		if err != nil {
			t.Fatalf("AllocateID at a full table: %v", err)
		}
		if id != want {
			t.Fatalf("allocation %d at a full table took id %d, want the oldest placeholder %d", i, id, want)
		}
	}
	elapsed := time.Since(start)
	t.Logf("%d allocations at a full table: %v", len(order), elapsed)
	// Two walks of the table per allocation took about 0.5 ms each
	// (R2-P3-lan-dev-02), some 30 s for these; the queue takes a few
	// microseconds each. The bound leaves a wide margin for a loaded machine.
	if elapsed > 5*time.Second {
		t.Fatalf("%d allocations at a full table took %v", len(order), elapsed)
	}
}

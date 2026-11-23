// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"testing"
)

// TestCandidatesAreLookedUpBySession: the candidates of a group session
// id come from an index the fabric's load builds, so a datagram whose
// session id no key carries costs a walk of the fabrics, not a rebuild
// of every fabric's key sets (review II, R2-C2-bound-0301).
func TestCandidatesAreLookedUpBySession(t *testing.T) {
	m, carried := newBenchManager(t, 20, maxBenchKeySets, false)
	if err := m.ensureAllLoaded(context.Background(), loadForDatagram); err != nil {
		t.Fatal(err)
	}
	unused := unusedSessionID(t, m)
	if allocs := testing.AllocsPerRun(50, func() { _ = m.candidates(unused) }); allocs > 1 {
		t.Errorf("candidates of an unused session id: %.0f allocations, want at most 1 (the fabric order)", allocs)
	}
	got := m.candidates(carried)
	if len(got) != 1 || got[0].fabric.index != 1 || got[0].keySetID != 1 {
		t.Fatalf("candidates of the first key's session id: %+v, want fabric 1, key set 1", got)
	}
}

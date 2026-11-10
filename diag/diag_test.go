// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package diag_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/diag"
)

// TestDiagnosticsEffective: a capacity below one keeps the default, any
// other is taken as it is.
func TestDiagnosticsEffective(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ capacity, want int }{
		{0, diag.DefaultDiagnosticsCapacity},
		{-3, diag.DefaultDiagnosticsCapacity},
		{1, 1},
		{40, 40},
	} {
		if got := (diag.Diagnostics{Capacity: tc.capacity}).Effective(); got != tc.want {
			t.Errorf("Capacity %d: Effective %d, want %d", tc.capacity, got, tc.want)
		}
	}
}

// TestZeroValues: the zero snapshot holds no session and no peer, and its
// occupancy reads as untracked (Capacity 0).
func TestZeroValues(t *testing.T) {
	t.Parallel()
	var d diag.SessionDiagnostics
	if d.Sessions != nil || d.Peers != nil || d.Occupancy != (diag.Occupancy{}) || d.Occupancy.Capacity != 0 {
		t.Fatalf("zero snapshot %+v", d)
	}
	var p diag.PeerInfo
	if p.Registered || p.State != "" || p.SessionIDs != nil {
		t.Fatalf("zero peer %+v", p)
	}
}

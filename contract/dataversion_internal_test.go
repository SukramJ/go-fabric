// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

// Whitebox tests of DataVersionTracker that set the packed state directly.

import (
	"math"
	"testing"
)

// TestDataVersionTracker_Bump_WrapsToZero (owner decision 146, review II,
// F-db021da0-17): 0 is an ordinary version — the tracker holds "installed"
// beside the value, so Bump wraps from 0xFFFFFFFF to 0, and a tracker
// whose version is 0 keeps it, as matter.js's datasource version, which
// starts at a random uint32 and wraps to 0 (fable-6a-w3 H4:
// Datasource.ts:597, :690-699).
func TestDataVersionTracker_Bump_WrapsToZero(t *testing.T) {
	t.Parallel()
	var dv DataVersionTracker
	dv.Current()
	dv.v.Store(installedVersion | math.MaxUint32)
	if next := dv.Bump(); next != 0 {
		t.Fatalf("Bump after 0xFFFFFFFF = %d, want 0", next)
	}
	if got := dv.Current(); got != 0 {
		t.Fatalf("Current after the wrap = %d, want 0 kept, not a fresh random version", got)
	}
	if next := dv.Bump(); next != 1 {
		t.Fatalf("Bump after 0 = %d, want 1", next)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import "testing"

// TestSlotOfAny: a forgotten key names its own clusters' slots, a
// manufacturer-specific cluster's included, and not those of a key that
// extends it by a dot.
func TestSlotOfAny(t *testing.T) {
	t.Parallel()
	for slot, want := range map[string]bool{
		"nonvolatile.x.0201":       true,
		"nonvolatile.x.fff1fc00":   true,
		"nonvolatile.x.ab.0201":    false,
		"nonvolatile.xy.0201":      false,
		"nonvolatile.x.":           false,
		"nonvolatile.x.0201.0012":  false,
		"bridged.userlabel.x.0041": false,
	} {
		if got := slotOfAny(slot, []string{"x"}); got != want {
			t.Errorf("slotOfAny(%q, [x]) = %v, want %v", slot, got, want)
		}
	}
}

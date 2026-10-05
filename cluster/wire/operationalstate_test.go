// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/wire"
)

// TestHasOperationalStateLabel pins the manufacturer-specific range the
// OperationalStateLabel / ErrorStateLabel conformance names:
// "OperationalStateID >= 128 & OperationalStateID <= 191"
// (operational-state.element.ts:93-95, :110-113).
func TestHasOperationalStateLabel(t *testing.T) {
	t.Parallel()
	for id, want := range map[uint8]bool{0x00: false, 0x03: false, 0x46: false, 0x7F: false, 0x80: true, 0xBF: true, 0xC0: false, 0xFF: false} {
		if got := wire.HasOperationalStateLabel(id); got != want {
			t.Errorf("HasOperationalStateLabel(0x%02X) = %v, want %v", id, got, want)
		}
	}
}

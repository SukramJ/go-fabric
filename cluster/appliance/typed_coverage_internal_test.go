// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package appliance

import (
	"slices"
	"testing"
)

// TestListOfTakesBothListForms: a list is the host's plain slice or the
// server's stored form; anything else is no list of T.
func TestListOfTakesBothListForms(t *testing.T) {
	t.Parallel()

	if got, ok := listOf[uint8]([]uint8{1, 2}); !ok || !slices.Equal(got, []uint8{1, 2}) {
		t.Errorf("plain slice = %v, %v", got, ok)
	}
	if got, ok := listOf[uint8](uintList[uint8]{3, 4}); !ok || !slices.Equal(got, []uint8{3, 4}) {
		t.Errorf("stored list = %v, %v", got, ok)
	}
	for _, v := range []any{nil, "x", []uint16{1}, uint8(1)} {
		if got, ok := listOf[uint8](v); ok || got != nil {
			t.Errorf("listOf(%#v) = %v, %v, want no list", v, got, ok)
		}
	}
}

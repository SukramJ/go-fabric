// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"strings"
	"testing"
)

// TestCpMaxParity: a string constraint with a codepoint bound ("max
// 128{32}", Actions ActionStruct.Name) refuses more codepoints than it
// states after the length check, as matter.js
// packages/node/src/behavior/state/validation/constraint.ts:213-232 at
// 862ad75f does (W3-clugen-02).
func TestCpMaxParity(t *testing.T) {
	t.Parallel()
	inst := &Instance{def: &Cluster{Name: "T"}}
	typ := Type{Name: "string", Kind: KindString}
	c := Constraint{Text: "max 128{32}", Max: &Bound{Int: 128}, CpMax: 32}
	for _, tc := range []struct {
		value string
		ok    bool
	}{
		{strings.Repeat("a", 32), true},
		{strings.Repeat("a", 33), false},
		{strings.Repeat("語", 32), true},
		{strings.Repeat("語", 33), false},
		{strings.Repeat("a", 129), false},
	} {
		_, why := inst.checkLength(typ, c, tc.value, nil)
		if (why == "") != tc.ok {
			t.Errorf("%d codepoints / %d bytes: %q, want ok %v", len([]rune(tc.value)), len(tc.value), why, tc.ok)
		}
	}
}

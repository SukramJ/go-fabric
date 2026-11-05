// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import "testing"

// TestDefaultValueParityMatterJS mirrors
// packages/model/test/logic/DefaultValueTest.ts:49-70 at 862ad75f: a
// default stated in a unit is counted in the units of its type (1 % of
// 0.01 % steps is 1), a floating-point default keeps its fraction, and a
// value with no unit stays as it is — the literal script/clustergen
// writes into the definition (defaultLiteral). The list and bitmap
// defaults matter.js derives without a stated one, and the scale of a
// type derived from a scaled one, are findings-register rows.
func TestDefaultValueParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		it, raw, typeName, metatype, want string
	}{
		{"counts the units of the type", `{"type":"percent","value":0.01}`, "percent100ths", "integer", "float64(1)"},
		{"keeps the fraction of a floating point default (double)", `1e-16`, "double", "float", "float64(1e-16)"},
		{"keeps the fraction of a floating point default (double, 1+ε)", `1.0000000000000002`, "double", "float", "float64(1.0000000000000002)"},
		{"keeps the fraction of a floating point default (single)", `0.1`, "single", "float", "float64(0.1)"},
		{"leaves a value with no unit alone", `5`, "uint8", "integer", "float64(5)"},
		{"is absent when the type it derives from requires an entry", ``, "list", "array", ""},
	} {
		if got := defaultLiteral([]byte(c.raw), c.typeName, c.metatype); got != c.want {
			t.Errorf("%s: defaultLiteral(%s, %s) = %q, want %q", c.it, c.raw, c.typeName, got, c.want)
		}
	}
}

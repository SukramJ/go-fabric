// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import "testing"

// TestDefaultValueParityMatterJS mirrors
// packages/model/test/logic/DefaultValueTest.ts:49-70 at db021da0: a
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

// TestStructDefaultFromItsFields: a struct-typed attribute without a
// default of its own gets the one its fields state — OperationalState's
// OperationalError starts as {errorStateId: 0}
// (operational-state.element.ts:110-111 at db021da0) — and only a
// mandatory, non-nullable field with a scalar default enters it — this
// generator's narrowing of matter.js's rule (DefaultValue.ts:153-179 at
// db021da0 takes every conformant child with a default).
func TestStructDefaultFromItsFields(t *testing.T) {
	t.Parallel()
	mk := func(name string, k kind, mandatory, nullable bool, def string) *fieldInfo {
		f := &fieldInfo{name: name, goName: name, t: typeInfo{kind: k}, mandatory: mandatory, nullable: nullable, v: &value{}}
		if def != "" {
			f.v.Default = []byte(def)
		}
		return f
	}
	st := &dataInfo{goName: "ErrorStateStruct", kind: kindStruct, fields: []*fieldInfo{
		mk("ErrorStateId", kindEnum, true, false, "0"),
		mk("ErrorStateLabel", kindString, false, false, `"x"`),
		mk("Nullable", kindUint, true, true, "1"),
		mk("NoDefault", kindUint, true, false, ""),
		mk("Flag", kindBool, true, false, "true"),
	}}
	if got, want := structDefault(typeInfo{kind: kindStruct, dt: st}), "ErrorStateStruct{ErrorStateId: 0, Flag: true}"; got != want {
		t.Errorf("structDefault = %q, want %q", got, want)
	}
	if got := structDefault(typeInfo{kind: kindUint}); got != "" {
		t.Errorf("a scalar: %q", got)
	}
	if got := structDefault(typeInfo{kind: kindStruct, dt: &dataInfo{goName: "Empty", kind: kindStruct}}); got != "" {
		t.Errorf("a struct without field defaults: %q", got)
	}
}

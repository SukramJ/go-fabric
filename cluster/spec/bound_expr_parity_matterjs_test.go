// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "testing"

// TestBoundExpressionParity: a bound the generator keeps as an expression
// tree is computed against the sibling attributes, one row per kind of
// expression the snapshot states (W3-clugen-01). The vectors follow
// matter.js packages/model/test/aspects/ConstraintTest.ts at 862ad75f
// ("2^10", "min maxOf(holdTimeMin, 10)", "0 to segmentDuration / 2",
// "max (2^62) - 1") over Constraint.ts:969-1096 (evaluate): a name that
// does not resolve leaves the bound without a number, which admits.
func TestBoundExpressionParity(t *testing.T) {
	t.Parallel()
	def := &Cluster{Name: "T", Attributes: []Attribute{
		{ID: 1, Name: "NumberOfPositions"},
		{ID: 2, Name: "HoldTimeMin"},
		{ID: 3, Name: "SegmentDuration"},
		{ID: 4, Name: "PanMax"},
		{ID: 5, Name: "PanMin"},
		{ID: 6, Name: "Unserved"},
		{ID: 7, Name: "Limits"},
	}}
	inst := &Instance{def: def}
	type limits struct{ Max int16 }
	peers := func(id uint32) (any, bool) {
		switch id {
		case 1:
			return uint8(3), true
		case 2:
			return uint16(25), true
		case 3:
			return uint32(9), true
		case 4:
			return int16(180), true
		case 5:
			return int16(-180), true
		case 7:
			return limits{Max: 40}, true
		}
		return nil, false
	}
	ref := func(name string) string { return `{"type":"reference","name":"` + name + `"}` }
	for _, c := range []struct {
		name, expr string
		want       float64
		ok         bool
	}{
		{"reference - literal", `{"type":"-","lhs":` + ref("numberOfPositions") + `,"rhs":1}`, 2, true},
		{"reference + literal", `{"type":"+","lhs":` + ref("holdTimeMin") + `,"rhs":1}`, 26, true},
		{"reference - reference", `{"type":"-","lhs":` + ref("panMax") + `,"rhs":` + ref("panMin") + `}`, 360, true},
		{"literal * expression", `{"type":"*","lhs":-1,"rhs":{"type":"-","lhs":` + ref("panMax") + `,"rhs":` + ref("panMin") + `}}`, -360, true},
		{"literal - reference", `{"type":"-","lhs":1500,"rhs":` + ref("holdTimeMin") + `}`, 1475, true},
		{"reference / literal", `{"type":"/","lhs":` + ref("segmentDuration") + `,"rhs":2}`, 4.5, true},
		{"power", `{"type":"^","lhs":2,"rhs":10}`, 1024, true},
		{"negative power is -(|a|^b)", `{"type":"^","lhs":-2,"rhs":62}`, -(1 << 62), true},
		{"power - literal", `{"type":"-","lhs":{"type":"^","lhs":2,"rhs":62},"rhs":1}`, 1<<62 - 1, true},
		{"maxOf reference and literal", `{"type":"maxOf","args":[` + ref("holdTimeMin") + `,10]}`, 25, true},
		{"maxOf skips what does not resolve", `{"type":"maxOf","args":[` + ref("unserved") + `,10]}`, 10, true},
		{"minOf + literal", `{"type":"+","lhs":{"type":"minOf","args":[` + ref("panMax") + `,` + ref("holdTimeMin") + `]},"rhs":1}`, 26, true},
		{"dotted reference", `{"type":".","lhs":` + ref("limits") + `,"rhs":` + ref("max") + `}`, 40, true},
		{"unresolved operand", `{"type":"-","lhs":` + ref("unserved") + `,"rhs":1}`, 0, false},
		{"maxOf without a number", `{"type":"maxOf","args":[` + ref("unserved") + `]}`, 0, false},
		{"unscaled unit", `{"type":"celsius","value":12.7}`, 0, false},
		{"no JSON", `x - 1`, 0, false},
	} {
		got, ok := inst.bound(&Bound{Expr: c.expr}, peers)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: bound = %v, %v; want %v, %v", c.name, got, ok, c.want, c.ok)
		}
	}

	// The constraint holds or fails on the computed bound; a bound naming
	// what does not resolve admits ("max numberOfPositions - 1").
	c := Constraint{Text: "max numberOfPositions - 1", Max: &Bound{Expr: `{"type":"-","lhs":` + ref("numberOfPositions") + `,"rhs":1}`}}
	if !inst.satisfies(c, 2, peers) || inst.satisfies(c, 3, peers) {
		t.Error("max numberOfPositions - 1 against 3 positions: want 2 admitted, 3 refused")
	}
	if !inst.satisfies(c, 3, nil) {
		t.Error("an unresolved bound refused")
	}
	// Literal bounds hold without peers ("-2^62 to 2^62").
	r := Constraint{Text: "-2^62 to 2^62", Min: &Bound{Expr: `{"type":"^","lhs":-2,"rhs":62}`}, Max: &Bound{Expr: `{"type":"^","lhs":2,"rhs":62}`}}
	if !inst.satisfies(r, 1<<62, nil) || inst.satisfies(r, 1<<63, nil) || inst.satisfies(r, -(1<<63), nil) {
		t.Error("-2^62 to 2^62: want 2^62 admitted, ±2^63 refused")
	}
}

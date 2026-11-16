// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"math"
	"math/big"
	"testing"
)

// TestBoundExactArithmetic holds the exact bound an integer value is
// compared against ([Instance.boundExact], [Instance.evalBoundExact],
// exactOp, ratOf) to each of its operators and operand kinds: "-", "*" and
// "/" exactly, a division by zero without a number, a power exact for a
// whole exponent and computed as float64 otherwise (an infinity without a
// number), maxOf / minOf over the numeric arguments only, a sibling of any
// integer width exactly, a float sibling as the value it holds, and NaN, a
// string and an opaque node without a number (review II, wave 1's exact
// integer bounds).
func TestBoundExactArithmetic(t *testing.T) {
	t.Parallel()
	def := &Cluster{Name: "T", Attributes: []Attribute{
		{ID: 1, Name: "Big"},
		{ID: 2, Name: "Half"},
		{ID: 3, Name: "NotANumber"},
		{ID: 4, Name: "Text"},
		{ID: 5, Name: "Unserved"},
	}}
	inst := &Instance{def: def}
	peers := func(id uint32) (any, bool) {
		switch id {
		case 1:
			return uint64(math.MaxUint64), true
		case 2:
			return 1.5, true
		case 3:
			return math.NaN(), true
		case 4:
			return "x", true
		}
		return nil, false
	}
	ref := func(name string) string { return `{"type":"reference","name":"` + name + `"}` }
	op := func(o, l, r string) *Bound {
		return &Bound{Expr: `{"type":"` + o + `","lhs":` + l + `,"rhs":` + r + `}`}
	}
	rat := func(s string) *big.Rat {
		r, ok := new(big.Rat).SetString(s)
		if !ok {
			t.Fatalf("bad rational %q", s)
		}
		return r
	}
	for _, c := range []struct {
		name  string
		bound *Bound
		want  *big.Rat // nil: no number
	}{
		{"no bound", nil, nil},
		{"integer bound", &Bound{Int: -5}, rat("-5")},
		{"uint64 sibling beyond int64", &Bound{Ref: "big"}, rat("18446744073709551615")},
		{"float sibling", &Bound{Ref: "half"}, rat("3/2")},
		{"NaN sibling", &Bound{Ref: "notANumber"}, nil},
		{"string sibling", &Bound{Ref: "text"}, nil},
		{"difference", op("-", "7", "2"), rat("5")},
		{"product", op("*", "3", ref("big")), rat("55340232221128654845")},
		{"quotient", op("/", "7", "2"), rat("7/2")},
		{"division by zero", op("/", "7", "0"), nil},
		{"negative base", op("^", "-2", "3"), rat("-8")},
		{"negative exponent as float64", op("^", "2", "-1"), rat("1/2")},
		{"fractional exponent as float64", op("^", "4", "0.5"), rat("2")},
		{"float64 power beyond range", op("^", "10", "400"), nil},
		{"unresolved operand", op("+", ref("unserved"), "1"), nil},
		{"maxOf skips what does not resolve", &Bound{Expr: `{"type":"maxOf","args":[` + ref("unserved") + `,1,3]}`}, rat("3")},
		{"minOf", &Bound{Expr: `{"type":"minOf","args":[3,1,2]}`}, rat("1")},
		{"minOf without a number", &Bound{Expr: `{"type":"minOf","args":[` + ref("unserved") + `]}`}, nil},
		{"opaque", &Bound{Expr: `{"type":"celsius","value":12.7}`}, nil},
	} {
		got, ok := inst.boundExact(c.bound, peers)
		switch {
		case c.want == nil && ok:
			t.Errorf("%s: boundExact = %v, want no number", c.name, got)
		case c.want != nil && (!ok || got.Cmp(c.want) != 0):
			t.Errorf("%s: boundExact = %v, %v; want %v", c.name, got, ok, c.want)
		}
	}
	if v, ok := inst.evalBoundExact(&boundExpr{op: "num", num: math.NaN()}, peers); ok {
		t.Errorf("a NaN literal evaluated to %v, want no number", v)
	}
}

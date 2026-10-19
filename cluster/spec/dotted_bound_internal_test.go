// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "testing"

// TestDottedReference: the qualified name a "." tree of references spells
// (DotQualifiedConstraintTest.ts at 862ad75f), nothing for any other tree.
func TestDottedReference(t *testing.T) {
	t.Parallel()
	for expr, want := range map[string]string{
		`{"type":".","lhs":{"type":"reference","name":"holdTimeLimits"},"rhs":{"type":"reference","name":"holdTimeMin"}}`:                                   "holdTimeLimits.holdTimeMin",
		`{"type":".","lhs":{"type":".","lhs":{"type":"reference","name":"a"},"rhs":{"type":"reference","name":"b"}},"rhs":{"type":"reference","name":"c"}}`: "a.b.c",
		`{"type":"reference","name":"x"}`: "x",
		`{"type":".","lhs":{"type":"value","value":1},"rhs":{"type":"reference","name":"y"}}`: "",
		`{"type":"+","lhs":{"type":"reference","name":"a"}}`:                                  "",
		`not json`: "",
	} {
		if got := dottedReference(expr); got != want {
			t.Errorf("dottedReference(%s) = %q, want %q", expr, got, want)
		}
	}
}

// TestStructField: a field by name without regard to case, through
// pointers; nothing for a missing field or a value that is no struct.
func TestStructField(t *testing.T) {
	t.Parallel()
	type limits struct {
		HoldTimeMin uint16
		Optional    *uint16
	}
	v := uint16(7)
	for _, c := range []struct {
		value any
		name  string
		want  any
		ok    bool
	}{
		{limits{HoldTimeMin: 10}, "holdTimeMin", uint16(10), true},
		{&limits{HoldTimeMin: 11}, "HOLDTIMEMIN", uint16(11), true},
		{limits{Optional: &v}, "optional", uint16(7), true},
		{limits{}, "optional", nil, true},
		{limits{}, "missing", nil, false},
		{(*limits)(nil), "holdTimeMin", nil, false},
		{uint16(3), "holdTimeMin", nil, false},
	} {
		got, ok := structField(c.value, c.name)
		if ok != c.ok || got != c.want {
			t.Errorf("structField(%#v, %q) = %v, %v; want %v, %v", c.value, c.name, got, ok, c.want, c.ok)
		}
	}
}

// TestBoundQualified: a bound naming a sibling's field resolves through
// the peers, as Ref and as expression; an unresolved one admits.
func TestBoundQualified(t *testing.T) {
	t.Parallel()
	def := &Cluster{Name: "T", Attributes: []Attribute{
		{ID: 1, Name: "Limits", Type: Type{Name: "LimitsStruct", Kind: KindStruct}},
	}}
	inst := &Instance{def: def}
	type limitsStruct struct{ Min uint16 }
	peers := func(id uint32) (any, bool) { return limitsStruct{Min: 5}, id == 1 }
	expr := `{"type":".","lhs":{"type":"reference","name":"limits"},"rhs":{"type":"reference","name":"min"}}`
	for _, b := range []*Bound{{Ref: "limits.min"}, {Expr: expr}} {
		if got, ok := inst.bound(b, peers); !ok || got != 5 {
			t.Errorf("bound(%+v) = %v, %v; want 5", b, got, ok)
		}
	}
	for _, b := range []*Bound{nil, {Ref: "limits.max"}, {Ref: "other.min"}, {Expr: `{"type":"+"}`}} {
		if _, ok := inst.bound(b, peers); ok {
			t.Errorf("bound(%+v) resolved", b)
		}
	}
	if _, ok := inst.bound(&Bound{Ref: "limits.min"}, nil); ok {
		t.Error("bound resolved without peers")
	}
	if got, ok := inst.bound(&Bound{Int: 4}, nil); !ok || got != 4 {
		t.Errorf("integer bound %v, %v", got, ok)
	}
}

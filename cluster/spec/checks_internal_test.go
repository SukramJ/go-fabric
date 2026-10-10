// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec

import "testing"

type nested struct {
	Level    Nullable[uint8]
	Note     *string
	V2two    uint8
	FreeForm any
}

// TestStructAndListChecks runs the struct and list checks over a
// synthetic definition: optional (pointer) and nullable fields, a field
// whose Go name the generator prefixes, a list of scalars with an entry
// constraint, a list without an entry type, and the shape refusals.
func TestStructAndListChecks(t *testing.T) {
	t.Parallel()
	max4 := Constraint{Text: "max 4", Max: &Bound{Int: 4}}
	st := &Struct{Name: "Nested", Fields: []Field{
		{ID: 0, Name: "level", Type: Type{Kind: KindUint, Bits: 8}, Quality: Quality{Nullable: true}, Constraint: Constraint{Text: "max 10", Max: &Bound{Int: 10}}},
		{ID: 1, Name: "note", Type: Type{Kind: KindString}, Constraint: max4},
		{ID: 2, Name: "2two", Type: Type{Kind: KindUint, Bits: 8}},
		{ID: 3, Name: "free-form", Type: Type{Kind: KindAny}},
	}}
	def := &Cluster{ID: 0xFFF1, Name: "Synthetic", Revision: 1, Attributes: []Attribute{
		{ID: 0, Name: "Nested", Type: Type{Kind: KindStruct, Struct: st}, Conformance: Conformance{Op: ConfMandatory}},
		{ID: 1, Name: "Bytes", Type: Type{Kind: KindList, Entry: &Field{Name: "entry", Type: Type{Kind: KindUint, Bits: 8}}}, Conformance: Conformance{Op: ConfMandatory}, Constraint: Constraint{Text: "all[min 1]", Entry: &Constraint{Text: "min 1", Min: &Bound{Int: 1}}}},
		{ID: 2, Name: "Untyped", Type: Type{Kind: KindList}, Conformance: Conformance{Op: ConfMandatory}},
		{ID: 3, Name: "Opaque", Type: Type{Kind: KindStruct}, Conformance: Conformance{Op: ConfMandatory}},
		{ID: 4, Name: "Missing", Type: Type{Kind: KindStruct, Struct: &Struct{Fields: []Field{{Name: "absent", Type: Type{Kind: KindBool}}}}}, Conformance: Conformance{Op: ConfMandatory}},
	}}
	inst, err := New(def, Options{})
	if err != nil {
		t.Fatal(err)
	}
	note, long := "ok", "too long"
	cases := []struct {
		attr  uint32
		value any
		ok    bool
	}{
		{0, nested{Level: NullOf[uint8](), Note: &note, FreeForm: "x"}, true},
		{0, &nested{Level: ValueOf[uint8](10), FreeForm: 1}, true},
		{0, nested{Level: ValueOf[uint8](11)}, false},
		{0, nested{Note: &long}, false},
		{0, 7, false},
		{1, []uint8{1, 2}, true},
		{1, [2]uint64{1, 0}, false},
		{1, []any{uint8(1), nil}, false},
		{2, []string{"anything"}, true},
		{3, struct{ X int }{1}, true},
		{3, (*nested)(nil), false},
		{4, struct{ Other bool }{}, false},
	}
	for n, c := range cases {
		_, err := inst.CheckValue(c.attr, c.value, nil)
		if (err == nil) != c.ok {
			t.Errorf("case %d: attribute %d = %#v: %v", n, c.attr, c.value, err)
		}
	}
	if goFieldName("") != "X" || goFieldName("2two") != "V2two" || goFieldName("free-form") != "FreeForm" {
		t.Errorf("goFieldName: %q %q %q", goFieldName(""), goFieldName("2two"), goFieldName("free-form"))
	}
}

// TestDefaultValue holds the conversion of a rendered model default to the
// stored form.
func TestDefaultValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a    Attribute
		want any
		ok   bool
	}{
		{Attribute{Type: Type{Kind: KindEnum, Bits: 8}, Default: float64(2)}, uint8(2), true},
		{Attribute{Type: Type{Kind: KindUint, Bits: 8}, Default: float64(-1)}, nil, false},
		{Attribute{Type: Type{Kind: KindInt, Bits: 16}, Default: float64(-5)}, int16(-5), true},
		{Attribute{Type: Type{Kind: KindFloat32}, Default: float64(0.5)}, float32(0.5), true},
		{Attribute{Type: Type{Kind: KindString}, Default: float64(1)}, nil, false},
		{Attribute{Type: Type{Kind: KindBool}, Default: true}, true, true},
		{Attribute{Type: Type{Kind: KindUint}, Default: true}, nil, false},
		{Attribute{Type: Type{Kind: KindString}, Default: "XX"}, "XX", true},
		{Attribute{Type: Type{Kind: KindAny}, Default: "0"}, nil, false},
		{Attribute{Type: Type{Kind: KindUint}, Default: NullDefault{}}, nil, true},
		{Attribute{Type: Type{Kind: KindUint}}, nil, false},
	}
	for n, c := range cases {
		if got, ok := defaultValue(&c.a); ok != c.ok || got != c.want {
			t.Errorf("case %d: %#v (%v), want %#v (%v)", n, got, ok, c.want, c.ok)
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

type typedEnum uint8

type typedBool bool

type typedPair struct{ A uint8 }

type typedPairAlias struct{ A uint8 }

// typedWire is a self-held server's wire struct of typedState's attribute:
// plain fields where the generated type has an enum and optional pointers.
type typedWire struct {
	ID     uint8
	Label  string
	Detail string
	Extra  bool
}

type typedState struct {
	Id     typedEnum //nolint:revive // the generated types spell an id "Id"
	Label  *string
	Detail *string
}

// TestTypedConvert covers each shape a generated State field takes, from
// each stored form a value arrives in.
func TestTypedConvert(t *testing.T) {
	t.Parallel()
	seven := uint8(7)
	cases := []struct {
		name string
		v    any
		to   any // a value of the target type
		want any
		ok   bool
	}{
		{"width to enum", uint8(3), typedEnum(0), typedEnum(3), true},
		{"wide to narrow", uint64(3), uint16(0), uint16(3), true},
		{"bool", true, typedBool(false), typedBool(true), true},
		{"string to int", "x", 0, nil, false},
		{"int to string", 65, "", nil, false},
		{"nil to pointer", nil, (*uint8)(nil), (*uint8)(nil), true},
		{"nil to slice", nil, []uint8(nil), []uint8(nil), true},
		{"nil to scalar", nil, uint8(0), nil, false},
		{"nil to nullable", nil, Nullable[uint8]{}, Nullable[uint8]{Null: true}, true},
		{"value to pointer", uint8(7), (*typedEnum)(nil), ptrTo(typedEnum(7)), true},
		{"bad value to pointer", "x", (*uint8)(nil), nil, false},
		{"pointer to value", &seven, uint8(0), uint8(7), true},
		{"nil pointer to pointer", (*uint8)(nil), (*uint16)(nil), (*uint16)(nil), true},
		{"value to nullable", uint64(4), Nullable[typedEnum]{}, Nullable[typedEnum]{Value: 4}, true},
		{"nullable to nullable", Nullable[uint8]{Value: 5}, Nullable[typedEnum]{}, Nullable[typedEnum]{Value: 5}, true},
		{"null to nullable", Nullable[uint8]{Null: true}, Nullable[typedEnum]{}, Nullable[typedEnum]{Null: true}, true},
		{"bad value to nullable", "x", Nullable[uint8]{}, nil, false},
		{"list entry by entry", []any{uint64(1), uint64(2)}, []typedEnum(nil), []typedEnum{1, 2}, true},
		{"array", [2]uint8{1, 2}, []uint16(nil), []uint16{1, 2}, true},
		{"nullable entries", []any{nil, uint8(1)}, []Nullable[uint8](nil), []Nullable[uint8]{{Null: true}, {Value: 1}}, true},
		{"bad entry", []any{"x"}, []uint8(nil), nil, false},
		{"scalar to list", uint8(1), []uint8(nil), nil, false},
		{"struct", typedPairAlias{A: 1}, typedPair{}, typedPair{A: 1}, true},
		{"list of structs", List[typedPair]{{A: 2}}, []typedPair(nil), []typedPair{{A: 2}}, true},
		{"struct mismatch", struct{ B string }{}, typedPair{}, nil, false},
		{
			"wire struct by field name",
			typedWire{ID: 3, Label: "x", Extra: true},
			typedState{},
			typedState{Id: 3, Label: ptrTo("x")},
			true,
		},
		{"wire struct field misfit", struct{ ID string }{"x"}, typedState{}, nil, false},
	}
	for _, c := range cases {
		got, ok := convert(c.v, reflect.TypeOf(c.to))
		if ok != c.ok {
			t.Errorf("%s: ok %v", c.name, ok)
			continue
		}
		if ok && !reflect.DeepEqual(got.Interface(), c.want) {
			t.Errorf("%s: %#v, want %#v", c.name, got.Interface(), c.want)
		}
	}
	if got, ok := convert(nil, reflect.TypeFor[any]()); !ok || got.IsValid() && !got.IsZero() {
		t.Errorf("nil to any: %v %v", got, ok)
	}
}

func ptrTo[T any](v T) *T { return &v }

// List needs an Encodable entry type.
func (typedPair) EncodeTLV(*tlv.Encoder, tlv.Tag) {}


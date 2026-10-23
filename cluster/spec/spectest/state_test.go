// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestWireValues encodes every stored form by its Go type and decodes it
// back into a node.
func TestWireValues(t *testing.T) {
	t.Parallel()
	one := uint8(1)
	var nilPtr *uint8
	stored := map[uint32]any{
		0: nil, 1: true, 2: int16(-3), 3: uint32(7), 4: float32(1.5), 5: 2.5,
		6: "x", 7: []byte{1}, 8: &one, 9: nilPtr, 10: []uint16{1, 2}, 11: spec.Uint8(9),
	}
	v := spectest.WireValues(t, stored)
	check := func(id uint32, want tlv.ElementType) {
		t.Helper()
		n, ok := v.Value(id)
		if !ok || n.El.Type != want {
			t.Errorf("attribute %d: %v %v, want type %v", id, n.El.Type, ok, want)
		}
	}
	check(0, tlv.TypeNull)
	check(1, tlv.TypeBoolTrue)
	check(7, tlv.TypeOctetStr1)
	check(9, tlv.TypeNull)
	check(10, tlv.TypeArray)
	if n, _ := v.Value(10); len(n.Children) != 2 {
		t.Errorf("list: %d entries", len(n.Children))
	}
	if n, _ := v.Value(8); n.El.Uint != 1 {
		t.Errorf("pointer: %+v", n.El)
	}
	if errs := run(t, func(r *recorder) { spectest.WireValues(r, map[uint32]any{0: struct{ A int }{1}}) }); len(errs) == 0 {
		t.Error("a struct without EncodeTLV was encoded")
	}
}

// TestEqualState compares field by field, a nil slice equal to an empty
// one, pointers by what they point to.
func TestEqualState(t *testing.T) {
	t.Parallel()
	type inner struct{ N int }
	type state struct {
		L []int
		P *uint8
		I inner
		M map[string]int
		A any
	}
	one, also := uint8(1), uint8(1)
	spectest.EqualState(t, state{L: nil, P: &one, M: map[string]int{"a": 1}, A: 1}, state{L: []int{}, P: &also, M: map[string]int{"a": 1}, A: 1})
	for name, pair := range map[string][2]state{
		"slice":   {{L: []int{1}}, {L: []int{2}}},
		"length":  {{L: []int{1}}, {}},
		"pointer": {{P: &one}, {}},
		"struct":  {{I: inner{1}}, {}},
		"map":     {{M: map[string]int{"a": 1}}, {M: map[string]int{"a": 2}}},
		"maplen":  {{M: map[string]int{"a": 1}}, {}},
		"any":     {{A: 1}, {A: "1"}},
	} {
		if errs := run(t, func(r *recorder) { spectest.EqualState(r, pair[0], pair[1]) }); len(errs) == 0 {
			t.Errorf("%s: unequal states compared equal", name)
		}
	}
	if errs := run(t, func(r *recorder) { spectest.EqualState(r, nil, state{}) }); len(errs) == 0 {
		t.Error("nil and a state compared equal")
	}
	spectest.EqualState(t, nil, nil)
}

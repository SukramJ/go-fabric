// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// TestStoredNarrowsToTheElementsWidth: [spec.Stored] gives the validator's
// 64-bit values and a named scalar the Go type of the element's width, and
// leaves every other value as it is.
func TestStoredNarrowsToTheElementsWidth(t *testing.T) {
	t.Parallel()
	type enum8 uint8
	u8 := spec.Type{Kind: spec.KindEnum, Bits: 8}
	cases := []struct {
		name string
		t    spec.Type
		v    any
		want any
	}{
		{"validator uint64 to enum8", u8, uint64(3), uint8(3)},
		{"named enum to enum8", u8, enum8(3), uint8(3)},
		{"uint8 stays", u8, uint8(3), uint8(3)},
		{"uint32", spec.Type{Kind: spec.KindUint, Bits: 32}, uint64(70000), uint32(70000)},
		{"int16", spec.Type{Kind: spec.KindInt, Bits: 16}, int64(-5), int16(-5)},
		{"float32", spec.Type{Kind: spec.KindFloat32, Bits: 32}, float64(1.5), float32(1.5)},
		{"uint64 stays", spec.Type{Kind: spec.KindUint, Bits: 64}, uint64(1 << 40), uint64(1 << 40)},
		{"null", u8, nil, nil},
		{"string", spec.Type{Kind: spec.KindString}, "x", "x"},
		{"bool", spec.Type{Kind: spec.KindBool}, true, true},
	}
	for _, tc := range cases {
		got := spec.Stored(tc.t, tc.v)
		if !reflect.DeepEqual(got, tc.want) || reflect.TypeOf(got) != reflect.TypeOf(tc.want) {
			t.Errorf("%s: Stored(%T %v) = %T %v, want %T %v", tc.name, tc.v, tc.v, got, got, tc.want, tc.want)
		}
	}
}

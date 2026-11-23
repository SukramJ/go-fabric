// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"math"
	"reflect"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
)

// TestConvertRefusesANumberThatDoesNotFit holds Convert and Assign to
// their documented contract, "false when v does not fit": a number out of
// the target's range, a negative number to an unsigned type and a
// fraction, NaN or infinity to an integer are refused, and Assign leaves
// dst untouched. Before, Go's conversion wrapped or truncated each and
// reported ok (review II, R2-C2-api-1351).
func TestConvertRefusesANumberThatDoesNotFit(t *testing.T) {
	t.Parallel()
	type level uint8
	for _, c := range []struct {
		v    any
		to   reflect.Type
		want any // nil: refused
	}{
		{300, reflect.TypeFor[uint8](), nil},
		{int64(-1), reflect.TypeFor[uint16](), nil},
		{1.5, reflect.TypeFor[int16](), nil},
		{uint64(70000), reflect.TypeFor[uint16](), nil},
		{uint64(math.MaxUint64), reflect.TypeFor[int64](), nil},
		{int8(-1), reflect.TypeFor[level](), nil},
		{math.NaN(), reflect.TypeFor[int32](), nil},
		{math.Inf(1), reflect.TypeFor[uint32](), nil},
		{1e300, reflect.TypeFor[float32](), nil},
		{float64(1 << 63), reflect.TypeFor[int64](), nil},
		{-129, reflect.TypeFor[int8](), nil},
		// What fits converts.
		{255, reflect.TypeFor[uint8](), uint8(255)},
		{int64(-128), reflect.TypeFor[int8](), int8(-128)},
		{uint64(65535), reflect.TypeFor[uint16](), uint16(65535)},
		{2.0, reflect.TypeFor[int16](), int16(2)},
		{-3.0, reflect.TypeFor[int64](), int64(-3)},
		{uint8(7), reflect.TypeFor[level](), level(7)},
		{uint64(math.MaxUint64), reflect.TypeFor[float64](), float64(math.MaxUint64)},
		{int64(-1), reflect.TypeFor[float32](), float32(-1)},
		{1.5, reflect.TypeFor[float32](), float32(1.5)},
		{float32(2.5), reflect.TypeFor[float64](), 2.5},
	} {
		got, ok := spec.Convert(c.v, c.to)
		switch {
		case c.want == nil && ok:
			t.Errorf("Convert(%T %v → %v) = %v, ok; want refused", c.v, c.v, c.to, got.Interface())
		case c.want != nil && !ok:
			t.Errorf("Convert(%T %v → %v) refused, want %v", c.v, c.v, c.to, c.want)
		case c.want != nil && got.Interface() != c.want:
			t.Errorf("Convert(%T %v → %v) = %v, want %v", c.v, c.v, c.to, got.Interface(), c.want)
		}
	}
	dst := uint8(9)
	if spec.Assign(&dst, 300) || dst != 9 {
		t.Errorf("Assign[uint8](300): dst %d, want refused with dst 9 untouched", dst)
	}
	var ptr *uint8
	if spec.Assign(&ptr, -1) || ptr != nil {
		t.Errorf("Assign[*uint8](-1): %v, want refused", ptr)
	}
}

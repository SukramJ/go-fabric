// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec

import "testing"

// TestStoredForm holds the Go type a checked value is kept in to the
// width the definition states.
func TestStoredForm(t *testing.T) {
	t.Parallel()
	cases := []struct {
		t    Type
		in   any
		want any
	}{
		{Type{Kind: KindUint, Bits: 8}, uint64(1), uint8(1)},
		{Type{Kind: KindEnum, Bits: 16}, uint64(1), uint16(1)},
		{Type{Kind: KindBitmap, Bits: 24}, uint64(1), uint32(1)},
		{Type{Kind: KindUint, Bits: 32}, uint64(1), uint32(1)},
		{Type{Kind: KindUint, Bits: 40}, uint64(1), uint64(1)},
		{Type{Kind: KindUint}, uint64(1), uint64(1)},
		{Type{Kind: KindInt, Bits: 8}, int64(-1), int8(-1)},
		{Type{Kind: KindInt, Bits: 16}, int64(-1), int16(-1)},
		{Type{Kind: KindInt, Bits: 32}, int64(-1), int32(-1)},
		{Type{Kind: KindInt, Bits: 64}, int64(-1), int64(-1)},
		{Type{Kind: KindFloat32}, 1.5, float32(1.5)},
		{Type{Kind: KindFloat64}, 1.5, 1.5},
		{Type{Kind: KindBool}, true, true},
		{Type{Kind: KindUint, Bits: 8}, nil, nil},
	}
	for _, c := range cases {
		if got := stored(c.t, c.in); got != c.want {
			t.Errorf("stored(%+v, %#v) = %#v, want %#v", c.t, c.in, got, c.want)
		}
	}
}

// TestUnderlying holds the unwrapping of named scalar types.
func TestUnderlying(t *testing.T) {
	t.Parallel()
	type (
		u8  uint8
		i16 int16
		f32 float32
		b   bool
		s   string
	)
	bytes := []byte{1}
	cases := []struct{ in, want any }{
		{u8(3), uint64(3)},
		{i16(-3), int64(-3)},
		{f32(0.5), 0.5},
		{b(true), true},
		{s("x"), "x"},
		{nil, nil},
	}
	for _, c := range cases {
		if got := underlying(c.in); got != c.want {
			t.Errorf("underlying(%#v) = %#v, want %#v", c.in, got, c.want)
		}
	}
	if got, ok := underlying(bytes).([]byte); !ok || &got[0] != &bytes[0] {
		t.Errorf("a slice is returned as given: %#v", got)
	}
}

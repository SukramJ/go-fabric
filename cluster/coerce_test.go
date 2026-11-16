// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cluster

import "testing"

func TestAsUint8AcceptsDecoderWidths(t *testing.T) {
	for _, v := range []any{uint8(4), uint16(4), uint32(4), uint64(4), int(4), int64(4), float64(4)} {
		if got, ok := AsUint8(v); !ok || got != 4 {
			t.Errorf("AsUint8(%T %v) = (%d, %v); want (4, true)", v, v, got, ok)
		}
	}
	if _, ok := AsUint8("nope"); ok {
		t.Error("AsUint8(string) must return false")
	}
}

func TestAsInt16AcceptsDecoderWidths(t *testing.T) {
	for _, v := range []any{int16(2100), int32(2100), int64(2100), int(2100), uint64(2100), float64(2100)} {
		if got, ok := AsInt16(v); !ok || got != 2100 {
			t.Errorf("AsInt16(%T %v) = (%d, %v); want (2100, true)", v, v, got, ok)
		}
	}
	if _, ok := AsInt16([]byte{1}); ok {
		t.Error("AsInt16([]byte) must return false")
	}
}

func TestAsUintMaxChecksRangeInsteadOfWrapping(t *testing.T) {
	t.Parallel()
	for _, v := range []any{uint8(100), uint16(100), uint32(100), uint64(100), int(100), int8(100), int16(100), int32(100), int64(100)} {
		if got, ok := AsUintMax(v, 100); !ok || got != 100 {
			t.Errorf("AsUintMax(%T %v, 100) = (%d, %v); want (100, true)", v, v, got, ok)
		}
	}
	for _, v := range []any{uint64(101), int64(-1), float64(5), "5", nil} {
		if got, ok := AsUintMax(v, 100); ok {
			t.Errorf("AsUintMax(%T %v, 100) = (%d, true); want a rejection", v, v, got)
		}
	}
}

// TestNarrowingCoercionsRefuseOutOfRange holds AsUint8 and AsInt16 to the
// width they narrow to: a value outside it, a uint64 beyond int64 and a
// float outside it or with a fraction are refused, not wrapped. Before,
// AsInt16(int64(65541)) was 5 and passed the thermostat's int8 check, and
// AsUint8(258) was 2 (review II, R2-P6-api-1310).
func TestNarrowingCoercionsRefuseOutOfRange(t *testing.T) {
	t.Parallel()
	for _, v := range []any{int64(256), int(258), int64(-1), uint64(1 << 63), uint64(1<<64 - 1), float64(256), float64(1.5), float64(-0.5)} {
		if got, ok := AsUint8(v); ok {
			t.Errorf("AsUint8(%T %v) = (%d, true); want a rejection", v, v, got)
		}
	}
	for _, v := range []any{int64(65541), int64(32768), int64(-32769), uint64(1<<64 - 1), uint64(40000), float64(1e9), float64(2.5)} {
		if got, ok := AsInt16(v); ok {
			t.Errorf("AsInt16(%T %v) = (%d, true); want a rejection", v, v, got)
		}
	}
	for v, want := range map[any]uint8{uint64(255): 255, int64(0): 0, float64(7): 7} {
		if got, ok := AsUint8(v); !ok || got != want {
			t.Errorf("AsUint8(%T %v) = (%d, %v); want (%d, true)", v, v, got, ok, want)
		}
	}
	for v, want := range map[any]int16{int64(-32768): -32768, uint64(32767): 32767, float64(-5): -5} {
		if got, ok := AsInt16(v); !ok || got != want {
			t.Errorf("AsInt16(%T %v) = (%d, %v); want (%d, true)", v, v, got, ok, want)
		}
	}
}

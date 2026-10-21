// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "testing"

// TestStoredKeepsAnOutOfWidthValue: stored narrows only a value that fits
// the attribute's width; anything else comes back unchanged (the width
// check before it is what refuses the value), never truncated.
func TestStoredKeepsAnOutOfWidthValue(t *testing.T) {
	t.Parallel()
	u8 := Type{Kind: KindUint, Bits: 8}
	if got := stored(u8, uint64(200)); got != uint8(200) {
		t.Fatalf("200 in uint8: got %T %v", got, got)
	}
	if got := stored(u8, uint64(300)); got != uint64(300) {
		t.Fatalf("300 in uint8: got %T %v, want the uint64 unchanged", got, got)
	}
	i8 := Type{Kind: KindInt, Bits: 8}
	if got := stored(i8, int64(-128)); got != int8(-128) {
		t.Fatalf("-128 in int8: got %T %v", got, got)
	}
	if got := stored(i8, int64(128)); got != int64(128) {
		t.Fatalf("128 in int8: got %T %v, want the int64 unchanged", got, got)
	}
	if got := stored(i8, int64(-129)); got != int64(-129) {
		t.Fatalf("-129 in int8: got %T %v, want the int64 unchanged", got, got)
	}
}

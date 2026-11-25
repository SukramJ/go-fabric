// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"encoding/binary"
	"errors"
	"testing"
)

// TestFieldNumberBound: a field number above protobuf's 2^29-1 is
// malformed, refused before it is converted, so the decode is the same on
// 32 and 64 bit; the largest one decodes (review II, R3-wire-3352).
func TestFieldNumberBound(t *testing.T) {
	for _, tc := range []struct {
		name string
		num  uint64
		ok   bool
	}{
		{"largest", 1<<29 - 1, true},
		{"one above", 1 << 29, false},
		{"one above 2^32 tags", 1 << 32, false},
		{"top of the varint", 1<<61 - 1, false},
	} {
		b := binary.AppendUvarint(nil, tc.num<<3|wireVarint)
		b = append(b, 0x01)
		var got int
		err := fields(b, func(f field) error { got = f.num; return nil })
		if tc.ok && (err != nil || uint64(got) != tc.num) { //nolint:gosec // got is a field number
			t.Errorf("%s: field %d, %v; want %d", tc.name, got, err, tc.num)
		}
		if !tc.ok && !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: %v, want ErrMalformed", tc.name, err)
		}
	}
}

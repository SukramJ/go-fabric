// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"errors"
	"testing"
)

// TestDERLongLengthNeverWraps (review II, R2-P6-wire-1917): a length of
// five or six bytes beyond what follows is a truncated element on every
// build — on a 32-bit one such a length used to wrap to a small int and
// read as the bytes that follow. The CI's 32-bit leg vets this test; it
// runs it on a 64-bit build, where the lengths cannot wrap.
func TestDERLongLengthNeverWraps(t *testing.T) {
	t.Parallel()
	payload := []byte{1, 2, 3, 4, 5}
	for _, length := range [][]byte{
		{0x85, 0x01, 0x00, 0x00, 0x00, 0x05},       // 2^32 + 5: 5 on a 32-bit int
		{0x86, 0x00, 0x01, 0x00, 0x00, 0x00, 0x05}, // the same in six bytes
		{0x85, 0x80, 0x00, 0x00, 0x00, 0x00},       // a negative int on 32 bits
		{0x86, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	} {
		der := append(append([]byte{0x04}, length...), payload...)
		if _, err := derDecode(der); !errors.Is(err, errDERTruncated) {
			t.Fatalf("length % x: %v, want errDERTruncated", length, err)
		}
	}
	// A long form that fits still decodes.
	n, err := derDecode([]byte{0x04, 0x85, 0, 0, 0, 0, 5, 1, 2, 3, 4, 5})
	if err != nil || len(n.bytes) != 5 {
		t.Fatalf("a five-byte length of 5: %v, %d bytes", err, len(n.bytes))
	}
}

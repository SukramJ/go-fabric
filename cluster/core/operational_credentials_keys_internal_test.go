// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"testing"
)

// TestComputeCompressedFabricIDRefusesAMalformedRootKey: only a 65-byte
// uncompressed point (leading 0x04) is a root public key; a key of the
// right length with another leading byte, and one of another length, are
// refused (R2-P5-tmut-18 rest).
func TestComputeCompressedFabricIDRefusesAMalformedRootKey(t *testing.T) {
	t.Parallel()
	good := append([]byte{0x04}, bytes.Repeat([]byte{0x11}, 64)...)
	if _, err := computeCompressedFabricID(good, 1); err != nil {
		t.Fatalf("a 65-byte uncompressed key: %v", err)
	}
	compressed := append([]byte{0x02}, bytes.Repeat([]byte{0x11}, 64)...)
	if _, err := computeCompressedFabricID(compressed, 1); err == nil {
		t.Error("a 65-byte key without the 0x04 prefix was accepted")
	}
	if _, err := computeCompressedFabricID(good[:64], 1); err == nil {
		t.Error("a 64-byte key was accepted")
	}
}

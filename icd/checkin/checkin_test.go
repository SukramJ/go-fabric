// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package checkin

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/secure/aesccm"
)

// TestKeySize holds the one failure matter.js's crypto port raises before
// the codec runs: a key that is not 16 bytes is aesccm.ErrKeySize, not
// ErrMalformed, so a receiver never mistakes a broken registration for a
// payload of another key.
func TestKeySize(t *testing.T) {
	t.Parallel()
	short := make([]byte, 15)
	if _, err := Encode(short, 1, nil); !errors.Is(err, aesccm.ErrKeySize) {
		t.Errorf("Encode = %v, want ErrKeySize", err)
	}
	if _, err := EncodeICD(short, 1, 5000); !errors.Is(err, aesccm.ErrKeySize) {
		t.Errorf("EncodeICD = %v, want ErrKeySize", err)
	}
	if _, _, err := Decode(short, make([]byte, MinPayloadLength)); !errors.Is(err, aesccm.ErrKeySize) || errors.Is(err, ErrMalformed) {
		t.Errorf("Decode = %v, want ErrKeySize alone", err)
	}
	if _, _, err := DecodeICD(short, make([]byte, MinPayloadLength)); !errors.Is(err, aesccm.ErrKeySize) {
		t.Errorf("DecodeICD = %v, want ErrKeySize", err)
	}
}

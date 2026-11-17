// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package aesccm

import (
	"errors"
	"testing"
)

// TestSealAndOpenRefuseAnAADBeyondTheTwoByteLengthForm holds
// R2-P6-bound-0112: the CBC-MAC encodes the additional data's length in
// the 2-byte form only, which covers fewer than 0xFF00 bytes (RFC 3610
// §2.2). Seal and Open refuse an AAD of 0xFF00 bytes or more instead of
// truncating its length prefix; Open's refusal is an authentication
// failure. One byte below the bound is the control: it seals and opens.
func TestSealAndOpenRefuseAnAADBeyondTheTwoByteLengthForm(t *testing.T) {
	t.Parallel()
	c, err := New(make([]byte, 16))
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, NonceSize)
	below := make([]byte, maxAADLen)
	sealed, err := c.Seal(nil, nonce, []byte("payload"), below)
	if err != nil {
		t.Fatalf("control: Seal with %d bytes of AAD: %v", len(below), err)
	}
	if _, err := c.Open(nil, nonce, sealed, below); err != nil {
		t.Fatalf("control: Open with %d bytes of AAD: %v", len(below), err)
	}
	for _, n := range []int{maxAADLen + 1, 0x10000 + maxAADLen} {
		aad := make([]byte, n)
		if _, err := c.Seal(nil, nonce, []byte("payload"), aad); err == nil {
			t.Errorf("Seal with %d bytes of AAD succeeded, want a refusal", n)
		}
		if _, err := c.Open(nil, nonce, sealed, aad); !errors.Is(err, ErrAuthFailed) {
			t.Errorf("Open with %d bytes of AAD: %v, want ErrAuthFailed", n, err)
		}
	}
}

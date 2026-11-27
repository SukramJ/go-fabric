// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tlv

import (
	"encoding/hex"
	"strings"
	"testing"
)

// TestTlvStringLengthFieldInBytesParityMatterJS ports the three encode
// cases of packages/types/test/tlv/TlvStringTest.ts:40-64 at db91886c
// (#4995): the width of a character string's length field follows its
// length in UTF-8 bytes, not its character count. The encoder chose the
// width by bytes already ([Encoder.PutUTF8] measures the encoded slice);
// the cases pin it.
func TestTlvStringLengthFieldInBytesParityMatterJS(t *testing.T) {
	t.Parallel()
	roundTrip := func(t *testing.T, value string) []byte {
		t.Helper()
		enc := NewEncoder()
		enc.PutUTF8(AnonymousTag(), value)
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		el, err := NewDecoder(b).Next()
		if err != nil {
			t.Fatal(err)
		}
		if el.String != value {
			t.Fatalf("decoded %d bytes, want the %d encoded", len(el.String), len(value))
		}
		return b
	}
	t.Run("uses a two-byte length field from 256 bytes even when the string has fewer characters", func(t *testing.T) {
		t.Parallel()
		b := roundTrip(t, strings.Repeat("日", 86))
		if got := hex.EncodeToString(b)[:6]; got != "0d0201" {
			t.Errorf("head %s, want 0d0201", got)
		}
	})
	t.Run("uses a four-byte length field from 65536 bytes even when the string has fewer characters", func(t *testing.T) {
		t.Parallel()
		b := roundTrip(t, strings.Repeat("日", 21_846))
		if got := hex.EncodeToString(b)[:10]; got != "0e02000100" {
			t.Errorf("head %s, want 0e02000100", got)
		}
	})
	t.Run("uses a two-byte length field when multi-byte characters exceed 255 bytes", func(t *testing.T) {
		t.Parallel()
		b := roundTrip(t, strings.Repeat("日", 100))
		if got := hex.EncodeToString(b)[:6]; got != "0d2c01" {
			t.Errorf("head %s, want 0d2c01", got)
		}
		if len(b) != 303 {
			t.Errorf("%d bytes, want 303", len(b))
		}
	})
}

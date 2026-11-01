// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tlv

import (
	"bytes"
	"testing"
)

// TestUTF8String_IS1Truncation verifies that a UTF-8 string element containing
// an IS1 byte (0x1F, Information Separator 1) is truncated at the first IS1 so
// el.String contains only the text before the separator. Conformant peers never
// emit IS1 inside a character string; the decoder must still tolerate
// non-conformant payloads gracefully by discarding everything from IS1 onward.
// Mirrors matter.js packages/types/src/tlv/TlvString.ts decodeTlvInternalValue (#3977).
// Matter §7.19.2.40.
func TestUTF8String_IS1Truncation(t *testing.T) {
	t.Parallel()
	roundTrip(t, "utf8-is1-truncation",
		func(e *Encoder) { e.PutUTF8(AnonymousTag(), "hello\x1fworld") },
		func(t *testing.T, el Element) {
			t.Helper()
			if el.String != "hello" {
				t.Errorf("el.String = %q, want %q (must truncate at IS1 0x1F)",
					el.String, "hello")
			}
		})
}

// TestUTF8String_NoIS1RemainsComplete verifies that a clean UTF-8 string without
// any IS1 byte is decoded verbatim — the IS1-truncation guard must not shorten
// clean strings. Only payloads that actually contain 0x1F are affected.
// Mirrors matter.js packages/types/src/tlv/TlvString.ts decodeTlvInternalValue (#3977).
func TestUTF8String_NoIS1RemainsComplete(t *testing.T) {
	t.Parallel()
	roundTrip(t, "utf8-no-is1",
		func(e *Encoder) { e.PutUTF8(AnonymousTag(), "NodeLabel-Test") },
		func(t *testing.T, el Element) {
			t.Helper()
			if el.String != "NodeLabel-Test" {
				t.Errorf("el.String = %q, want %q (clean strings must not be truncated)",
					el.String, "NodeLabel-Test")
			}
		})
}

// TestOctetString_IS1BytePreserved verifies that an octet-string element
// containing a 0x1F byte is NOT truncated — the IS1-truncation rule applies
// exclusively to UTF-8 character strings, not to byte strings. Callers that
// carry binary data (e.g. NOC TLV, ephemeral keys) must receive the full body.
// Mirrors matter.js packages/types/src/tlv/TlvString.ts decodeTlvInternalValue (#3977):
// only the character-string path truncates at IS1; the byte-string path leaves
// the body untouched.
func TestOctetString_IS1BytePreserved(t *testing.T) {
	t.Parallel()
	body := []byte{0xAA, 0x1F, 0xBB}
	roundTrip(t, "octets-with-is1",
		func(e *Encoder) { e.PutOctets(AnonymousTag(), body) },
		func(t *testing.T, el Element) {
			t.Helper()
			if !bytes.Equal(el.Octets, body) {
				t.Errorf("el.Octets = % X, want % X (IS1 must not truncate octet strings)",
					el.Octets, body)
			}
		})
}

// A string cut at its IS1 keeps the length it arrived with, so a bounded
// consumer can check that first, as matter.js TlvString.ts:57-69 at
// 862ad75f does (`value.length > this.maxLength` before the slice): a
// 40-byte NodeLabel whose sixth byte is 0x1F reads as "ABCDE" with
// RawLength 40, which a "max 32" consumer refuses (cluster/spec
// DecodeStringIn, the bridge's write path). A string without IS1 has no
// RawLength.
func TestUTF8String_IS1KeepsTheRawLength(t *testing.T) {
	t.Parallel()
	wire := append([]byte{0x0C, 0x28, 'A', 'B', 'C', 'D', 'E', 0x1F}, bytes.Repeat([]byte{'x'}, 34)...)
	el, err := NewDecoder(wire).Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if el.String != "ABCDE" || el.RawLength != 40 {
		t.Fatalf("String %q RawLength %d, want \"ABCDE\" 40", el.String, el.RawLength)
	}
	el, err = NewDecoder([]byte{0x0C, 0x03, 'a', 'b', 'c'}).Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if el.RawLength != 0 {
		t.Fatalf("RawLength %d without an IS1, want 0", el.RawLength)
	}
	// Counted in UTF-16 code units, as a JavaScript string's length: an
	// astral rune counts two.
	el, err = NewDecoder([]byte{0x0C, 0x06, 'a', 0x1F, 0xF0, 0x9F, 0x98, 0x80}).Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if el.String != "a" || el.RawLength != 4 {
		t.Fatalf("String %q RawLength %d, want \"a\" 4", el.String, el.RawLength)
	}
}

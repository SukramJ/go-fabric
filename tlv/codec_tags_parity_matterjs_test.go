// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tlv

import (
	"encoding/hex"
	"errors"
	"testing"
)

// TestTlvCodecTagsParityMatterJS mirrors
// packages/types/test/tlv/TlvCodecTest.ts:23-75 at 862ad75f: the tag
// encodings of Core Appendix A.12 (vendor 0xFFF1, profile 0xDEED) read and
// written, a structure member under a 4-octet fully qualified tag, and an
// implicit-profile tag — 2 or 4 octets, alone or inside a structure —
// refused as unexpected data (TlvCodec.ts:169-171 readTag).
func TestTlvCodecTagsParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		encoded string
		tag     Tag
	}{
		{"context tag", "2401", ContextTag(1)},
		{"common profile tag, 2 octets", "440100", CommonTag(1)},
		{"common profile tag, 4 octets", "64a0860100", CommonTag(100000)},
		{"fully qualified tag, 2 octets", "c4f1ffedde0100", FullyQualifiedTag(0xFFF1, 0xDEED, 1)},
		{"fully qualified tag, 4 octets", "e4f1ffeddeedfe55aa", FullyQualifiedTag(0xFFF1, 0xDEED, 0xAA55FEED)},
	} {
		t.Run("reads "+c.name, func(t *testing.T) {
			t.Parallel()
			raw, _ := hex.DecodeString(c.encoded + "00")
			el, err := NewDecoder(raw).Next()
			if err != nil {
				t.Fatal(err)
			}
			if el.Tag != c.tag {
				t.Errorf("tag = %+v, want %+v", el.Tag, c.tag)
			}
		})
		t.Run("writes "+c.name, func(t *testing.T) {
			t.Parallel()
			enc := NewEncoder()
			enc.PutUint(c.tag, 0)
			got, err := enc.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			// The control octet and the tag; the value octet follows.
			if hex.EncodeToString(got[:len(got)-1]) != c.encoded {
				t.Errorf("wrote %x, want %s followed by the value", got, c.encoded)
			}
		})
	}

	t.Run("decodes a structure member with a 4-octet fully qualified tag", func(t *testing.T) {
		t.Parallel()
		raw, _ := hex.DecodeString("15e4f1ffeddeedfe55aa2a18")
		d := NewDecoder(raw)
		if _, err := d.Next(); err != nil {
			t.Fatal(err)
		}
		el, err := d.Next()
		if err != nil {
			t.Fatal(err)
		}
		if el.Tag != FullyQualifiedTag(0xFFF1, 0xDEED, 0xAA55FEED) || el.Type != TypeUnsignedInt1 || el.Uint != 42 {
			t.Errorf("member = %+v, want 42 under the fully qualified tag", el)
		}
	})

	for _, c := range []struct{ name, encoded string }{
		{"rejects implicit profile tag, 2 octets as unexpected data", "840100"},
		{"rejects implicit profile tag, 4 octets as unexpected data", "a401000000"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			raw, _ := hex.DecodeString(c.encoded)
			if _, err := NewDecoder(raw).Next(); !errors.Is(err, ErrImplicitProfileTag) {
				t.Errorf("Next = %v, want ErrImplicitProfileTag", err)
			}
		})
	}

	t.Run("rejects a structure containing an implicit profile tag as unexpected data", func(t *testing.T) {
		t.Parallel()
		raw, _ := hex.DecodeString("158401002a18")
		d := NewDecoder(raw)
		if _, err := d.Next(); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Next(); !errors.Is(err, ErrImplicitProfileTag) {
			t.Errorf("member: %v, want ErrImplicitProfileTag", err)
		}
		if err := Validate(raw); !errors.Is(err, ErrImplicitProfileTag) {
			t.Errorf("Validate = %v, want ErrImplicitProfileTag", err)
		}
	})
}

// TestTlvStringByteSizeParityMatterJS mirrors
// packages/types/test/tlv/TlvStringTest.ts:73-83 at 862ad75f: "test"
// encodes in 6 bytes, "testè" (è is two UTF-8 bytes) in 8.
func TestTlvStringByteSizeParityMatterJS(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]int{"test": 6, "testè": 8} {
		enc := NewEncoder()
		enc.PutUTF8(AnonymousTag(), s)
		got, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != want {
			t.Errorf("%q encodes in %d bytes, want %d", s, len(got), want)
		}
	}
}

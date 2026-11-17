// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tlv

import "testing"

// TestDecodeUTF8_ReplacesIllFormedLikeTextDecoder pins the matter.js read
// of an ill-formed UTF-8 string element: DataReader.readUtf8String uses a
// non-fatal TextDecoder (packages/general/src/util/DataReader.ts:66-68 at
// 0ceb202d), which replaces each maximal subpart with one U+FFFD.
func TestDecodeUTF8_ReplacesIllFormedLikeTextDecoder(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"valid", []byte("Küche"), "Küche"},
		{"lone continuation", []byte{'a', 0x80, 'b'}, "a�b"},
		{"two invalid bytes", []byte{0xFF, 0xFE}, "��"},
		{"truncated three-byte", []byte{0xE2, 0x82, 'x'}, "�x"},
		{"truncated at end", []byte{'a', 0xF0, 0x9F, 0x98}, "a�"},
		{"surrogate", []byte{0xED, 0xA0, 0x80}, "���"},
		{"overlong", []byte{0xC0, 0xAF}, "��"},
		{"broken then valid lead", []byte{0xE2, 0xC3, 0xA4}, "�ä"},
		// Each lead byte range and continuation bound at its edges, beside
		// an ill-formed byte so the decode takes the replacing path
		// (R2-P5-tmut-18 rest).
		{"lead C2, continuation 80", []byte{0xC2, 0x80, 0xFF}, "\u0080�"},
		{"lead DF, continuation BF", []byte{0xDF, 0xBF, 0xFF}, "\u07FF�"},
		{"lead E1", []byte{0xE1, 0x80, 0x80, 0xFF}, "\u1000�"},
		{"lead EF", []byte{0xEF, 0xBC, 0x81, 0xFF}, "\uFF01�"},
		{"lead F1", []byte{0xF1, 0x80, 0x80, 0x80, 0xFF}, "\U00040000�"},
		{"lead F3", []byte{0xF3, 0xBF, 0xBF, 0xBF, 0xFF}, "\U000FFFFF�"},
	}
	for _, tc := range cases {
		if got := decodeUTF8(tc.in); got != tc.want {
			t.Errorf("%s: decodeUTF8(% X) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestDecoder_IllFormedUTF8StringElement checks the decoder applies the
// replacement to a UTF-8 string element.
func TestDecoder_IllFormedUTF8StringElement(t *testing.T) {
	// Anonymous UTF8Str1 (0x0C), length 3: 'a' 0xFF 'b'.
	dec := NewDecoder([]byte{0x0C, 0x03, 'a', 0xFF, 'b'})
	el, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	if el.String != "a�b" {
		t.Fatalf("String = %q, want %q", el.String, "a�b")
	}
}

// TestBoundedWriters_SmallestFit pins the nullable-bounded writers to
// matter.js's encoding: the nullable schema wraps a TlvNumber schema
// whose length follows the value (packages/types/src/tlv/TlvNumber.ts:247-297
// at 0ceb202d), so 1 goes out as a one-byte integer whatever the type.
func TestBoundedWriters_SmallestFit(t *testing.T) {
	writes := map[string]func(*Encoder) error{
		"Uint8":  func(e *Encoder) error { return e.PutUint8Bounded(AnonymousTag(), 1) },
		"Uint16": func(e *Encoder) error { return e.PutUint16Bounded(AnonymousTag(), 1) },
		"Uint32": func(e *Encoder) error { return e.PutUint32Bounded(AnonymousTag(), 1) },
		"Uint64": func(e *Encoder) error { return e.PutUint64Bounded(AnonymousTag(), 1) },
		"Int8":   func(e *Encoder) error { return e.PutInt8Bounded(AnonymousTag(), 1) },
		"Int16":  func(e *Encoder) error { return e.PutInt16Bounded(AnonymousTag(), 1) },
		"Int32":  func(e *Encoder) error { return e.PutInt32Bounded(AnonymousTag(), 1) },
	}
	for name, w := range writes {
		enc := NewEncoder()
		if err := w(enc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, _ := enc.Bytes()
		if len(got) != 2 || got[1] != 1 {
			t.Errorf("%s: % X, want a one-byte integer", name, got)
		}
	}
}

// TestUTF16Length counts a supplementary-plane rune, from U+10000 on, as two
// UTF-16 code units and every other rune as one (R2-P5-tmut-18 rest).
func TestUTF16Length(t *testing.T) {
	for s, want := range map[string]uint64{
		"a": 1, "\uFFFF": 1, "\U00010000": 2, "a\U0010FFFF": 3,
	} {
		if got := utf16Length(s); got != want {
			t.Errorf("utf16Length(%q) = %d, want %d", s, got, want)
		}
	}
}

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

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tlv

import (
	"bytes"
	"strings"
	"testing"
)

// TestPutUTF8WithMax_WithinLimit verifies that a string shorter than maxBytes
// is written verbatim (no trimming, same output as PutUTF8).
func TestPutUTF8WithMax_WithinLimit(t *testing.T) {
	t.Parallel()
	s := "hello"
	enc := NewEncoder()
	enc.PutUTF8WithMax(AnonymousTag(), s, 32)
	wire, err := enc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	dec := NewDecoder(wire)
	el, err := dec.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	got := el.String
	if el.Type != TypeUTF8Str1 && el.Type != TypeUTF8Str2 && el.Type != TypeUTF8Str4 && el.Type != TypeUTF8Str8 {
		t.Fatalf("element type %v is not a UTF-8 string type", el.Type)
	}
	if got != s {
		t.Errorf("got %q, want %q", got, s)
	}
}

// TestPutUTF8WithMax_ExceedsLimit verifies that an over-long string is trimmed
// to at most maxBytes and the result is still valid UTF-8.
func TestPutUTF8WithMax_ExceedsLimit(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("a", 100)
	maxBytes := 10

	enc := NewEncoder()
	enc.PutUTF8WithMax(AnonymousTag(), s, maxBytes)
	wire, err := enc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	dec := NewDecoder(wire)
	el, err := dec.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	got := el.String
	if el.Type != TypeUTF8Str1 && el.Type != TypeUTF8Str2 && el.Type != TypeUTF8Str4 && el.Type != TypeUTF8Str8 {
		t.Fatalf("element type %v is not a UTF-8 string type", el.Type)
	}
	if len(got) > maxBytes {
		t.Errorf("trimmed string length %d > maxBytes %d", len(got), maxBytes)
	}
}

// TestPutUTF8WithMax_MultibyteRune verifies rune-boundary safety: trimming a
// string containing multi-byte runes must not produce invalid UTF-8.
func TestPutUTF8WithMax_MultibyteRune(t *testing.T) {
	t.Parallel()
	// "€" is 3 bytes in UTF-8; build a string where naive byte-truncation
	// would land mid-rune.
	s := "ab€cd"
	maxBytes := 4 // cuts mid-€ at naive truncation

	enc := NewEncoder()
	enc.PutUTF8WithMax(AnonymousTag(), s, maxBytes)
	wire, err := enc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	dec := NewDecoder(wire)
	el, err := dec.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	got := el.String
	if el.Type != TypeUTF8Str1 && el.Type != TypeUTF8Str2 && el.Type != TypeUTF8Str4 && el.Type != TypeUTF8Str8 {
		t.Fatalf("element type %v is not a UTF-8 string type", el.Type)
	}
	// Result must be valid UTF-8 and at most maxBytes bytes.
	if len(got) > maxBytes {
		t.Errorf("trimmed length %d > maxBytes %d", len(got), maxBytes)
	}
	for i, r := range got {
		if r == '�' {
			t.Errorf("replacement rune at position %d — invalid UTF-8 after trim", i)
		}
	}
}

// TestPutUTF8WithMax_ZeroMaxSkipsValidation verifies that maxBytes ≤ 0
// disables validation and the string is written as-is.
func TestPutUTF8WithMax_ZeroMaxSkipsValidation(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("x", 500)

	enc := NewEncoder()
	enc.PutUTF8WithMax(AnonymousTag(), s, 0)
	wire, err := enc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	dec := NewDecoder(wire)
	el, err := dec.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	got := el.String
	if el.Type != TypeUTF8Str1 && el.Type != TypeUTF8Str2 && el.Type != TypeUTF8Str4 && el.Type != TypeUTF8Str8 {
		t.Fatalf("element type %v is not a UTF-8 string type", el.Type)
	}
	if got != s {
		t.Errorf("got len %d, want len %d", len(got), len(s))
	}
}

// PutUTF8Bounded with maxBytes ≤ 0 imposes no bound, as PutUTF8WithMax: a
// negative maximum from a host's BoundedString must not panic the encoder
// (it sliced b[:maxBytes]). matter.js refuses a negative maxLength at
// schema construction (TlvString.ts:46 at 862ad75f), never per encode.
func TestPutUTF8Bounded_NonPositiveMaxIsUnbounded(t *testing.T) {
	t.Parallel()
	for _, maxBytes := range []int{-1, 0} {
		for _, s := range []string{"", "go-fabric"} {
			enc := NewEncoder()
			enc.PutUTF8Bounded(AnonymousTag(), s, maxBytes)
			want := NewEncoder()
			want.PutUTF8(AnonymousTag(), s)
			got, err := enc.Bytes()
			if err != nil {
				t.Fatalf("Bytes: %v", err)
			}
			wantBytes, _ := want.Bytes()
			if !bytes.Equal(got, wantBytes) {
				t.Errorf("PutUTF8Bounded(%q, %d) = %x, want PutUTF8's %x", s, maxBytes, got, wantBytes)
			}
		}
	}
}

// A character string is never emitted ill-formed: each ill-formed byte
// sequence becomes U+FFFD, as matter.js's TextEncoder-based
// Bytes.fromString (Bytes.ts:125-131 at 862ad75f) can produce nothing
// else. The bounded writers trim the sanitised string, so an input that
// is ill-formed from its first byte no longer trims to "".
func TestPutUTF8_IllFormedInputIsSanitised(t *testing.T) {
	t.Parallel()
	enc := NewEncoder()
	enc.PutUTF8(AnonymousTag(), "a\xffb")
	got, err := enc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if want := []byte{0x0C, 0x05, 0x61, 0xEF, 0xBF, 0xBD, 0x62}; !bytes.Equal(got, want) {
		t.Fatalf("PutUTF8(\"a\\xffb\") = % X, want % X", got, want)
	}
	for name, put := range map[string]func(*Encoder){
		"Bounded": func(e *Encoder) { e.PutUTF8Bounded(AnonymousTag(), "\xff\xfeabc", 4) },
		"WithMax": func(e *Encoder) { e.PutUTF8WithMax(AnonymousTag(), "\xff\xfeabc", 4) },
	} {
		e := NewEncoder()
		put(e)
		b, err := e.Bytes()
		if err != nil {
			t.Fatalf("%s: Bytes: %v", name, err)
		}
		if want := []byte{0x0C, 0x04, 0xEF, 0xBF, 0xBD, 0x61}; !bytes.Equal(b, want) {
			t.Errorf("%s = % X, want % X (U+FFFD then \"a\")", name, b, want)
		}
	}
}

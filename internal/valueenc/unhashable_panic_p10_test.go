// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valueenc_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// unhashableP10 is a comparable type whose value can be unhashable: its
// interface field may hold a slice.
type unhashableP10 struct{ V any }

// encodeEscapes runs encode and returns what panicked out of it, if
// anything, with encode's error.
func encodeEscapes(encode func() error) (escaped any, err error) {
	defer func() { escaped = recover() }()
	return nil, encode()
}

// TestUnhashablePanicIsContainedAndCountedP10 (P10-err-6401): a panic
// value of a comparable type that is unhashable — the tripwire's lookup
// hashes it — is contained by Encode and by Scratch.Encode like any other
// panic ([valueenc.ErrPanic]), and counted as undeclared. Red at
// d1ab4e970: the lookup's own panic left the deferred recover, so the
// panic escaped both entry points.
func TestUnhashablePanicIsContainedAndCountedP10(t *testing.T) {
	var s valueenc.Scratch
	for name, encode := range map[string]func(func(*tlv.Encoder, tlv.Tag)) ([]byte, error){
		"Encode":         valueenc.Encode,
		"Scratch.Encode": s.Encode,
	} {
		before, _ := valueenc.UnexpectedPanics()
		escaped, err := encodeEscapes(func() error {
			_, err := encode(func(*tlv.Encoder, tlv.Tag) { panic(unhashableP10{V: []int{1}}) })
			return err
		})
		if escaped != nil {
			t.Fatalf("%s: panic left it: %T %v", name, escaped, escaped)
		}
		if !errors.Is(err, valueenc.ErrPanic) {
			t.Fatalf("%s: err = %v, want ErrPanic", name, err)
		}
		n, _ := valueenc.UnexpectedPanics()
		if n != before+1 {
			t.Fatalf("%s: undeclared panics %d, want %d: an unhashable value cannot have been declared", name, n, before+1)
		}
		valueenc.ForgetUnexpectedPanics(1) // provoked on purpose; an unhashable value cannot be declared
	}
}

// TestDeclaredPanicControlsP10: the controls beside P10-err-6401 — a
// hashable value of the same type, declared, is contained and not counted;
// a value of a type that is not comparable is contained and counted.
func TestDeclaredPanicControlsP10(t *testing.T) {
	valueenc.ExpectPanic(unhashableP10{V: 1})
	before, _ := valueenc.UnexpectedPanics()
	if _, err := valueenc.Encode(func(*tlv.Encoder, tlv.Tag) { panic(unhashableP10{V: 1}) }); !errors.Is(err, valueenc.ErrPanic) {
		t.Fatalf("declared: err = %v, want ErrPanic", err)
	}
	if n, _ := valueenc.UnexpectedPanics(); n != before {
		t.Fatalf("a declared panic was counted: %d, want %d", n, before)
	}
	if _, err := valueenc.Encode(func(*tlv.Encoder, tlv.Tag) { panic([]int{1}) }); !errors.Is(err, valueenc.ErrPanic) {
		t.Fatalf("not comparable: err = %v, want ErrPanic", err)
	}
	if n, _ := valueenc.UnexpectedPanics(); n != before+1 {
		t.Fatalf("a non-comparable panic counted %d, want %d", n, before+1)
	}
	valueenc.ForgetUnexpectedPanics(1)
}

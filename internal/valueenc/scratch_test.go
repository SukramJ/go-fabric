// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valueenc_test

import (
	"bytes"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// plain is write's bytes on a fresh encoder, the reference every Scratch
// result is held to.
func plain(t *testing.T, write func(*tlv.Encoder, tlv.Tag)) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	write(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestScratchReuse: values encoded one after another on one Scratch come
// back each as Encode returns it, and an element handed out earlier is
// not changed by the values after it.
func TestScratchReuse(t *testing.T) {
	t.Parallel()
	long := func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.PutUTF8(tlv.ContextTag(0), "a longer value that grows the buffer")
		enc.StartArray(tlv.ContextTag(1))
		for i := range 16 {
			enc.PutUint(tlv.AnonymousTag(), uint64(i))
		}
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	}
	short := func(enc *tlv.Encoder, tag tlv.Tag) { enc.PutUint(tag, 3) }

	var s valueenc.Scratch
	first, err := s.Encode(long)
	if err != nil {
		t.Fatal(err)
	}
	keep := bytes.Clone(first)
	for range 3 {
		got, err := s.Encode(short)
		if err != nil || !bytes.Equal(got, plain(t, short)) {
			t.Fatalf("short after long: %x, %v; want %x", got, err, plain(t, short))
		}
		got, err = s.Encode(long)
		if err != nil || !bytes.Equal(got, plain(t, long)) {
			t.Fatalf("long after short: %x, %v; want %x", got, err, plain(t, long))
		}
	}
	if !bytes.Equal(first, keep) {
		t.Fatalf("an element handed out earlier changed: %x, want %x", first, keep)
	}
}

// TestScratchAfterFailure: a value after one that panicked, left
// containers open (an Array among them, whose rule would refuse the next
// value's context tag) or wrote two elements is encoded exactly as on a
// fresh encoder, and the failures answer as Encode's do.
func TestScratchAfterFailure(t *testing.T) {
	t.Parallel()
	valueenc.ExpectPanic("scratch host bug")
	next := func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(1), 9)
		_ = enc.EndContainer()
	}
	for _, tc := range []struct {
		name  string
		write func(*tlv.Encoder, tlv.Tag)
		is    error
	}{
		{"panic mid-container", func(enc *tlv.Encoder, tag tlv.Tag) {
			enc.StartArray(tag)
			enc.PutUint(tlv.AnonymousTag(), 1)
			panic("scratch host bug")
		}, valueenc.ErrPanic},
		{"open array", func(enc *tlv.Encoder, tag tlv.Tag) {
			enc.StartStruct(tag)
			enc.StartArray(tlv.ContextTag(0))
		}, tlv.ErrUnbalancedContainer},
		{"two elements", func(enc *tlv.Encoder, tag tlv.Tag) {
			enc.PutUint(tag, 1)
			enc.PutUint(tag, 2)
		}, valueenc.ErrNotOneElement},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var s valueenc.Scratch
			if _, err := s.Encode(next); err != nil {
				t.Fatal(err)
			}
			got, err := s.Encode(tc.write)
			if !errors.Is(err, tc.is) || got != nil {
				t.Fatalf("failing value: %x, %v; want %v", got, err, tc.is)
			}
			got, err = s.Encode(next)
			if err != nil || !bytes.Equal(got, plain(t, next)) {
				t.Fatalf("value after the failure: %x, %v; want %x", got, err, plain(t, next))
			}
		})
	}
}

// TestScratchDropsEncoderAfterFailure: after a failed value the Scratch
// writes on a fresh encoder, so an encoder a failed write kept is never
// the one the next value is written on.
func TestScratchDropsEncoderAfterFailure(t *testing.T) {
	t.Parallel()
	var s valueenc.Scratch
	var kept, afterOK, afterFail *tlv.Encoder
	_, _ = s.Encode(func(enc *tlv.Encoder, tag tlv.Tag) { kept = enc; enc.PutUint(tag, 1) })
	_, _ = s.Encode(func(enc *tlv.Encoder, tag tlv.Tag) { afterOK = enc; enc.PutUint(tag, 1); enc.PutUint(tag, 2) })
	if afterOK != kept {
		t.Fatal("a successful value did not keep the encoder for the next")
	}
	_, _ = s.Encode(func(enc *tlv.Encoder, tag tlv.Tag) { afterFail = enc; enc.PutUint(tag, 1) })
	if afterFail == kept {
		t.Fatal("the encoder of a failed value was reused")
	}
}

// TestScratchTripwire: a panic contained by a Scratch counts on the
// tripwire as one contained by Encode does.
func TestScratchTripwire(t *testing.T) {
	type undeclaredScratch struct{ n int }
	before, _ := valueenc.UnexpectedPanics()
	var s valueenc.Scratch
	_, _ = s.Encode(func(*tlv.Encoder, tlv.Tag) { panic(undeclaredScratch{1}) })
	if n, _ := valueenc.UnexpectedPanics(); n != before+1 {
		t.Fatalf("undeclared panic on a Scratch counted %d, want %d", n, before+1)
	}
	valueenc.ForgetUnexpectedPanics(1)
}

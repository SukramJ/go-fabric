// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valueenc_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// TestEncode holds the containment: a well-formed value comes back as the
// one anonymous element it wrote, byte for byte what a plain encoder
// produces; every other outcome is an error and nothing else.
func TestEncode(t *testing.T) {
	t.Parallel()
	good := func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), 7)
		enc.StartList(tlv.ContextTag(1))
		enc.PutUTF8(tlv.ContextTag(1), "x")
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	}
	want := tlv.NewEncoder()
	good(want, tlv.AnonymousTag())
	wantBytes, _ := want.Bytes()

	got, err := valueenc.Encode(good)
	if err != nil || !bytes.Equal(got, wantBytes) {
		t.Fatalf("good value: %x, %v; want %x", got, err, wantBytes)
	}
	if got, err := valueenc.Encode(func(enc *tlv.Encoder, tag tlv.Tag) { enc.PutNull(tag) }); err != nil || !bytes.Equal(got, []byte{byte(tlv.TypeNull)}) {
		t.Fatalf("null: %x, %v", got, err)
	}
	// A surplus EndContainer writes nothing on an encoder of its own: the
	// value is intact and nothing outside it is closed.
	if got, err := valueenc.Encode(func(enc *tlv.Encoder, tag tlv.Tag) { good(enc, tag); _ = enc.EndContainer() }); err != nil || !bytes.Equal(got, wantBytes) {
		t.Fatalf("surplus EndContainer: %x, %v", got, err)
	}

	for _, tc := range []struct {
		name  string
		write func(*tlv.Encoder, tlv.Tag)
		is    error
	}{
		{"panic", func(*tlv.Encoder, tlv.Tag) { panic("host bug") }, valueenc.ErrPanic},
		{"one open", func(enc *tlv.Encoder, tag tlv.Tag) {
			enc.StartStruct(tag)
			enc.PutUint(tlv.ContextTag(0), 1)
		}, tlv.ErrUnbalancedContainer},
		{"two open", func(enc *tlv.Encoder, tag tlv.Tag) {
			enc.StartStruct(tag)
			enc.StartList(tlv.ContextTag(0))
		}, tlv.ErrUnbalancedContainer},
		{"context tag in array panics", func(enc *tlv.Encoder, tag tlv.Tag) {
			enc.StartArray(tag)
			enc.PutUint(tlv.ContextTag(0), 1)
			_ = enc.EndContainer()
		}, valueenc.ErrPanic},
		{"zero elements", func(*tlv.Encoder, tlv.Tag) {}, valueenc.ErrNotOneElement},
		{"two elements", func(enc *tlv.Encoder, tag tlv.Tag) {
			enc.PutUint(tag, 1)
			enc.PutUint(tag, 2)
		}, valueenc.ErrNotOneElement},
		{"other tag", func(enc *tlv.Encoder, _ tlv.Tag) { enc.PutUint(tlv.ContextTag(3), 1) }, valueenc.ErrNotOneElement},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := valueenc.Encode(tc.write)
			if !errors.Is(err, tc.is) || got != nil {
				t.Fatalf("%x, %v; want %v", got, err, tc.is)
			}
		})
	}
	_, err = valueenc.Encode(func(*tlv.Encoder, tlv.Tag) { panic("host bug") })
	if msg := err.Error(); !strings.Contains(msg, "host bug") || !strings.Contains(msg, "goroutine") {
		t.Fatalf("panic error without its value and stack: %q", msg)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tlv_test

import (
	"bytes"
	"testing"

	"thingfabric.dev/go-fabric/tlv"
)

// TestEncoderResetClearsBytes: after Reset an encoder that wrote an
// element holds nothing, and a slice Bytes returned before is unchanged
// by what the encoder writes next.
func TestEncoderResetClearsBytes(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.PutUint(tlv.AnonymousTag(), 7)
	first, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	keep := bytes.Clone(first)

	enc.Reset()
	got, err := enc.Bytes()
	if err != nil {
		t.Fatalf("Bytes after Reset: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Bytes after Reset = % x, want empty", got)
	}
	enc.PutUTF8(tlv.AnonymousTag(), "overwrite")
	if !bytes.Equal(first, keep) {
		t.Fatalf("an earlier Bytes result changed: % x, want % x", first, keep)
	}
}

// TestEncoderResetClearsOpenContainers: Reset discards open containers —
// Bytes no longer refuses the stream — and the container stack with them,
// so the array rule of the abandoned stream (no context tag inside an
// Array) does not apply to the next one.
func TestEncoderResetClearsOpenContainers(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartArray(tlv.ContextTag(1))
	if _, err := enc.Bytes(); err == nil {
		t.Fatal("Bytes with two open containers: no error")
	}

	enc.Reset()
	if err := enc.EndContainer(); err == nil {
		t.Fatal("EndContainer after Reset: no error, want ErrUnbalancedContainer")
	}
	// A context tag at the top level: refused only inside an Array, so a
	// panic here would mean the abandoned Array were still on the stack.
	enc.PutUint(tlv.ContextTag(2), 1)
	got, err := enc.Bytes()
	if err != nil {
		t.Fatalf("Bytes after Reset: %v", err)
	}
	want := tlv.NewEncoder()
	want.PutUint(tlv.ContextTag(2), 1)
	wantBytes, _ := want.Bytes()
	if !bytes.Equal(got, wantBytes) {
		t.Fatalf("stream after Reset = % x, want % x", got, wantBytes)
	}
}

// TestEncoderResetKeepsCapacity: once the buffer has grown, a value
// written after Reset costs no allocation but Bytes' copy.
func TestEncoderResetKeepsCapacity(t *testing.T) {
	enc := tlv.NewEncoder()
	write := func() {
		enc.StartStruct(tlv.AnonymousTag())
		enc.StartArray(tlv.ContextTag(1))
		for i := range 32 {
			enc.PutUint(tlv.AnonymousTag(), uint64(i))
		}
		_ = enc.EndContainer()
		enc.PutUTF8(tlv.ContextTag(2), "a value of some length")
		_ = enc.EndContainer()
	}
	write()
	if _, err := enc.Bytes(); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		enc.Reset()
		write()
		if _, err := enc.Bytes(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 1 {
		t.Fatalf("allocations per reused value = %v, want 1 (Bytes' copy)", allocs)
	}
}

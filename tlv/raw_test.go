// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package tlv_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// TestSplitArrayMembersRoundTrip splits an array of mixed members — a
// scalar, a nested struct holding a list, a string — and writes them back
// under the original array: the bytes are identical.
func TestSplitArrayMembersRoundTrip(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.StartArray(tlv.AnonymousTag())
	enc.PutUint16(tlv.AnonymousTag(), 0x1234)
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(1), 7)
	enc.StartList(tlv.ContextTag(2))
	enc.PutBool(tlv.AnonymousTag(), true)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutUTF8(tlv.AnonymousTag(), "x")
	_ = enc.EndContainer()
	orig, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	members, err := tlv.SplitArrayMembers(orig)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 3 {
		t.Fatalf("%d members, want 3", len(members))
	}
	out := tlv.NewEncoder()
	out.StartArray(tlv.AnonymousTag())
	for _, m := range members {
		if err := out.PutRawElement(tlv.AnonymousTag(), m); err != nil {
			t.Fatal(err)
		}
	}
	_ = out.EndContainer()
	again, err := out.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(orig, again) {
		t.Fatalf("round trip\n got %x\nwant %x", again, orig)
	}

	// A member re-tagged under a context tag keeps its value.
	st := tlv.NewEncoder()
	st.StartStruct(tlv.AnonymousTag())
	if err := st.PutRawElement(tlv.ContextTag(2), members[0]); err != nil {
		t.Fatal(err)
	}
	_ = st.EndContainer()
	b, _ := st.Bytes()
	dec := tlv.NewDecoder(b)
	_, _ = dec.Next()
	el, err := dec.Next()
	if err != nil || el.Tag != tlv.ContextTag(2) || el.Uint != 0x1234 {
		t.Fatalf("re-tagged member: %+v %v", el, err)
	}
}

// TestSplitArrayMembersRejects pins the error paths: a non-array, an
// unclosed array, trailing bytes, and a tagged raw element.
func TestSplitArrayMembersRejects(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.PutUint(tlv.AnonymousTag(), 1)
	scalar, _ := enc.Bytes()
	if _, err := tlv.SplitArrayMembers(scalar); !errors.Is(err, tlv.ErrNotArray) {
		t.Errorf("scalar: %v, want ErrNotArray", err)
	}
	if _, err := tlv.SplitArrayMembers([]byte{0x16, 0x04, 0x01}); !errors.Is(err, tlv.ErrUnbalancedContainer) {
		t.Errorf("unclosed: %v, want ErrUnbalancedContainer", err)
	}
	if _, err := tlv.SplitArrayMembers([]byte{0x16, 0x15, 0x04, 0x01, 0x18}); !errors.Is(err, tlv.ErrUnbalancedContainer) {
		t.Errorf("unclosed nested: %v, want ErrUnbalancedContainer", err)
	}
	if _, err := tlv.SplitArrayMembers([]byte{0x16, 0x18, 0x00}); err == nil {
		t.Error("trailing bytes accepted")
	}
	if _, err := tlv.SplitArrayMembers(nil); err == nil {
		t.Error("empty input accepted")
	}
	e := tlv.NewEncoder()
	if err := e.PutRawElement(tlv.AnonymousTag(), nil); err == nil {
		t.Error("empty raw element accepted")
	}
	if err := e.PutRawElement(tlv.AnonymousTag(), []byte{0x24, 0x01, 0x02}); err == nil {
		t.Error("context-tagged raw element accepted")
	}
}

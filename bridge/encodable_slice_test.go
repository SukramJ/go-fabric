// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// writeValue runs the attribute writer over v under context tag 2.
func writeValue(t *testing.T, v any) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	defaultAttributeValueWriter(enc, tlv.ContextTag(2), im.AttributeValue{Value: v})
	out, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestEncodableSliceWritesAnArray: a plain slice of a type that encodes
// itself — Descriptor.TagList's []Semtag, as cluster/core cloneTags
// returns it — goes on the wire as the array spec.List writes, not as the
// null the writer's default arm emitted before (G-N4, issue #388).
func TestEncodableSliceWritesAnArray(t *testing.T) {
	t.Parallel()
	label := spec.Nullable[string]{Value: "Left"}
	tags := []descdef.Semtag{
		{MfgCode: spec.Nullable[uint16]{Null: true}, NamespaceId: 8, Tag: 2},
		{MfgCode: spec.Nullable[uint16]{Value: 0xFFF1}, NamespaceId: 7, Tag: 1, Label: &label},
	}
	got := writeValue(t, tags)
	want := writeValue(t, spec.List[descdef.Semtag](tags))
	if !bytes.Equal(got, want) {
		t.Fatalf("[]Semtag wrote % X, want the spec.List bytes % X", got, want)
	}
	if !EncodesAttributeValue(tags) || !EncodesAttributeValue([]descdef.Semtag{}) {
		t.Fatal("a slice of an Encodable type reported as not encodable")
	}
	// An empty list is an empty array, not null.
	null := tlv.NewEncoder()
	null.PutNull(tlv.ContextTag(2))
	nb, _ := null.Bytes()
	if empty := writeValue(t, []descdef.Semtag{}); bytes.Equal(empty, nb) {
		t.Fatal("an empty []Semtag wrote null")
	}
	// A nil element of a slice of pointers is a null element.
	ptrs := writeValue(t, []*descdef.Semtag{&tags[0], nil})
	enc := tlv.NewEncoder()
	enc.StartArray(tlv.ContextTag(2))
	tags[0].EncodeTLV(enc, tlv.AnonymousTag())
	enc.PutNull(tlv.AnonymousTag())
	_ = enc.EndContainer()
	wantPtrs, _ := enc.Bytes()
	if !bytes.Equal(ptrs, wantPtrs) {
		t.Fatalf("[]*Semtag wrote % X, want % X", ptrs, wantPtrs)
	}
	// A slice of a type without EncodeTLV still falls through to null.
	type stranger struct{ A int }
	if EncodesAttributeValue([]stranger{{1}}) {
		t.Fatal("a slice of an unknown struct reported as encodable")
	}
}

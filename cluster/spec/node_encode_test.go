// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"
)

// tree is an Encodable writing every TLV element kind.
type tree struct{}

func (tree) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartArray(tag)
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutInt(tlv.ContextTag(0), -5)
	enc.PutUint(tlv.ContextTag(1), 300)
	enc.PutBool(tlv.ContextTag(2), true)
	enc.PutFloat32(tlv.ContextTag(3), 1.5)
	enc.PutFloat64(tlv.ContextTag(4), 2.5)
	enc.PutUTF8(tlv.ContextTag(5), "x")
	enc.PutOctets(tlv.ContextTag(6), []byte{1})
	enc.PutNull(tlv.ContextTag(7))
	enc.StartList(tlv.ContextTag(8))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutUint(tlv.AnonymousTag(), 7)
	_ = enc.EndContainer()
}

func encodeOf(t *testing.T, v spec.Encodable) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestNodeEncodeTLVRoundTrip: a decoded node tree encodes to the bytes it
// was read from (every integer at its smallest width, as written).
func TestNodeEncodeTLVRoundTrip(t *testing.T) {
	t.Parallel()
	want := encodeOf(t, tree{})
	n, err := spec.ReadElement(tlv.NewDecoder(want))
	if err != nil {
		t.Fatal(err)
	}
	if got := encodeOf(t, n); !bytes.Equal(got, want) {
		t.Fatalf("re-encoded % X, want % X", got, want)
	}
}

// TestListElements: a list value yields its elements; any other value is
// not a list (Write.ts:154-155 at 0ceb202d).
func TestListElements(t *testing.T) {
	t.Parallel()
	elements, isList, err := spec.ListElements(tree{})
	if err != nil || !isList || len(elements) != 2 {
		t.Fatalf("ListElements(list) = %d elements, %v, %v", len(elements), isList, err)
	}
	if _, isList, err := spec.ListElements(spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: 1}}); err != nil || isList {
		t.Fatalf("ListElements(uint) = %v, %v", isList, err)
	}
}

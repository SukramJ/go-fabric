// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestScalarOfAnotherTypeIsUndecodable (gate round 4, decision 161): a
// scalar of another TLV type than the attribute's — a string, a boolean or
// a signed integer to an unsigned attribute — and an array element with a
// tag are undecodable, so the write interaction ends with FAILURE, as
// matter.js's TlvNumber / TlvString / TlvBoolean / TlvArray throw for them
// (fable-read-6a-gate, re-read §1-2). A value of the right type, in or out
// of range, and a null to a nullable attribute stay on the path.
func TestScalarOfAnotherTypeIsUndecodable(t *testing.T) {
	t.Parallel()
	onLevel := attrPath(lvldef.ClusterID, lvldef.AttrOnLevel)
	for name, build := range map[string]func(*tlv.Encoder){
		"a string":         func(enc *tlv.Encoder) { enc.PutUTF8(tlv.AnonymousTag(), "x") },
		"a boolean":        func(enc *tlv.Encoder) { enc.PutBool(tlv.AnonymousTag(), true) },
		"a signed integer": func(enc *tlv.Encoder) { enc.PutInt(tlv.AnonymousTag(), 5) },
		"a float":          func(enc *tlv.Encoder) { enc.PutFloat32(tlv.AnonymousTag(), 5) },
	} {
		if av := writeValueOf(t, onLevel, build); !im.IsUndecodable(av) {
			t.Errorf("OnLevel written %s: decoded to %#v, want undecodable", name, av.Value)
		}
	}
	for name, build := range map[string]func(*tlv.Encoder){
		"5":    func(enc *tlv.Encoder) { enc.PutUint(tlv.AnonymousTag(), 5) },
		"300":  func(enc *tlv.Encoder) { enc.PutUint(tlv.AnonymousTag(), 300) },
		"null": func(enc *tlv.Encoder) { enc.PutNull(tlv.AnonymousTag()) },
	} {
		if av := writeValueOf(t, onLevel, build); im.IsUndecodable(av) {
			t.Errorf("OnLevel written %s: undecodable, want it on the path", name)
		}
	}
	// A null to a non-nullable scalar is undecodable too (owner decision
	// 170, R4-1): matter.js decodes a write with the attribute's TLV
	// schema, which is nullable only when the attribute is, and its
	// TlvNumber throws on the null before any validation, ending the write
	// interaction with FAILURE (fable-6a-w3 H3: TlvOfModel.ts:234-236,
	// TlvNumber.ts:124-131, AttributeWriteResponse.ts:429, :452-460).
	offTransition := attrPath(lvldef.ClusterID, lvldef.AttrOnOffTransitionTime)
	if av := writeValueOf(t, offTransition, func(enc *tlv.Encoder) { enc.PutNull(tlv.AnonymousTag()) }); !im.IsUndecodable(av) {
		t.Errorf("OnOffTransitionTime (not nullable) written null: decoded to %#v, want undecodable", av.Value)
	}
	// An array whose one element, a structure, carries context tag 1 (the
	// encoder refuses to write it, so the bytes are spelled out).
	dec := tlv.NewDecoder([]byte{0x16, 0x35, 0x01, 0x18, 0x18})
	open, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	tagged, err := attributeValueReader(attrPath(bindingdef.ClusterID, bindingdef.AttrBinding), open, dec)
	if err != nil {
		t.Fatal(err)
	}
	if !im.IsUndecodable(tagged) {
		t.Errorf("Binding with a tagged array element: decoded to %#v, want undecodable", tagged.Value)
	}
}

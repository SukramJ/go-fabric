// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	bindingdef "thingfabric.dev/go-fabric/cluster/spec/binding"
	"thingfabric.dev/go-fabric/tlv"
)

// TestWrongTypeIsAClass (gate round 4): a decoder's refusal of a value of
// another TLV type is errors.Is ErrWrongType — the class a write path
// routes to an interaction-level FAILURE — and no other refusal is: not a
// constraint, not invalid UTF-8, not a missing mandatory field. A TLV list
// where an array belongs is of another type (matter.js TlvArray.ts:73-74).
func TestWrongTypeIsAClass(t *testing.T) {
	t.Parallel()
	node := func(el tlv.Element) spec.Node { return spec.Node{El: el} }
	str := tlv.Element{Type: tlv.TypeUTF8Str1, String: "x"}
	u := tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: 300}
	signed := tlv.Element{Type: tlv.TypeSignedInt1, Int: 1}
	list := spec.Node{El: tlv.Element{Type: tlv.TypeList, IsContainer: true}}
	wrong := map[string]error{
		"string for a uint": func() error { _, err := spec.DecodeUint[uint8](8)(node(str)); return err }(),
		"signed for a uint": func() error { _, err := spec.DecodeUint[uint8](8)(node(signed)); return err }(),
		"uint for a string": func() error { _, err := spec.DecodeString(node(u)); return err }(),
		"uint for a bool":   func() error { _, err := spec.DecodeBool(node(u)); return err }(),
		"string for octets": func() error { _, err := spec.DecodeBytes(node(str)); return err }(),
		"a mistyped field of a struct": func() error {
			st := spec.Node{El: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}, Children: []spec.Node{
				{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: "x", Tag: tlv.ContextTag(1)}},
				{El: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: 1, Tag: tlv.ContextTag(254)}},
			}}
			_, err := spec.DecodeStruct[bindingdef.TargetStruct](st)
			return err
		}(),
		"a TLV list for an array": func() error {
			_, err := spec.DecodeList(spec.DecodeUint[uint8](8))(list)
			return err
		}(),
	}
	for name, err := range wrong {
		if !errors.Is(err, spec.ErrWrongType) {
			t.Errorf("%s: %v, want ErrWrongType", name, err)
		}
	}
	other := map[string]error{
		"uint out of range": func() error { _, err := spec.DecodeUint[uint8](8)(node(u)); return err }(),
		"invalid UTF-8": func() error {
			_, err := spec.DecodeString(node(tlv.Element{Type: tlv.TypeUTF8Str1, String: "\xff"}))
			return err
		}(),
	}
	for name, err := range other {
		if err == nil || errors.Is(err, spec.ErrWrongType) {
			t.Errorf("%s: %v, want a refusal that is not ErrWrongType", name, err)
		}
	}
}

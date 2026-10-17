// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"fmt"

	"github.com/SukramJ/go-fabric/tlv"
)

// EncodeTLV writes the node tree at tag, integers at their minimal width
// (as matter.js encodes every integer, TlvNumber.ts:247-297 at 0ceb202d).
// It makes a decoded value [Encodable] again.
func (n Node) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { writeNode(enc, tag, n) }

// writeNode writes n at tag, integers at their minimal width.
func writeNode(enc *tlv.Encoder, tag tlv.Tag, n Node) {
	el := n.El
	switch {
	case el.IsContainer:
		switch el.Type {
		case tlv.TypeArray:
			enc.StartArray(tag)
		case tlv.TypeList:
			enc.StartList(tag)
		default:
			enc.StartStruct(tag)
		}
		for _, c := range n.Children {
			writeNode(enc, c.El.Tag, c)
		}
		_ = enc.EndContainer()
	case el.IsNull || el.Type == tlv.TypeNull:
		enc.PutNull(tag)
	case el.Type <= tlv.TypeSignedInt8:
		enc.PutInt(tag, el.Int)
	case el.Type <= tlv.TypeUnsignedInt8:
		enc.PutUint(tag, el.Uint)
	case el.Type == tlv.TypeBoolFalse || el.Type == tlv.TypeBoolTrue:
		enc.PutBool(tag, el.Bool)
	case el.Type == tlv.TypeFloat4:
		enc.PutFloat32(tag, float32(el.Float))
	case el.Type == tlv.TypeFloat8:
		enc.PutFloat64(tag, el.Float)
	case el.Type >= tlv.TypeUTF8Str1 && el.Type <= tlv.TypeUTF8Str8:
		enc.PutUTF8(tag, el.String)
	default:
		enc.PutOctets(tag, el.Octets)
	}
}

// ListElements encodes v and reports whether it is a list (a TLV array),
// with its elements — the value side of matter.js's
// `Array.isArray(value) && tlv instanceof ArraySchema`
// (packages/protocol/src/action/request/Write.ts:154-155 at 0ceb202d), which
// decides whether a write is list-chunked.
func ListElements(v Encodable) (elements []Node, isList bool, err error) {
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		return nil, false, fmt.Errorf("spec: encode value: %w", err)
	}
	n, err := ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return nil, false, fmt.Errorf("spec: encode value: %w", err)
	}
	if n.El.Type != tlv.TypeArray {
		return nil, false, nil
	}
	return n.Children, true, nil
}

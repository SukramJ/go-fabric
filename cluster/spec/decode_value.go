// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"fmt"

	"github.com/SukramJ/go-fabric/tlv"
)

// DecodeValue decodes a value of type t from its node, by the type's Kind:
//
//   - KindBool: bool;
//   - KindUint, KindEnum, KindBitmap: uint64 (at t.Bits, 64 when 0);
//   - KindInt: int64 (at t.Bits, 64 when 0);
//   - KindFloat32: float32; KindFloat64: float64;
//   - KindString: string; KindBytes: []byte;
//   - KindList: []any, each entry decoded by t.Entry's type;
//   - KindStruct: map[uint32]any by field id, each field of t.Struct decoded
//     by its type (a member the struct does not define is left out);
//   - KindAny, and a struct without its fields: the node itself.
//
// TLV null is nil for every kind. It is the attribute-level counterpart of
// matter.js TlvOfModel for the values the generated decoders do not cover:
// matter.js decodes every reported attribute by its model as it arrives
// (packages/protocol/src/action/client/InputChunk.ts:89-124 at 0ceb202d);
// the controller keeps the node and decodes it here on access. A value of
// the wrong shape is an error; constraints are not checked.
func DecodeValue(t Type, n Node) (any, error) {
	if n.El.Type == tlv.TypeNull || n.El.IsNull {
		return nil, nil
	}
	bits := t.Bits
	if bits == 0 {
		bits = 64
	}
	switch t.Kind {
	case KindBool:
		return DecodeBool(n)
	case KindUint, KindEnum, KindBitmap:
		return DecodeUint[uint64](bits)(n)
	case KindInt:
		return DecodeInt[int64](bits)(n)
	case KindFloat32:
		return DecodeFloat32(n)
	case KindFloat64:
		return DecodeFloat64(n)
	case KindString:
		return DecodeString(n)
	case KindBytes:
		return DecodeBytes(n)
	case KindList:
		return decodeListValue(t, n)
	case KindStruct:
		return decodeStructValue(t, n)
	case KindAny:
	}
	return n, nil
}

func decodeListValue(t Type, n Node) (any, error) {
	if n.El.Type != tlv.TypeArray && n.El.Type != tlv.TypeList {
		return nil, invalid("value is not a list (type 0x%02X)", n.El.Type)
	}
	var entry Type
	if t.Entry != nil {
		entry = t.Entry.Type
	}
	out := make([]any, 0, len(n.Children))
	for i, c := range n.Children {
		v, err := DecodeValue(entry, c)
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		out = append(out, v)
	}
	return out, nil
}

func decodeStructValue(t Type, n Node) (any, error) {
	if n.El.Type != tlv.TypeStructure {
		return nil, invalid("value is not a struct (type 0x%02X)", n.El.Type)
	}
	if t.Struct == nil {
		return n, nil
	}
	out := make(map[uint32]any, len(n.Children))
	for _, c := range n.Children {
		if c.El.Tag.Kind != tlv.TagKindContext {
			continue
		}
		var f *Field
		for i := range t.Struct.Fields {
			if t.Struct.Fields[i].ID == c.El.Tag.Number {
				f = &t.Struct.Fields[i]
				break
			}
		}
		if f == nil {
			continue
		}
		v, err := DecodeValue(f.Type, c)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", f.Name, err)
		}
		out[f.ID] = v
	}
	return out, nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"fmt"
	"math"

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
		return DecodeUint[uint64](bits)(relaxedInteger(n, false))
	case KindInt:
		return DecodeInt[int64](bits)(relaxedInteger(n, true))
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

// relaxedInteger restates an integer element of the other signedness as
// the signedness the type wants, when its value is one that signedness
// holds: a reported value is decoded with matter.js's relaxNumberTypeChecks,
// so a signed-encoded unsigned value (or the reverse) from a non-compliant
// device is taken and still range-checked rather than dropped. A negative
// value for an unsigned type, an unsigned one beyond int64 for a signed
// type and every non-integer element stay as they are and are refused by
// the strict decoder. Mirrors matter.js
// packages/protocol/src/action/client/InputChunk.ts:42-48 at 862ad75f
// (DATA_REPORT_DECODE_OPTIONS) and packages/types/src/tlv/TlvNumber.ts:124-141
// at 862ad75f (decodeTlvInternalValue: an integer of the other signedness
// is read and checked against the schema's bounds). The server's decoders
// (DecodeUintIn, DecodeIntIn) stay strict, as matter.js's default.
func relaxedInteger(n Node, signed bool) Node {
	el := n.El
	switch {
	case !signed && el.Type <= tlv.TypeSignedInt8 && el.Int >= 0:
		n.El.Type, n.El.Uint = tlv.TypeUnsignedInt8, uint64(el.Int)
	case signed && el.Type >= tlv.TypeUnsignedInt1 && el.Type <= tlv.TypeUnsignedInt8 && el.Uint <= math.MaxInt64:
		n.El.Type, n.El.Int = tlv.TypeSignedInt8, int64(el.Uint)
	}
	return n
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

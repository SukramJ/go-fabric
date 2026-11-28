// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"slices"

	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// fabricIndexFieldTag is the context tag of the FabricIndex field every
// entry of a fabric-scoped list carries (Matter Core §7.13.6).
const fabricIndexFieldTag = 0xFE

// FabricScope is the scope of a request's read of a fabric-scoped list
// attribute: who reads (the accessing fabric and the request's
// FabricFiltered flag) and what the model states of the attribute (whether
// it is fabric-sensitive itself, and its entry's fabric-sensitive fields).
// The endpoint dispatcher attaches it to a [ReadResult] for every server;
// [MaskFabricScoped] applies it to the encoded value.
type FabricScope struct {
	// Fabric is the accessing fabric; 0 for a session without one.
	Fabric uint8
	// Filtered is the request's FabricFiltered flag.
	Filtered bool
	// Sensitive marks an attribute whose own access is fabric-sensitive
	// ("S"): the request reads its own fabric's entries alone, filtered or
	// not.
	Sensitive bool
	// Fields are the context tags of the entry's fabric-sensitive fields,
	// withheld from an entry another fabric owns.
	Fields []uint32
}

// MaskFabricScoped returns element — one anonymous TLV element, a
// fabric-scoped list attribute's value as the value writer encodes it — as
// the reader scope describes receives it. ok is false when the value cannot
// be scoped: it is no list, an entry is no struct, or an entry names no
// valid owner — no FabricIndex, or one outside 1 to 254 (0 is no fabric,
// above 254 an index the node could not resolve); the path then answers a
// status instead of data.
// A null value is returned unchanged. The function is cluster-blind: it
// reads the FabricIndex field (tag 0xFE) of each entry and nothing else of
// its meaning.
//
// For each entry, owned by the fabric its FabricIndex names:
//
//   - an entry without a valid FabricIndex refuses the whole value;
//   - an entry another fabric owns — every entry, for a session without an
//     accessing fabric ([HasAccessingFabric]) — is dropped when the request is fabric-filtered or the
//     attribute is fabric-sensitive, and otherwise kept without the fields
//     scope.Fields names;
//   - the reader's own entry is kept as it is.
//
// Mirrors matter.js at db021da0: the list filter of
// packages/node/src/behavior/state/managed/values/ListManager.ts:609-616
// (createProxy filters a fabric-scoped list for a remote reader when the
// read is fabric-filtered, or for every remote read of a fabric-sensitive
// list) and the field mask of StructManager.ts:64, :78-80, :110-113 (an
// entry's owning fabric is its fabricIndex) with :303-306 and :373-378
// (a member access.mayRead refuses reads undefined, its parent stays
// visible): packages/protocol/src/action/server/AccessControl.ts:342-356
// refuses a fabric-sensitive member without session.fabric or when the
// owning fabric differs, whatever session.fabricFiltered says, and the
// encoder leaves the undefined member out
// (packages/types/src/tlv/TlvObject.ts:128-134;
// packages/protocol/src/interaction/InteractionMessenger.ts:579-582,
// :866). An entry without a FabricIndex is the one place it departs:
// matter.js's FabricFilteredListProxyHandler shows such an entry to every
// fabric (ListManager.ts:480-481); connectedhomeip's list encoder refuses
// it with CHIP_ERROR_INVALID_FABRIC_INDEX when its index is
// kUndefinedFabricIndex, 0 (src/app/AttributeValueEncoder.h:100,
// src/lib/core/DataModelTypes.h:69 at 6170af84), and the report engine
// answers the path with that error's status (src/app/reporting/Engine.cpp:487-498,
// with no test of an expanded path), which is FAILURE
// (src/protocols/interaction_model/StatusCode.cpp:41-63, :62;
// CHIP_ERROR_INVALID_FABRIC_INDEX is CHIP_CORE_ERROR(0x71),
// src/lib/core/CHIPError.h:1335) — on a concrete and a wildcard read, a
// priming and a change report alike. So does this function's caller: the
// status rides a wildcard read, a re-established subscription's priming
// report and a change report too, where the bridge otherwise leaves
// failing paths out. The mask rests on every entry naming its owner.
func MaskFabricScoped(element []byte, scope FabricScope) (masked []byte, ok bool) {
	dec := tlv.NewDecoder(element)
	open, err := dec.Next()
	if err != nil {
		return nil, false
	}
	if open.Type == tlv.TypeNull {
		if dec.Remaining() != 0 {
			return nil, false
		}
		return element, true
	}
	if open.Type != tlv.TypeArray && open.Type != tlv.TypeList {
		return nil, false
	}
	out := []byte{byte(open.Type)}
	for {
		start := dec.Pos()
		el, err := dec.Next()
		if err != nil {
			return nil, false
		}
		if el.IsEndContainer {
			if dec.Remaining() != 0 {
				return nil, false
			}
			return append(out, byte(tlv.TypeEndContainer)), true
		}
		if el.Type != tlv.TypeStructure {
			return nil, false
		}
		entry, keep, ok := maskEntry(dec, element, start, scope)
		if !ok {
			return nil, false
		}
		if keep {
			out = append(out, entry...)
		}
	}
}

// scopeAttributeValue encodes v with write as one anonymous element, in
// isolation ([valueenc.Encode]: a write that panics, leaves a container
// open, writes other than one element or tags a container's members
// against the container's rule fails), and applies scope to it
// ([MaskFabricScoped]); ok is false when the value cannot be encoded or
// scoped, and the path then answers FAILURE.
func scopeAttributeValue(v AttributeValue, scope FabricScope, write AttributeValueWriter) (element []byte, ok bool) {
	encoded, err := valueenc.Encode(func(enc *tlv.Encoder, tag tlv.Tag) { write(enc, tag, v) })
	if err != nil {
		return nil, false
	}
	return MaskFabricScoped(encoded, scope)
}

// fieldSpan is one field of an entry struct: its context tag and the bytes
// it spans in the encoded value.
type fieldSpan struct {
	tag        uint32
	start, end int
}

// maskEntry reads the entry struct of buf whose opener, at begin, dec has
// just returned and returns the entry as scope's reader receives it; keep
// is false when the reader does not receive it, ok false when it carries
// no FabricIndex.
func maskEntry(dec *tlv.Decoder, buf []byte, begin int, scope FabricScope) (entry []byte, keep, ok bool) {
	var fields []fieldSpan
	var owner uint64
	hasOwner := false
	for {
		start := dec.Pos()
		el, err := dec.Next()
		if err != nil {
			return nil, false, false
		}
		if el.IsEndContainer {
			break
		}
		if el.IsContainer {
			if err := skipContainer(dec); err != nil {
				return nil, false, false
			}
		}
		if el.Tag.Kind == tlv.TagKindContext && el.Tag.Number == fabricIndexFieldTag {
			switch el.Type {
			case tlv.TypeUnsignedInt1, tlv.TypeUnsignedInt2, tlv.TypeUnsignedInt4, tlv.TypeUnsignedInt8:
				owner, hasOwner = el.Uint, true
			default:
				return nil, false, false
			}
		}
		span := fieldSpan{tag: el.Tag.Number, start: start, end: dec.Pos()}
		if el.Tag.Kind != tlv.TagKindContext {
			span.tag = ^uint32(0) // only a context tag names a field
		}
		fields = append(fields, span)
	}
	whole := buf[begin:dec.Pos()]
	if !hasOwner || owner > 0xFF || !HasAccessingFabric(uint8(owner)) {
		return nil, false, false
	}
	if HasAccessingFabric(scope.Fabric) && owner == uint64(scope.Fabric) {
		return whole, true, true
	}
	if scope.Filtered || scope.Sensitive {
		return nil, false, true
	}
	if len(scope.Fields) == 0 {
		return whole, true, true
	}
	out := []byte{byte(tlv.TypeStructure)}
	for _, f := range fields {
		if slices.Contains(scope.Fields, f.tag) {
			continue
		}
		out = append(out, buf[f.start:f.end]...)
	}
	return append(out, byte(tlv.TypeEndContainer)), true, true
}

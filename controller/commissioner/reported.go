// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"errors"
	"log/slog"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// The shapes a decoded value can take that the flow's typed field cannot:
// each is logged as the skipped value's error.
var (
	errReportedNullStruct = errors.New("null for a struct")
	errReportedNullList   = errors.New("null for a list")
	errReportedNullEntry  = errors.New("null entry")
)

// reportedValue decodes an attribute value the device reported by its
// shape alone: spec.DecodeValue checks no length or range constraint
// beyond the type's width (matter.js checks not even that,
// packages/types/src/tlv/TlvNumber.ts:142 at db021da0 — a difference),
// takes an integer of the other signedness and bounds-checks it, and
// leaves out a struct field the value does not carry. A value whose shape
// does not decode is logged and reported absent (false), and the read goes
// on. Mirrors matter.js packages/protocol/src/action/client/InputChunk.ts:
// 299-303 at db021da0 (decodeAttributeValueWithSchema with
// DATA_REPORT_DECODE_OPTIONS, ending in the schema's decodeTlv, which does
// not validate: AttributeDataDecoder.ts:311-335, TlvSchema.ts:64-66;
// the other signedness under relaxNumberTypeChecks, TlvNumber.ts:124-141)
// and :312-320 (an UnexpectedDataError is a warning and the entry is
// skipped). An absent value the flow cannot do without is then its "no …"
// refusal, where both references fail too (matter.js
// ControllerCommissioningFlow.ts:861-872 at db021da0; connectedhomeip
// src/controller/CHIPDeviceController.cpp:2781-2790 at 6170af84). Review
// II, F-db021da0-15.
func reportedValue(log *slog.Logger, what string, t spec.Type, n spec.Node) (any, bool) {
	v, err := spec.DecodeValue(t, n)
	if err != nil {
		log.Warn("matter.controller.commissioning.reported_value_skipped", slog.String("attribute", what), slog.Any("err", err))
		return nil, false
	}
	return v, true
}

// reportedUint is [reportedValue] for an unsigned integer of bits width
// (an integer of the other signedness taken and bounds-checked); null is
// isNull.
func reportedUint[T uint8 | uint16 | uint32 | uint64](log *slog.Logger, what string, bits int, n spec.Node) (v T, isNull, ok bool) {
	val, ok := reportedValue(log, what, spec.Type{Kind: spec.KindUint, Bits: bits}, n)
	if !ok {
		return 0, false, false
	}
	if val == nil {
		return 0, true, true
	}
	u, _ := val.(uint64)
	return T(u), false, true // DecodeValue held it to bits
}

// reportedStruct is [reportedValue] for a struct of def; a null value is
// one whose shape does not decode.
func reportedStruct(log *slog.Logger, what string, def *spec.Struct, n spec.Node) (map[uint32]any, bool) {
	v, ok := reportedValue(log, what, spec.Type{Kind: spec.KindStruct, Struct: def}, n)
	if !ok {
		return nil, false
	}
	m, ok := v.(map[uint32]any)
	if !ok {
		log.Warn("matter.controller.commissioning.reported_value_skipped", slog.String("attribute", what), slog.Any("err", errReportedNullStruct))
	}
	return m, ok
}

// reportedStructList is [reportedValue] for a list of structs of def; a
// null list or a null entry is a value whose shape does not decode.
func reportedStructList(log *slog.Logger, what string, def *spec.Struct, n spec.Node) ([]map[uint32]any, bool) {
	t := spec.Type{Kind: spec.KindList, Entry: &spec.Field{Type: spec.Type{Kind: spec.KindStruct, Struct: def}}}
	v, ok := reportedValue(log, what, t, n)
	if !ok {
		return nil, false
	}
	entries, ok := v.([]any)
	if !ok {
		log.Warn("matter.controller.commissioning.reported_value_skipped", slog.String("attribute", what), slog.Any("err", errReportedNullList))
		return nil, false
	}
	out := make([]map[uint32]any, 0, len(entries))
	for _, e := range entries {
		m, ok := e.(map[uint32]any)
		if !ok {
			log.Warn("matter.controller.commissioning.reported_value_skipped", slog.String("attribute", what), slog.Any("err", errReportedNullEntry))
			return nil, false
		}
		out = append(out, m)
	}
	return out, true
}

// field is the value of the field def names in a struct decoded by
// [reportedStruct]: false when the value does not carry it (or def has no
// such field). The id comes from the generated definition, not from here.
func field(def *spec.Struct, m map[uint32]any, name string) (any, bool) {
	for i := range def.Fields {
		if def.Fields[i].Name == name {
			v, ok := m[def.Fields[i].ID]
			return v, ok
		}
	}
	return nil, false
}

// fieldUint is an unsigned field of a decoded struct; DecodeValue checked
// it against the field's width, so the conversion keeps it.
func fieldUint[T uint8 | uint16 | uint32 | uint64](def *spec.Struct, m map[uint32]any, name string) (T, bool) {
	v, _ := field(def, m, name)
	u, ok := v.(uint64)
	return T(u), ok
}

// fieldBytes is an octet-string field of a decoded struct.
func fieldBytes(def *spec.Struct, m map[uint32]any, name string) ([]byte, bool) {
	v, _ := field(def, m, name)
	b, ok := v.([]byte)
	return b, ok
}

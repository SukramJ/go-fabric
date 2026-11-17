// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"log/slog"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// reportedValue decodes an attribute value the device reported by its
// shape alone: spec.DecodeValue checks no length or range constraint and
// leaves out a struct field the value does not carry. A value whose shape
// does not decode is logged and reported absent (false), and the read goes
// on. Mirrors matter.js packages/protocol/src/action/client/InputChunk.ts:
// 299-303 at db021da0 (decodeAttributeValueWithSchema with
// DATA_REPORT_DECODE_OPTIONS, ending in the schema's decodeTlv, which does
// not validate: AttributeDataDecoder.ts:311-335, TlvSchema.ts:64-66) and
// :312-320 (an UnexpectedDataError is a warning and the entry is skipped).
// A value the flow cannot do without is then its "no …" refusal, where
// both references fail too (matter.js ControllerCommissioningFlow.ts:
// 861-872 at db021da0; connectedhomeip
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

// reportedScalar is [reportedValue] for a scalar, through a decoder that
// reads it by shape alone (an integer's width, null): the typed result
// without a conversion.
func reportedScalar[T any](log *slog.Logger, what string, d spec.Decoder[T], n spec.Node) (T, bool) {
	v, err := d(n)
	if err != nil {
		log.Warn("matter.controller.commissioning.reported_value_skipped", slog.String("attribute", what), slog.Any("err", err))
		return v, false
	}
	return v, true
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
		log.Warn("matter.controller.commissioning.reported_value_skipped", slog.String("attribute", what), slog.String("err", "null for a struct"))
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
		log.Warn("matter.controller.commissioning.reported_value_skipped", slog.String("attribute", what), slog.String("err", "null for a list"))
		return nil, false
	}
	out := make([]map[uint32]any, 0, len(entries))
	for _, e := range entries {
		m, ok := e.(map[uint32]any)
		if !ok {
			log.Warn("matter.controller.commissioning.reported_value_skipped", slog.String("attribute", what), slog.String("err", "null entry"))
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

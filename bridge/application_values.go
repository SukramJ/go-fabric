// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"reflect"

	"github.com/SukramJ/go-fabric/cluster/spec"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/tlv"
)

// encodeApplicationValue writes the structured values of the application
// cluster servers (cluster/alarm, cluster/fan, cluster/pump,
// cluster/opstate, cluster/modebase) and the Switch events: event
// payloads, whose EventDataIB Data slot goes through the same value
// writer as attribute data, and the list / struct attributes. Every one
// of them encodes through a generated codec (spec.Encodable), as
// matter.js's TlvOfModel writes it (application-wire-fixtures.json,
// appliance-wire-fixtures.json); the event and struct types of
// cluster/wire and cluster/alarm implement it themselves. A list arrives
// as a plain slice, which carries no method, so the three list shapes
// are converted to their generated list here. It reports false for a
// value it does not know, which the caller then encodes as null. Kept out
// of defaultAttributeValueWriter's own switch so each cluster's shapes sit
// next to each other rather than among the root clusters'.
func encodeApplicationValue(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	switch x := v.(type) {
	case []string:
		// A list of strings: OperationalState.PhaseList (a null list is
		// a nil value and never reaches here), list[string] in the
		// generated codec.
		spec.PutList(spec.PutString)(enc, tag, x)
	case []clusterwire.OperationalStateStruct:
		// OperationalState.OperationalStateList.
		clusterwire.OperationalStateList(x).EncodeTLV(enc, tag)
	case []clusterwire.ModeOptionStruct:
		// ModeBase SupportedModes.
		clusterwire.ModeOptionList(x).EncodeTLV(enc, tag)
	case spec.Encodable:
		// A value of a generated cluster definition (cluster/spec/...):
		// a struct, a spec.List of structs or an event payload, which
		// encodes itself as matter.js's TlvOfModel does.
		x.EncodeTLV(enc, tag)
	default:
		return encodeEncodableSlice(enc, tag, v)
	}
	return true
}

// encodableType is the reflect type of [spec.Encodable].
var encodableType = reflect.TypeFor[spec.Encodable]()

// encodeEncodableSlice writes a plain slice whose element type encodes
// itself ([spec.Encodable]) as a TLV array of its elements — the general
// rule for a list attribute a server returns as []T rather than
// spec.List[T]: Descriptor.TagList ([]Semtag, cluster/core cloneTags) and
// any list of a generated or hand-written struct with an EncodeTLV. A
// plain slice carries no method, so without this rule each such list fell
// through to the writer's default arm and went on the wire as TLV null.
// A nil pointer or interface element encodes as null. It reports false
// for any other value.
func encodeEncodableSlice(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return false
	}
	if !rv.Type().Elem().Implements(encodableType) {
		return encodeScalarSlice(enc, tag, rv)
	}
	enc.StartArray(tag)
	for i := range rv.Len() {
		el := rv.Index(i)
		if (el.Kind() == reflect.Pointer || el.Kind() == reflect.Interface) && el.IsNil() {
			enc.PutNull(tlv.AnonymousTag())
			continue
		}
		if e, ok := el.Interface().(spec.Encodable); ok {
			e.EncodeTLV(enc, tlv.AnonymousTag())
		}
	}
	_ = enc.EndContainer()
	return true
}

// encodeScalarSlice writes a plain slice of a named integer, bool or
// string type — a list of enums (CameraAvStreamManagement's
// StreamUsagePriorities, []StreamUsageEnum) or of a bitmap — as a TLV
// array of its values, the integers at the smallest width that holds
// them as the writer's integer rule does (matter.js TlvUInt/TlvInt).
// Without it such a list went on the wire as TLV null, which a
// controller rejects (connectedhomeip's Python TH: ValueDecodeFailure,
// TC-WEBRTCP-2.8). It reports false for any other element kind.
func encodeScalarSlice(enc *tlv.Encoder, tag tlv.Tag, rv reflect.Value) bool {
	var put func(reflect.Value)
	switch rv.Type().Elem().Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		put = func(el reflect.Value) { enc.PutUint(tlv.AnonymousTag(), el.Uint()) }
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		put = func(el reflect.Value) { enc.PutInt(tlv.AnonymousTag(), el.Int()) }
	case reflect.Bool:
		put = func(el reflect.Value) { enc.PutBool(tlv.AnonymousTag(), el.Bool()) }
	case reflect.String:
		put = func(el reflect.Value) { enc.PutUTF8(tlv.AnonymousTag(), el.String()) }
	default:
		return false
	}
	enc.StartArray(tag)
	for i := range rv.Len() {
		put(rv.Index(i))
	}
	_ = enc.EndContainer()
	return true
}

// encodeApplicationResponse writes the command responses of the
// application cluster servers — OperationalCommandResponse,
// ChangeToModeResponse and every generated response payload — through
// their generated codecs; it reports false for a value it does not know.
func encodeApplicationResponse(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	x, ok := v.(spec.Encodable)
	if !ok {
		return false
	}
	x.EncodeTLV(enc, tag)
	return true
}

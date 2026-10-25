// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"
)

// The JSON value route: a value of the image, a report, an event or a
// command response ([Value]) rendered as JSON, and a JSON value encoded
// for a write or an invoke ([Encodable]), typed by the cluster
// definitions the module generates from the matter.js model
// (cluster/spec, linked in by this package for every cluster of the
// snapshot). A host speaks JSON to its own clients without reading a TLV
// element.
//
// The forms are those of matterjs-server's WebSocket API
// (docs/websockets_api.md of github.com/matter-js/matterjs-server at
// a7922e85, the examples under testdata/compat): a struct is an object
// keyed by the field id in decimal ([TagBased], the attribute form) or by
// the camelCase field name ([NameBased], the form of a command's fields
// and an event's data); bytes are standard base64; integers, enums and
// bitmaps are JSON numbers, 64-bit ones in full as plain decimals; a list
// is an array; null is a nullable value's null. matter.js itself hands
// values to JavaScript as objects and has no JSON form of its own.
type (
	// Value is a decoded TLV value as the role hands it out: an
	// attribute's value in the image and in a report
	// ([AttributeData].Value, [Cluster.Value]), an event's data
	// ([EventData]), a command response's fields ([Result].Fields). Render
	// it with [DecodeJSON], or decode it with a generated <cluster>def
	// package's StateFrom.
	Value = spec.Node
	// Encodable is a value a write or an invoke sends: a generated request
	// or struct, a scalar spec.Value, or what [EncodeJSON] returns.
	Encodable = spec.Encodable
)

// Form is how [DecodeJSON] keys a struct.
type Form uint8

// The forms of a struct.
const (
	// TagBased keys a struct by its field ids in decimal ({"0": true}),
	// matterjs-server's form of an attribute value. A member the model
	// does not name is left out.
	TagBased Form = iota
	// NameBased keys a struct by its camelCase field names
	// ({"newPosition": 1}), matterjs-server's form of a command's fields
	// and an event's data. A member the model does not name is left out.
	NameBased
)

// ErrJSONValue is a JSON value that does not fit its type; the message
// says where and why.
var ErrJSONValue = errors.New("client: JSON value does not fit its type")

// Field is the type a JSON value is a value of: an attribute's, a
// command's fields, a command response's or an event's data, looked up in
// the generated cluster definitions. The zero Field, and a Field of an
// element no definition names, is untyped: a struct is keyed by its
// context tags, bytes are base64, a number keeps the TLV's signedness.
type Field struct {
	f *spec.Field
	// request marks a request command's fields, whose missing mandatory
	// members EncodeJSON fills with their fallback.
	request bool
}

// AttributeField is the type of an attribute's value: a global attribute's
// (FeatureMap, ClusterRevision, the id lists) for any cluster, else the
// cluster definition's.
func AttributeField(cluster, attribute uint32) Field {
	if t, ok := spec.GlobalAttributeType(attribute); ok {
		return Field{f: &spec.Field{ID: attribute, Type: t}}
	}
	if def := spec.Lookup(cluster); def != nil {
		if a := def.Attribute(attribute); a != nil {
			return Field{f: spec.AttributeField(a)}
		}
	}
	return Field{}
}

// CommandField is the type of a request command's fields: a struct of
// them.
func CommandField(cluster, command uint32) Field {
	f := commandField(cluster, command, spec.Request)
	f.request = f.f != nil
	return f
}

// ResponseField is the type of a response command's fields ([Result].Fields
// of its Command).
func ResponseField(cluster, command uint32) Field {
	return commandField(cluster, command, spec.Response)
}

func commandField(cluster, command uint32, dir spec.Direction) Field {
	if def := spec.Lookup(cluster); def != nil {
		if c := def.Command(command, dir); c != nil {
			return Field{f: spec.CommandField(c)}
		}
	}
	return Field{}
}

// EventField is the type of an event's data: a struct of its fields.
func EventField(cluster, event uint32) Field {
	if def := spec.Lookup(cluster); def != nil {
		if e := def.Event(event); e != nil {
			return Field{f: spec.EventField(e)}
		}
	}
	return Field{}
}

// Typed reports whether f names a type of the model.
func (f Field) Typed() bool { return f.f != nil }

// EncodeJSON encodes the JSON value raw as a value of f for a write
// ([WriteRequest].Value) or the fields of an invoke. A struct takes its
// members by camelCase or model name (compared without regard to case) or
// by decimal field id; an unknown one is refused, and null for an optional
// member that is not nullable counts as left out. A mandatory member must
// be there, except in a command's fields ([CommandField]), where a
// mandatory member left out takes the specification's fallback as
// matter.js's MandatoryDefaultValue computes it
// (packages/model/src/logic/SelectDefaultValue.ts:72-131 at 862ad75f): null
// when nullable, 0 for a number or bitmap, false, "", empty bytes, an empty
// list, a struct of its members' fallbacks; an enum has none and stays
// required — so {"level": 128, "transitionTime": 10} is a
// MoveToLevelWithOnOff with OptionsMask and OptionsOverride 0. Bytes are
// standard base64. An empty raw, or null, for a command's fields is the
// empty payload. An untyped f takes go-fabric's
// untyped form (spec.EncodeJSON with no field: an object is a struct keyed
// by context tags).
func EncodeJSON(f Field, raw json.RawMessage) (Encodable, error) {
	trimmed := bytes.TrimSpace(raw)
	if f.f != nil && f.f.Type.Kind == spec.KindStruct && f.f.Type.Struct != nil && (len(trimmed) == 0 || string(trimmed) == "null") && !f.f.Quality.Nullable {
		trimmed = []byte("{}")
	}
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: no value", ErrJSONValue)
	}
	if f.f == nil {
		n, err := spec.EncodeJSON(nil, trimmed)
		if err != nil {
			return nil, errors.Join(ErrJSONValue, err)
		}
		return interaction.Raw(n), nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, errors.Join(ErrJSONValue, err)
	}
	b, err := marshalJSON(prepareJSON(f.f.Type, v, f.request))
	if err != nil {
		return nil, errors.Join(ErrJSONValue, err)
	}
	n, err := spec.EncodeJSON(f.f, b)
	if err != nil {
		return nil, errors.Join(ErrJSONValue, err)
	}
	return interaction.Raw(n), nil
}

// prepareJSON rewrites a decoded JSON value of the protocol's form into the
// form spec.EncodeJSON reads: base64 bytes get its "base64:" prefix, a
// null for an optional member that is not nullable is dropped, and with
// fill a mandatory member left out takes its fallback ([mandatoryFallback]).
func prepareJSON(t spec.Type, v any, fill bool) any {
	switch t.Kind {
	case spec.KindBytes:
		if s, ok := v.(string); ok {
			return "base64:" + s
		}
	case spec.KindStruct:
		m, ok := v.(map[string]any)
		if !ok || t.Struct == nil {
			return v
		}
		out := make(map[string]any, len(m))
		for k, val := range m {
			f := memberByKey(t.Struct, k)
			if f == nil {
				out[k] = val
				continue
			}
			if val == nil && !f.Quality.Nullable && f.Conformance.Op != spec.ConfMandatory {
				continue
			}
			out[k] = prepareJSON(f.Type, val, fill)
		}
		if fill {
			for i := range t.Struct.Fields {
				f := &t.Struct.Fields[i]
				if f.Conformance.Op != spec.ConfMandatory || hasMember(m, f) {
					continue
				}
				if fb, ok := mandatoryFallback(f); ok {
					out[strconv.FormatUint(uint64(f.ID), 10)] = fb
				}
			}
		}
		return out
	case spec.KindList:
		arr, ok := v.([]any)
		if !ok || t.Entry == nil {
			return v
		}
		out := make([]any, len(arr))
		for i, e := range arr {
			out[i] = prepareJSON(t.Entry.Type, e, fill)
		}
		return out
	default:
	}
	return v
}

// hasMember reports whether the JSON object m names member f by any key
// [memberByKey] accepts.
func hasMember(m map[string]any, f *spec.Field) bool {
	for k := range m {
		if strings.EqualFold(k, f.Name) || strconv.FormatUint(uint64(f.ID), 10) == k {
			return true
		}
	}
	return false
}

// mandatoryFallback is the value a mandatory member left out of a
// command's fields takes, in the form spec.EncodeJSON reads — matter.js
// MandatoryDefaultValue (SelectDefaultValue.ts:72-131 at 862ad75f): the
// stated default when there is one, else null when nullable, 0 for a
// number or bitmap, false for a boolean, "" for a string, empty bytes, an
// empty list, a struct of its mandatory members' fallbacks; false for an
// enum, whose fallback is manufacturer-specific.
func mandatoryFallback(f *spec.Field) (any, bool) {
	switch d := f.Default.(type) {
	case float64, bool, string:
		return d, true
	case spec.NullDefault:
		return nil, true
	}
	if f.Quality.Nullable {
		return nil, true
	}
	switch f.Type.Kind {
	case spec.KindUint, spec.KindInt, spec.KindFloat32, spec.KindFloat64, spec.KindBitmap:
		return 0, true
	case spec.KindBool:
		return false, true
	case spec.KindString:
		return "", true
	case spec.KindBytes:
		return "hex:", true
	case spec.KindList:
		return []any{}, true
	case spec.KindStruct:
		if f.Type.Struct == nil {
			return nil, false
		}
		out := map[string]any{}
		for i := range f.Type.Struct.Fields {
			m := &f.Type.Struct.Fields[i]
			if m.Conformance.Op != spec.ConfMandatory {
				continue
			}
			v, ok := mandatoryFallback(m)
			if !ok {
				return nil, false
			}
			out[strconv.FormatUint(uint64(m.ID), 10)] = v
		}
		return out, true
	case spec.KindEnum, spec.KindAny:
	}
	return nil, false
}

// memberByKey is the struct member a JSON key names: its model name or its
// camelCase name without regard to case, or its decimal id.
func memberByKey(s *spec.Struct, key string) *spec.Field {
	for i := range s.Fields {
		f := &s.Fields[i]
		if strings.EqualFold(key, f.Name) || strconv.FormatUint(uint64(f.ID), 10) == key {
			return f
		}
	}
	return nil
}

// DecodeJSON renders v, a value of f, as JSON in form: a struct keyed by
// field id ([TagBased]) or camelCase name ([NameBased]), the members the
// model does not name left out; bytes standard base64; numbers as JSON
// numbers (a 64-bit integer in full); a list an array; null for TLV null.
// A value whose shape does not match f, and any value of an untyped f, is
// rendered untyped: a struct as an object keyed by its context tags, bytes
// base64, lists arrays.
func DecodeJSON(f Field, v Value, form Form) (json.RawMessage, error) {
	var out any
	if f.f == nil {
		out = untypedJSON(v)
	} else {
		out = typedJSON(f.f.Type, v, form)
	}
	b, err := marshalJSON(out)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// marshalJSON is json.Marshal without its HTML escaping: "<", ">" and "&"
// stay as they are, as JavaScript's JSON.stringify — the writer of
// matterjs-server's JSON — leaves them, so a string attribute renders the
// bytes a matter.js-based server sends. No trailing newline.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func typedJSON(t spec.Type, n Value, form Form) any {
	el := n.El
	if el.IsNull || el.Type == tlv.TypeNull {
		return nil
	}
	switch t.Kind {
	case spec.KindBool:
		if isBoolEl(el.Type) {
			return el.Bool
		}
	case spec.KindUint, spec.KindEnum, spec.KindBitmap, spec.KindInt, spec.KindFloat32, spec.KindFloat64:
		if isNumberEl(el.Type) {
			return numberJSON(el)
		}
	case spec.KindString:
		if isStringEl(el.Type) {
			return el.String
		}
	case spec.KindBytes:
		if isBytesEl(el.Type) {
			return base64.StdEncoding.EncodeToString(el.Octets)
		}
	case spec.KindStruct:
		if el.Type == tlv.TypeStructure && t.Struct != nil {
			out := map[string]any{}
			for _, child := range n.Children {
				f := memberByTag(t.Struct, child.El.Tag)
				if f == nil {
					continue
				}
				key := spec.PropertyName(f.Name)
				if form == TagBased {
					key = strconv.FormatUint(uint64(f.ID), 10)
				}
				out[key] = typedJSON(f.Type, child, form)
			}
			return out
		}
	case spec.KindList:
		if el.IsContainer && el.Type != tlv.TypeStructure {
			out := make([]any, 0, len(n.Children))
			for _, child := range n.Children {
				if t.Entry != nil {
					out = append(out, typedJSON(t.Entry.Type, child, form))
				} else {
					out = append(out, untypedJSON(child))
				}
			}
			return out
		}
	case spec.KindAny:
	}
	return untypedJSON(n)
}

func memberByTag(s *spec.Struct, tag tlv.Tag) *spec.Field {
	if tag.Kind != tlv.TagKindContext {
		return nil
	}
	for i := range s.Fields {
		if s.Fields[i].ID == tag.Number {
			return &s.Fields[i]
		}
	}
	return nil
}

func untypedJSON(n Value) any {
	el := n.El
	switch {
	case el.IsNull || el.Type == tlv.TypeNull:
		return nil
	case el.Type == tlv.TypeStructure:
		out := make(map[string]any, len(n.Children))
		for _, child := range n.Children {
			out[strconv.FormatUint(uint64(child.El.Tag.Number), 10)] = untypedJSON(child)
		}
		return out
	case el.IsContainer:
		out := make([]any, 0, len(n.Children))
		for _, child := range n.Children {
			out = append(out, untypedJSON(child))
		}
		return out
	case isBoolEl(el.Type):
		return el.Bool
	case isNumberEl(el.Type):
		return numberJSON(el)
	case isStringEl(el.Type):
		return el.String
	case isBytesEl(el.Type):
		return base64.StdEncoding.EncodeToString(el.Octets)
	}
	return nil
}

// numberJSON is a TLV number as a JSON number: an integer in full (64-bit
// values as plain decimals, no float rounding), a float as the shortest
// decimal that reads back; a non-finite float is null.
func numberJSON(el tlv.Element) any {
	switch {
	case el.Type <= tlv.TypeSignedInt8:
		return json.Number(strconv.FormatInt(el.Int, 10))
	case el.Type <= tlv.TypeUnsignedInt8:
		return json.Number(strconv.FormatUint(el.Uint, 10))
	}
	if math.IsInf(el.Float, 0) || math.IsNaN(el.Float) {
		return nil
	}
	bits := 64
	if el.Type == tlv.TypeFloat4 {
		bits = 32
	}
	return json.Number(strconv.FormatFloat(el.Float, 'g', -1, bits))
}

func isBoolEl(t tlv.ElementType) bool { return t == tlv.TypeBoolFalse || t == tlv.TypeBoolTrue }

func isNumberEl(t tlv.ElementType) bool {
	return t <= tlv.TypeUnsignedInt8 || t == tlv.TypeFloat4 || t == tlv.TypeFloat8
}

func isStringEl(t tlv.ElementType) bool { return t >= tlv.TypeUTF8Str1 && t <= tlv.TypeUTF8Str8 }

func isBytesEl(t tlv.ElementType) bool { return t >= tlv.TypeOctetStr1 && t <= tlv.TypeOctetStr8 }

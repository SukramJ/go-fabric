// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package valuejson renders a decoded TLV value as JSON in
// matterjs-server's forms — the decode half of controller/client's JSON
// codec (client.DecodeJSON), shared with the node facade
// (node.AttributeValue.JSON, node.Event.JSON) so a node and a controller
// render one value alike. It sits below both roles: the node facade
// importing controller/client would close an import cycle through
// controller/commissioner's tests.
package valuejson

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"
)

// AttributeField is the type of an attribute's value: a global attribute's
// (FeatureMap, ClusterRevision, the id lists) for any cluster, else the
// cluster definition's; nil when no definition names it.
func AttributeField(cluster, attribute uint32) *spec.Field {
	if t, ok := spec.GlobalAttributeType(attribute); ok {
		return &spec.Field{ID: attribute, Type: t}
	}
	if def := spec.Lookup(cluster); def != nil {
		if a := def.Attribute(attribute); a != nil {
			return spec.AttributeField(a)
		}
	}
	return nil
}

// EventField is the type of an event's data, a struct of its fields; nil
// when no definition names the event.
func EventField(cluster, event uint32) *spec.Field {
	if def := spec.Lookup(cluster); def != nil {
		if e := def.Event(event); e != nil {
			return spec.EventField(e)
		}
	}
	return nil
}

// Decode renders n, a value of f, as JSON: a struct keyed by its camelCase
// field names (byName) or its field ids in decimal, the members the model
// does not name left out; bytes standard base64; numbers as JSON numbers
// (a 64-bit integer in full); a list an array; null for TLV null. A value
// whose shape does not match f, and any value of a nil f, is rendered
// untyped: a struct as an object keyed by its context tags.
func Decode(f *spec.Field, n spec.Node, byName bool) (json.RawMessage, error) {
	var out any
	if f == nil {
		out = untypedJSON(n)
	} else {
		out = typedJSON(f.Type, n, byName)
	}
	return Marshal(out)
}

// Marshal is json.Marshal without its HTML escaping: "<", ">" and "&"
// stay as they are, as JavaScript's JSON.stringify — the writer of
// matterjs-server's JSON — leaves them, so a string attribute renders the
// bytes a matter.js-based server sends. No trailing newline.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func typedJSON(t spec.Type, n spec.Node, byName bool) any {
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
				if !byName {
					key = strconv.FormatUint(uint64(f.ID), 10)
				}
				out[key] = typedJSON(f.Type, child, byName)
			}
			return out
		}
	case spec.KindList:
		if el.IsContainer && el.Type != tlv.TypeStructure {
			out := make([]any, 0, len(n.Children))
			for _, child := range n.Children {
				if t.Entry != nil {
					out = append(out, typedJSON(t.Entry.Type, child, byName))
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

func untypedJSON(n spec.Node) any {
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

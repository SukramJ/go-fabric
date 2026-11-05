// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The application codec: how a value, a response and a request of a
// generated cluster definition cross the wire. matter.js encodes every
// value through the cluster's TLV schema (TlvSchema.encode, the attribute
// and command types of packages/types/src/cluster); here a generated value
// is a [spec.Encodable], a generated request has its definition's Decode
// ([spec.DecodeRequest]), and the scalars are the TLV element types. The
// device-side bridge adds its hand-written core tables in front of these;
// the client stack's Interaction Model server (client/interaction) uses
// them as they are, so both roles write the same bytes for the same
// generated value.

// AttributeValueWriter is an [im.AttributeValueWriter] over the scalars,
// [spec.Encodable] values and lists of either. A value of no such kind
// encodes as null, the bridge's choice for a value its writer does not
// know (bridge/reply.go defaultAttributeValueWriter).
func AttributeValueWriter(enc *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
	if v.IsNull || v.Value == nil {
		enc.PutNull(tag)
		return
	}
	if putScalar(enc, tag, v.Value) || EncodeApplicationValue(enc, tag, v.Value) {
		return
	}
	enc.PutNull(tag)
}

// CommandFieldsWriter is an [im.CommandFieldsWriter] for a response that
// is a [spec.Encodable]; any other response encodes as an empty structure,
// as the bridge encodes a response it does not know.
func CommandFieldsWriter(enc *tlv.Encoder, tag tlv.Tag, fields any) {
	if EncodeApplicationResponse(enc, tag, fields) {
		return
	}
	enc.StartStruct(tag)
	_ = enc.EndContainer()
}

// CommandFieldsReader is an [im.CommandFieldsReader]: the request decoder
// of the command's generated definition ([spec.DecodeRequest]), else the
// generic context-tag map a server without a typed decoder takes
// ([DecodeGenericTagMap]).
func CommandFieldsReader(path im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
	if fields, ok, err := spec.DecodeRequest(path.Cluster, path.Command, dec, el); ok {
		return fields, err
	}
	return DecodeGenericTagMap(dec)
}

// AttributeValueReader is an [im.AttributeValueReader]: a written value
// decoded by the attribute's generated definition ([spec.DecodeAttribute]),
// a container read whole first; a cluster or attribute without a
// definition keeps the raw [spec.Node], which a server without a typed
// decoder refuses.
func AttributeValueReader(path im.ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, error) {
	n := spec.Node{El: el}
	if el.IsContainer {
		var err error
		if n, err = spec.ReadContainer(dec, el); err != nil {
			return im.AttributeValue{}, err
		}
	}
	if el.IsNull {
		return im.AttributeValue{IsNull: true}, nil
	}
	v, err := spec.DecodeAttribute(spec.Lookup(path.Cluster), path.Attribute, n)
	if err != nil {
		if errors.Is(err, spec.ErrUntyped) {
			return im.AttributeValue{Value: n}, nil
		}
		return im.AttributeValue{}, err
	}
	return im.AttributeValue{Value: v}, nil
}

// ResponseCommandOf sets ent's command id from a response that names its
// own command ([spec.ResponsePayload]): a handler returns the response
// value, the path still carries the request's id.
func ResponseCommandOf(ent *im.InvokeResponseEntry) {
	if ent.IsStatus || ent.Response == nil {
		return
	}
	if x, ok := ent.Response.(spec.ResponsePayload); ok {
		_, ent.Path.Command = x.ResponseCommand()
	}
}

// EncodeApplicationValue encodes v when it is a [spec.Encodable], a list of
// them, a list of strings or a list of scalars, and reports whether it did.
func EncodeApplicationValue(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	switch x := v.(type) {
	case []string:
		spec.PutList(spec.PutString)(enc, tag, x)
	case []OperationalStateStruct:
		OperationalStateList(x).EncodeTLV(enc, tag)
	case []ModeOptionStruct:
		ModeOptionList(x).EncodeTLV(enc, tag)
	case spec.Encodable:
		x.EncodeTLV(enc, tag)
	default:
		return encodeEncodableSlice(enc, tag, v)
	}
	return true
}

// EncodeApplicationResponse encodes v when it is a [spec.Encodable] and
// reports whether it did.
func EncodeApplicationResponse(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	x, ok := v.(spec.Encodable)
	if !ok {
		return false
	}
	x.EncodeTLV(enc, tag)
	return true
}

var encodableType = reflect.TypeFor[spec.Encodable]()

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

func putScalar(enc *tlv.Encoder, tag tlv.Tag, v any) bool {
	switch x := v.(type) {
	case bool:
		enc.PutBool(tag, x)
	case uint8:
		enc.PutUint(tag, uint64(x))
	case uint16:
		enc.PutUint(tag, uint64(x))
	case uint32:
		enc.PutUint(tag, uint64(x))
	case uint64:
		enc.PutUint(tag, x)
	case int8:
		enc.PutInt(tag, int64(x))
	case int16:
		enc.PutInt(tag, int64(x))
	case int32:
		enc.PutInt(tag, int64(x))
	case int64:
		enc.PutInt(tag, x)
	case float32:
		enc.PutFloat32(tag, x)
	case float64:
		enc.PutFloat64(tag, x)
	case string:
		enc.PutUTF8(tag, x)
	case tlv.BoundedString:
		enc.PutUTF8Bounded(tag, x.Value, x.MaxBytes)
	case []byte:
		enc.PutOctets(tag, x)
	default:
		return false
	}
	return true
}

// DecodeGenericTagMap drains the open container into a map keyed by
// context-tag number — the field payload a server without a typed decoder
// receives. Nested containers are drained and dropped; an empty container
// is a nil map.
func DecodeGenericTagMap(dec *tlv.Decoder) (map[uint8]any, error) {
	var out map[uint8]any
	for {
		el, err := dec.Next()
		if err != nil {
			return nil, fmt.Errorf("generic fields: %w", err)
		}
		if el.IsEndContainer {
			return out, nil
		}
		if el.IsContainer {
			if err := DrainContainer(dec); err != nil {
				return nil, fmt.Errorf("drain nested container: %w", err)
			}
			continue
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		if out == nil {
			out = make(map[uint8]any, 4)
		}
		tag := uint8(el.Tag.Number & 0xFF)
		switch {
		case el.Type >= tlv.TypeUnsignedInt1 && el.Type <= tlv.TypeUnsignedInt8:
			out[tag] = el.Uint
		case el.Type >= tlv.TypeSignedInt1 && el.Type <= tlv.TypeSignedInt8:
			out[tag] = el.Int
		case el.Type == tlv.TypeBoolFalse || el.Type == tlv.TypeBoolTrue:
			out[tag] = el.Bool
		case el.Type == tlv.TypeFloat4 || el.Type == tlv.TypeFloat8:
			out[tag] = el.Float
		case el.Type >= tlv.TypeUTF8Str1 && el.Type <= tlv.TypeUTF8Str8:
			out[tag] = el.String
		case el.IsNull:
			out[tag] = nil
		}
		if el.Type >= tlv.TypeOctetStr1 && el.Type <= tlv.TypeOctetStr8 {
			out[tag] = append([]byte(nil), el.Octets...)
		}
	}
}

// DrainContainer reads to the end of the container dec is inside of.
func DrainContainer(dec *tlv.Decoder) error {
	depth := 1
	for depth > 0 {
		el, err := dec.Next()
		if err != nil {
			return err
		}
		if el.IsContainer {
			depth++
		}
		if el.IsEndContainer {
			depth--
		}
	}
	return nil
}

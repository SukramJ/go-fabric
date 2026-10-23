// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spectest

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"
)

// WireValues encodes stored attribute values — a generated
// Patch.AttributeValues, a server's MatterRead — as a device puts them on
// the wire and decodes each back into its node: the [spec.Values] a
// controller holds after a read. A value is encoded by its Go type: nil
// as null, an [spec.Encodable] by its EncodeTLV, a number at the smallest
// width, a string as UTF-8, []byte as an octet string, any other slice as
// an array of its entries.
func WireValues(tb testing.TB, stored map[uint32]any) spec.AttributeValues {
	tb.Helper()
	out := spec.AttributeValues{}
	for id, v := range stored {
		enc := tlv.NewEncoder()
		if err := putStored(enc, tlv.AnonymousTag(), v); err != nil {
			tb.Fatalf("attribute 0x%04X: %v", id, err)
		}
		b, err := enc.Bytes()
		if err != nil {
			tb.Fatalf("attribute 0x%04X: %v", id, err)
		}
		out[id] = Decode(tb, b)
	}
	return out
}

// putStored encodes one stored value by its Go type.
func putStored(enc *tlv.Encoder, tag tlv.Tag, v any) error {
	if v == nil {
		enc.PutNull(tag)
		return nil
	}
	if e, ok := v.(spec.Encodable); ok {
		e.EncodeTLV(enc, tag)
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Bool:
		enc.PutBool(tag, rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		enc.PutInt(tag, rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		enc.PutUint(tag, rv.Uint())
	case reflect.Float32:
		enc.PutFloat32(tag, float32(rv.Float()))
	case reflect.Float64:
		enc.PutFloat64(tag, rv.Float())
	case reflect.String:
		enc.PutUTF8(tag, rv.String())
	case reflect.Pointer:
		if rv.IsNil() {
			enc.PutNull(tag)
			return nil
		}
		return putStored(enc, tag, rv.Elem().Interface())
	case reflect.Slice:
		if rv.Type() == reflect.TypeFor[[]byte]() {
			enc.PutOctets(tag, rv.Bytes())
			return nil
		}
		enc.StartArray(tag)
		for i := range rv.Len() {
			if err := putStored(enc, tlv.AnonymousTag(), rv.Index(i).Interface()); err != nil {
				return err
			}
		}
		return enc.EndContainer()
	default:
		return fmt.Errorf("spectest: no wire form for a stored %T", v)
	}
	return nil
}

// EqualState fails tb unless got and want — two generated States — hold
// the same values: field by field, pointers by what they point to, a nil
// slice equal to an empty one (a list that went over the wire comes back
// empty, not nil).
func EqualState(tb testing.TB, got, want any) {
	tb.Helper()
	if !equalValue(reflect.ValueOf(got), reflect.ValueOf(want)) {
		tb.Errorf("state %+v, want %+v", got, want)
	}
}

// equalValue is reflect.DeepEqual with a nil slice equal to an empty one.
func equalValue(a, b reflect.Value) bool {
	if a.IsValid() != b.IsValid() {
		return false
	}
	if !a.IsValid() {
		return true
	}
	if a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return equalValue(a.Elem(), b.Elem())
	case reflect.Slice:
		if a.Len() != b.Len() {
			return false
		}
		for i := range a.Len() {
			if !equalValue(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for i := range a.NumField() {
			if !equalValue(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if a.Len() != b.Len() {
			return false
		}
		for _, k := range a.MapKeys() {
			if !equalValue(a.MapIndex(k), b.MapIndex(k)) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a.Interface(), b.Interface())
	}
}

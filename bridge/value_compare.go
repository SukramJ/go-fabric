// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"reflect"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// attributeReading is one attribute value held for comparison with a later
// reading of the same path: whether a command or a change notification
// moved the attribute is decided by value, not by the identity of what
// MatterRead returned. A server may answer every read with a freshly
// allocated struct or with nested pointers; an unchanged value read twice
// is still the same value.
//
// Mirrors matter.js #noteIfChanged, which notes a property as changed only
// when the committed value is neither identical to the old one nor
// isDeepEqual to it
// (packages/node/src/behavior/state/managed/Datasource.ts:1461-1466 at
// 862ad75f). isDeepEqual compares content, not identity, so a fresh object
// of equal content is no change
// (packages/general/src/util/DeepEqual.ts:23-80 at 862ad75f). The Go value
// is compared in its wire form, the TLV element the bridge writes for it:
// that is the value a subscriber sees, and it erases what the Go value
// carries beyond it — a pointer's address, a nil slice against an empty
// one — which a JavaScript value does not have.
type attributeReading struct {
	// wire is the value as [defaultAttributeValueWriter] writes it; nil
	// when the encoder failed.
	wire []byte
	// raw is the Go value with its outer pointers followed, compared
	// with [reflect.DeepEqual] when the wire form cannot tell two values
	// apart: the writer encodes a type it does not know as null, like a
	// real null.
	raw any
}

// readingOf captures v for comparison.
func readingOf(v im.AttributeValue) attributeReading {
	r := attributeReading{}
	if !v.IsNull {
		r.raw = derefValue(v.Value)
	}
	enc := tlv.NewEncoder()
	defaultAttributeValueWriter(enc, tlv.AnonymousTag(), v)
	if out, err := enc.Bytes(); err == nil {
		r.wire = out
	}
	return r
}

// equal reports whether a and b are the same attribute value.
func (a attributeReading) equal(b attributeReading) bool {
	if !bytes.Equal(a.wire, b.wire) {
		return false
	}
	if a.wire != nil && !bytes.Equal(a.wire, nullElement) {
		return true
	}
	return reflect.DeepEqual(a.raw, b.raw)
}

// nullElement is an anonymous TLV null, the writer's form of a null value
// and of a value of a type it does not know.
var nullElement = []byte{byte(tlv.TypeNull)}

// derefValue follows pointers so two reads of an unchanged pointer-typed
// value compare alike.
func derefValue(v any) any {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}
	return rv.Interface()
}

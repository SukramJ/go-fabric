// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// The kinds of a request field, and the wrong-typed elements substituted
// for each.
const (
	kindBool  = "bool"
	kindU16   = "uint16"
	kindArray = "array"
)

type requestField struct {
	tag  uint8
	kind string
	// fixed fields are written well-formed but not varied.
	fixed bool
}

// putRequestElement writes a well-formed element of kind under tag:
// false, 5, or an empty array.
func putRequestElement(e *tlv.Encoder, tag tlv.Tag, kind string) {
	switch kind {
	case kindBool:
		e.PutBool(tag, false)
	case kindU16:
		e.PutUint(tag, 5)
	case kindArray:
		e.StartArray(tag)
		_ = e.EndContainer()
	}
}

// wrongElements are the elements of the wrong type for kind, each with a
// name and a writer.
func wrongElements(kind string) map[string]func(*tlv.Encoder, tlv.Tag) {
	scalar := func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 1) }
	structure := func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartStruct(tag)
		_ = e.EndContainer()
	}
	switch kind {
	case kindBool:
		return map[string]func(*tlv.Encoder, tlv.Tag){"an unsigned integer": scalar, "a structure": structure}
	case kindU16:
		return map[string]func(*tlv.Encoder, tlv.Tag){
			"a boolean":           func(e *tlv.Encoder, tag tlv.Tag) { e.PutBool(tag, true) },
			"a value over 16 bit": func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 0x1_0000) },
		}
	default:
		return map[string]func(*tlv.Encoder, tlv.Tag){
			"an unsigned integer": scalar,
			"a structure":         structure,
			"a list":              func(e *tlv.Encoder, tag tlv.Tag) { e.StartList(tag); _ = e.EndContainer() },
		}
	}
}

// encodeRequest writes fields in a structure (or a list when asList),
// substituting wrong for the field with tag subst.
func encodeRequest(t *testing.T, fields []requestField, asList bool, subst uint8, wrong func(*tlv.Encoder, tlv.Tag)) *tlv.Decoder {
	t.Helper()
	e := tlv.NewEncoder()
	if asList {
		e.StartList(tlv.AnonymousTag())
	} else {
		e.StartStruct(tlv.AnonymousTag())
	}
	for _, f := range fields {
		if wrong != nil && f.tag == subst {
			wrong(e, tlv.ContextTag(f.tag))
			continue
		}
		putRequestElement(e, tlv.ContextTag(f.tag), f.kind)
	}
	if err := e.EndContainer(); err != nil {
		t.Fatal(err)
	}
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return tlv.NewDecoder(b)
}

// TestRequestDecodersRefuseWrongTypes is the type-strictness table of the
// request decoders a device runs on what a controller sends — Read,
// Subscribe, Write, Invoke and Timed. The well-formed request decodes; the
// same request as a list instead of a structure is refused; and each
// type-checked field carrying an element of another type — a scalar or a
// structure for an array, an integer for a boolean, a boolean or a
// 17-bit value for a 16-bit integer — is refused with the decoder's
// sentinel, never read as a zero value; a profile-tagged member numbered
// like a field is not that field (R2-P5-tmut-13). The fields the
// decoders deliberately tolerate (EventFilters of the wrong type is
// skipped) are not in the table.
func TestRequestDecodersRefuseWrongTypes(t *testing.T) {
	t.Parallel()
	msgs := []struct {
		name     string
		sentinel error
		fields   []requestField
		decode   func(*tlv.Decoder) error
	}{
		{
			name: "ReadRequest", sentinel: ErrInvalidReadRequest,
			fields: []requestField{
				{tagReadReqAttributeRequests, kindArray, false},
				{tagReadReqEventRequests, kindArray, false},
				{tagReadReqFabricFiltered, kindBool, false},
				{tagReadReqDataVersionFilters, kindArray, false},
			},
			decode: func(d *tlv.Decoder) error { _, err := UnmarshalReadRequestTLV(d); return err },
		},
		{
			name: "SubscribeRequest", sentinel: ErrInvalidSubscribeRequest,
			fields: []requestField{
				{tagSubReqKeepSubscriptions, kindBool, false},
				{tagSubReqMinIntervalFloor, kindU16, false},
				{tagSubReqMaxIntervalCeiling, kindU16, false},
				{tagSubReqAttributeRequests, kindArray, false},
				{tagSubReqEventRequests, kindArray, false},
				{tagSubReqFabricFiltered, kindBool, false},
				{tagSubReqDataVersionFilters, kindArray, false},
			},
			decode: func(d *tlv.Decoder) error { _, err := UnmarshalSubscribeRequestTLV(d); return err },
		},
		{
			name: "WriteRequest", sentinel: ErrInvalidWriteRequest,
			fields: []requestField{
				{tagWriteReqSuppressResponse, kindBool, false}, {tagWriteReqTimedRequest, kindBool, true}, {tagWriteReqWriteRequests, kindArray, false},
			},
			decode: func(d *tlv.Decoder) error { _, err := UnmarshalWriteRequestTLV(d, nil); return err },
		},
		{
			name: "InvokeRequest", sentinel: ErrInvalidInvokeRequest,
			fields: []requestField{
				{tagInvokeReqSuppressResponse, kindBool, false}, {tagInvokeReqTimedRequest, kindBool, true}, {tagInvokeReqInvokeRequests, kindArray, false},
			},
			decode: func(d *tlv.Decoder) error { _, err := UnmarshalInvokeRequestTLV(d, nil); return err },
		},
		{
			name: "TimedRequest", sentinel: ErrInvalidTimedRequest,
			fields: []requestField{{tagTimedReqTimeout, kindU16, false}},
			decode: func(d *tlv.Decoder) error { _, err := UnmarshalTimedRequestTLV(d); return err },
		},
	}
	for _, m := range msgs {
		if err := m.decode(encodeRequest(t, m.fields, false, 0, nil)); err != nil {
			t.Fatalf("%s: the well-formed request is refused: %v", m.name, err)
		}
		if err := m.decode(encodeRequest(t, m.fields, true, 0, nil)); !errors.Is(err, m.sentinel) {
			t.Errorf("%s as a list: err = %v, want %v", m.name, err, m.sentinel)
		}
		// A member without a context tag refuses the structure, as
		// matter.js TlvObject (TlvObject.ts:212-213 at b25ed960; review
		// II, R2-P6-wire-0504) — it is never read as the field of its
		// number.
		if err := m.decode(encodeWithProfileShadows(t, m.fields)); !errors.Is(err, m.sentinel) || !errors.Is(err, errNonContextMember) {
			t.Errorf("%s with profile-tagged members numbered like its fields: err = %v, want %v (a non-context member)", m.name, err, m.sentinel)
		}
		for _, f := range m.fields {
			if f.fixed {
				continue
			}
			for what, wrong := range wrongElements(f.kind) {
				err := m.decode(encodeRequest(t, m.fields, false, f.tag, wrong))
				if !errors.Is(err, m.sentinel) {
					t.Errorf("%s: tag %d (%s) carrying %s: err = %v, want %v", m.name, f.tag, f.kind, what, err, m.sentinel)
				}
			}
		}
	}
}

// encodeWithProfileShadows writes the well-formed fields, then for each a
// common-profile-tagged member with the same number and a type the field
// would refuse. A decoder that reads the member as the field fails.
func encodeWithProfileShadows(t *testing.T, fields []requestField) *tlv.Decoder {
	t.Helper()
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	for _, f := range fields {
		putRequestElement(e, tlv.ContextTag(f.tag), f.kind)
	}
	for _, f := range fields {
		if f.kind == kindBool {
			e.PutUint(tlv.CommonTag(uint32(f.tag)), 1)
		} else {
			e.PutBool(tlv.CommonTag(uint32(f.tag)), true)
		}
	}
	if err := e.EndContainer(); err != nil {
		t.Fatal(err)
	}
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return tlv.NewDecoder(b)
}

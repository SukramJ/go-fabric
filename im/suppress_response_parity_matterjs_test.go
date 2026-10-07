// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

func encodeStruct(t *testing.T, body func(enc *tlv.Encoder)) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	body(enc)
	_ = enc.EndContainer()
	out, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestParityMatterJS_SuppressResponseOf mirrors matter.js
// InteractionMessenger.ts suppressResponseOf (4bf21e80, #4570): only field
// 0, as a boolean; the rest may break the request's schema; malformed TLV,
// a non-boolean field 0 or a non-structure reads as not suppressed.
func TestParityMatterJS_SuppressResponseOf(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    bool
	}{
		{"true with a broken rest", encodeStruct(t, func(e *tlv.Encoder) {
			e.PutBool(tlv.ContextTag(0), true)
			e.PutUint(tlv.ContextTag(2), 7)
			e.StartList(tlv.ContextTag(9))
			e.PutUint(tlv.ContextTag(1), 1)
			_ = e.EndContainer()
		}), true},
		{"false", encodeStruct(t, func(e *tlv.Encoder) { e.PutBool(tlv.ContextTag(0), false) }), false},
		{"absent", encodeStruct(t, func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(2), 7) }), false},
		{"field 0 not a boolean", encodeStruct(t, func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(0), 1) }), false},
		{"nested tag 0 does not count", encodeStruct(t, func(e *tlv.Encoder) {
			e.StartStruct(tlv.ContextTag(3))
			e.PutBool(tlv.ContextTag(0), true)
			_ = e.EndContainer()
		}), false},
		{"truncated", encodeStruct(t, func(e *tlv.Encoder) { e.PutBool(tlv.ContextTag(0), true) })[:2], false},
		{"not a structure", []byte{0x09}, false},
		{"empty", nil, false},
	}
	for _, c := range cases {
		if got := SuppressResponseOf(c.payload); got != c.want {
			t.Errorf("%s: SuppressResponseOf = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestParityMatterJS_InvokeRequestDelayReportData mirrors matter.js
// packages/types/src/protocol/messages/TlvInvokeRequest.ts (67be3a83,
// #4569): DelayReportData (tag 3) is always decoded — both uint16 fields
// default to 0 — and a malformed one fails the request: a wrong type with
// InvalidAction, a value outside uint16 with ConstraintError.
func TestParityMatterJS_InvokeRequestDelayReportData(t *testing.T) {
	request := func(drd func(e *tlv.Encoder)) []byte {
		return encodeStruct(t, func(e *tlv.Encoder) {
			e.PutBool(tlv.ContextTag(0), false)
			e.PutBool(tlv.ContextTag(1), false)
			e.StartArray(tlv.ContextTag(2))
			_ = e.EndContainer()
			if drd != nil {
				drd(e)
			}
		})
	}
	req, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(request(nil)), nil)
	if err != nil || req.DelayReportData != nil {
		t.Fatalf("absent: %+v, %v", req.DelayReportData, err)
	}
	req, err = UnmarshalInvokeRequestTLV(tlv.NewDecoder(request(func(e *tlv.Encoder) {
		e.StartStruct(tlv.ContextTag(3))
		e.PutUint(tlv.ContextTag(1), 250)
		_ = e.EndContainer()
	})), nil)
	if err != nil || req.DelayReportData == nil || *req.DelayReportData != (DelayReportData{DelayMinMs: 0, DelayJitterWindowMs: 250}) {
		t.Fatalf("partial: %+v, %v", req.DelayReportData, err)
	}
	for _, c := range []struct {
		name string
		drd  func(e *tlv.Encoder)
		want StatusCode
	}{
		{"not a structure", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(3), 5) }, StatusInvalidAction},
		{"field not an unsigned integer", func(e *tlv.Encoder) {
			e.StartStruct(tlv.ContextTag(3))
			e.PutBool(tlv.ContextTag(0), true)
			_ = e.EndContainer()
		}, StatusInvalidAction},
		{"field above uint16", func(e *tlv.Encoder) {
			e.StartStruct(tlv.ContextTag(3))
			e.PutUint(tlv.ContextTag(0), 0x10000)
			_ = e.EndContainer()
		}, StatusConstraintError},
	} {
		_, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(request(c.drd)), nil)
		sce, ok := errors.AsType[StatusCodeError](err)
		if !ok || sce.MatterStatusCode() != c.want || !errors.Is(err, ErrInvalidInvokeRequest) {
			t.Errorf("%s: error %v, want %v", c.name, err, c.want)
		}
	}
}

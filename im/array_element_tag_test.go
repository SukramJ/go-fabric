// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// tagFirstElement gives the first element of the array at context tag
// arrayTag a context tag of its own: the element's control byte gains the
// one-byte context tag form and the tag byte follows it.
func tagFirstElement(t *testing.T, wire []byte, arrayTag uint8) []byte {
	t.Helper()
	at := bytes.Index(wire, []byte{0x36, arrayTag})
	if at < 0 || at+2 >= len(wire) || wire[at+2]&0xE0 != 0 {
		t.Fatalf("no array at context tag %d with an anonymous first element in % X", arrayTag, wire)
	}
	el := at + 2
	out := append([]byte(nil), wire[:el]...)
	out = append(out, wire[el]|0x20, 0x05)
	return append(out, wire[el+1:]...)
}

// TestArrayElementWithATagIsMalformed is the standing guard of
// F-db021da0-48: every array a message reader walks treats an element that
// carries a tag as it treats an element of the wrong type, since array
// elements are anonymous (matter.js TlvArray.ts:77-78, connectedhomeip
// TLVReader.cpp:873-875). Each case decodes untagged first, so the
// refusal is the tag's alone; the two filter arrays, which skip an element
// of the wrong type, skip the tagged one.
func TestArrayElementWithATagIsMalformed(t *testing.T) {
	t.Parallel()
	attrPath := []ConcreteAttributePath{{Endpoint: 1, Cluster: 6, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}}
	eventPath := []ConcreteEventPath{{Endpoint: 1, Cluster: 0x3B, Event: 1, HasEndpoint: true, HasCluster: true, HasEvent: true}}
	read := func(r ReadRequest) []byte { return encodeWith(r.MarshalTLV) }
	sub := func(r SubscribeRequest) []byte { return encodeWith(r.MarshalTLV) }
	uintWriter := func(enc *tlv.Encoder, tag tlv.Tag, _ AttributeValue) { enc.PutUint(tag, 1) }
	statusIB := StatusIB{Status: StatusSuccess}
	cmdPath := ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: 1}

	type decoded struct {
		n   int // elements decoded from the array under test
		err error
	}
	cases := []struct {
		name     string
		wire     []byte
		arrayTag uint8
		decode   func([]byte) decoded
		skips    bool
	}{
		{"ReadRequest AttributeRequests", read(ReadRequest{AttributeRequests: attrPath, FabricFiltered: true}), tagReadReqAttributeRequests, func(b []byte) decoded {
			r, err := UnmarshalReadRequestTLV(tlv.NewDecoder(b))
			return decoded{len(r.AttributeRequests), err}
		}, false},
		{"ReadRequest EventRequests", read(ReadRequest{EventRequests: eventPath, FabricFiltered: true}), tagReadReqEventRequests, func(b []byte) decoded {
			r, err := UnmarshalReadRequestTLV(tlv.NewDecoder(b))
			return decoded{len(r.EventRequests), err}
		}, false},
		{"ReadRequest EventFilters", read(ReadRequest{EventRequests: eventPath, EventFilters: []EventMinimumNumber{{EventMin: 1}}, FabricFiltered: true}), tagReadReqEventFilters, func(b []byte) decoded {
			r, err := UnmarshalReadRequestTLV(tlv.NewDecoder(b))
			return decoded{len(r.EventFilters), err}
		}, true},
		{"ReadRequest DataVersionFilters", read(ReadRequest{AttributeRequests: attrPath, DataVersionFilters: []DataVersionFilter{{Endpoint: 1, Cluster: 6, DataVersion: 1}}, FabricFiltered: true}), tagReadReqDataVersionFilters, func(b []byte) decoded {
			r, err := UnmarshalReadRequestTLV(tlv.NewDecoder(b))
			return decoded{len(r.DataVersionFilters), err}
		}, true},
		{"SubscribeRequest AttributeRequests", sub(SubscribeRequest{AttributeRequests: attrPath, MaxIntervalCeiling: 10, FabricFiltered: true}), tagSubReqAttributeRequests, func(b []byte) decoded {
			r, err := UnmarshalSubscribeRequestTLV(tlv.NewDecoder(b))
			return decoded{len(r.AttributeRequests), err}
		}, false},
		{"SubscribeRequest EventRequests", sub(SubscribeRequest{EventRequests: eventPath, MaxIntervalCeiling: 10, FabricFiltered: true}), tagSubReqEventRequests, func(b []byte) decoded {
			r, err := UnmarshalSubscribeRequestTLV(tlv.NewDecoder(b))
			return decoded{len(r.EventRequests), err}
		}, false},
		{"WriteRequest WriteRequests", encodeWith(func(enc *tlv.Encoder) {
			WriteRequest{Writes: []AttributeWrite{{Path: attrPath[0]}}}.MarshalTLV(enc, uintWriter)
		}), tagWriteReqWriteRequests, func(b []byte) decoded {
			r, err := UnmarshalWriteRequestTLV(tlv.NewDecoder(b), nil)
			return decoded{len(r.Writes), err}
		}, false},
		{"InvokeRequest InvokeRequests", encodeWith(func(enc *tlv.Encoder) {
			MarshalInvokeRequestTLV(enc, InvokeRequest{Invokes: []CommandInvocation{{Path: cmdPath}}}, func(enc *tlv.Encoder, tag tlv.Tag, _ any) {
				enc.StartStruct(tag)
				_ = enc.EndContainer()
			})
		}), tagInvokeReqInvokeRequests, func(b []byte) decoded {
			r, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(b), nil)
			return decoded{len(r.Invokes), err}
		}, false},
		{"WriteResponse WriteResponses", encodeWith(WriteResponse{Responses: []AttributeStatus{{Path: attrPath[0], Status: statusIB}}}.MarshalTLV), tagWriteRespResponses, func(b []byte) decoded {
			r, err := UnmarshalWriteResponseTLV(tlv.NewDecoder(b))
			return decoded{len(r.Responses), err}
		}, false},
		{"ReportData AttributeReports", encodeWith(func(enc *tlv.Encoder) {
			ReportData{Reports: []AttributeReport{{Path: attrPath[0], Status: statusIB, IsStatus: true}}}.MarshalTLV(enc, uintWriter)
		}), tagReportAttributeReports, func(b []byte) decoded {
			r, err := UnmarshalReportDataTLV(tlv.NewDecoder(b), nil, nil)
			return decoded{len(r.Reports), err}
		}, false},
		{"ReportData EventReports", encodeWith(func(enc *tlv.Encoder) {
			ReportData{EventReports: []EventReport{{Path: eventPath[0], Status: statusIB, IsStatus: true}}}.MarshalTLV(enc, uintWriter)
		}), tagReportEventReports, func(b []byte) decoded {
			r, err := UnmarshalReportDataTLV(tlv.NewDecoder(b), nil, nil)
			return decoded{len(r.EventReports), err}
		}, false},
		{"InvokeResponse InvokeResponses", encodeWith(func(enc *tlv.Encoder) {
			InvokeResponse{Responses: []InvokeResponseEntry{{Path: cmdPath, Status: statusIB, IsStatus: true}}}.MarshalTLV(enc, nil)
		}), tagInvokeRespResponses, func(b []byte) decoded {
			r, err := UnmarshalInvokeResponseTLV(tlv.NewDecoder(b), nil)
			return decoded{len(r.Responses), err}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.decode(tc.wire); got.err != nil || got.n != 1 {
				t.Fatalf("untagged: %d element(s), err %v; want 1 and nil", got.n, got.err)
			}
			got := tc.decode(tagFirstElement(t, tc.wire, tc.arrayTag))
			switch {
			case tc.skips && (got.err != nil || got.n != 0):
				t.Fatalf("tagged: %d element(s), err %v; want the element skipped", got.n, got.err)
			case !tc.skips && got.err == nil:
				t.Fatalf("tagged: decoded %d element(s), want an error", got.n)
			}
		})
	}
}

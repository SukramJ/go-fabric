// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"testing"

	"thingfabric.dev/go-fabric/tlv"
)

// TestRequestEncodingOmitFalseFlags: with OmitFalseFlags the client
// encoders leave out a SuppressResponse and a TimedRequest that are false
// and keep a true one; MoreChunkedMessages stays on the wire. The zero
// RequestEncoding writes both flags, as MarshalInvokeRequestTLV and
// WriteRequest.MarshalTLV do. Each encoding decodes back to the same flags.
func TestRequestEncodingOmitFalseFlags(t *testing.T) {
	t.Parallel()
	fields := func(enc *tlv.Encoder, tag tlv.Tag, _ any) {
		enc.StartStruct(tag)
		_ = enc.EndContainer()
	}
	values := func(enc *tlv.Encoder, tag tlv.Tag, _ AttributeValue) { enc.PutBool(tag, true) }
	tags := func(t *testing.T, b []byte) map[uint64]bool {
		t.Helper()
		dec := tlv.NewDecoder(b)
		if _, err := dec.Next(); err != nil {
			t.Fatal(err)
		}
		got := map[uint64]bool{}
		for depth := 0; ; {
			el, err := dec.Next()
			if err != nil {
				t.Fatal(err)
			}
			if el.IsEndContainer {
				if depth == 0 {
					return got
				}
				depth--
				continue
			}
			if depth == 0 {
				got[uint64(el.Tag.Number)] = true
			}
			if el.IsContainer {
				depth++
			}
		}
	}
	inv := InvokeRequest{Invokes: []CommandInvocation{{Path: ConcreteCommandPath{Cluster: 6, Command: 1, HasEndpoint: true, HasCluster: true, HasCommand: true}}}}
	wr := WriteRequest{Writes: []AttributeWrite{{Path: ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: 0x4001, HasEndpoint: true, HasCluster: true, HasAttribute: true}}}}
	for _, tc := range []struct {
		name                     string
		omit, suppress, timed    bool
		wantSuppress, wantTimed  bool
		wantWriteSup, wantWriteT bool
	}{
		{"zero encoding writes both", false, false, false, true, true, false, true},
		{"omit drops false flags", true, false, false, false, false, false, false},
		{"omit keeps true flags", true, true, true, true, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := inv
			r.SuppressResponse, r.TimedRequest = tc.suppress, tc.timed
			enc := tlv.NewEncoder()
			MarshalInvokeRequestTLVWith(enc, r, fields, RequestEncoding{OmitFalseFlags: tc.omit})
			b, err := enc.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			got := tags(t, b)
			if got[uint64(tagInvokeReqSuppressResponse)] != tc.wantSuppress || got[uint64(tagInvokeReqTimedRequest)] != tc.wantTimed || !got[uint64(tagInvokeReqInvokeRequests)] {
				t.Fatalf("invoke tags %v, want SuppressResponse %v, TimedRequest %v", got, tc.wantSuppress, tc.wantTimed)
			}
			back, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(b), nil)
			if err != nil || back.SuppressResponse != tc.suppress || back.TimedRequest != tc.timed {
				t.Fatalf("invoke decodes to %+v, %v", back, err)
			}

			w := wr
			w.SuppressResponse, w.TimedRequest = tc.suppress, tc.timed
			enc = tlv.NewEncoder()
			w.MarshalTLVWith(enc, values, RequestEncoding{OmitFalseFlags: tc.omit})
			if b, err = enc.Bytes(); err != nil {
				t.Fatal(err)
			}
			got = tags(t, b)
			if got[uint64(tagWriteReqSuppressResponse)] != tc.wantWriteSup || got[uint64(tagWriteReqTimedRequest)] != tc.wantWriteT || !got[uint64(tagWriteReqMoreChunked)] {
				t.Fatalf("write tags %v, want SuppressResponse %v, TimedRequest %v, MoreChunkedMessages kept", got, tc.wantWriteSup, tc.wantWriteT)
			}
			wb, err := UnmarshalWriteRequestTLV(tlv.NewDecoder(b), nil)
			if err != nil || wb.SuppressResponse != tc.suppress || wb.TimedRequest != tc.timed {
				t.Fatalf("write decodes to %+v, %v", wb, err)
			}
		})
	}
}

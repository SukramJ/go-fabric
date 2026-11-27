// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"encoding/hex"
	"testing"

	"thingfabric.dev/go-fabric/tlv"
)

// TestTlvInvokeRequestParityMatterJS mirrors
// packages/types/test/protocol/TlvInvokeRequestTest.ts:37-95 at db91886c
// on the decode side: a group invoke as the CHIP SDK sends it, without
// SuppressResponse and TimedRequest (:44-53), decodes with both flags false
// and its one command; an InvokeRequest's DelayReportData in context tag 3
// decodes with its two uint16 fields, a request without it decodes to none,
// a missing field reads as 0, and a DelayReportData that is not a structure
// refuses the request.
func TestTlvInvokeRequestParityMatterJS(t *testing.T) {
	t.Parallel()
	skip := func(_ ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		if el.IsContainer {
			return nil, skipContainer(dec)
		}
		return nil, nil
	}
	decode := func(t *testing.T, h string) (InvokeRequest, error) {
		t.Helper()
		raw, err := hex.DecodeString(h)
		if err != nil {
			t.Fatal(err)
		}
		return UnmarshalInvokeRequestTLV(tlv.NewDecoder(raw), skip)
	}
	// suppressResponse false, timedRequest false, one command (endpoint 0,
	// cluster 0x30, command 0), then the field under test and the
	// InteractionModelRevision 12.
	const head = "1528002801" + "3602" + "15" + "3700" + "240000" + "240130" + "240200" + "18" + "18" + "18"
	const tail = "24ff0c18"
	t.Run("decodes a group request that leaves out SuppressResponse and TimedRequest", func(t *testing.T) {
		t.Parallel()
		// An AddGroup group invoke, InteractionModelRevision 14, starting
		// directly with InvokeRequests (TlvInvokeRequestTest.ts:45-48).
		req, err := decode(t, "153602153700240104240200183501250001012c010018181824ff0e18")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.SuppressResponse || req.TimedRequest || len(req.Invokes) != 1 || req.InteractionModelRevision != 14 {
			t.Fatalf("request = %+v, want both flags false, one invoke, revision 14", req)
		}
	})
	t.Run("round-trips DelayReportData in context tag 3", func(t *testing.T) {
		t.Parallel()
		req, err := decode(t, head+"35032500e8032501e80318"+tail)
		if err != nil || req.DelayReportData == nil || *req.DelayReportData != (DelayReportData{DelayMinMs: 1000, DelayJitterWindowMs: 1000}) {
			t.Fatalf("DelayReportData = %+v, %v; want 1000/1000", req.DelayReportData, err)
		}
	})
	t.Run("decodes a request without DelayReportData", func(t *testing.T) {
		t.Parallel()
		req, err := decode(t, head+tail)
		if err != nil || req.DelayReportData != nil || len(req.Invokes) != 1 {
			t.Fatalf("request = %+v, %v; want one invoke and no DelayReportData", req, err)
		}
	})
	t.Run("decodes a missing DelayReportData field as 0", func(t *testing.T) {
		t.Parallel()
		for in, want := range map[string]DelayReportData{
			"35032400c818": {DelayMinMs: 200},
			"350318":       {},
		} {
			req, err := decode(t, head+in+tail)
			if err != nil || req.DelayReportData == nil || *req.DelayReportData != want {
				t.Errorf("%s: DelayReportData = %+v, %v; want %+v", in, req.DelayReportData, err, want)
			}
		}
	})
	t.Run("rejects DelayReportData that is not a structure", func(t *testing.T) {
		t.Parallel()
		if _, err := decode(t, head+"2403c8"+tail); err == nil {
			t.Fatal("a DelayReportData of uint 200 was accepted")
		}
	})
}

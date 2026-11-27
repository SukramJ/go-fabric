// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/tlv"
)

// TestInvokeClientRequestParityMatterJS: the client's InvokeRequest encoder
// writes matter.js's TlvInvokeRequest bytes for every InvokeRequest fixture
// (commandFields left out, as the fixtures carry none).
func TestInvokeClientRequestParityMatterJS(t *testing.T) {
	t.Parallel()
	n := 0
	for _, rec := range loadFixtures(t) {
		if rec.Type != "InvokeRequest" {
			continue
		}
		got, ok := encodeClientRequest(t, rec)
		if !ok {
			t.Fatalf("%s: no client encoder for InvokeRequest", rec.Label)
		}
		want, _ := hex.DecodeString(rec.BytesHex)
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: %s", rec.Label, bytesDiff(got, want))
		}
		n++
	}
	if n == 0 {
		t.Fatal("no InvokeRequest fixture")
	}
}

// TestInvokeClientResponseRoundTrip: the client decodes what the server's
// InvokeResponse encoder writes — matter.js's fixtures for the empty
// responses, the command-data and command-status branches with a
// cluster status and command refs.
func TestInvokeClientResponseRoundTrip(t *testing.T) {
	t.Parallel()
	for _, rec := range loadFixtures(t) {
		if rec.Type != "InvokeResponse" {
			continue
		}
		b, _ := hex.DecodeString(rec.BytesHex)
		resp, err := UnmarshalInvokeResponseTLV(tlv.NewDecoder(b), nil)
		if err != nil || len(resp.Responses) != 0 {
			t.Fatalf("%s: %+v %v", rec.Label, resp, err)
		}
	}

	path := ConcreteCommandPath{Endpoint: 0, Cluster: 0x29, Command: 1, HasEndpoint: true, HasCluster: true, HasCommand: true}
	in := InvokeResponse{Responses: []InvokeResponseEntry{
		{Path: path, Response: uint64(7), HasResponse: true, CommandRef: 1, HasCommandRef: true},
		{Path: path, IsStatus: true, Status: StatusIB{Status: StatusFailure, ClusterStatus: 3, HasClusterStatus: true}, CommandRef: 2, HasCommandRef: true},
	}, MoreChunkedMessages: true}
	enc := tlv.NewEncoder()
	in.MarshalTLV(enc, func(enc *tlv.Encoder, tag tlv.Tag, v any) {
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), v.(uint64)) //nolint:forcetypeassert // test value
		_ = enc.EndContainer()
	})
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var seen []uint64
	out, err := UnmarshalInvokeResponseTLV(tlv.NewDecoder(b), func(_ ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		if !el.IsContainer {
			return nil, nil
		}
		for {
			f, err := dec.Next()
			if err != nil || f.IsEndContainer {
				return seen, err
			}
			seen = append(seen, f.Uint)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.MoreChunkedMessages || len(out.Responses) != 2 {
		t.Fatalf("%+v", out)
	}
	data, status := out.Responses[0], out.Responses[1]
	if data.IsStatus || !data.HasResponse || data.Path != path || data.CommandRef != 1 || len(seen) != 1 || seen[0] != 7 {
		t.Fatalf("command data %+v", data)
	}
	if !status.IsStatus || status.Status.Status != StatusFailure || status.Status.ClusterStatus != 3 || !status.Status.HasClusterStatus || status.CommandRef != 2 || status.Path != path {
		t.Fatalf("command status %+v", status)
	}
}

// TestInvokeClientRequestOptionsParityMatterJS: the client's InvokeRequest
// carries a DelayReportData under tag 3, both its fields written, ahead of
// the InteractionModelRevision, and the revision the request names — the
// field order of matter.js TlvInvokeRequest
// (packages/types/src/protocol/messages/TlvInvokeRequest.ts:17-31 at
// b25ed960) and TlvDelayReportData's two TlvFields. A request without a
// revision carries [MatterInteractionModelRevision]; the decoder reads
// back what the encoder wrote.
func TestInvokeClientRequestOptionsParityMatterJS(t *testing.T) {
	t.Parallel()
	encode := func(req InvokeRequest) []byte {
		enc := tlv.NewEncoder()
		MarshalInvokeRequestTLV(enc, req, func(*tlv.Encoder, tlv.Tag, any) {})
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	want := func(drd *DelayReportData, revision uint8) []byte {
		return encodeStruct(t, func(e *tlv.Encoder) {
			e.PutBool(tlv.ContextTag(0), false)
			e.PutBool(tlv.ContextTag(1), false)
			e.StartArray(tlv.ContextTag(2))
			_ = e.EndContainer()
			if drd != nil {
				e.StartStruct(tlv.ContextTag(3))
				e.PutUint(tlv.ContextTag(0), uint64(drd.DelayMinMs))
				e.PutUint(tlv.ContextTag(1), uint64(drd.DelayJitterWindowMs))
				_ = e.EndContainer()
			}
			e.PutUint(tlv.ContextTag(0xFF), uint64(revision))
		})
	}
	for _, c := range []struct {
		name     string
		req      InvokeRequest
		drd      *DelayReportData
		revision uint8
	}{
		{"defaults", InvokeRequest{}, nil, MatterInteractionModelRevision},
		{"zero DelayReportData", InvokeRequest{DelayReportData: &DelayReportData{}}, &DelayReportData{}, MatterInteractionModelRevision},
		{
			"DelayReportData and revision",
			InvokeRequest{DelayReportData: &DelayReportData{DelayMinMs: 100, DelayJitterWindowMs: 250}, InteractionModelRevision: 11},
			&DelayReportData{DelayMinMs: 100, DelayJitterWindowMs: 250}, 11,
		},
	} {
		got := encode(c.req)
		if w := want(c.drd, c.revision); !bytes.Equal(got, w) {
			t.Fatalf("%s: %s", c.name, bytesDiff(got, w))
		}
		back, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(got), nil)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if back.InteractionModelRevision != c.revision || (c.drd == nil) != (back.DelayReportData == nil) || (c.drd != nil && *back.DelayReportData != *c.drd) {
			t.Fatalf("%s: decoded %+v", c.name, back)
		}
	}
	// matter.js's literals: :55 "round-trips DelayReportData in context tag
	// 3" (35032500e8032501e80318 for {1000, 1000}) and :38 "encodes
	// DelayReportDataIB with context tags 0 and 1" (152400c82501f40118 for
	// {200, 500}, here inside tag 3, so with its control byte 0x35 0x03 in
	// place of the anonymous 0x15) — TlvInvokeRequestTest.ts at db91886c.
	for _, c := range []struct {
		drd DelayReportData
		hex string
	}{
		{DelayReportData{DelayMinMs: 1000, DelayJitterWindowMs: 1000}, "35032500e8032501e80318"},
		{DelayReportData{DelayMinMs: 200, DelayJitterWindowMs: 500}, "35032400c82501f40118"},
	} {
		got := hex.EncodeToString(encode(InvokeRequest{DelayReportData: &c.drd}))
		if !strings.Contains(got, c.hex) {
			t.Fatalf("%+v: %s does not contain matter.js's %s", c.drd, got, c.hex)
		}
	}
}

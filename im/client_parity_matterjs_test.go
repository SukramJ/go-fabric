// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// Parity of the client encoders and decoders (ADR 0020, stage 3) with
// matter.js at 0ceb202d: the ReadRequest, SubscribeRequest, WriteRequest
// and TimedRequest fixtures of testdata/im-wire-fixtures.json are encoded
// with this package's MarshalTLV and compared byte for byte; the
// WriteResponse, SubscribeResponse and ReportData fixtures are decoded
// with the client decoders and compared field for field. Generator:
// notes/parity/matter/generate-im-wire-fixtures.ts.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// clientPathJSON is an AttributePathIB / EventPathIB / ClusterPathIB of
// a fixture; ListIndex is raw so null and absent differ.
type clientPathJSON struct {
	NodeID      *uint64         `json:"nodeId"`
	EndpointID  *uint16         `json:"endpointId"`
	ClusterID   *uint32         `json:"clusterId"`
	AttributeID *uint32         `json:"attributeId"`
	EventID     *uint32         `json:"eventId"`
	ListIndex   json.RawMessage `json:"listIndex"`
}

func (p clientPathJSON) attribute() ConcreteAttributePath {
	var out ConcreteAttributePath
	if p.NodeID != nil {
		out.Node, out.HasNode = *p.NodeID, true
	}
	if p.EndpointID != nil {
		out.Endpoint, out.HasEndpoint = *p.EndpointID, true
	}
	if p.ClusterID != nil {
		out.Cluster, out.HasCluster = *p.ClusterID, true
	}
	if p.AttributeID != nil {
		out.Attribute, out.HasAttribute = *p.AttributeID, true
	}
	switch s := string(p.ListIndex); {
	case s == "null":
		out.HasListIndex, out.ListAppend = true, true
	case s != "":
		var i uint16
		_ = json.Unmarshal(p.ListIndex, &i)
		out.ListIndex, out.HasListIndex = i, true
	}
	return out
}

func (p clientPathJSON) event() ConcreteEventPath {
	var out ConcreteEventPath
	if p.NodeID != nil {
		out.Node, out.HasNode = *p.NodeID, true
	}
	if p.EndpointID != nil {
		out.Endpoint, out.HasEndpoint = *p.EndpointID, true
	}
	if p.ClusterID != nil {
		out.Cluster, out.HasCluster = *p.ClusterID, true
	}
	if p.EventID != nil {
		out.Event, out.HasEvent = *p.EventID, true
	}
	return out
}

type readingRequestJSON struct {
	KeepSubscriptions  bool             `json:"keepSubscriptions"`
	MinIntervalFloor   uint16           `json:"minIntervalFloorSeconds"`
	MaxIntervalCeiling uint16           `json:"maxIntervalCeilingSeconds"`
	AttributeRequests  []clientPathJSON `json:"attributeRequests"`
	EventRequests      []clientPathJSON `json:"eventRequests"`
	EventFilters       []struct {
		NodeID   *uint64 `json:"nodeId"`
		EventMin uint64  `json:"eventMin"`
	} `json:"eventFilters"`
	IsFabricFiltered   bool `json:"isFabricFiltered"`
	DataVersionFilters []struct {
		Path        clientPathJSON `json:"path"`
		DataVersion uint32         `json:"dataVersion"`
	} `json:"dataVersionFilters"`
}

func (f readingRequestJSON) parts() (attrs []ConcreteAttributePath, events []ConcreteEventPath, filters []EventMinimumNumber, dvf []DataVersionFilter) {
	for _, p := range f.AttributeRequests {
		attrs = append(attrs, p.attribute())
	}
	for _, p := range f.EventRequests {
		events = append(events, p.event())
	}
	for _, e := range f.EventFilters {
		m := EventMinimumNumber{EventMin: e.EventMin}
		if e.NodeID != nil {
			m.NodeID, m.HasNodeID = *e.NodeID, true
		}
		filters = append(filters, m)
	}
	for _, d := range f.DataVersionFilters {
		dvf = append(dvf, DataVersionFilter{Endpoint: *d.Path.EndpointID, Cluster: *d.Path.ClusterID, DataVersion: d.DataVersion})
	}
	return attrs, events, filters, dvf
}

// tlvStreamJSON is matter.js's JSON form of a TlvStream element.
type tlvStreamJSON struct {
	TypeLength struct {
		Type  uint8           `json:"type"`
		Value json.RawMessage `json:"value"` // a Boolean's value
	} `json:"typeLength"`
	Value json.RawMessage `json:"value"`
}

// writeTLVStream replays a TlvStream (matter.js TlvType values:
// UnsignedInt 0x04, Boolean 0x08, Utf8String 0x0C, Null 0x14, Structure
// 0x15, Array 0x16, List 0x17, EndOfContainer 0x18) at tag.
func writeTLVStream(t *testing.T, enc *tlv.Encoder, tag tlv.Tag, stream []tlvStreamJSON) {
	t.Helper()
	for i, el := range stream {
		elTag := tlv.AnonymousTag()
		if i == 0 {
			elTag = tag
		}
		switch el.TypeLength.Type {
		case 0x04:
			var v uint64
			_ = json.Unmarshal(el.Value, &v)
			enc.PutUint(elTag, v)
		case 0x08:
			var v bool
			_ = json.Unmarshal(el.TypeLength.Value, &v)
			enc.PutBool(elTag, v)
		case 0x0C:
			var v string
			_ = json.Unmarshal(el.Value, &v)
			enc.PutUTF8(elTag, v)
		case 0x14:
			enc.PutNull(elTag)
		case 0x15:
			enc.StartStruct(elTag)
		case 0x16:
			enc.StartArray(elTag)
		case 0x17:
			enc.StartList(elTag)
		case 0x18:
			_ = enc.EndContainer()
		default:
			t.Fatalf("TlvStream type 0x%02X not replayed by the test", el.TypeLength.Type)
		}
	}
}

type writeRequestJSON struct {
	SuppressResponse    bool `json:"suppressResponse"`
	TimedRequest        bool `json:"timedRequest"`
	MoreChunkedMessages bool `json:"moreChunkedMessages"`
	WriteRequests       []struct {
		DataVersion *uint32         `json:"dataVersion"`
		Path        clientPathJSON  `json:"path"`
		Data        []tlvStreamJSON `json:"data"`
	} `json:"writeRequests"`
}

// encodeClientRequest encodes a request fixture with the client encoder;
// ok is false for a type that is not a client request.
func encodeClientRequest(t *testing.T, rec fixtureRecord) (got []byte, ok bool) {
	t.Helper()
	enc := tlv.NewEncoder()
	switch rec.Type {
	case "ReadRequest":
		var f readingRequestJSON
		if err := json.Unmarshal(rec.Fixture, &f); err != nil {
			t.Fatal(err)
		}
		attrs, events, filters, dvf := f.parts()
		ReadRequest{AttributeRequests: attrs, EventRequests: events, EventFilters: filters, FabricFiltered: f.IsFabricFiltered, DataVersionFilters: dvf}.MarshalTLV(enc)
	case "SubscribeRequest":
		var f readingRequestJSON
		if err := json.Unmarshal(rec.Fixture, &f); err != nil {
			t.Fatal(err)
		}
		attrs, events, filters, dvf := f.parts()
		SubscribeRequest{
			KeepSubscriptions: f.KeepSubscriptions, MinIntervalFloor: f.MinIntervalFloor, MaxIntervalCeiling: f.MaxIntervalCeiling,
			AttributeRequests: attrs, EventRequests: events, EventFilters: filters, FabricFiltered: f.IsFabricFiltered, DataVersionFilters: dvf,
		}.MarshalTLV(enc)
	case "WriteRequest":
		var f writeRequestJSON
		if err := json.Unmarshal(rec.Fixture, &f); err != nil {
			t.Fatal(err)
		}
		w := WriteRequest{SuppressResponse: f.SuppressResponse, TimedRequest: f.TimedRequest, MoreChunkedMessages: f.MoreChunkedMessages}
		for _, wr := range f.WriteRequests {
			a := AttributeWrite{Path: wr.Path.attribute(), Value: AttributeValue{Value: wr.Data}}
			if wr.DataVersion != nil {
				a.DataVersion, a.HasDataVersion = *wr.DataVersion, true
			}
			w.Writes = append(w.Writes, a)
		}
		w.MarshalTLV(enc, func(e *tlv.Encoder, tag tlv.Tag, v AttributeValue) {
			writeTLVStream(t, e, tag, v.Value.([]tlvStreamJSON))
		})
	case "TimedRequest":
		var f struct {
			Timeout uint16 `json:"timeout"`
		}
		if err := json.Unmarshal(rec.Fixture, &f); err != nil {
			t.Fatal(err)
		}
		TimedRequest{TimeoutMs: f.Timeout}.MarshalTLV(enc)
	default:
		return nil, false
	}
	b, err := enc.Bytes()
	if err != nil {
		t.Fatalf("%s: %v", rec.Label, err)
	}
	return b, true
}

// TestParityClientRequestFixtures: every ReadRequest, SubscribeRequest,
// WriteRequest and TimedRequest fixture is the client encoder's output
// byte for byte — the optional lists omitted when empty, the
// InteractionModelRevision stamped (TlvReadRequest.ts:19-35,
// TlvSubscribeRequest.ts:19-44, TlvWriteRequest.ts:16-27 with Write.ts:77,
// TlvTimedRequest.ts:13-17). None of them is a by-design divergence any
// more.
func TestParityClientRequestFixtures(t *testing.T) {
	t.Parallel()
	seen := map[string]int{}
	for _, rec := range loadFixtures(t) {
		got, ok := encodeClientRequest(t, rec)
		if !ok {
			continue
		}
		seen[rec.Type]++
		if rec.ByDesignDivergence {
			t.Errorf("%s: still marked as a by-design divergence", rec.Label)
		}
		want, _ := hex.DecodeString(rec.BytesHex)
		if !bytes.Equal(got, want) {
			t.Errorf("%s: %s", rec.Label, bytesDiff(got, want))
		}
	}
	for _, typ := range []string{"ReadRequest", "SubscribeRequest", "WriteRequest", "TimedRequest"} {
		if seen[typ] < 3 {
			t.Errorf("%d %s fixtures, want at least 3", seen[typ], typ)
		}
	}
}

// TestParityClientDecoderFixtures: the WriteResponse, SubscribeResponse
// and ReportData fixtures decode with the client decoders into the values
// the fixture states (TlvWriteResponse.ts, TlvSubscribeResponse.ts,
// TlvDataReport.ts).
func TestParityClientDecoderFixtures(t *testing.T) {
	t.Parallel()
	for _, rec := range loadFixtures(t) {
		raw, _ := hex.DecodeString(rec.BytesHex)
		switch rec.Type {
		case "WriteResponse":
			var f writeRespFixture
			_ = json.Unmarshal(rec.Fixture, &f)
			wr, err := UnmarshalWriteResponseTLV(tlv.NewDecoder(raw))
			if err != nil || len(wr.Responses) != len(f.WriteResponses) {
				t.Errorf("%s: %+v, %v", rec.Label, wr, err)
				continue
			}
			for i, e := range f.WriteResponses {
				got := wr.Responses[i]
				if got.Path.Attribute != *e.Path.AttributeID || got.Path.Endpoint != *e.Path.EndpointID || uint8(got.Status.Status) != e.Status.Status {
					t.Errorf("%s[%d]: %+v", rec.Label, i, got)
				}
			}
		case "SubscribeResponse":
			var f subscribeRespFixture
			_ = json.Unmarshal(rec.Fixture, &f)
			sr, err := UnmarshalSubscribeResponseTLV(tlv.NewDecoder(raw))
			if err != nil || sr.SubscriptionID != f.SubscriptionID || sr.MaxInterval != f.MaxInterval {
				t.Errorf("%s: %+v, %v", rec.Label, sr, err)
			}
		case "ReportData":
			var f rdFixture
			_ = json.Unmarshal(rec.Fixture, &f)
			rd, err := UnmarshalReportDataTLV(tlv.NewDecoder(raw), nil, nil)
			if err != nil || rd.MoreChunkedMessages != f.MoreChunkedMessages || rd.SuppressResponse != f.SuppressResponse ||
				rd.HasSubscription != (f.SubscriptionID != nil) || (f.SubscriptionID != nil && rd.SubscriptionID != *f.SubscriptionID) {
				t.Errorf("%s: %+v, %v", rec.Label, rd, err)
			}
		}
	}
}

// TestClientReportDataRoundTrip: a ReportData with attribute data and
// status, chunked-list entries, tag compression and an event with each
// optional field decodes back through UnmarshalReportDataTLV, values
// through the readers.
func TestClientReportDataRoundTrip(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(0), 0x1234)
	enc.StartArray(tlv.ContextTag(1))
	// data, version 7, value "x"
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartStruct(tlv.ContextTag(1))
	enc.PutUint(tlv.ContextTag(0), 7)
	ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}.MarshalTLV(enc, tlv.ContextTag(1))
	enc.PutUTF8(tlv.ContextTag(2), "x")
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	// data with tag compression and a null list index, no version
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartStruct(tlv.ContextTag(1))
	enc.StartList(tlv.ContextTag(1))
	enc.PutBool(tlv.ContextTag(0), true)
	enc.PutNull(tlv.ContextTag(5))
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(2), 9)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	// status with a cluster status
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartStruct(tlv.ContextTag(0))
	ConcreteAttributePath{Endpoint: 2, Cluster: 6, Attribute: 1, HasEndpoint: true, HasCluster: true, HasAttribute: true}.MarshalTLV(enc, tlv.ContextTag(0))
	StatusIB{Status: StatusFailure, ClusterStatus: 3, HasClusterStatus: true}.MarshalTLV(enc, tlv.ContextTag(1))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.StartArray(tlv.ContextTag(2))
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartStruct(tlv.ContextTag(1))
	ConcreteEventPath{Endpoint: 1, Cluster: 0x3B, Event: 1, HasEndpoint: true, HasCluster: true, HasEvent: true}.MarshalTLV(enc, tlv.ContextTag(0))
	enc.PutUint(tlv.ContextTag(1), 42)
	enc.PutUint(tlv.ContextTag(2), 1)
	enc.PutUint(tlv.ContextTag(4), 1000)
	enc.PutUint(tlv.ContextTag(6), 5)
	enc.StartStruct(tlv.ContextTag(7))
	enc.PutUint(tlv.ContextTag(0), 2)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartStruct(tlv.ContextTag(0))
	ConcreteEventPath{Endpoint: 1, Cluster: 0x3B, Event: 2, HasEndpoint: true, HasCluster: true, HasEvent: true}.MarshalTLV(enc, tlv.ContextTag(0))
	StatusIB{Status: StatusUnsupportedEvent}.MarshalTLV(enc, tlv.ContextTag(1))
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutBool(tlv.ContextTag(3), true)
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	reader := func(p ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (AttributeValue, error) {
		seen = append(seen, el.String+strings.Repeat("u", int(el.Uint)))
		return AttributeValue{Value: el}, nil
	}
	events := func(p ConcreteEventPath, el tlv.Element, dec *tlv.Decoder) (AttributeValue, error) {
		if !el.IsContainer {
			t.Fatal("event data is not the structure")
		}
		return AttributeValue{Value: "event"}, skipContainer(dec)
	}
	rd, err := UnmarshalReportDataTLV(tlv.NewDecoder(b), reader, events)
	if err != nil {
		t.Fatal(err)
	}
	if !rd.HasSubscription || rd.SubscriptionID != 0x1234 || !rd.MoreChunkedMessages || rd.SuppressResponse || len(rd.Reports) != 3 || len(rd.EventReports) != 2 {
		t.Fatalf("report %+v", rd)
	}
	a, c, s := rd.Reports[0], rd.Reports[1], rd.Reports[2]
	if !a.HasDataVersion || a.DataVersion != 7 || a.TagCompression || a.Path.Attribute != 0 || a.IsStatus {
		t.Errorf("data %+v", a)
	}
	if c.HasDataVersion || !c.TagCompression || !c.Path.ListAppend || c.Path.HasEndpoint {
		t.Errorf("compressed %+v", c)
	}
	if !s.IsStatus || s.Status.Status != StatusFailure || !s.Status.HasClusterStatus || s.Status.ClusterStatus != 3 || s.Path.Endpoint != 2 {
		t.Errorf("status %+v", s)
	}
	if len(seen) != 2 || seen[0] != "x" || seen[1] != strings.Repeat("u", 9) {
		t.Errorf("values read %q", seen)
	}
	e, es := rd.EventReports[0], rd.EventReports[1]
	if r := e.Received; e.Number != 42 || e.Priority != 1 || r == nil || r.HasEpochTimestamp || !r.HasSystemTimestamp || r.SystemTimestamp != 1000 ||
		!r.HasDeltaSystemTimestamp || r.DeltaSystemTimestamp != 5 || !r.HasData || e.Data.Value != "event" || e.Path.Event != 1 {
		t.Errorf("event %+v", e)
	}
	if !es.IsStatus || es.Status.Status != StatusUnsupportedEvent || es.Path.Event != 2 {
		t.Errorf("event status %+v", es)
	}
	// Truncations and wrong shapes are ErrInvalidReportData.
	for i := 1; i < len(b); i++ {
		if _, err := UnmarshalReportDataTLV(tlv.NewDecoder(b[:i]), reader, events); err == nil {
			t.Fatalf("truncation at %d decoded", i)
		}
	}
	if _, err := UnmarshalReportDataTLV(tlv.NewDecoder([]byte{0x16, 0x18}), nil, nil); err == nil {
		t.Fatal("an array decoded as ReportData")
	}
	if _, err := UnmarshalSubscribeResponseTLV(tlv.NewDecoder([]byte{0x15, 0x24, 0x00, 0x01, 0x18})); err == nil {
		t.Fatal("a SubscribeResponse without MaxInterval decoded")
	}
	if _, err := UnmarshalWriteResponseTLV(tlv.NewDecoder([]byte{0x15, 0x24, 0x00, 0x01, 0x18})); err == nil {
		t.Fatal("a WriteResponse with a scalar WriteResponses decoded")
	}
	if got := DataVersionFilterSize(DataVersionFilter{Endpoint: 1, Cluster: 6, DataVersion: 100}); got != 14 {
		t.Fatalf("DataVersionFilterSize = %d, want 14 (subscribe_req_with_dvf's member)", got)
	}
}

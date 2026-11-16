// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// TestAttributeDataIsDecodedWithTheFinalPath (review II, R2-P6-wire-0505):
// an AttributeDataIB whose Data precedes its Path hands the value reader
// the final path — on a server's write and in a client's report — as one
// in wire order does.
func TestAttributeDataIsDecodedWithTheFinalPath(t *testing.T) {
	t.Parallel()
	want := ConcreteAttributePath{Endpoint: 1, HasEndpoint: true, Cluster: 6, HasCluster: true, Attribute: 0x4003, HasAttribute: true}
	ib := func(enc *tlv.Encoder, tag tlv.Tag, dataFirst bool) {
		enc.StartStruct(tag)
		data := func() { enc.PutUint(tlv.ContextTag(tagAttributeDataValue), 7) }
		if dataFirst {
			data()
		}
		enc.StartList(tlv.ContextTag(tagAttributeDataPath))
		enc.PutUint(tlv.ContextTag(tagAttrPathEndpoint), 1)
		enc.PutUint(tlv.ContextTag(tagAttrPathCluster), 6)
		enc.PutUint(tlv.ContextTag(tagAttrPathAttribute), 0x4003)
		_ = enc.EndContainer()
		if !dataFirst {
			data()
		}
		_ = enc.EndContainer()
	}
	for _, dataFirst := range []bool{false, true} {
		var seen ConcreteAttributePath
		reader := func(path ConcreteAttributePath, el tlv.Element, _ *tlv.Decoder) (AttributeValue, error) {
			seen = path
			return AttributeValue{Value: el.Uint}, nil
		}
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutBool(tlv.ContextTag(tagWriteReqTimedRequest), false)
		enc.StartArray(tlv.ContextTag(tagWriteReqWriteRequests))
		ib(enc, tlv.AnonymousTag(), dataFirst)
		_ = enc.EndContainer()
		_ = enc.EndContainer()
		wire, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		req, err := UnmarshalWriteRequestTLV(tlv.NewDecoder(wire), reader)
		if err != nil {
			t.Fatalf("dataFirst=%v: %v", dataFirst, err)
		}
		if seen != want || req.Writes[0].Path != want || req.Writes[0].Value.Value != uint64(7) {
			t.Errorf("write, dataFirst=%v: reader saw %+v, write %+v", dataFirst, seen, req.Writes[0])
		}

		seen = ConcreteAttributePath{}
		enc = tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		ib(enc, tlv.ContextTag(tagAttributeReportData), dataFirst)
		_ = enc.EndContainer()
		wire, err = enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		dec := tlv.NewDecoder(wire)
		_, _ = dec.Next() // the AttributeReportIB opener
		_, _ = dec.Next() // the AttributeDataIB opener
		var rep AttributeReport
		if err := readAttributeReportData(dec, &rep, reader); err != nil {
			t.Fatalf("report, dataFirst=%v: %v", dataFirst, err)
		}
		if seen.Endpoint != 1 || seen.Cluster != 6 || seen.Attribute != 0x4003 || rep.Value.Value != uint64(7) {
			t.Errorf("report, dataFirst=%v: reader saw %+v, value %+v", dataFirst, seen, rep.Value)
		}
	}
}

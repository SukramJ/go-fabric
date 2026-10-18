// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// A port of matter.js packages/protocol/test/protocol/interaction/AttributeDataEncoderTest.ts
// at 862ad75f: the §8.9 field guarantees of the report encoder. The
// isUrgent case is TestEventReport_Marshal_DataIB_IsUrgentNotInDataPath;
// the array chunking lives with the splitter in bridge.

import (
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// reportIBFields marshals one report IB through write and returns the
// context tags of the IB struct's direct children, in wire order.
func reportIBFields(t *testing.T, write func(*tlv.Encoder)) []uint32 {
	t.Helper()
	enc := tlv.NewEncoder()
	write(enc)
	wire, err := enc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	dec := tlv.NewDecoder(wire)
	if _, err := dec.Next(); err != nil { // the ReportIB struct
		t.Fatalf("IB opener: %v", err)
	}
	var tags []uint32
	for {
		el, err := dec.Next()
		if err != nil {
			t.Fatalf("decode IB: %v", err)
		}
		if el.IsEndContainer {
			return tags
		}
		tags = append(tags, el.Tag.Number)
		if el.IsContainer {
			if err := skipContainer(dec); err != nil {
				t.Fatalf("skip: %v", err)
			}
		}
	}
}

// TestAttributeDataEncoderFieldGuarantees ports the "spec §8.9 encoder
// field guarantees" group. matter.js throws a MatterFlowError on a payload
// with both or neither of data and status
// (AttributeDataEncoder.ts:95-100,114-119); the Go report types hold the
// choice in one IsStatus flag, so such a payload cannot be built and the
// encoder writes exactly one IB of the two. The tests assert that
// guarantee on the wire.
func TestAttributeDataEncoderFieldGuarantees(t *testing.T) {
	t.Parallel()
	eventPath := ConcreteEventPath{Endpoint: 1, HasEndpoint: true, Cluster: 0x28, HasCluster: true, Event: 0, HasEvent: true}
	attrPath := ConcreteAttributePath{Endpoint: 1, HasEndpoint: true, Cluster: 0x28, HasCluster: true, Attribute: 2, HasAttribute: true}
	writeAny := func(enc *tlv.Encoder, tag tlv.Tag, v AttributeValue) {
		if u, ok := v.Value.(uint8); ok {
			enc.PutUint(tag, uint64(u))
		}
	}
	event := func(rep EventReport) func(*tlv.Encoder) {
		return func(enc *tlv.Encoder) { rep.marshal(enc, writeAny) }
	}
	attr := func(rep AttributeReport) func(*tlv.Encoder) {
		return func(enc *tlv.Encoder) { rep.marshal(enc, writeAny) }
	}
	exactlyOne := func(t *testing.T, tags []uint32, want uint8) {
		t.Helper()
		if len(tags) != 1 || tags[0] != uint32(want) {
			t.Fatalf("IB children = %v, want exactly [%d]", tags, want)
		}
	}

	t.Run("rejects an EventReport carrying both data and status (§8.9.3.5)", func(t *testing.T) {
		t.Parallel()
		// AttributeDataEncoderTest.ts:309-327: data and a Failure status.
		rep := EventReport{
			Path: eventPath, Number: 1, Priority: EventPriorityInfo, Timestamp: 12345,
			Data:   AttributeValue{Value: uint8(5)},
			Status: StatusIB{Status: StatusFailure}, IsStatus: true,
		}
		exactlyOne(t, reportIBFields(t, event(rep)), tagEventReportStatus)
	})

	t.Run("rejects an AttributeReport carrying both data and status (§8.9.2.9)", func(t *testing.T) {
		t.Parallel()
		// AttributeDataEncoderTest.ts:329-352.
		rep := AttributeReport{
			Path:   attrPath,
			Value:  AttributeValue{Value: uint8(1)},
			Status: StatusIB{Status: StatusFailure}, IsStatus: true,
		}
		exactlyOne(t, reportIBFields(t, attr(rep)), tagAttributeReportStatus)
	})

	t.Run("rejects an EventReport carrying neither data nor status (§8.9.3.5)", func(t *testing.T) {
		t.Parallel()
		// AttributeDataEncoderTest.ts:354-356: the zero report is a data IB.
		exactlyOne(t, reportIBFields(t, event(EventReport{})), tagEventReportData)
	})

	t.Run("rejects an AttributeReport carrying neither data nor status (§8.9.2.9)", func(t *testing.T) {
		t.Parallel()
		// AttributeDataEncoderTest.ts:358-360.
		exactlyOne(t, reportIBFields(t, attr(AttributeReport{})), tagAttributeReportData)
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

// A port of matter.js packages/protocol/test/protocol/interaction/EventDataDecoderTest.ts
// at 862ad75f: decoding the events of a report, known and unknown. The Go
// report is InputChunk's flat event stream, so a case whose expectation is
// the grouping of several events by path stays in the register's missing
// list; a case with one event per path is that grouping already.

import (
	"strconv"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// edEvent is one EventDataIB of the test: data is the startUp payload
// {softwareVersion} (field 0), or no data for TlvVoid.
type edEvent struct {
	cluster  uint32
	event    uint32
	number   uint64
	software uint64
	void     bool
}

func edAssemble(t *testing.T, events ...edEvent) *Report {
	t.Helper()
	rd := im.ReportData{}
	for _, e := range events {
		rep := im.EventReport{
			Path:     im.ConcreteEventPath{Endpoint: 0, HasEndpoint: true, Cluster: e.cluster, HasCluster: true, Event: e.event, HasEvent: true},
			Number:   e.number,
			Priority: im.EventPriorityInfo,
		}
		if !e.void {
			rep.Data = im.AttributeValue{Value: e.software}
		}
		rd.EventReports = append(rd.EventReports, rep)
	}
	enc := tlv.NewEncoder()
	rd.MarshalTLV(enc, func(enc *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), v.Value.(uint64))
		_ = enc.EndContainer()
	})
	b, err := enc.Bytes()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return adAssemble(t, b)
}

// edExpect checks the report's events against want in order: path,
// number, priority Info, epoch timestamp 0 and no other, the data.
func edExpect(t *testing.T, r *Report, want ...edEvent) {
	t.Helper()
	if len(r.Events) != len(want) {
		t.Fatalf("events = %d, want %d", len(r.Events), len(want))
	}
	for i, w := range want {
		e := r.Events[i]
		if e.Path.Endpoint != 0 || e.Path.Cluster != w.cluster || e.Path.Event != w.event || e.Path.HasNode {
			t.Fatalf("[%d] path = %+v", i, e.Path)
		}
		if e.Number != w.number || e.Priority != im.EventPriorityInfo {
			t.Fatalf("[%d] number=%d priority=%d", i, e.Number, e.Priority)
		}
		if e.EpochTimestamp == nil || *e.EpochTimestamp != 0 || e.SystemTimestamp != nil || e.DeltaEpochTimestamp != nil || e.DeltaSystemTimestamp != nil {
			t.Fatalf("[%d] timestamps epoch=%v system=%v deltaEpoch=%v deltaSystem=%v",
				i, e.EpochTimestamp, e.SystemTimestamp, e.DeltaEpochTimestamp, e.DeltaSystemTimestamp)
		}
		switch {
		case w.void && e.HasValue:
			t.Fatalf("[%d] data = %s, want none", i, adRender(e.Value))
		case !w.void && (!e.HasValue || adRender(e.Value) != "{0:"+strconv.FormatUint(w.software, 10)+"}"):
			t.Fatalf("[%d] data = %s (has=%v), want {0:%d}", i, adRender(e.Value), e.HasValue, w.software)
		}
	}
}

// EventDataDecoderTest.ts:25-40: Basic Information startUp
// {softwareVersion: 1} and shutDown (void).
var edSingle = []edEvent{
	{cluster: 0x28, event: 0, number: 1, software: 1},
	{cluster: 0x28, event: 1, number: 2, void: true},
}

// TestEventDataDecoderNormalize ports "normalizeEventData"
// (EventDataDecoderTest.ts:23-121).
func TestEventDataDecoderNormalize(t *testing.T) {
	t.Parallel()
	t.Run("normalize data with all paths given for single event entries", func(t *testing.T) {
		t.Parallel()
		edExpect(t, edAssemble(t, edSingle...), edSingle...)
	})
}

// TestEventDataDecoderDecode ports "normalize and Decode EventData"
// (EventDataDecoderTest.ts:123-287).
func TestEventDataDecoderDecode(t *testing.T) {
	t.Parallel()
	t.Run("normalize and decode data with all paths given for single event entries", func(t *testing.T) {
		t.Parallel()
		edExpect(t, edAssemble(t, edSingle...), edSingle...)
	})
}

// TestEventDataDecoderUnknownEvent ports "normalize and Decode EventReport
// for unknown event" (EventDataDecoderTest.ts:289-361): cluster 0x999,
// the startUp payload decoded as {"0": 1}.
func TestEventDataDecoderUnknownEvent(t *testing.T) {
	t.Parallel()
	t.Run("normalize and decode data with all paths given for single event entries", func(t *testing.T) {
		t.Parallel()
		events := []edEvent{
			{cluster: 0x999, event: 0, number: 1, software: 1},
			{cluster: 0x999, event: 1, number: 2, void: true},
		}
		edExpect(t, edAssemble(t, events...), events...)
	})
}

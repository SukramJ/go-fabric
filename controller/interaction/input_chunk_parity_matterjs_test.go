// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

// A port of matter.js packages/protocol/test/action/client/InputChunkTest.ts at
// 862ad75f: the controller's parsing of report chunks (path order, tag
// compression, chunked-array tails, events).
//
// matter.js InputChunk yields one chunk per value or status in wire order;
// the Go assembler collects them into a Report with one slice per kind, so
// a case asserting the order across kinds checks each slice and the order
// within it. The `received` flag of a status chunk has no Go field (every
// status in a Report was received).

import (
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// BasicInformation (0x28) and OnOff (6) ids the matter.js test names.
const (
	icBasic            = 0x28
	icDataModelRev     = 0
	icVendorID         = 2
	icProductID        = 4
	icCapabilityMinima = 0x13
	icOnOff            = 6
	icStartUp          = 0
	icShutDown         = 1
)

func icAssemble(t *testing.T, leftover []im.AttributeReport, rds ...*im.ReportData) *assembler {
	t.Helper()
	a := newAssembler(slog.New(slog.DiscardHandler))
	a.leftover = leftover
	for _, rd := range rds {
		a.add(rd)
	}
	return a
}

func icVersioned(e im.AttributeReport, v uint32) im.AttributeReport {
	e.DataVersion, e.HasDataVersion = v, true
	return e
}

func icStatus(p im.ConcreteAttributePath, s im.StatusCode) im.AttributeReport {
	return im.AttributeReport{IsStatus: true, Path: p, Status: im.StatusIB{Status: s}}
}

func icEventPath(ep uint16, cl, ev uint32) im.ConcreteEventPath {
	return im.ConcreteEventPath{Endpoint: ep, Cluster: cl, Event: ev, HasEndpoint: true, HasCluster: true, HasEvent: true}
}

func icEvent(p im.ConcreteEventPath, number uint64) im.EventReport {
	return im.EventReport{Path: p, Number: number, Priority: 1, Received: &im.ReceivedEventData{HasEpochTimestamp: true, HasData: true}, Data: im.AttributeValue{Value: uintNode(number)}}
}

func TestInputChunkAttributes(t *testing.T) {
	t.Parallel()
	t.Run("yields one attr-value per fully-qualified path entry", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{Reports: []im.AttributeReport{
			icVersioned(dataEntry(full(0, icBasic, icDataModelRev), uintNode(17)), 100),
		}})
		r := a.report
		if len(r.Attributes) != 1 || len(r.Statuses) != 0 {
			t.Fatalf("report %+v", r)
		}
		if got := r.Attributes[0]; got.Path != full(0, icBasic, icDataModelRev) || got.Path.HasNode || got.Value.El.Uint != 17 || got.DataVersion != 100 {
			t.Fatalf("attr-value %+v", got)
		}
	})
	t.Run("preserves cross-path order (A.1, B.1, A.2, B.2 → same order out)", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{Reports: []im.AttributeReport{
			icVersioned(dataEntry(full(0, icBasic, icDataModelRev), uintNode(11)), 1),
			icVersioned(dataEntry(full(1, icOnOff, 0), spec.Node{El: tlv.Element{Type: tlv.TypeBoolTrue, Bool: true}}), 2),
			icVersioned(dataEntry(full(0, icBasic, icVendorID), uintNode(0xfff1)), 1),
		}})
		r := a.report.Attributes
		if len(r) != 3 {
			t.Fatalf("attributes %+v", r)
		}
		if r[0].Path != full(0, icBasic, icDataModelRev) || r[0].Value.El.Uint != 11 ||
			r[1].Path != full(1, icOnOff, 0) || !r[1].Value.El.Bool ||
			r[2].Path != full(0, icBasic, icVendorID) || r[2].Value.El.Uint != 0xfff1 {
			t.Fatalf("order %+v", r)
		}
	})
	t.Run("restores tag-compressed paths from the previous fully-qualified entry", func(t *testing.T) {
		t.Parallel()
		compressed := dataEntry(im.ConcreteAttributePath{Attribute: icVendorID, HasAttribute: true}, uintNode(0xfff1))
		compressed.TagCompression = true
		a := icAssemble(t, nil, &im.ReportData{Reports: []im.AttributeReport{
			icVersioned(dataEntry(full(0, icBasic, icDataModelRev), uintNode(1)), 200),
			compressed,
		}})
		r := a.report.Attributes
		if len(r) != 2 {
			t.Fatalf("attributes %+v", r)
		}
		if s := r[1]; s.Path != full(0, icBasic, icVendorID) || s.Value.El.Uint != 0xfff1 || s.DataVersion != 200 {
			t.Fatalf("second %+v", s)
		}
	})
	t.Run("emits attr-value then attr-status when a different-path status follows data", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{Reports: []im.AttributeReport{
			icVersioned(dataEntry(full(0, icBasic, icDataModelRev), uintNode(7)), 1),
			icStatus(full(0, icBasic, icVendorID), im.StatusUnsupportedAttribute),
		}})
		r := a.report
		if len(r.Attributes) != 1 || len(r.Statuses) != 1 || r.Statuses[0].Status != im.StatusUnsupportedAttribute {
			t.Fatalf("report %+v", r)
		}
	})
	t.Run("keeps adjacent same-cluster different-attribute entries from being lumped", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{Reports: []im.AttributeReport{
			icVersioned(dataEntry(full(0, icBasic, icDataModelRev), uintNode(11)), 1),
			icStatus(full(0, icBasic, icVendorID), im.StatusUnsupportedAttribute),
			icVersioned(dataEntry(full(0, icBasic, icProductID), uintNode(33)), 1),
		}})
		r := a.report
		if len(r.Attributes) != 2 || len(r.Statuses) != 1 {
			t.Fatalf("report %+v", r)
		}
		if r.Attributes[0].Path.Attribute != icDataModelRev || r.Statuses[0].Path.Attribute != icVendorID ||
			r.Attributes[1].Path.Attribute != icProductID || r.Attributes[1].Value.El.Uint != 33 {
			t.Fatalf("ids %+v", r)
		}
	})
	t.Run("emits data and status for the same path in wire order", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{Reports: []im.AttributeReport{
			icVersioned(dataEntry(full(0, icBasic, icDataModelRev), uintNode(5)), 1),
			icStatus(full(0, icBasic, icDataModelRev), im.StatusFailure),
		}})
		r := a.report
		if len(r.Attributes) != 1 || r.Attributes[0].Value.El.Uint != 5 || len(r.Statuses) != 1 || r.Statuses[0].Status != im.StatusFailure {
			t.Fatalf("report %+v", r)
		}
	})
	t.Run("stashes a chunked-array tail (listIndex entries) when moreChunkedMessages is set", func(t *testing.T) {
		t.Parallel()
		appendPath := full(0, icBasic, icCapabilityMinima)
		appendPath.HasListIndex, appendPath.ListAppend = true, true
		a := icAssemble(t, nil, &im.ReportData{MoreChunkedMessages: true, Reports: []im.AttributeReport{
			icVersioned(dataEntry(full(0, icBasic, icCapabilityMinima), uintNode(1)), 5),
			dataEntry(appendPath, uintNode(2)),
		}})
		if len(a.report.Attributes) != 0 || len(a.leftover) != 2 {
			t.Fatalf("attributes %+v, leftover %d", a.report.Attributes, len(a.leftover))
		}
	})
	t.Run("stashes a chunked-array tail (single array-typed initial value) when moreChunkedMessages is set", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{MoreChunkedMessages: true, Reports: []im.AttributeReport{
			icVersioned(dataEntry(full(0, icBasic, icCapabilityMinima), arrayNode(1, 2, 3)), 11),
		}})
		if len(a.report.Attributes) != 0 || len(a.leftover) != 1 {
			t.Fatalf("attributes %+v, leftover %d", a.report.Attributes, len(a.leftover))
		}
	})
	t.Run("prepends leftover into the next call and clears the leftover buffer", func(t *testing.T) {
		t.Parallel()
		appendPath := full(0, icBasic, icCapabilityMinima)
		appendPath.HasListIndex, appendPath.ListAppend = true, true
		a := icAssemble(t, []im.AttributeReport{
			icVersioned(dataEntry(full(0, icBasic, icCapabilityMinima), uintNode(1)), 5),
			dataEntry(appendPath, uintNode(2)),
		}, &im.ReportData{})
		if len(a.leftover) != 0 {
			t.Fatalf("leftover %d", len(a.leftover))
		}
	})
	t.Run("flushes (not stashes) when moreChunkedMessages is false", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{Reports: []im.AttributeReport{
			icVersioned(dataEntry(full(0, icBasic, icDataModelRev), uintNode(3)), 9),
		}})
		if len(a.report.Attributes) != 1 || len(a.leftover) != 0 {
			t.Fatalf("attributes %+v, leftover %d", a.report.Attributes, len(a.leftover))
		}
	})
	t.Run("skips a tag-compressed leading entry with no prior path and still yields the next", func(t *testing.T) {
		t.Parallel()
		lead := icVersioned(dataEntry(im.ConcreteAttributePath{Attribute: icDataModelRev, HasAttribute: true}, uintNode(7)), 1)
		lead.TagCompression = true
		a := icAssemble(t, nil, &im.ReportData{Reports: []im.AttributeReport{
			lead,
			icVersioned(dataEntry(full(0, icBasic, icVendorID), uintNode(0xfff1)), 1),
		}})
		r := a.report.Attributes
		if len(r) != 1 || r[0].Path.Attribute != icVendorID || r[0].Value.El.Uint != 0xfff1 {
			t.Fatalf("attributes %+v", r)
		}
	})
	t.Run("skips an entry with an incomplete uncompressed path and still yields the next", func(t *testing.T) {
		t.Parallel()
		incomplete := im.ConcreteAttributePath{Endpoint: 0, Attribute: icDataModelRev, HasEndpoint: true, HasAttribute: true}
		a := icAssemble(t, nil, &im.ReportData{Reports: []im.AttributeReport{
			icVersioned(dataEntry(incomplete, uintNode(7)), 1),
			icVersioned(dataEntry(full(0, icBasic, icVendorID), uintNode(0xfff1)), 1),
		}})
		r := a.report.Attributes
		if len(r) != 1 || r[0].Path.Attribute != icVendorID {
			t.Fatalf("attributes %+v", r)
		}
	})
}

func TestInputChunkEvents(t *testing.T) {
	t.Parallel()
	t.Run("preserves wire (EventNumber) order across interleaved event types (#3785)", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{EventReports: []im.EventReport{
			icEvent(icEventPath(0, icBasic, icStartUp), 1),
			icEvent(icEventPath(0, icBasic, icShutDown), 2),
			icEvent(icEventPath(0, icBasic, icStartUp), 3),
		}})
		r := a.report.Events
		if len(r) != 3 {
			t.Fatalf("events %+v", r)
		}
		want := []struct {
			number uint64
			event  uint32
		}{{1, icStartUp}, {2, icShutDown}, {3, icStartUp}}
		for i, w := range want {
			if r[i].Number != w.number || r[i].Path.Event != w.event {
				t.Fatalf("event %d: %+v, want number %d event %d", i, r[i], w.number, w.event)
			}
		}
	})
	t.Run("propagates the wire timestamp variant fields onto the chunk", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{EventReports: []im.EventReport{{
			Path: icEventPath(0, icBasic, icStartUp), Number: 1, Priority: 1,
			Received: &im.ReceivedEventData{SystemTimestamp: 1234, HasSystemTimestamp: true, HasData: true},
			Data:     im.AttributeValue{Value: uintNode(1)},
		}}})
		r := a.report.Events
		if len(r) != 1 {
			t.Fatalf("events %+v", r)
		}
		ev := r[0]
		if ev.SystemTimestamp == nil || *ev.SystemTimestamp != 1234 || ev.EpochTimestamp != nil ||
			ev.DeltaEpochTimestamp != nil || ev.DeltaSystemTimestamp != nil || ev.Timestamp != 1234 {
			t.Fatalf("event %+v", ev)
		}
	})
	t.Run("emits a single event-status chunk when both status and clusterStatus are set (B2 regression)", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{EventReports: []im.EventReport{{
			Path: icEventPath(0, icBasic, icStartUp), IsStatus: true,
			Status: im.StatusIB{Status: im.StatusFailure, ClusterStatus: 42, HasClusterStatus: true},
		}}})
		r := a.report
		if len(r.EventStatuses) != 1 || len(r.Events) != 0 {
			t.Fatalf("report %+v", r)
		}
		if s := r.EventStatuses[0]; s.Status != im.StatusFailure || s.ClusterStatus == nil || *s.ClusterStatus != 42 {
			t.Fatalf("status %+v", s)
		}
	})
	t.Run("skips an event entry with an invalid path and still yields the next", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{EventReports: []im.EventReport{
			icEvent(im.ConcreteEventPath{Endpoint: 0, Event: icStartUp, HasEndpoint: true, HasEvent: true}, 1),
			icEvent(icEventPath(0, icBasic, icStartUp), 2),
		}})
		r := a.report.Events
		if len(r) != 1 || r[0].Number != 2 {
			t.Fatalf("events %+v", r)
		}
	})
}

func TestInputChunkOrderingAcrossAttributesAndEvents(t *testing.T) {
	t.Parallel()
	t.Run("yields attributes first, then events (matches wire layout)", func(t *testing.T) {
		t.Parallel()
		a := icAssemble(t, nil, &im.ReportData{
			Reports:      []im.AttributeReport{icVersioned(dataEntry(full(0, icBasic, icDataModelRev), uintNode(1)), 1)},
			EventReports: []im.EventReport{icEvent(icEventPath(0, icBasic, icStartUp), 1)},
		})
		r := a.report
		if len(r.Attributes) != 1 || len(r.Events) != 1 {
			t.Fatalf("report %+v", r)
		}
	})
}

func TestInputChunkRelaxedIntegerTypeChecks(t *testing.T) {
	t.Parallel()
	t.Run("decodes a signed-encoded value for an unsigned attribute", func(t *testing.T) {
		t.Parallel()
		// Through the wire: the ReportData carries a signed Int32 17 for the
		// unsigned dataModelRevision (InputChunkTest.ts:650-671).
		rd := im.ReportData{Reports: []im.AttributeReport{{Path: full(0, icBasic, icDataModelRev), DataVersion: 1, HasDataVersion: true}}}
		e := tlv.NewEncoder()
		rd.MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, _ im.AttributeValue) { e.PutInt32(tag, 17) })
		payload, err := e.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeReport(payload)
		if err != nil {
			t.Fatal(err)
		}
		a := icAssemble(t, nil, decoded)
		r := a.report.Attributes
		if len(r) != 1 || r[0].Value.El.Int != 17 {
			t.Fatalf("attributes %+v", r)
		}
	})
}

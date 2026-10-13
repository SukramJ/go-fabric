// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

func uintNode(v uint64) spec.Node {
	return spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: v}}
}

func arrayNode(vs ...uint64) spec.Node {
	n := spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}}
	for _, v := range vs {
		n.Children = append(n.Children, uintNode(v))
	}
	return n
}

func full(ep uint16, cl, at uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: cl, Attribute: at, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

func dataEntry(p im.ConcreteAttributePath, v spec.Node) im.AttributeReport {
	return im.AttributeReport{Path: p, Value: im.AttributeValue{Value: v}}
}

func values(n spec.Node) []uint64 {
	out := make([]uint64, 0, len(n.Children))
	for _, c := range n.Children {
		out = append(out, c.El.Uint)
	}
	return out
}

// TestAssemblerTagCompression: a compressed path takes the missing fields
// and the data version of the last complete path (InputChunk.ts:192-204,
// :252-273); a compressed entry without a previous path and an incomplete
// uncompressed one are skipped (:227-230); a status takes part in path
// resolution and ends a group (:215-226).
func TestAssemblerTagCompression(t *testing.T) {
	t.Parallel()
	a := newAssembler(slog.New(slog.DiscardHandler))
	first := dataEntry(full(1, 6, 0), uintNode(1))
	first.DataVersion, first.HasDataVersion = 5, true
	compressed := dataEntry(im.ConcreteAttributePath{Attribute: 0x4000, HasAttribute: true}, uintNode(2))
	compressed.TagCompression = true
	status := im.AttributeReport{
		IsStatus: true, Path: im.ConcreteAttributePath{Attribute: 0x4001, HasAttribute: true}, TagCompression: true,
		Status: im.StatusIB{Status: im.StatusUnsupportedAttribute, ClusterStatus: 1, HasClusterStatus: true},
	}
	a.add(&im.ReportData{Reports: []im.AttributeReport{
		{TagCompression: true, Path: full(9, 9, 9), Value: im.AttributeValue{Value: uintNode(9)}}, // no previous path
		first, compressed, status,
		dataEntry(im.ConcreteAttributePath{Cluster: 6, HasCluster: true}, uintNode(3)), // incomplete
	}})
	r := a.report
	if len(r.Attributes) != 2 || len(r.Statuses) != 1 {
		t.Fatalf("report %+v", r)
	}
	if c := r.Attributes[1]; c.Path != full(1, 6, 0x4000) || c.DataVersion != 5 || c.Value.El.Uint != 2 {
		t.Fatalf("compressed %+v", c)
	}
	if s := r.Statuses[0]; s.Path != full(1, 6, 0x4001) || *s.ClusterStatus != 1 {
		t.Fatalf("status %+v", s)
	}
}

// TestAssemblerLists: adjacent entries of one path form a list
// (decodeFromChunkedArray, TlvArray.ts:130-154): no index replaces, null
// appends, an index with null removes, an index sets; a group whose first
// entry carries an index is skipped; a list at the end of a chunk with more
// to come waits for the next chunk (InputChunk.ts:233-241).
func TestAssemblerLists(t *testing.T) {
	t.Parallel()
	a := newAssembler(slog.New(slog.DiscardHandler))
	p := full(0, 0x1D, 3)
	appendPath, at := p, p
	appendPath.HasListIndex, appendPath.ListAppend = true, true
	at.HasListIndex, at.ListIndex = true, 0
	a.add(&im.ReportData{MoreChunkedMessages: true, Reports: []im.AttributeReport{
		dataEntry(full(0, 0x1D, 1), arrayNode(7)), // flushed: the next path differs
		dataEntry(appendPath, uintNode(1)),        // a group starting with an index: skipped
		dataEntry(full(0, 0x28, 2), uintNode(0xFFF1)),
		dataEntry(p, arrayNode(1, 2)),
		dataEntry(appendPath, uintNode(3)),
	}})
	if len(a.report.Attributes) != 2 || len(a.leftover) != 2 {
		t.Fatalf("after the first chunk: %+v, leftover %d", a.report.Attributes, len(a.leftover))
	}
	a.add(&im.ReportData{Reports: []im.AttributeReport{
		dataEntry(appendPath, uintNode(4)),
		dataEntry(at, spec.Node{El: tlv.Element{Type: tlv.TypeNull, IsNull: true}}),
		dataEntry(at, uintNode(9)),
	}})
	r := a.report
	if len(r.Attributes) != 3 {
		t.Fatalf("attributes %+v", r.Attributes)
	}
	if got := values(r.Attributes[2].Value); len(got) != 3 || got[0] != 9 || got[1] != 3 || got[2] != 4 {
		t.Fatalf("list %v", got)
	}
	if got := values(r.Attributes[0].Value); len(got) != 1 || got[0] != 7 {
		t.Fatalf("first list %v", got)
	}
	// A chunk that is not an array where the list is replaced, and an index
	// past the end, are malformed groups.
	if _, err := decodeGroup([]*im.AttributeReport{ptr(dataEntry(p, arrayNode())), ptr(dataEntry(p, uintNode(1)))}); err == nil {
		t.Fatal("a non-array replacement decoded")
	}
	past := p
	past.HasListIndex, past.ListIndex = true, 5
	if _, err := decodeGroup([]*im.AttributeReport{ptr(dataEntry(p, arrayNode())), ptr(dataEntry(past, uintNode(1)))}); err == nil {
		t.Fatal("an index past the list decoded")
	}
}

// TestAssemblerEvents: the event value and status of InputChunk.ts:356-432:
// the timestamp precedence epoch, system, delta epoch, delta system; an
// event without a complete path skipped.
func TestAssemblerEvents(t *testing.T) {
	t.Parallel()
	a := newAssembler(slog.New(slog.DiscardHandler))
	path := im.ConcreteEventPath{Endpoint: 1, Cluster: 0x3B, Event: 1, HasEndpoint: true, HasCluster: true, HasEvent: true}
	a.add(&im.ReportData{EventReports: []im.EventReport{
		{Path: path, Number: 1, Received: &im.ReceivedEventData{SystemTimestamp: 10, HasSystemTimestamp: true, DeltaEpochTimestamp: 20, HasDeltaEpochTimestamp: true}},
		{Path: path, Number: 2, Timestamp: 30, Received: &im.ReceivedEventData{HasEpochTimestamp: true, DeltaSystemTimestamp: 40, HasDeltaSystemTimestamp: true, HasData: true}, Data: im.AttributeValue{Value: uintNode(5)}},
		{Path: path, Number: 3, Received: &im.ReceivedEventData{DeltaSystemTimestamp: 50, HasDeltaSystemTimestamp: true}},
		{Path: im.ConcreteEventPath{Cluster: 0x3B, HasCluster: true}, Number: 4},
		{Path: path, IsStatus: true, Status: im.StatusIB{Status: im.StatusUnsupportedEvent, ClusterStatus: 2, HasClusterStatus: true}},
	}})
	r := a.report
	if len(r.Events) != 3 || len(r.EventStatuses) != 1 {
		t.Fatalf("events %+v, statuses %+v", r.Events, r.EventStatuses)
	}
	if e := r.Events[0]; e.Timestamp != 10 || e.EpochTimestamp != nil || *e.DeltaEpochTimestamp != 20 || e.HasValue {
		t.Fatalf("first %+v", e)
	}
	if e := r.Events[1]; e.Timestamp != 30 || !e.HasValue || e.Value.El.Uint != 5 {
		t.Fatalf("second %+v", e)
	}
	if e := r.Events[2]; e.Timestamp != 50 {
		t.Fatalf("third %+v", e)
	}
	if s := r.EventStatuses[0]; s.Status != im.StatusUnsupportedEvent || *s.ClusterStatus != 2 || r.Empty() {
		t.Fatalf("status %+v", s)
	}
}

// TestEncodeFitting: a request that fits is sent as it is; one that does
// not loses DataVersionFilters from the end until it fits, the space of
// the empty array counted (InteractionMessenger.ts:998-1050); one that
// does not fit without any is refused.
func TestEncodeFitting(t *testing.T) {
	t.Parallel()
	filters := make([]im.DataVersionFilter, 0, 200)
	for i := range 200 {
		filters = append(filters, im.DataVersionFilter{Endpoint: uint16(i), Cluster: 6, DataVersion: 0x80000000})
	}
	req := im.ReadRequest{AttributeRequests: []im.ConcreteAttributePath{full(1, 6, 0)}, FabricFiltered: true, DataVersionFilters: filters[:2]}
	b, err := encodeReadingRequest(req)
	if err != nil || len(b) > 100 {
		t.Fatalf("small request %d bytes (%v)", len(b), err)
	}
	req.DataVersionFilters = filters
	b, err = encodeReadingRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := im.UnmarshalReadRequestTLV(tlv.NewDecoder(b))
	if err != nil || len(b) > 1178 || len(parsed.DataVersionFilters) == 0 || len(parsed.DataVersionFilters) == len(filters) {
		t.Fatalf("trimmed to %d bytes, %d filters (%v)", len(b), len(parsed.DataVersionFilters), err)
	}
	if parsed.DataVersionFilters[0].Endpoint != 0 {
		t.Fatal("the filters were not kept in order")
	}
	// One more filter does not fit the space the kept ones leave.
	without, _ := readRequest(req).encode(nil)
	used := len(without) + emptyArraySize
	for _, f := range parsed.DataVersionFilters {
		used += im.DataVersionFilterSize(f)
	}
	if used > 1178 || used+im.DataVersionFilterSize(filters[len(parsed.DataVersionFilters)]) <= 1178 {
		t.Fatalf("budget %d", used)
	}
	paths := make([]im.ConcreteAttributePath, 0, 200)
	for i := range 200 {
		paths = append(paths, full(uint16(i), 6, 0))
	}
	if _, err := encodeReadingRequest(im.ReadRequest{AttributeRequests: paths, DataVersionFilters: filters}); err == nil {
		t.Fatal("a request too large without filters encoded")
	}
}

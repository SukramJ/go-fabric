// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package reportchunk_test

import (
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/reportchunk"
	"thingfabric.dev/go-fabric/tlv"
)

// write encodes strings and []uint32 lists; anything else as null.
func write(enc *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
	if v.IsNull {
		enc.PutNull(tag)
		return
	}
	switch x := v.Value.(type) {
	case string:
		enc.PutUTF8(tag, x)
	case []uint32:
		enc.StartArray(tag)
		for _, n := range x {
			enc.PutUint(tlv.AnonymousTag(), uint64(n))
		}
		_ = enc.EndContainer()
	default:
		enc.PutNull(tag)
	}
}

func attr(id uint32, v any) im.AttributeReport {
	return im.AttributeReport{
		Path:  im.ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: id, HasEndpoint: true, HasCluster: true, HasAttribute: true},
		Value: im.AttributeValue{Value: v},
	}
}

func encodedSize(t *testing.T, rd im.ReportData) int {
	t.Helper()
	b, err := reportchunk.Encode(rd, reportchunk.Writer(write))
	if err != nil {
		t.Fatal(err)
	}
	return len(b)
}

// TestSplit: a report that fits is its one chunk; one that does not is
// split greedily, every chunk within the budget, MoreChunkedMessages on
// all but the last and SuppressResponse on the last alone; a list too
// large for a chunk is split into a REPLACE-ALL and appends, an entry no
// message carries is ResourceExhausted, and the events follow the
// attributes.
func TestSplit(t *testing.T) {
	t.Parallel()
	small := im.ReportData{SuppressResponse: true, Reports: []im.AttributeReport{attr(1, "x")}}
	got, err := reportchunk.Split(small, 200, 400, write, nil)
	if err != nil || len(got) != 1 || !got[0].SuppressResponse {
		t.Fatalf("small report: %+v, %v", got, err)
	}

	long := make([]uint32, 120)
	for i := range long {
		long[i] = uint32(1000 + i)
	}
	rd := im.ReportData{
		SuppressResponse: true,
		Reports: []im.AttributeReport{
			attr(1, strings.Repeat("a", 80)),
			attr(2, long),
			attr(3, strings.Repeat("b", 500)),
			attr(4, strings.Repeat("c", 80)),
		},
		EventReports: []im.EventReport{{Path: im.ConcreteEventPath{Endpoint: 1, Cluster: 6, Event: 0, HasEndpoint: true, HasCluster: true, HasEvent: true}, IsStatus: true, Status: im.StatusIB{Status: im.StatusUnsupportedEvent}}},
	}
	const budget, hardCap = 200, 300
	chunks, err := reportchunk.Split(rd, budget, hardCap, write, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 3 {
		t.Fatalf("%d chunks, want the report split", len(chunks))
	}
	var appends, exhausted, events int
	for i, c := range chunks {
		last := i == len(chunks)-1
		if c.MoreChunkedMessages == last || c.SuppressResponse != last {
			t.Errorf("chunk %d: More %v Suppress %v", i, c.MoreChunkedMessages, c.SuppressResponse)
		}
		if n := encodedSize(t, c); n > budget {
			t.Errorf("chunk %d: %d bytes over the budget of %d", i, n, budget)
		}
		for _, r := range c.Reports {
			if r.Path.ListAppend {
				appends++
			}
			if r.IsStatus && r.Status.Status == im.StatusResourceExhausted && r.Path.Attribute == 3 {
				exhausted++
			}
		}
		events += len(c.EventReports)
	}
	if appends == 0 || exhausted != 1 || events != 1 {
		t.Errorf("appends %d, ResourceExhausted %d, events %d; want some, 1, 1", appends, exhausted, events)
	}
}

// TestChunkerEmpty: a report with no entry is one chunk, the template's.
func TestChunkerEmpty(t *testing.T) {
	t.Parallel()
	c := reportchunk.New(im.ReportData{HasSubscription: true, SubscriptionID: 7, SuppressResponse: true}, 100, 100, write, reportchunk.Slice(nil), nil, nil)
	chunk, ok, err := c.Next()
	if err != nil || !ok || !chunk.HasSubscription || chunk.SubscriptionID != 7 || !chunk.SuppressResponse || chunk.MoreChunkedMessages {
		t.Fatalf("empty report: %+v %v %v", chunk, ok, err)
	}
	if _, ok, _ := c.Next(); ok {
		t.Fatal("a second chunk after the last")
	}
}

// TestReadsPullsLazily: the source over a read's reports pulls them from
// the read as the chunker asks.
func TestReadsPullsLazily(t *testing.T) {
	t.Parallel()
	src := reportchunk.Reads(im.NewReadReports(t.Context(), nil, im.ReadRequest{}))
	if _, ok := src(); ok {
		t.Fatal("a report from a read of no path")
	}
}

// TestApplyFabricScope: a scoped list becomes its members, kept as
// written for a reader of the owning fabric; a value without a scope and
// a status are returned as they are.
func TestApplyFabricScope(t *testing.T) {
	t.Parallel()
	plain := attr(1, "x")
	if got := reportchunk.ApplyFabricScope(plain, write); got.Value.Value != "x" {
		t.Fatalf("an unscoped report changed: %+v", got)
	}
	status := reportchunk.AttributeStatus(attr(1, "x"), im.StatusFailure)
	status.FabricScope = &im.FabricScope{}
	if got := reportchunk.ApplyFabricScope(status, write); !got.IsStatus {
		t.Fatalf("a status changed: %+v", got)
	}
	scoped := attr(2, "not a list")
	scoped.FabricScope = &im.FabricScope{Fabric: 1, Filtered: true}
	if got := reportchunk.ApplyFabricScope(scoped, write); !got.IsStatus || got.Status.Status != im.StatusFailure {
		t.Fatalf("a scope over a value that is no list: %+v, want FAILURE", got)
	}
}

// TestWriter: a List and a Member are written verbatim, anything else by
// the writer it wraps.
func TestWriter(t *testing.T) {
	t.Parallel()
	w := reportchunk.Writer(write)
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	w(enc, tlv.ContextTag(0), im.AttributeValue{Value: reportchunk.List{{0x04, 0x01}, {0x04, 0x02}}})
	w(enc, tlv.ContextTag(1), im.AttributeValue{Value: reportchunk.Member{0x04, 0x03}})
	w(enc, tlv.ContextTag(2), im.AttributeValue{Value: "s"})
	w(enc, tlv.ContextTag(3), im.AttributeValue{IsNull: true})
	_ = enc.EndContainer()
	if _, err := enc.Bytes(); err != nil {
		t.Fatal(err)
	}
}

// TestSplitOversizedListMember: a list one of whose members no message
// carries is answered ResourceExhausted on the attribute's path, never on
// a list item's; many small events are spread over chunks.
func TestSplitOversizedListMember(t *testing.T) {
	t.Parallel()
	big := tlv.NewEncoder()
	big.PutUTF8(tlv.AnonymousTag(), strings.Repeat("z", 400))
	member, err := big.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	rd := im.ReportData{Reports: []im.AttributeReport{attr(5, reportchunk.List{{0x04, 0x01}, member})}}
	for i := range 40 {
		rd.EventReports = append(rd.EventReports, im.EventReport{
			Path:     im.ConcreteEventPath{Endpoint: 1, Cluster: 6, Event: uint32(i), HasEndpoint: true, HasCluster: true, HasEvent: true},
			IsStatus: true, Status: im.StatusIB{Status: im.StatusUnsupportedEvent},
		})
	}
	chunks, err := reportchunk.Split(rd, 200, 300, write, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := chunks[0].Reports[0]
	if !first.IsStatus || first.Status.Status != im.StatusResourceExhausted || first.Path.ListAppend || first.Path.HasListIndex {
		t.Fatalf("first report %+v, want ResourceExhausted on the attribute's path", first)
	}
	events := 0
	for _, c := range chunks {
		events += len(c.EventReports)
	}
	if len(chunks) < 2 || events != 40 {
		t.Fatalf("%d chunks with %d events, want the 40 events spread over several", len(chunks), events)
	}
}

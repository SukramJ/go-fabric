// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"runtime"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// reportHeapFactor is how many heap bytes an interaction may hold for one
// byte charged against the report bound.
const reportHeapFactor = 2

// nullsReport is a ReportData chunk of one attribute whose value is an
// array of n anonymous nulls, one byte each.
func nullsReport(t *testing.T, n int) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	im.ReportData{SubscriptionID: 1, HasSubscription: true, Reports: []im.AttributeReport{{
		Path:        im.ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: 0x4000, HasEndpoint: true, HasCluster: true, HasAttribute: true},
		Value:       im.AttributeValue{Value: struct{}{}},
		DataVersion: 1,
	}}}.MarshalTLV(enc, func(enc *tlv.Encoder, tag tlv.Tag, _ im.AttributeValue) {
		enc.StartArray(tag)
		for range n {
			enc.PutNull(tlv.AnonymousTag())
		}
		_ = enc.EndContainer()
	})
	payload, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func heapAlloc() int64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	return int64(m.HeapAlloc) //nolint:gosec // a heap size fits int64
}

// entriesReport is a ReportData chunk of n attribute reports, each a
// status (status) or a scalar value, of distinct concrete paths.
func entriesReport(t *testing.T, n int, status bool) []byte {
	t.Helper()
	reports := make([]im.AttributeReport, n)
	for i := range reports {
		p := im.ConcreteAttributePath{Endpoint: uint16(i), Cluster: 6, Attribute: uint32(i), HasEndpoint: true, HasCluster: true, HasAttribute: true} //nolint:gosec // a small test count
		if status {
			reports[i] = im.AttributeReport{Path: p, IsStatus: true, Status: im.StatusIB{Status: im.StatusUnsupportedAttribute}}
		} else {
			reports[i] = im.AttributeReport{Path: p, DataVersion: 1}
		}
	}
	enc := tlv.NewEncoder()
	im.ReportData{Reports: reports, MoreChunkedMessages: true}.MarshalTLV(enc, func(e *tlv.Encoder, tag tlv.Tag, _ im.AttributeValue) { e.PutUint(tag, 1) })
	payload, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

// invokeStatuses is an InvokeResponse chunk of n command statuses.
func invokeStatuses(t *testing.T, n int) []byte {
	t.Helper()
	entries := make([]im.InvokeResponseEntry, n)
	for i := range entries {
		entries[i] = im.InvokeResponseEntry{
			Path:     im.ConcreteCommandPath{Endpoint: uint16(i), Cluster: 6, Command: 1, HasEndpoint: true, HasCluster: true, HasCommand: true}, //nolint:gosec // a small test count
			IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess},
			CommandRef: uint16(i), HasCommandRef: true, //nolint:gosec // a small test count
		}
	}
	enc := tlv.NewEncoder()
	im.InvokeResponse{Responses: entries, MoreChunkedMessages: true}.MarshalTLV(enc, func(*tlv.Encoder, tlv.Tag, any) {})
	payload, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

// TestReportChargeBoundsTheHeap: what a chunk is charged against the
// report bound bounds the heap its decoded and kept entries hold, within
// reportHeapFactor — for values of one-byte elements, the shape that holds
// the most heap a payload byte, for scalar values, and for the entries
// that carry no value at all: attribute statuses and command statuses
// (review II, R2-P6-bound-2102, R2-P6-bound-2150, Fable F4).
func TestReportChargeBoundsTheHeap(t *testing.T) { //nolint:paralleltest // measures the heap
	type shape struct {
		name    string
		chunks  int
		payload []byte
		// hold decodes payload, keeps what the interaction keeps, and
		// returns the chunk's charge.
		hold func(t *testing.T, payload []byte) int
	}
	a := newAssembler(nil)
	var entries []im.InvokeResponseEntry
	report := func(t *testing.T, payload []byte) int {
		t.Helper()
		rd, err := decodeReport(payload)
		if err != nil {
			t.Fatal(err)
		}
		a.add(rd)
		return reportCharge(payload, rd)
	}
	perChunk := func(t *testing.T, payload []byte) int { // one assembler a chunk, as round 1 measured
		t.Helper()
		a = newAssembler(nil)
		return report(t, payload)
	}
	var kept []*assembler
	for _, sh := range []shape{
		{"values of 100 nulls", 100, nullsReport(t, 100), func(t *testing.T, p []byte) int { t.Helper(); c := perChunk(t, p); kept = append(kept, a); return c }},
		{"values of 1000 nulls", 100, nullsReport(t, 1000), func(t *testing.T, p []byte) int { t.Helper(); c := perChunk(t, p); kept = append(kept, a); return c }},
		{"scalar values", 2000, entriesReport(t, 60, false), report},
		{"attribute statuses", 2000, entriesReport(t, 60, true), report},
		{"command statuses", 2000, invokeStatuses(t, 60), func(t *testing.T, p []byte) int {
			t.Helper()
			resp, err := im.UnmarshalInvokeResponseTLV(tlv.NewDecoder(p), readFields)
			if err != nil {
				t.Fatal(err)
			}
			entries = append(entries, resp.Responses...)
			return invokeCharge(p, &resp)
		}},
	} {
		a, entries, kept = newAssembler(nil), nil, nil
		charged := 0
		before := heapAlloc()
		for range sh.chunks {
			charged += sh.hold(t, sh.payload)
		}
		held := heapAlloc() - before
		t.Logf("%s: %d chunks of %d bytes held %d heap bytes, charged %d (%.2f a charged byte)",
			sh.name, sh.chunks, len(sh.payload), held, charged, float64(held)/float64(charged))
		if held > reportHeapFactor*int64(charged) {
			t.Errorf("%s: %d heap bytes held for %d charged (%.2f a charged byte), want at most %d",
				sh.name, held, charged, float64(held)/float64(charged), reportHeapFactor)
		}
		runtime.KeepAlive(a)
		runtime.KeepAlive(entries)
		runtime.KeepAlive(kept)
	}
}

// TestInvokeChargeCountsTheFields: a chunk of an InvokeResponse is charged
// its payload and every element of its response commands' fields, as a
// report chunk is (review II, R2-P6-bound-2150).
func TestInvokeChargeCountsTheFields(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	path := im.ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: 1}
	im.InvokeResponse{Responses: []im.InvokeResponseEntry{
		{Path: path, Response: struct{}{}, HasResponse: true},
		{Path: path, IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}},
	}}.MarshalTLV(enc, func(enc *tlv.Encoder, tag tlv.Tag, _ any) {
		enc.StartStruct(tag)
		enc.StartArray(tlv.ContextTag(0))
		for range 1000 {
			enc.PutNull(tlv.AnonymousTag())
		}
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	})
	payload, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := im.UnmarshalInvokeResponseTLV(tlv.NewDecoder(payload), readFields)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := invokeCharge(payload, &resp), len(payload)+1002*decodedNodeBytes+2*invokeEntryBytes; got != want {
		t.Fatalf("charged %d, want %d (the payload, 1002 elements and two entries)", got, want)
	}
}

// TestReportChargeOfScalarAttributes: an attribute report of a scalar
// value is charged at most scalarAttributeCharge bytes, so a read of
// DefaultMaxReportBytesPerSession / scalarAttributeCharge such attributes
// (about 70 000) stays below the default bound, as
// DefaultMaxReportBytesPerSession's doc states.
func TestReportChargeOfScalarAttributes(t *testing.T) {
	t.Parallel()
	const scalarAttributeCharge, attrs = 240, 40
	reports := make([]im.AttributeReport, attrs)
	for i := range reports {
		reports[i] = im.AttributeReport{
			Path:        im.ConcreteAttributePath{Endpoint: uint16(i), Cluster: 6, HasEndpoint: true, HasCluster: true, HasAttribute: true}, //nolint:gosec // a small test count
			DataVersion: 0x12345678,
		}
	}
	enc := tlv.NewEncoder()
	im.ReportData{Reports: reports, MoreChunkedMessages: true}.MarshalTLV(enc, func(e *tlv.Encoder, tag tlv.Tag, _ im.AttributeValue) { e.PutUint(tag, 200) })
	payload, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	rd, err := decodeReport(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got := reportCharge(payload, rd); got > attrs*scalarAttributeCharge {
		t.Fatalf("%d scalar attributes charged %d bytes, want at most %d each", attrs, got, scalarAttributeCharge)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"runtime"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
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

// TestReportChargeBoundsTheHeap: what a report chunk is charged against
// the report bound bounds the heap its decoded and assembled values hold,
// within reportHeapFactor — for values of one-byte elements, the shape
// that holds the most heap a payload byte (review II, R2-P6-bound-2102).
func TestReportChargeBoundsTheHeap(t *testing.T) { //nolint:paralleltest // measures the heap
	for _, n := range []int{100, 1000} { // a smaller value holds too little heap to measure beside the package's other tests
		payload := nullsReport(t, n)
		const chunks = 100
		keep := make([]*assembler, 0, chunks)
		charged := 0
		before := heapAlloc()
		for range chunks {
			rd, err := decodeReport(payload)
			if err != nil {
				t.Fatal(err)
			}
			charged += reportCharge(payload, rd)
			a := newAssembler(nil)
			a.add(rd)
			keep = append(keep, a)
		}
		held := heapAlloc() - before
		t.Logf("n=%d: %d chunks of %d bytes held %d heap bytes, charged %d", n, chunks, len(payload), held, charged)
		if held > reportHeapFactor*int64(charged) {
			t.Errorf("n=%d: %d heap bytes held for %d charged (%.1f a charged byte), want at most %d",
				n, held, charged, float64(held)/float64(charged), reportHeapFactor)
		}
		runtime.KeepAlive(keep)
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
	if got, want := invokeCharge(payload, &resp), len(payload)+1002*decodedNodeBytes; got != want {
		t.Fatalf("charged %d, want %d (the payload and 1002 elements)", got, want)
	}
}

// TestReportChargeOfScalarAttributes: an attribute report of a scalar
// value is charged at most scalarAttributeCharge bytes, so a read of
// DefaultMaxReportBytesPerSession / scalarAttributeCharge such attributes
// (about 15 000) stays below the default bound, as
// DefaultMaxReportBytesPerSession's doc states.
func TestReportChargeOfScalarAttributes(t *testing.T) {
	t.Parallel()
	const scalarAttributeCharge, attrs = 140, 40
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

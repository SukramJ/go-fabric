// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package tlv

// Benchmarks for the codec every other layer sits on. TLV is the one
// package a Matter bridge cannot avoid: each inbound IM message is decoded
// through it and each report chunk is encoded through it, so a regression
// here is a regression on every path at once and shows up nowhere else as
// its own symptom.
//
// The fixtures are built once, outside the timed region, and are shaped like
// the traffic that actually dominates a running bridge — a ReportData chunk
// with many AttributeReport entries — rather than like a single scalar,
// which would measure the per-call overhead of one Put and nothing about the
// container bookkeeping that carries the real cost.

import (
	"errors"
	"io"
	"testing"
)

// benchReportChunkAttributes is the number of AttributeReport entries the
// benchmark fixtures carry. A wildcard Subscribe-Initial over a modest
// bridged fleet fills a chunk with roughly this many entries before the
// datagram limit forces a split, so it is the size the encoder and decoder
// see per message in practice.
const benchReportChunkAttributes = 40

// Sinks. Package-level so the compiler cannot discard the work whose cost
// the benchmark is trying to attribute.
var (
	benchEncodedBytes []byte
	benchElementCount int
	benchValidateErr  error
)

// encodeBenchReportChunk writes a ReportData-shaped TLV stream: an outer
// anonymous structure, an array of per-attribute structures each carrying a
// DataVersion, a nested AttributePathIB list and a value, then the trailing
// SuppressResponse and interaction-model-revision fields. It mirrors the
// shape im.ReportData.MarshalTLV produces without importing im, which would
// pull the benchmark's fixture out of this package's own vocabulary.
func encodeBenchReportChunk(enc *Encoder, attributes int) {
	enc.StartStruct(AnonymousTag())
	enc.PutUint32(ContextTag(0), 0x1234ABCD) // SubscriptionId
	enc.StartArray(ContextTag(1))            // AttributeReports
	for i := range attributes {
		enc.StartStruct(AnonymousTag())
		enc.StartStruct(ContextTag(1)) // AttributeDataIB
		enc.PutUint32(ContextTag(0), 1)
		enc.StartList(ContextTag(1)) // AttributePathIB
		enc.PutUint(ContextTag(2), uint64(i%64)+2)
		enc.PutUint(ContextTag(3), 0x0402)
		enc.PutUint(ContextTag(4), uint64(i%16))
		_ = enc.EndContainer()
		enc.PutInt(ContextTag(2), int64(1800+i))
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	enc.PutBool(ContextTag(4), false)
	enc.PutUint(ContextTag(0xFF), 12)
	_ = enc.EndContainer()
}

// benchReportChunkBytes returns the encoded fixture the decode benchmarks
// read. Built once per benchmark, before the timer starts.
func benchReportChunkBytes(tb testing.TB) []byte {
	tb.Helper()
	enc := NewEncoder()
	encodeBenchReportChunk(enc, benchReportChunkAttributes)
	buf, err := enc.Bytes()
	if err != nil {
		tb.Fatalf("encode fixture: %v", err)
	}
	return buf
}

// BenchmarkEncodeReportChunk measures building one report-sized TLV stream
// from scratch, the way the bridge builds every outbound chunk: a fresh
// Encoder per message, so the growth of its internal buffer is part of what
// is measured. The allocation count is the number worth watching — the
// encoder appends into one slice, and a change that makes it reallocate per
// element would be invisible in wall time on a small payload but linear in
// garbage on a full fleet report.
func BenchmarkEncodeReportChunk(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		enc := NewEncoder()
		encodeBenchReportChunk(enc, benchReportChunkAttributes)
		out, err := enc.Bytes()
		if err != nil {
			b.Fatalf("Bytes: %v", err)
		}
		benchEncodedBytes = out
	}
}

// BenchmarkDecodeReportChunk measures a full element walk over a
// report-sized payload. Every inbound IM message is consumed exactly this
// way — Next() in a loop until io.EOF, with the caller tracking container
// depth — so this is the decode cost the receive path pays per datagram.
func BenchmarkDecodeReportChunk(b *testing.B) {
	buf := benchReportChunkBytes(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		dec := NewDecoder(buf)
		n := 0
		for {
			_, err := dec.Next()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				b.Fatalf("Next: %v", err)
			}
			n++
		}
		benchElementCount = n
	}
}

// BenchmarkValidateReportChunk measures the strict tag/container check the
// bridge runs on its own bytes before sending a subscription report
// (bridge/subscribe_dispatch.go). It is a second full decode of every chunk
// on the outbound path, so its cost is additive to the encode above and
// belongs in the same picture.
func BenchmarkValidateReportChunk(b *testing.B) {
	buf := benchReportChunkBytes(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchValidateErr = Validate(buf)
	}
	if benchValidateErr != nil {
		b.Fatalf("Validate: %v", benchValidateErr)
	}
}

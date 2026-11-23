// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package reportchunk_test

import (
	"strconv"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/reportchunk"
	"thingfabric.dev/go-fabric/tlv"
)

// tcpBudget is the order of the chunk budget over TCP
// (bridge/reply.go), where one chunk holds thousands of small reports.
const tcpBudget = 63800

// countingWrite encodes a uint64 value and counts its calls.
func countingWrite(calls *int) im.AttributeValueWriter {
	return func(enc *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
		*calls++
		if u, ok := v.Value.(uint64); ok {
			enc.PutUint(tag, u)
			return
		}
		if l, ok := v.Value.([]uint32); ok {
			enc.StartArray(tag)
			for _, n := range l {
				enc.PutUint(tlv.AnonymousTag(), uint64(n))
			}
			_ = enc.EndContainer()
			return
		}
		enc.PutNull(tag)
	}
}

func smallReports(n int) []im.AttributeReport {
	reps := make([]im.AttributeReport, n)
	for i := range reps {
		reps[i] = im.AttributeReport{
			Path:  im.ConcreteAttributePath{Endpoint: uint16(i / 100), Cluster: 6, Attribute: uint32(i % 100), HasEndpoint: true, HasCluster: true, HasAttribute: true},
			Value: im.AttributeValue{Value: uint64(i)},
		}
	}
	return reps
}

func smallEvents(n int) []im.EventReport {
	evs := make([]im.EventReport, n)
	for i := range evs {
		evs[i] = im.EventReport{
			Path:   im.ConcreteEventPath{Endpoint: 1, Cluster: 0x3B, Event: uint32(i % 3), HasEndpoint: true, HasCluster: true, HasEvent: true},
			Number: uint64(1000 + i), Priority: im.EventPriorityInfo, Timestamp: 1_700_000_000_000 + uint64(i)*1000,
			Data: im.AttributeValue{Value: uint64(i)},
		}
	}
	return evs
}

func drain(tb testing.TB, c *reportchunk.Chunker) []im.ReportData {
	tb.Helper()
	var out []im.ReportData
	for {
		chunk, ok, err := c.Next()
		if err != nil {
			tb.Fatal(err)
		}
		if !ok {
			return out
		}
		out = append(out, chunk)
	}
}

// TestChunkerEncodesEachReportAConstantNumberOfTimes (R2-C2-bound-0555):
// the greedy fill keeps a running size and encodes each report a bounded
// number of times, however many reports a chunk holds — at the TCP budget
// a chunk holds thousands, and re-encoding the chunk per report made the
// cost grow with their square.
func TestChunkerEncodesEachReportAConstantNumberOfTimes(t *testing.T) {
	t.Parallel()
	const n = 4000
	calls := 0
	c := reportchunk.New(im.ReportData{HasSubscription: true, SubscriptionID: 1}, tcpBudget, tcpBudget, countingWrite(&calls),
		reportchunk.Slice(smallReports(n)), func() []im.EventReport { return smallEvents(n) })
	chunks := drain(t, c)
	if len(chunks) < 2 {
		t.Fatalf("%d chunks; the test needs several", len(chunks))
	}
	if perEntry := float64(calls) / (2 * n); perEntry > 4 {
		t.Errorf("%d value writes for %d entries (%.1f per entry), want a constant few", calls, 2*n, perEntry)
	}
}

// TestChunkerFillIsTheExactGreedyFill holds the running size to what an
// encode of the chunk measures: every chunk but its own lone first entry
// fits the budget, and the next chunk's first entry would not have fit —
// the chunk boundaries of the fill that encoded the whole candidate per
// report — over attributes, events, a split list and statuses, at several
// budgets.
func TestChunkerFillIsTheExactGreedyFill(t *testing.T) {
	t.Parallel()
	list := make([]uint32, 300)
	for i := range list {
		list[i] = uint32(i * 7919)
	}
	reps := smallReports(500)
	reps = append(reps[:250], append([]im.AttributeReport{
		{Path: im.ConcreteAttributePath{Endpoint: 3, Cluster: 0x1F, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}, Value: im.AttributeValue{Value: list}},
		{Path: im.ConcreteAttributePath{Endpoint: 3, Cluster: 0x1F, Attribute: 9, HasEndpoint: true, HasCluster: true, HasAttribute: true}, IsStatus: true, Status: im.StatusIB{Status: im.StatusUnsupportedAttribute}},
	}, reps[250:]...)...)
	for _, budget := range []int{200, 1032, 4096, tcpBudget} {
		t.Run(strconv.Itoa(budget), func(t *testing.T) {
			t.Parallel()
			calls := 0
			write := reportchunk.Writer(countingWrite(&calls))
			c := reportchunk.New(im.ReportData{HasSubscription: true, SubscriptionID: 7}, budget, 1280, countingWrite(&calls),
				reportchunk.Slice(reps), func() []im.EventReport { return smallEvents(300) })
			chunks := drain(t, c)
			size := func(rd im.ReportData) int {
				rd.MoreChunkedMessages, rd.SuppressResponse = false, false
				b, err := reportchunk.Encode(rd, write)
				if err != nil {
					t.Fatal(err)
				}
				return len(b)
			}
			entries := func(rd im.ReportData) int { return len(rd.Reports) + len(rd.EventReports) }
			total := 0
			for i, ch := range chunks {
				total += entries(ch)
				if entries(ch) > 1 && size(ch) > budget {
					t.Errorf("chunk %d: %d bytes, budget %d", i, size(ch), budget)
				}
				if i+1 == len(chunks) {
					continue
				}
				next := chunks[i+1]
				grown := ch
				if len(next.Reports) > 0 {
					grown.Reports = append(append([]im.AttributeReport(nil), ch.Reports...), next.Reports[0])
				} else {
					grown.EventReports = append(append([]im.EventReport(nil), ch.EventReports...), next.EventReports[0])
				}
				if size(grown) <= budget {
					t.Errorf("chunk %d closed at %d bytes though the next entry fit (%d bytes with it, budget %d)", i, size(ch), size(grown), budget)
				}
			}
			if total < len(reps)+300 {
				t.Errorf("%d entries across the chunks, want at least the %d given", total, len(reps)+300)
			}
		})
	}
}

// BenchmarkChunkerTCPBudget is the chunker's cost per report at the TCP
// budget (R2-C2-bound-0555).
func BenchmarkChunkerTCPBudget(b *testing.B) {
	reps := smallReports(20000)
	calls := 0
	write := countingWrite(&calls)
	b.ReportAllocs()
	for b.Loop() {
		drain(b, reportchunk.New(im.ReportData{HasSubscription: true, SubscriptionID: 1}, tcpBudget, tcpBudget, write, reportchunk.Slice(reps), nil))
	}
}

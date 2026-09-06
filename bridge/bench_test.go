// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// Benchmarks for the report-data path — the work a bridge does most of once
// a controller is paired.
//
// buildInitialReport is the function a wildcard Subscribe lands in: it runs
// every requested path through the dispatcher, authorizes each result,
// evaluates the DataVersion filters, sorts the report and formats one
// diagnostic line per attribute. Apple re-subscribes on every reconnect —
// app foreground, network change, controller restart — so this is not a
// once-per-pairing cost, and it scales with the fleet rather than with the
// request: one wildcard path over a 30-endpoint bridge produces a report
// entry per attribute of every endpoint.
//
// It lives in package bridge because that is where the function lives; the
// fixture is the same populated topology the chunking tests use, so the
// benchmark and those tests measure the same shape.
//
// Everything expensive — the UDP listener, the assembly, the subscribe
// request — is built before the timer starts. The timed loop calls only
// buildInitialReport, which sends nothing over the wire.

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

// Sinks. Package-level so the report cannot be optimised away.
var (
	benchReport      im.ReportData
	benchMatched     int
	benchReportBytes []byte
)

// newBenchBridge is newStartedBridge for a benchmark: same construction,
// but on testing.TB and with a discarding logger so log formatting never
// lands in the timed region. It is a separate helper rather than a widened
// signature on the test one so the tests keep their default logger.
func newBenchBridge(tb testing.TB, snap Snapshotter) *Bridge {
	tb.Helper()
	b, err := New(
		NewFakeStore(),
		snap,
		nil,
		Config{
			Listen:    ":0",
			VendorID:  0x1234,
			ProductID: 0x5678,
			NodeLabel: "bench",
		},
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		tb.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		tb.Fatalf("Start: %v", err)
	}
	tb.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = b.Stop(stopCtx)
	})
	return b
}

// BenchmarkBuildInitialReportWildcard measures the Subscribe-Initial
// assembly for the request shape Apple Home sends: a single fully-wildcard
// attribute path, which expands to every attribute of every endpoint. That
// expansion is the reason the cost belongs to the fleet size and not to the
// request, and it is what a regression in the dispatcher, the authorization
// gate or the per-attribute diagnostic loop would show up in first.
func BenchmarkBuildInitialReportWildcard(b *testing.B) {
	br := newBenchBridge(b, manyTempSensorsSnapshotterForTest())
	dispatcher := br.Dispatcher()
	if dispatcher == nil {
		b.Fatal("dispatcher nil after Start — topology not assembled")
	}
	req := im.SubscribeRequest{
		AttributeRequests: []im.ConcreteAttributePath{{}}, // no Has* flag set = wildcard on every level
	}
	ctx := context.Background()
	// One warm-up outside the timed region: the first call populates the
	// per-endpoint DataVersion trackers the dispatcher lazily creates, and
	// a running bridge answers its second subscribe against warm ones.
	if _, matched := br.buildInitialReport(ctx, dispatcher, req); matched == 0 {
		b.Fatal("wildcard subscribe matched no paths — fixture assembles no reportable attributes")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		report, matched := br.buildInitialReport(ctx, dispatcher, req)
		benchReport, benchMatched = report, matched
	}
}

// BenchmarkEncodeReportData measures the other half of the report path: the
// TLV serialisation the bridge runs on every report it sends, through the
// production attribute-value writer rather than a stand-in. Assembly and
// encoding are separately regressible — one is dispatcher and authorization
// work, the other is codec and value-shape work — so they are measured
// apart, over the same report a wildcard Subscribe-Initial produces.
func BenchmarkEncodeReportData(b *testing.B) {
	br := newBenchBridge(b, manyTempSensorsSnapshotterForTest())
	dispatcher := br.Dispatcher()
	if dispatcher == nil {
		b.Fatal("dispatcher nil after Start — topology not assembled")
	}
	report, matched := br.buildInitialReport(context.Background(), dispatcher, im.SubscribeRequest{
		AttributeRequests: []im.ConcreteAttributePath{{}},
	})
	if matched == 0 {
		b.Fatal("wildcard subscribe matched no paths — fixture assembles no reportable attributes")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		buf, err := EncodeReportData(report)
		if err != nil {
			b.Fatalf("EncodeReportData: %v", err)
		}
		benchReportBytes = buf
	}
}

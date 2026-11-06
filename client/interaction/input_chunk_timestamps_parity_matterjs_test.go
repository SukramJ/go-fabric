// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

// A port of the "resolved timestamps" describe matter.js #4832 added to
// packages/protocol/test/action/client/InputChunkTest.ts at db021da0
// (:613-746): a delta timestamp resolves against the event directly
// before it in the same ReportData message when that event used the same
// clock (InputChunk.ts:360-405 EventTimestampChain); the chain never spans
// messages. matter.js skips an entry whose payload fails to decode and
// keeps the chain; the Go assembler keeps every payload as a node tree, so
// that case is ported as the chain running through an entry whose payload
// is of an unexpected type.

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// tsVariant is one of the four wire timestamp variants, or none.
type tsVariant struct {
	epoch, system, deltaEpoch, deltaSystem *uint64
}

func tsEvent(number uint64, v tsVariant, path im.ConcreteEventPath) im.EventReport {
	r := &im.ReceivedEventData{HasData: true}
	ev := im.EventReport{Path: path, Number: number, Priority: 1, Received: r, Data: im.AttributeValue{Value: uintNode(1)}}
	switch {
	case v.epoch != nil:
		ev.Timestamp, r.HasEpochTimestamp = *v.epoch, true
	case v.system != nil:
		r.SystemTimestamp, r.HasSystemTimestamp = *v.system, true
	case v.deltaEpoch != nil:
		r.DeltaEpochTimestamp, r.HasDeltaEpochTimestamp = *v.deltaEpoch, true
	case v.deltaSystem != nil:
		r.DeltaSystemTimestamp, r.HasDeltaSystemTimestamp = *v.deltaSystem, true
	}
	return ev
}

func sys(v uint64) tsVariant    { return tsVariant{system: &v} }
func epoch(v uint64) tsVariant  { return tsVariant{epoch: &v} }
func dsys(v uint64) tsVariant   { return tsVariant{deltaSystem: &v} }
func depoch(v uint64) tsVariant { return tsVariant{deltaEpoch: &v} }

// resolvedOf assembles one ReportData message per slice and returns the
// resolved timestamps of its events, -1 for an unresolved one.
func resolvedOf(t *testing.T, messages ...[]im.EventReport) []int64 {
	t.Helper()
	rds := make([]*im.ReportData, 0, len(messages))
	for _, m := range messages {
		rds = append(rds, &im.ReportData{EventReports: m})
	}
	var out []int64
	for _, ev := range icAssemble(t, nil, rds...).report.Events {
		if ev.ResolvedTimestamp == nil {
			out = append(out, -1)
			continue
		}
		out = append(out, int64(*ev.ResolvedTimestamp)) //nolint:gosec // test values
	}
	return out
}

func TestInputChunkResolvedTimestampsParity(t *testing.T) {
	t.Parallel()
	p := icEventPath(0, icBasic, icStartUp)
	check := func(t *testing.T, got, want []int64) {
		t.Helper()
		if !slices.Equal(got, want) {
			t.Fatalf("resolved %v, want %v", got, want)
		}
	}
	t.Run("adds each system delta to the event before it, as the CHIP SDK encodes a report", func(t *testing.T) {
		t.Parallel()
		const base = 0x4364c617
		deltas := []uint64{1, 4, 1, 3, 1, 4, 1}
		evs := []im.EventReport{tsEvent(1, sys(base), p)}
		want := []int64{base}
		for i, d := range deltas {
			evs = append(evs, tsEvent(uint64(i)+2, dsys(d), p)) //nolint:gosec // test index
			want = append(want, want[len(want)-1]+int64(d))     //nolint:gosec // test values
		}
		a := icAssemble(t, nil, &im.ReportData{EventReports: evs})
		got := make([]int64, 0, len(want))
		for _, ev := range a.report.Events {
			got = append(got, int64(*ev.ResolvedTimestamp)) //nolint:gosec // test values
		}
		check(t, got, want)
		last := a.report.Events[len(a.report.Events)-1]
		if last.DeltaSystemTimestamp == nil || *last.DeltaSystemTimestamp != 1 || last.Timestamp != 1 {
			t.Fatalf("the last event's wire variant %+v", last)
		}
	})
	t.Run("resolves epoch deltas against the prior epoch timestamp", func(t *testing.T) {
		t.Parallel()
		check(t, resolvedOf(t, []im.EventReport{tsEvent(1, epoch(1_700_000_000_000), p), tsEvent(2, depoch(250), p)}),
			[]int64{1_700_000_000_000, 1_700_000_000_250})
	})
	t.Run("leaves a delta unresolved when no absolute timestamp precedes it in the message", func(t *testing.T) {
		t.Parallel()
		check(t, resolvedOf(t, []im.EventReport{tsEvent(1, dsys(5), p), tsEvent(2, dsys(7), p)}), []int64{-1, -1})
	})
	t.Run("leaves a delta unresolved when the prior event used the other clock", func(t *testing.T) {
		t.Parallel()
		check(t, resolvedOf(t, []im.EventReport{
			tsEvent(1, sys(1000), p), tsEvent(2, epoch(1_700_000_000_000), p), tsEvent(3, dsys(5), p), tsEvent(4, dsys(7), p),
		}), []int64{1000, 1_700_000_000_000, -1, -1})
	})
	t.Run("does not skip back over an unresolved delta to an older absolute timestamp", func(t *testing.T) {
		t.Parallel()
		check(t, resolvedOf(t, []im.EventReport{tsEvent(1, sys(1000), p), tsEvent(2, depoch(5), p), tsEvent(3, dsys(7), p)}),
			[]int64{1000, -1, -1})
	})
	t.Run("does not skip back over an event without a timestamp", func(t *testing.T) {
		t.Parallel()
		check(t, resolvedOf(t, []im.EventReport{tsEvent(1, sys(1000), p), tsEvent(2, tsVariant{}, p), tsEvent(3, dsys(7), p)}),
			[]int64{1000, -1, -1})
	})
	t.Run("keeps the chain across an entry with an invalid path", func(t *testing.T) {
		t.Parallel()
		invalid := im.ConcreteEventPath{Endpoint: 0, Event: icStartUp, HasEndpoint: true, HasEvent: true}
		check(t, resolvedOf(t, []im.EventReport{tsEvent(1, sys(1000), p), tsEvent(2, dsys(5), invalid), tsEvent(3, dsys(7), p)}),
			[]int64{1000, 1012})
	})
	t.Run("keeps the chain across an entry whose payload fails to decode", func(t *testing.T) {
		t.Parallel()
		odd := tsEvent(2, dsys(5), p)
		odd.Data = im.AttributeValue{Value: spec.Node{El: tlv.Element{Type: tlv.TypeBoolTrue}}}
		check(t, resolvedOf(t, []im.EventReport{tsEvent(1, sys(1000), p), odd, tsEvent(3, dsys(7), p)}),
			[]int64{1000, 1005, 1012})
	})
	t.Run("starts each report message without a prior timestamp", func(t *testing.T) {
		t.Parallel()
		check(t, resolvedOf(t, []im.EventReport{tsEvent(1, sys(1000), p)}, []im.EventReport{tsEvent(2, dsys(5), p)}),
			[]int64{1000, -1})
	})
}

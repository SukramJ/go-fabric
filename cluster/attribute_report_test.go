// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cluster_test

import (
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
)

var _ contract.AttributeReportNotifier = (*cluster.AttributeChanges)(nil)

// TestAttributeChangesTwoViews: NotifyFrom tells the change and the report
// listeners, ChangedFrom the change listeners alone, ReportFrom the report
// listeners alone — the host's `$Changed` view and the wire's `online`
// view of matter.js's events (packages/node/src/behavior/Events.ts:228-236,
// :267-297) — in registration order across both, with the origin.
func TestAttributeChangesTwoViews(t *testing.T) {
	t.Parallel()
	var c cluster.AttributeChanges
	var got []string
	record := func(name string) func(contract.Change) {
		return func(ch contract.Change) { got = append(got, name+ch.Origin.String()) }
	}
	unChange := c.OnAttributeChange(record("change:"))
	unReport := c.OnAttributeReport(record("report:"))
	c.NotifyFrom(contract.OriginHostSet, 1)
	c.ChangedFrom(contract.OriginCommand, 1)
	c.ReportFrom(contract.OriginControllerWrite, 1)
	c.ChangedFrom(contract.OriginCommand) // an empty list tells nobody
	unChange()
	unReport()
	unReport()
	c.NotifyFrom(contract.OriginHostSet, 2)
	want := []string{"change:host set", "report:host set", "change:command", "report:controller write"}
	if !slices.Equal(got, want) {
		t.Errorf("calls %v, want %v", got, want)
	}
}

// TestQuieterReportsAtOnceWhenTheClockMovedBack: a last report later than
// the clock's now — a device's servers moving from the system clock to the
// node's — is long past: the next change is reported at once, not after
// the distance between the clocks.
func TestQuieterReportsAtOnceWhenTheClockMovedBack(t *testing.T) {
	t.Parallel()
	later := clocktest.New(time.Unix(2000, 0))
	earlier := clocktest.New(time.Unix(1000, 0))
	clk := &switchable{Fake: later}
	reports := 0
	q := &cluster.Quieter{Report: func() { reports++ }, Clock: clk}
	q.Changed(false, false) // the first: at once, at 2000 s
	clk.Fake = earlier      // the clock moves back by 1000 s
	q.Changed(false, false)
	if reports != 2 {
		t.Errorf("%d reports, want 2: the change after the clock moved back at once", reports)
	}
}

// switchable is a clock whose fake can be swapped, as a device's moves
// from the system clock to its node's.
type switchable struct{ *clocktest.Fake }

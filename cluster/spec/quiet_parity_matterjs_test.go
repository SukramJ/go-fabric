// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/cluster/spec/powersource"
	"thingfabric.dev/go-fabric/contract"
)

// TestQuieterAttributeReportsThroughItsThrottleParityMatterJS: PowerSource's
// BatPercentRemaining is quieter with a ten-second interval
// (PowerSourceServer.ts:25-34 at b25ed960, the definition's QuietInterval).
// Its first change is reported at once; three more inside the interval
// reach the change listeners at once and the wire once, when the interval
// has passed. Each stored change advances the DataVersion at the change —
// matter.js advances it at the commit for every noted property
// (Datasource.ts:1446-1457) and on the quiet emit only for a dynamic one
// (ServerBehaviorBacking.ts:259 → Datasource.ts:679-688) — so the emit adds
// none; a Source-computed change (MarkQuietChanged) advances it at the
// emit. MatterQuiesce drops a held report (R2-P5-apispec-03,
// F-db021da0-22).
func TestQuieterAttributeReportsThroughItsThrottleParityMatterJS(t *testing.T) {
	t.Parallel()
	if a := powersource.Definition.Attribute(powersource.AttrBatPercentRemaining); a.QuietInterval != 10*time.Second {
		t.Fatalf("BatPercentRemaining QuietInterval %v, want 10s", a.QuietInterval)
	}
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	srv, err := spec.NewServer(powersource.Definition, spec.Options{Features: uint32(powersource.FeatureBattery), Attributes: []uint32{powersource.AttrBatPercentRemaining}}, spec.ServerConfig{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	var changes, reports int
	srv.OnAttributeChange(func(contract.Change) { changes++ })
	srv.OnAttributeReport(func(contract.Change) { reports++ })
	v0 := srv.MatterDataVersion()
	set := func(p uint8) {
		t.Helper()
		if err := srv.Set(powersource.AttrBatPercentRemaining, p); err != nil {
			t.Fatal(err)
		}
	}
	set(200)
	if changes != 1 || reports != 1 || srv.MatterDataVersion() != v0+1 {
		t.Fatalf("first change: %d changes, %d reports, version +%d; want 1, 1, +1", changes, reports, srv.MatterDataVersion()-v0)
	}
	clk.Advance(time.Second)
	set(198)
	set(196)
	set(194)
	if changes != 4 || reports != 1 || srv.MatterDataVersion() != v0+4 {
		t.Fatalf("inside the interval: %d changes, %d reports, version +%d; want 4, 1, +4 (one per stored change)", changes, reports, srv.MatterDataVersion()-v0)
	}
	clk.Advance(9 * time.Second)
	if reports != 2 || srv.MatterDataVersion() != v0+4 {
		t.Fatalf("after the interval: %d reports, version +%d; want 2, +4 (the emit adds none)", reports, srv.MatterDataVersion()-v0)
	}
	if err := srv.MarkChanged(powersource.AttrBatPercentRemaining); !errors.Is(err, spec.ErrNotReportable) {
		t.Errorf("MarkChanged of a quieter attribute: %v", err)
	}
	clk.Advance(time.Second)
	if err := srv.MarkQuietChanged(powersource.AttrBatPercentRemaining); err != nil {
		t.Fatal(err)
	}
	if changes != 5 || reports != 2 || srv.MatterDataVersion() != v0+4 {
		t.Fatalf("MarkQuietChanged inside the interval: %d changes, %d reports, version +%d; want 5, 2, +4", changes, reports, srv.MatterDataVersion()-v0)
	}
	clk.Advance(10 * time.Second)
	if reports != 3 || srv.MatterDataVersion() != v0+5 {
		t.Fatalf("MarkQuietChanged's emit: %d reports, version +%d; want 3, +5 (a computed change advances at the emit)", reports, srv.MatterDataVersion()-v0)
	}
	clk.Advance(time.Second)
	set(190)
	srv.MatterQuiesce()
	clk.Advance(time.Minute)
	if reports != 3 || srv.MatterDataVersion() != v0+6 {
		t.Errorf("after MatterQuiesce: %d reports, version +%d; want the held report dropped, the change's advance kept", reports, srv.MatterDataVersion()-v0)
	}
	if err := srv.MarkQuietChanged(powersource.AttrBatChargeLevel); !errors.Is(err, spec.ErrNotReportable) {
		t.Errorf("MarkQuietChanged of an attribute without Q: %v", err)
	}
}

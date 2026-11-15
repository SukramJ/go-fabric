// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/contract"
)

// TestQuieterAttributeReportsThroughItsThrottleParityMatterJS: PowerSource's
// BatPercentRemaining is quieter with a ten-second interval
// (PowerSourceServer.ts:25-34 at db021da0, the definition's QuietInterval).
// Its first change is reported at once; three more inside the interval
// reach the change listeners at once and the wire once, with one data
// version advance, when the interval has passed — matter.js advances the
// version and broadcasts a quieter property on the quiet emit
// (ServerBehaviorBacking.ts:230-262, :259). MarkQuietChanged takes the
// same throttle, and MatterQuiesce drops a held report (R2-P5-apispec-03,
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
	if changes != 4 || reports != 1 || srv.MatterDataVersion() != v0+1 {
		t.Fatalf("inside the interval: %d changes, %d reports, version +%d; want 4, 1, +1", changes, reports, srv.MatterDataVersion()-v0)
	}
	clk.Advance(9 * time.Second)
	if reports != 2 || srv.MatterDataVersion() != v0+2 {
		t.Fatalf("after the interval: %d reports, version +%d; want 2, +2", reports, srv.MatterDataVersion()-v0)
	}
	if err := srv.MarkChanged(powersource.AttrBatPercentRemaining); !errors.Is(err, spec.ErrNotReportable) {
		t.Errorf("MarkChanged of a quieter attribute: %v", err)
	}
	clk.Advance(time.Second)
	if err := srv.MarkQuietChanged(powersource.AttrBatPercentRemaining); err != nil {
		t.Fatal(err)
	}
	if changes != 5 || reports != 2 {
		t.Fatalf("MarkQuietChanged inside the interval: %d changes, %d reports; want 5, 2", changes, reports)
	}
	srv.MatterQuiesce()
	clk.Advance(time.Minute)
	if reports != 2 || srv.MatterDataVersion() != v0+2 {
		t.Errorf("after MatterQuiesce: %d reports, version +%d; want the held report dropped", reports, srv.MatterDataVersion()-v0)
	}
	if err := srv.MarkQuietChanged(powersource.AttrBatChargeLevel); !errors.Is(err, spec.ErrNotReportable) {
		t.Errorf("MarkQuietChanged of an attribute without Q: %v", err)
	}
}

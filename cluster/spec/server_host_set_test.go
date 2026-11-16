// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	eem "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	tm "github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
)

// TestSetAttributesJudgesBoundsAgainstItsOwnPatch holds a host's
// SetAttributes to the state its own patch makes: a value whose bound names
// a sibling ("minMeasuredValue to maxMeasuredValue") is judged against the
// sibling set in the same call, not the stored one. Before, a coherent new
// range was refused because MeasuredValue was judged against the old range
// (review II, R2-P6-fsm-1303), while NewServer judged the same values with
// every sibling in place.
func TestSetAttributesJudgesBoundsAgainstItsOwnPatch(t *testing.T) {
	t.Parallel()
	newSrv := func(t *testing.T) *spec.Server {
		t.Helper()
		s, err := spec.NewServer(tm.Definition, spec.Options{}, spec.ServerConfig{Initial: map[uint32]any{
			tm.AttrMinMeasuredValue: int16(0), tm.AttrMaxMeasuredValue: int16(50), tm.AttrMeasuredValue: int16(20),
		}})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	// A coherent new state the stored range would refuse.
	s := newSrv(t)
	if err := s.SetAttributes(map[uint32]any{tm.AttrMinMeasuredValue: int16(100), tm.AttrMaxMeasuredValue: int16(200), tm.AttrMeasuredValue: int16(150)}); err != nil {
		t.Errorf("SetAttributes{min 100, max 200, measured 150} over {0, 50, 20}: %v, want nil", err)
	}
	if v, _ := s.Value(tm.AttrMeasuredValue); v != int16(150) {
		t.Errorf("MeasuredValue = %v, want 150", v)
	}

	// An incoherent new state the stored range would admit: judged as
	// NewServer judges it, against the patch's own siblings.
	s = newSrv(t)
	err := s.SetAttributes(map[uint32]any{tm.AttrMinMeasuredValue: int16(100), tm.AttrMaxMeasuredValue: int16(200), tm.AttrMeasuredValue: int16(20)})
	if !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("SetAttributes{min 100, max 200, measured 20}: %v, want ErrInvalidValue", err)
	}
	if v, _ := s.Value(tm.AttrMinMeasuredValue); v != int16(0) {
		t.Errorf("a refused patch stored MinMeasuredValue %v, want the old 0", v)
	}
	if _, err := spec.NewServer(tm.Definition, spec.Options{}, spec.ServerConfig{Initial: map[uint32]any{
		tm.AttrMinMeasuredValue: int16(100), tm.AttrMaxMeasuredValue: int16(200), tm.AttrMeasuredValue: int16(20),
	}}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("the same state as Initial: %v, want ErrInvalidValue", err)
	}

	// A single Set is still judged against the stored siblings.
	s = newSrv(t)
	if err := s.Set(tm.AttrMeasuredValue, int16(51)); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("Set(MeasuredValue 51) in {0, 50}: %v, want ErrInvalidValue", err)
	}
}

// TestIntegerBoundsCompareExactly holds a 64-bit value to a bound beyond
// 2^53 exactly: "0 to 2^62" on ElectricalEnergyMeasurement's served
// CumulativeEnergyImported admits 2^62 and refuses 2^62 + 1. Before, the
// bound was compared as float64 and every value up to 2^62 + 512 passed
// (review II, R2-P6-par-1306).
func TestIntegerBoundsCompareExactly(t *testing.T) {
	t.Parallel()
	s, err := spec.NewServer(eem.Definition, spec.Options{Features: uint32(eem.FeatureImportedEnergy | eem.FeatureCumulativeEnergy)}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		energy int64
		ok     bool
	}{
		{0, true},
		{1 << 62, true},
		{1<<62 - 1, true},
		{1<<62 + 1, false},
		{1<<62 + 512, false},
		{1<<62 + 513, false},
		{-1, false},
	} {
		err := s.Set(eem.AttrCumulativeEnergyImported, eem.EnergyMeasurementStruct{Energy: c.energy})
		if (err == nil) != c.ok {
			t.Errorf("Energy %d against \"0 to 2^62\": %v, want accepted %v", c.energy, err, c.ok)
		}
	}
}

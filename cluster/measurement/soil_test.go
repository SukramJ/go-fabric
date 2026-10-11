// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	soildef "github.com/SukramJ/go-fabric/cluster/spec/soilmeasurement"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
)

// soilStub is a SoilMoistureSource whose reading the test moves.
type soilStub struct {
	value    float64
	observed bool
	limits   soildef.MeasurementAccuracyStruct
}

func (s *soilStub) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementSoilMoisture
}
func (s *soilStub) MatterFloatValue() (float64, bool) { return s.value, s.observed }
func (s *soilStub) MatterSoilMoistureLimits() soildef.MeasurementAccuracyStruct {
	return s.limits
}

// demoSoilLimits are the all-clusters app's limits
// (examples/all-clusters-app/all-clusters-common/src/soil-measurement-stub.cpp
// :30-41 at the harness pin), narrowed to 10..90 so a reading can fall
// outside them on both sides.
func demoSoilLimits() soildef.MeasurementAccuracyStruct {
	pm := uint16(10)
	return soildef.MeasurementAccuracyStruct{
		MeasurementType:  soildef.MeasurementTypeSoilMoisture,
		Measured:         true,
		MinMeasuredValue: 10,
		MaxMeasuredValue: 90,
		AccuracyRanges:   []soildef.MeasurementAccuracyRangeStruct{{RangeMin: 10, RangeMax: 90, PercentMax: &pm}},
	}
}

func TestSoilMeasurementReadsLimitsAsGiven(t *testing.T) {
	t.Parallel()
	src := &soilStub{limits: demoSoilLimits()}
	srv := NewSoilMeasurementServer(src)
	v, ok := srv.MatterRead(soildef.AttrSoilMoistureMeasurementLimits)
	got, isStruct := v.(soildef.MeasurementAccuracyStruct)
	if !ok || !isStruct || got.MinMeasuredValue != 10 || got.MaxMeasuredValue != 90 || len(got.AccuracyRanges) != 1 {
		t.Fatalf("limits = %#v (%v)", v, ok)
	}
	// The value encodes and decodes through the generated attribute codec.
	if _, err := soildef.Definition.Attribute(soildef.AttrSoilMoistureMeasurementLimits).Decode(spectest.Decode(t, spectest.Encode(t, got))); err != nil {
		t.Errorf("limits do not decode: %v", err)
	}
	if srv.MatterClusterID() != 0x0430 {
		t.Errorf("cluster id = 0x%04X", srv.MatterClusterID())
	}
	if rev, _ := srv.MatterRead(cluster.AttrGlobalClusterRevision); rev != soildef.Revision {
		t.Errorf("ClusterRevision = %v", rev)
	}
	if _, ok := srv.MatterRead(0x0002); ok {
		t.Error("an unknown attribute read succeeded")
	}
}

// TestSoilMeasurementMeasuredValueFollowsChipBounds walks the rules of
// SoilMeasurementCluster.cpp: null at start (:30), a reading inside the
// limits stored (:68), one outside refused and the previous value kept
// (:63-65), null again when the source loses its reading.
func TestSoilMeasurementMeasuredValueFollowsChipBounds(t *testing.T) {
	t.Parallel()
	src := &soilStub{limits: demoSoilLimits()}
	srv := NewSoilMeasurementServer(src)
	read := func() any {
		t.Helper()
		v, ok := srv.MatterRead(soildef.AttrSoilMoistureMeasuredValue)
		if !ok {
			t.Fatal("SoilMoistureMeasuredValue unreadable")
		}
		return v
	}
	if v := read(); v != nil {
		t.Fatalf("initial value = %v, want null", v)
	}
	src.value, src.observed = 5, true // below the limits, nothing stored yet
	if v := read(); v != nil {
		t.Errorf("below-limit reading with no previous value = %v, want null", v)
	}
	src.value = 42.4
	if v := read(); v != uint8(42) {
		t.Errorf("in-limit reading = %v, want 42", v)
	}
	for _, out := range []float64{95, -3, 300, math.NaN()} {
		src.value = out
		if v := read(); v != uint8(42) {
			t.Errorf("reading %v outside the limits = %v, want the previous 42", out, v)
		}
	}
	src.value = 90
	if v := read(); v != uint8(90) {
		t.Errorf("reading at the max bound = %v, want 90", v)
	}
	src.observed = false
	if v := read(); v != nil {
		t.Errorf("unobserved reading = %v, want null", v)
	}
}

func TestSoilMeasurementIsReadOnlyAndHasNoCommands(t *testing.T) {
	t.Parallel()
	srv := NewSoilMeasurementServer(&soilStub{limits: demoSoilLimits()})
	if err := srv.MatterWrite(context.Background(), soildef.AttrSoilMoistureMeasuredValue, uint8(1)); err == nil {
		t.Error("write accepted")
	}
	if _, err := srv.MatterInvoke(context.Background(), 0, nil); !errors.Is(err, errNoCommands) {
		t.Errorf("invoke = %v, want errNoCommands", err)
	}
	if got := srv.MatterReportable(); len(got) != 1 || got[0] != soildef.AttrSoilMoistureMeasuredValue {
		t.Errorf("reportable = %v", got)
	}
	if got := srv.MatterAttributes(); len(got) != 2 {
		t.Errorf("attributes = %v", got)
	}
	if srv.MatterDataVersion() != srv.Current() {
		t.Error("data version is not the tracker's")
	}
}

// TestSoilMoistureMaterializes checks the registered kind: a source with
// limits mounts SoilMeasurement, a plain float source mounts nothing.
func TestSoilMoistureMaterializes(t *testing.T) {
	t.Parallel()
	m, ok := contract.MeasurementMaterializerFor(contract.MeasurementSoilMoisture)
	if !ok {
		t.Fatal("no materialiser for MeasurementSoilMoisture")
	}
	got := m(&soilStub{limits: demoSoilLimits()}, contract.MeasurementContext{EndpointID: 3})
	if len(got) != 1 || got[0].MatterClusterID() != ClusterSoilMeasurement {
		t.Errorf("materialised %v", got)
	}
	if got := m(stubFloatSrc(1), contract.MeasurementContext{}); got != nil {
		t.Errorf("a source without limits materialised %v", got)
	}
}

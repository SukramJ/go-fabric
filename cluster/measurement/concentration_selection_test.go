// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement_test

import (
	"math"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/contract"
)

// selectedConc is a concentration source with a selection and, when
// readings is set, the readings it serves.
type selectedConc struct {
	class    contract.MeasurementClass
	val      float64
	sel      contract.ConcentrationSelection
	readings *contract.ConcentrationReadings
}

func (s selectedConc) MatterMeasurementClass() contract.MeasurementClass { return s.class }
func (s selectedConc) MatterFloatValue() (float64, bool)                 { return s.val, true }
func (s selectedConc) MatterConcentrationSelection() contract.ConcentrationSelection {
	return s.sel
}

// readingConc adds the readings capability.
type readingConc struct{ selectedConc }

func (s readingConc) MatterConcentrationReadings() contract.ConcentrationReadings {
	return *s.readings
}

// Concentration attribute ids (the base cluster's layout).
const (
	concPeak          = 0x0003
	concPeakWindow    = 0x0004
	concAverage       = 0x0005
	concAverageWindow = 0x0006
	concLevelValue    = 0x000A
	globalFeatureMap  = 0xFFFC
)

// TestConcentrationSelectionFeatureMap pins the FeatureMap of each
// selection: MEA always, the named bits, MED and CRI closed onto LEV as
// connectedhomeip's constructor closes them
// (ConcentrationMeasurementCluster.cpp:77-89 at 6170af84).
func TestConcentrationSelectionFeatureMap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		sel  contract.ConcentrationSelection
		want uint32
	}{
		{"zero", contract.ConcentrationSelection{}, 0x01},
		{"level", contract.ConcentrationSelection{Level: true}, 0x03},
		{"medium closes onto level", contract.ConcentrationSelection{MediumLevel: true}, 0x07},
		{"critical closes onto level", contract.ConcentrationSelection{CriticalLevel: true}, 0x0B},
		{"peak", contract.ConcentrationSelection{Peak: true}, 0x11},
		{"average", contract.ConcentrationSelection{Average: true}, 0x21},
		{"all", contract.ConcentrationSelection{Level: true, MediumLevel: true, CriticalLevel: true, Peak: true, Average: true}, 0x3F},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := measurement.NewCO2ConcentrationServer(selectedConc{class: contract.MeasurementCO2, sel: tc.sel})
			if v, ok := s.MatterRead(globalFeatureMap); !ok || v != tc.want {
				t.Errorf("FeatureMap = %v (%v), want 0x%02X", v, ok, tc.want)
			}
		})
	}
}

// TestConcentrationSelectionReadings covers the readings each feature
// serves: the source's peak and average held to the measured range,
// null while unobserved, the windows the selection states (held to one
// week), and the level, Unknown for a grade the selection lacks.
func TestConcentrationSelectionReadings(t *testing.T) {
	t.Parallel()
	sel := contract.ConcentrationSelection{Level: true, Peak: true, Average: true, PeakWindow: 3600, AverageWindow: 1 << 30}
	r := contract.ConcentrationReadings{Level: contract.ConcentrationLevelHigh, Peak: 1e9, PeakObserved: true, Average: 412.5, AverageObserved: true}
	s := measurement.NewPM25ConcentrationServer(readingConc{selectedConc{class: contract.MeasurementPM25, val: 8, sel: sel, readings: &r}})
	for _, tc := range []struct {
		id   uint32
		want any
	}{
		{concPeak, float32(100000)},
		{concPeakWindow, uint32(3600)},
		{concAverage, float32(412.5)},
		{concAverageWindow, uint32(contract.ConcentrationWindowMax)},
		{concLevelValue, uint8(3)},
	} {
		if v, ok := s.MatterRead(tc.id); !ok || v != tc.want {
			t.Errorf("attribute 0x%04X = %v (%v), want %v", tc.id, v, ok, tc.want)
		}
	}
	if got := s.MatterReportable(); !slices.Equal(got, []uint32{0x0000, concPeak, concAverage, concLevelValue}) {
		t.Errorf("MatterReportable = %v, want MeasuredValue, Peak, Average, LevelValue", got)
	}

	// Medium and Critical need their features; an out-of-enum level reads
	// Unknown as well.
	for level, want := range map[contract.ConcentrationLevel]uint8{
		contract.ConcentrationLevelLow: 1, contract.ConcentrationLevelMedium: 0,
		contract.ConcentrationLevelCritical: 0, contract.ConcentrationLevel(9): 0,
	} {
		r := contract.ConcentrationReadings{Level: level, Peak: math.NaN(), PeakObserved: true}
		s := measurement.NewPM25ConcentrationServer(readingConc{selectedConc{class: contract.MeasurementPM25, sel: sel, readings: &r}})
		if v, _ := s.MatterRead(concLevelValue); v != want {
			t.Errorf("LevelValue of %d = %v, want %d", level, v, want)
		}
		if v, ok := s.MatterRead(concPeak); !ok || v != nil {
			t.Errorf("a NaN peak reads %v (%v), want null", v, ok)
		}
	}
	full := contract.ConcentrationSelection{MediumLevel: true, CriticalLevel: true}
	for level, want := range map[contract.ConcentrationLevel]uint8{contract.ConcentrationLevelMedium: 2, contract.ConcentrationLevelCritical: 4} {
		r := contract.ConcentrationReadings{Level: level}
		s := measurement.NewPM25ConcentrationServer(readingConc{selectedConc{class: contract.MeasurementPM25, sel: full, readings: &r}})
		if v, _ := s.MatterRead(concLevelValue); v != want {
			t.Errorf("LevelValue of %d with MED|CRI = %v, want %d", level, v, want)
		}
	}
}

// TestConcentrationSelectionWithoutReadings: a source that selects the
// readings without supplying them reads Unknown and null, and the
// attributes outside the selection stay unserved.
func TestConcentrationSelectionWithoutReadings(t *testing.T) {
	t.Parallel()
	s := measurement.NewRadonConcentrationServer(selectedConc{class: contract.MeasurementRadon, sel: contract.ConcentrationSelection{Level: true, Peak: true}})
	if v, ok := s.MatterRead(concLevelValue); !ok || v != uint8(0) {
		t.Errorf("LevelValue = %v (%v), want Unknown", v, ok)
	}
	if v, ok := s.MatterRead(concPeak); !ok || v != nil {
		t.Errorf("PeakMeasuredValue = %v (%v), want null", v, ok)
	}
	if v, ok := s.MatterRead(concPeakWindow); !ok || v != uint32(0) {
		t.Errorf("PeakMeasuredValueWindow = %v (%v), want 0", v, ok)
	}
	for _, id := range []uint32{concAverage, concAverageWindow} {
		if _, ok := s.MatterRead(id); ok {
			t.Errorf("attribute 0x%04X served without AverageMeasurement", id)
		}
		if slices.Contains(s.MatterAttributes(), id) {
			t.Errorf("attribute 0x%04X listed without AverageMeasurement", id)
		}
	}
	plain := measurement.NewRadonConcentrationServer(fakeFloat{class: contract.MeasurementRadon, val: 1, obs: true})
	if _, ok := plain.MatterRead(concLevelValue); ok {
		t.Error("LevelValue served by the zero selection")
	}
}

// TestAirQualityGrades grades CO2, PM2.5 and PM10 on their scales into
// the grades the selection serves; a grade left out folds towards Poor,
// so the zero selection grades Good up to the guideline and Poor above,
// as before the selection.
func TestAirQualityGrades(t *testing.T) {
	t.Parallel()
	const (
		unknown, good, fair, moderate, poor, veryPoor, extremelyPoor = 0, 1, 2, 3, 4, 5, 6
	)
	all := contract.AirQualityFair | contract.AirQualityModerate | contract.AirQualityVeryPoor | contract.AirQualityExtremelyPoor
	for _, tc := range []struct {
		name   string
		class  contract.MeasurementClass
		levels contract.AirQualityLevels
		val    float64
		want   uint8
	}{
		{"co2 good", contract.MeasurementCO2, 0, 1000, good},
		{"co2 poor", contract.MeasurementCO2, 0, 1500, poor},
		{"co2 above 2000 without VPOOR", contract.MeasurementCO2, 0, 2500, poor},
		{"co2 above 2000", contract.MeasurementCO2, all, 2500, veryPoor},
		{"co2 between with every grade", contract.MeasurementCO2, all, 1500, poor},
		{"pm25 good", contract.MeasurementPM25, all, 15, good},
		{"pm25 fair", contract.MeasurementPM25, all, 25, fair},
		{"pm25 moderate", contract.MeasurementPM25, all, 37.5, moderate},
		{"pm25 poor", contract.MeasurementPM25, all, 50, poor},
		{"pm25 very poor", contract.MeasurementPM25, all, 75, veryPoor},
		{"pm25 extremely poor", contract.MeasurementPM25, all, 76, extremelyPoor},
		{"pm25 fair without FAIR is moderate", contract.MeasurementPM25, contract.AirQualityModerate, 20, moderate},
		{"pm25 fair without FAIR and MOD is poor", contract.MeasurementPM25, 0, 20, poor},
		{"pm25 extremely poor without XPOOR", contract.MeasurementPM25, contract.AirQualityVeryPoor, 200, veryPoor},
		{"pm25 extremely poor alone", contract.MeasurementPM25, contract.AirQualityExtremelyPoor, 60, poor},
		{"pm10 fair", contract.MeasurementPM10, all, 48, fair},
		{"pm10 extremely poor", contract.MeasurementPM10, all, 151, extremelyPoor},
		{"no guideline", contract.MeasurementRadon, all, 1, unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := selectedConc{class: tc.class, val: tc.val, sel: contract.ConcentrationSelection{AirQuality: tc.levels}}
			s := measurement.NewAirQualityServer(tc.class, src)
			if v, ok := s.MatterRead(0x0000); !ok || v != tc.want {
				t.Errorf("AirQuality = %v (%v), want %d", v, ok, tc.want)
			}
			if v, _ := s.MatterRead(globalFeatureMap); v != uint32(tc.levels) {
				t.Errorf("FeatureMap = %v, want 0x%X", v, tc.levels)
			}
		})
	}
}

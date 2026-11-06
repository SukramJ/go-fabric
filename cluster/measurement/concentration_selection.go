// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement

// The feature selection of the concentration servers and of the
// AirQuality server beside them: the source's
// [contract.ConcentrationSelection] when it states one, the numeric
// reading alone graded Unknown / Good / Poor otherwise.
//
// matter.js serves every conformant selection of these clusters as plain
// state and has no logic for them (packages/node/src/behaviors/air-quality/AirQualityServer.ts:14
// and packages/node/src/behaviors/carbon-dioxide-concentration-measurement/CarbonDioxideConcentrationMeasurementServer.ts:18
// at 862ad75f are empty subclasses). The selection's closure and the
// readings' feature checks follow connectedhomeip, whose server sets the
// values the application supplies.

import (
	"math"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	aqdef "github.com/SukramJ/go-fabric/cluster/spec/airquality"
	co2def "github.com/SukramJ/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement"
	"github.com/SukramJ/go-fabric/contract"
)

// The concentration attributes the selection gates; the ten clusters
// share the base cluster's layout.
const (
	attrConcPeakMeasuredValue          = co2def.AttrPeakMeasuredValue
	attrConcPeakMeasuredValueWindow    = co2def.AttrPeakMeasuredValueWindow
	attrConcAverageMeasuredValue       = co2def.AttrAverageMeasuredValue
	attrConcAverageMeasuredValueWindow = co2def.AttrAverageMeasuredValueWindow
	attrConcLevelValue                 = co2def.AttrLevelValue
)

// concentrationSelectionOf is the source's selection, closed as
// connectedhomeip's constructor closes the features: MediumLevel or
// CriticalLevel imply LevelIndication
// (src/app/clusters/concentration-measurement-server/ConcentrationMeasurementCluster.cpp:77-89
// at 6170af84). A window beyond the attribute's maximum is held to it,
// where the setter refuses one with CONSTRAINT_ERROR (:198): the
// projection has no caller to refuse.
func concentrationSelectionOf(src contract.FloatMeasurementSource) contract.ConcentrationSelection {
	var sel contract.ConcentrationSelection
	if ss, ok := src.(contract.ConcentrationSelectionSource); ok {
		sel = ss.MatterConcentrationSelection()
	}
	if sel.MediumLevel || sel.CriticalLevel {
		sel.Level = true
	}
	sel.PeakWindow = min(sel.PeakWindow, contract.ConcentrationWindowMax)
	sel.AverageWindow = min(sel.AverageWindow, contract.ConcentrationWindowMax)
	sel.AirQuality &= contract.AirQualityFair | contract.AirQualityModerate | contract.AirQualityVeryPoor | contract.AirQualityExtremelyPoor
	return sel
}

// concentrationFeatures is the concentration FeatureMap of a closed
// selection: NumericMeasurement always — the projection serves a numeric
// reading — and the bits the selection names. Uncertainty ("[MEA]") is
// not served.
func concentrationFeatures(sel contract.ConcentrationSelection) uint32 {
	f := co2def.FeatureNumericMeasurement
	if sel.Level {
		f |= co2def.FeatureLevelIndication
	}
	if sel.MediumLevel {
		f |= co2def.FeatureMediumLevel
	}
	if sel.CriticalLevel {
		f |= co2def.FeatureCriticalLevel
	}
	if sel.Peak {
		f |= co2def.FeaturePeakMeasurement
	}
	if sel.Average {
		f |= co2def.FeatureAverageMeasurement
	}
	return uint32(f)
}

// concInstKey names one bound concentration definition.
type concInstKey struct {
	cluster  uint32
	features uint32
}

// concInsts holds one instance per cluster and selection; the servers are
// rebuilt on every dispatch, their instances are not.
var concInsts sync.Map // concInstKey → *spec.Instance

// concInstance is def bound to the selection's features. Every closed
// selection is one the definition admits: MED and CRI come with LEV, PEA
// and AVG with MEA.
func concInstance(def *spec.Cluster, sel contract.ConcentrationSelection) *spec.Instance {
	key := concInstKey{def.ID, concentrationFeatures(sel)}
	if v, ok := concInsts.Load(key); ok {
		if inst, isInst := v.(*spec.Instance); isInst {
			return inst
		}
	}
	inst := mustInstance(def, spec.Options{Features: key.features})
	concInsts.Store(key, inst)
	return inst
}

// concentrationReadingsOf is the source's [contract.ConcentrationReadings];
// a source without them reads Unknown and null.
func concentrationReadingsOf(src contract.FloatMeasurementSource) contract.ConcentrationReadings {
	if rs, ok := src.(contract.ConcentrationReadingsSource); ok {
		return rs.MatterConcentrationReadings()
	}
	return contract.ConcentrationReadings{}
}

// concentrationLevel is the LevelValue a selection serves for level: a
// level outside the enum, Medium without MediumLevel and Critical without
// CriticalLevel read Unknown, the levels connectedhomeip's SetLevelValue
// refuses with CONSTRAINT_ERROR (ConcentrationMeasurementCluster.cpp:213-225
// at 6170af84).
func concentrationLevel(sel contract.ConcentrationSelection, level contract.ConcentrationLevel) uint8 {
	switch {
	case level > contract.ConcentrationLevelCritical,
		level == contract.ConcentrationLevelMedium && !sel.MediumLevel,
		level == contract.ConcentrationLevelCritical && !sel.CriticalLevel:
		return uint8(co2def.LevelValueUnknown)
	}
	return uint8(level)
}

// concentrationValue is a peak or average reading on the wire: null when
// not observed or NaN, else held to [MinMeasuredValue, MaxMeasuredValue]
// as MeasuredValue is. connectedhomeip refuses a value outside that range
// (ConcentrationMeasurementCluster.cpp:170-176 at 6170af84); the
// projection has no caller to refuse and holds it, as for MeasuredValue.
func concentrationValue(v float64, observed bool) any {
	if !observed || math.IsNaN(v) {
		return nil
	}
	return float32(min(max(v, concMinMeasured), concMaxMeasured))
}

// --- AirQuality grades ---------------------------------------------------

// airQualityInsts holds the AirQuality definition bound to each of the
// sixteen grade selections.
var airQualityInsts = func() [16]*spec.Instance {
	var insts [16]*spec.Instance
	for f := range insts {
		insts[f] = mustInstance(aqdef.Definition, spec.Options{Features: uint32(f)}) //nolint:gosec // 0..15
	}
	return insts
}()

// airQualityBand is one grade of a pollutant's scale: readings up to
// upTo (inclusive, in the source's unit) take level.
type airQualityBand struct {
	upTo  float64
	level aqdef.AirQualityEnum
}

// airQualityBands returns a pollutant's grading scale, ascending, its
// last band unbounded, and whether the class has one at all. A class
// without a published guideline reads Unknown.
//
// CO2 is ppm on the German Umweltbundesamt's indoor-air scale: up to
// 1000 ppm hygienically harmless, up to 2000 ppm conspicuous, above
// unacceptable — Good, Poor and VeryPoor; the 1000 ppm bound is the limit
// above which a room is considered inadequately ventilated. PM2.5 and
// PM10 are µg/m³ on the World Health Organization 2021 air-quality
// guideline for the 24-hour mean: the guideline level (15 and 45 µg/m³)
// bounds Good, the interim targets 4 to 1 (25 / 37.5 / 50 / 75 and
// 50 / 75 / 100 / 150 µg/m³) bound Fair, Moderate, Poor and VeryPoor,
// and ExtremelyPoor lies beyond the first. The model classifies exactly
// the 24-hour-average parameters onto these classes, so the averaging
// windows line up.
func airQualityBands(class contract.MeasurementClass) ([]airQualityBand, bool) {
	inf := math.Inf(1)
	switch class {
	case contract.MeasurementCO2:
		return []airQualityBand{
			{1000, aqdef.AirQualityGood}, {2000, aqdef.AirQualityPoor}, {inf, aqdef.AirQualityVeryPoor},
		}, true
	case contract.MeasurementPM25:
		return []airQualityBand{
			{15, aqdef.AirQualityGood},
			{25, aqdef.AirQualityFair},
			{37.5, aqdef.AirQualityModerate},
			{50, aqdef.AirQualityPoor},
			{75, aqdef.AirQualityVeryPoor},
			{inf, aqdef.AirQualityExtremelyPoor},
		}, true
	case contract.MeasurementPM10:
		return []airQualityBand{
			{45, aqdef.AirQualityGood},
			{50, aqdef.AirQualityFair},
			{75, aqdef.AirQualityModerate},
			{100, aqdef.AirQualityPoor},
			{150, aqdef.AirQualityVeryPoor},
			{inf, aqdef.AirQualityExtremelyPoor},
		}, true
	default:
		return nil, false
	}
}

// airQualityGrade grades v on bands into the grades levels serves. A
// grade the selection leaves out folds towards Poor, which with Good and
// Unknown is always served: Fair into Moderate, Moderate into Poor,
// ExtremelyPoor into VeryPoor, VeryPoor into Poor. Without optional
// grades every reading above the Good bound is Poor.
func airQualityGrade(bands []airQualityBand, levels contract.AirQualityLevels, v float64) uint8 {
	level := aqdef.AirQualityUnknown
	for _, b := range bands {
		if v <= b.upTo {
			level = b.level
			break
		}
	}
	if level == aqdef.AirQualityFair && levels&contract.AirQualityFair == 0 {
		level = aqdef.AirQualityModerate
	}
	if level == aqdef.AirQualityModerate && levels&contract.AirQualityModerate == 0 {
		level = aqdef.AirQualityPoor
	}
	if level == aqdef.AirQualityExtremelyPoor && levels&contract.AirQualityExtremelyPoor == 0 {
		level = aqdef.AirQualityVeryPoor
	}
	if level == aqdef.AirQualityVeryPoor && levels&contract.AirQualityVeryPoor == 0 {
		level = aqdef.AirQualityPoor
	}
	return uint8(level)
}

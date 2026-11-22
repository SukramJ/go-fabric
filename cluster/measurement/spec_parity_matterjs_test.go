// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	aqdef "thingfabric.dev/go-fabric/cluster/spec/airquality"
	booldef "thingfabric.dev/go-fabric/cluster/spec/booleanstate"
	co2def "thingfabric.dev/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement"
	codef "thingfabric.dev/go-fabric/cluster/spec/carbonmonoxideconcentrationmeasurement"
	eemdef "thingfabric.dev/go-fabric/cluster/spec/electricalenergymeasurement"
	epmdef "thingfabric.dev/go-fabric/cluster/spec/electricalpowermeasurement"
	flowdef "thingfabric.dev/go-fabric/cluster/spec/flowmeasurement"
	hchodef "thingfabric.dev/go-fabric/cluster/spec/formaldehydeconcentrationmeasurement"
	illdef "thingfabric.dev/go-fabric/cluster/spec/illuminancemeasurement"
	no2def "thingfabric.dev/go-fabric/cluster/spec/nitrogendioxideconcentrationmeasurement"
	occdef "thingfabric.dev/go-fabric/cluster/spec/occupancysensing"
	o3def "thingfabric.dev/go-fabric/cluster/spec/ozoneconcentrationmeasurement"
	pm10def "thingfabric.dev/go-fabric/cluster/spec/pm10concentrationmeasurement"
	pm1def "thingfabric.dev/go-fabric/cluster/spec/pm1concentrationmeasurement"
	pm25def "thingfabric.dev/go-fabric/cluster/spec/pm25concentrationmeasurement"
	psdef "thingfabric.dev/go-fabric/cluster/spec/powersource"
	ptdef "thingfabric.dev/go-fabric/cluster/spec/powertopology"
	prsdef "thingfabric.dev/go-fabric/cluster/spec/pressuremeasurement"
	radondef "thingfabric.dev/go-fabric/cluster/spec/radonconcentrationmeasurement"
	rhdef "thingfabric.dev/go-fabric/cluster/spec/relativehumiditymeasurement"
	soildef "thingfabric.dev/go-fabric/cluster/spec/soilmeasurement"
	"thingfabric.dev/go-fabric/cluster/spec/spectest"
	tmpdef "thingfabric.dev/go-fabric/cluster/spec/temperaturemeasurement"
	tvocdef "thingfabric.dev/go-fabric/cluster/spec/totalvolatileorganiccompoundsconcentrationmeasurement"
	"thingfabric.dev/go-fabric/contract"
)

// specReadings is a consolidated electrical group for the parity check.
type specReadings struct{ stubFloatSrc }

func (specReadings) ActivePower() (float64, bool) { return 1, true }
func (specReadings) Voltage() (float64, bool)     { return 230, true }
func (specReadings) Current() (float64, bool)     { return 100, true }
func (specReadings) Frequency() (float64, bool)   { return 50, true }
func (specReadings) Energy() (float64, bool)      { return 10, true }
func (specReadings) HasEnergy() bool              { return true }

// selectingOccupancy is an occupancy reading that states its sensor's
// selection.
type selectingOccupancy struct{ sel contract.OccupancySelection }

func (selectingOccupancy) MatterBoolValue() (value, observed bool) { return true, true }
func (selectingOccupancy) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementOccupancy
}
func (s selectingOccupancy) MatterOccupancySelection() contract.OccupancySelection { return s.sel }

// TestServersMatchTheGeneratedDefinitions holds every measurement server
// against its matter.js element (cluster/spec, ADR 0013) for the feature
// selection it serves: FeatureMap and ClusterRevision as the definition
// says, the mandatory attributes and events listed, nothing disallowed,
// UNSUPPORTED_WRITE on every read-only attribute.
func TestServersMatchTheGeneratedDefinitions(t *testing.T) {
	t.Parallel()
	f := stubFloatSrc(21.5)
	b := stubBoolSrc(true)
	cases := []struct {
		name     string
		srv      contract.ClusterServer
		def      *spec.Cluster
		features uint32
	}{
		{"Temperature", NewTemperatureServer(f), tmpdef.Definition, 0},
		{"Humidity", NewHumidityServer(f), rhdef.Definition, 0},
		{"Illuminance", NewIlluminanceServer(f), illdef.Definition, 0},
		{"Pressure", NewPressureServer(f), prsdef.Definition, 0},
		{"Flow", NewFlowServer(f), flowdef.Definition, 0},
		{"BooleanState", NewBooleanStateServer(b), booldef.Definition, uint32(booldef.FeatureChangeEvent)},
		{"OccupancySensing", NewOccupancySensingServer(b), occdef.Definition, uint32(occdef.FeaturePassiveInfrared | occdef.FeatureOccupancyEvent)},
		{"OccupancySensing selected", NewOccupancySensingServer(selectingOccupancy{
			contract.OccupancySelection{Detectors: contract.OccupancyDetectorUltrasonic | contract.OccupancyDetectorRadar, NoOccupancyEvent: true},
		}), occdef.Definition, uint32(occdef.FeatureUltrasonic | occdef.FeatureRadar)},
		{"AirQuality", NewAirQualityServer(contract.MeasurementCO2, f), aqdef.Definition, 0},
		{"CO2", NewCO2ConcentrationServer(f), co2def.Definition, uint32(co2def.FeatureNumericMeasurement)},
		{"PM2.5", NewPM25ConcentrationServer(f), pm25def.Definition, uint32(pm25def.FeatureNumericMeasurement)},
		{"PM10", NewPM10ConcentrationServer(f), pm10def.Definition, uint32(pm10def.FeatureNumericMeasurement)},
		{"CO", NewCOConcentrationServer(f), codef.Definition, uint32(codef.FeatureNumericMeasurement)},
		{"NO2", NewNO2ConcentrationServer(f), no2def.Definition, uint32(no2def.FeatureNumericMeasurement)},
		{"Ozone", NewOzoneConcentrationServer(f), o3def.Definition, uint32(o3def.FeatureNumericMeasurement)},
		{"Formaldehyde", NewFormaldehydeConcentrationServer(f), hchodef.Definition, uint32(hchodef.FeatureNumericMeasurement)},
		{"PM1", NewPM1ConcentrationServer(f), pm1def.Definition, uint32(pm1def.FeatureNumericMeasurement)},
		{"TVOC", NewTVOCConcentrationServer(f), tvocdef.Definition, uint32(tvocdef.FeatureNumericMeasurement)},
		{"Radon", NewRadonConcentrationServer(f), radondef.Definition, uint32(radondef.FeatureNumericMeasurement)},
		{"PowerSource bool", NewPowerSourceServer(b), psdef.Definition, uint32(psdef.FeatureBattery)},
		{"PowerSource float", NewPowerSourceServerFromFloat(f), psdef.Definition, uint32(psdef.FeatureBattery)},
		{"PowerSource wired", NewWiredPowerSourceServer(contract.WiredCurrentDC), psdef.Definition, uint32(psdef.FeatureWired)},
		{"PowerTopology", NewPowerTopologyServer(), ptdef.Definition, uint32(ptdef.FeatureNodeTopology)},
		{"ElectricalPower", NewElectricalPowerServer(f), epmdef.Definition, uint32(epmdef.FeatureAlternatingCurrent)},
		{"ElectricalPower readings", NewElectricalPowerServerFromReadings(specReadings{f}), epmdef.Definition, uint32(epmdef.FeatureAlternatingCurrent)},
		{"ElectricalEnergy", NewElectricalEnergyServer(f), eemdef.Definition, uint32(eemdef.FeatureImportedEnergy | eemdef.FeatureCumulativeEnergy)},
		{"SoilMeasurement", NewSoilMeasurementServer(&soilStub{value: 40, observed: true, limits: demoSoilLimits()}), soildef.Definition, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spectest.CheckServer(t, tc.srv, tc.def, tc.features)
			writeChangesNothing(t, tc.srv)
		})
	}
}

// writeChangesNothing holds MatterWriteChanged of a server that stores no
// written value: every listed attribute, and one the cluster does not
// define, answers unchanged with the refusal MatterWrite gives.
func writeChangesNothing(t *testing.T, srv contract.ClusterServer) {
	t.Helper()
	w, ok := srv.(contract.ChangeReportingWriter)
	if !ok {
		t.Fatalf("%T does not give the write verdict (contract.ChangeReportingWriter)", srv)
	}
	lister, ok := srv.(contract.ClusterAttributeLister)
	if !ok {
		t.Fatalf("%T lists no attributes", srv)
	}
	ctx := context.Background()
	for _, id := range append(lister.MatterAttributes(), 0x7FF0) {
		changed, err := w.MatterWriteChanged(ctx, id, uint64(0))
		werr := srv.MatterWrite(ctx, id, uint64(0))
		if changed || err == nil || werr == nil || err.Error() != werr.Error() || spec.StatusOf(err) != spec.StatusOf(werr) {
			t.Errorf("attribute 0x%04X: MatterWriteChanged = %v, %v; want unchanged and MatterWrite's refusal %v", id, changed, err, werr)
		}
	}
}

// TestElectricalValuesDecodeThroughTheGeneratedCodecs encodes the
// Accuracy and CumulativeEnergyImported values the electrical servers
// report and decodes them with the attribute decoders of matter.js's
// elements: the wire shape a typed controller reads is the generated one.
func TestElectricalValuesDecodeThroughTheGeneratedCodecs(t *testing.T) {
	t.Parallel()
	f := stubFloatSrc(12.5)
	v, ok := NewElectricalPowerServer(f).MatterRead(epmdef.AttrAccuracy)
	list, isList := v.([]AccuracyStruct)
	if !ok || !isList || len(list) != 4 {
		t.Fatalf("ElectricalPowerMeasurement Accuracy = %T (%v), want an entry per served measurement type", v, ok)
	}
	if _, err := epmdef.Definition.Attribute(epmdef.AttrAccuracy).Decode(spectest.Decode(t, spectest.Encode(t, AccuracyList(list)))); err != nil {
		t.Errorf("ElectricalPowerMeasurement Accuracy does not decode: %v", err)
	}
	v, ok = NewElectricalEnergyServer(f).MatterRead(eemdef.AttrAccuracy)
	acc, isStruct := v.(AccuracyStruct)
	if !ok || !isStruct {
		t.Fatalf("ElectricalEnergyMeasurement Accuracy = %T (%v), want one AccuracyStruct", v, ok)
	}
	got, err := eemdef.Definition.Attribute(eemdef.AttrAccuracy).Decode(spectest.Decode(t, spectest.Encode(t, acc)))
	if err != nil {
		t.Errorf("ElectricalEnergyMeasurement Accuracy does not decode: %v", err)
	} else if d, _ := got.(eemdef.MeasurementAccuracyStruct); d.MeasurementType != eemdef.MeasurementTypeElectricalEnergy || len(d.AccuracyRanges) != 1 || d.AccuracyRanges[0].FixedMax == nil {
		t.Errorf("ElectricalEnergyMeasurement Accuracy decoded as %+v", got)
	}
	v, _ = NewElectricalEnergyServer(f).MatterRead(eemdef.AttrCumulativeEnergyImported)
	e, isEnergy := v.(EnergyMeasurementStruct)
	if !isEnergy {
		t.Fatalf("CumulativeEnergyImported = %T", v)
	}
	spectest.RoundTrip(t, e, spec.DecodeStruct[EnergyMeasurementStruct])
	spectest.RoundTripEvent(t, booldef.Definition, BooleanStateEventStateChange, BooleanStateChangeEvent{StateValue: true})
}

// unobservedFloat, unobservedBool and unobservedReadings are sources that
// have observed nothing yet.
type (
	unobservedFloat    struct{}
	unobservedBool     struct{}
	unobservedReadings struct{ unobservedFloat }
)

func (unobservedFloat) MatterFloatValue() (float64, bool)                 { return 0, false }
func (unobservedFloat) MatterMeasurementClass() contract.MeasurementClass { return 0 }
func (unobservedBool) MatterBoolValue() (value, observed bool)            { return false, false }
func (unobservedBool) MatterMeasurementClass() contract.MeasurementClass  { return 0 }
func (unobservedReadings) ActivePower() (float64, bool)                   { return 0, false }
func (unobservedReadings) Voltage() (float64, bool)                       { return 0, false }
func (unobservedReadings) Current() (float64, bool)                       { return 0, false }
func (unobservedReadings) Frequency() (float64, bool)                     { return 0, false }
func (unobservedReadings) Energy() (float64, bool)                        { return 0, false }
func (unobservedReadings) HasEnergy() bool                                { return true }

// TestUnobservedReadsAreNullOnlyWhereTheDefinitionAllows holds every
// projection of a source that has observed nothing to its matter.js
// element: an attribute reads null only when the element gives it quality
// X. A non-nullable attribute reads its default instead — matter.js's
// default of a non-nullable member is never null (packages/model/src/
// logic/SelectDefaultValue.ts:155-157 at b25ed960, null only
// `if (member.nullable)`).
func TestUnobservedReadsAreNullOnlyWhereTheDefinitionAllows(t *testing.T) {
	t.Parallel()
	f, b := unobservedFloat{}, unobservedBool{}
	cases := []struct {
		name string
		srv  contract.ClusterServer
		def  *spec.Cluster
	}{
		{"Temperature", NewTemperatureServer(f), tmpdef.Definition},
		{"Humidity", NewHumidityServer(f), rhdef.Definition},
		{"Illuminance", NewIlluminanceServer(f), illdef.Definition},
		{"Pressure", NewPressureServer(f), prsdef.Definition},
		{"Flow", NewFlowServer(f), flowdef.Definition},
		{"BooleanState", NewBooleanStateServer(b), booldef.Definition},
		{"OccupancySensing", NewOccupancySensingServer(b), occdef.Definition},
		{"AirQuality", NewAirQualityServer(contract.MeasurementCO2, f), aqdef.Definition},
		{"CO2", NewCO2ConcentrationServer(f), co2def.Definition},
		{"PM2.5", NewPM25ConcentrationServer(f), pm25def.Definition},
		{"PM10", NewPM10ConcentrationServer(f), pm10def.Definition},
		{"CO", NewCOConcentrationServer(f), codef.Definition},
		{"NO2", NewNO2ConcentrationServer(f), no2def.Definition},
		{"Ozone", NewOzoneConcentrationServer(f), o3def.Definition},
		{"Formaldehyde", NewFormaldehydeConcentrationServer(f), hchodef.Definition},
		{"PM1", NewPM1ConcentrationServer(f), pm1def.Definition},
		{"TVOC", NewTVOCConcentrationServer(f), tvocdef.Definition},
		{"Radon", NewRadonConcentrationServer(f), radondef.Definition},
		{"PowerSource bool", NewPowerSourceServer(b), psdef.Definition},
		{"PowerSource float", NewPowerSourceServerFromFloat(f), psdef.Definition},
		{"ElectricalPower", NewElectricalPowerServer(f), epmdef.Definition},
		{"ElectricalPower readings", NewElectricalPowerServerFromReadings(unobservedReadings{}), epmdef.Definition},
		{"ElectricalEnergy", NewElectricalEnergyServer(f), eemdef.Definition},
		{"SoilMeasurement", NewSoilMeasurementServer(&soilStub{limits: demoSoilLimits()}), soildef.Definition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lister, ok := tc.srv.(contract.ClusterAttributeLister)
			if !ok {
				t.Fatalf("%T lists no attributes", tc.srv)
			}
			for _, id := range lister.MatterAttributes() {
				v, ok := tc.srv.MatterRead(id)
				if !ok || v != nil {
					continue
				}
				a := tc.def.Attribute(id)
				if a == nil {
					continue // a global; spectest.CheckServer holds those
				}
				if !a.Quality.Nullable {
					t.Errorf("%s (0x%04X) reads null while unobserved; the element has no quality X", a.Name, id)
				}
			}
		})
	}
}

// TestElectricalPowerReportsItsServedReadings: every reading the projected
// ElectricalPowerMeasurement serves is change-reported. matter.js
// broadcasts the changes of a behavior's state, a quieter property's when
// its QuietEvent emits and a changes-omitted one's never
// (packages/node/src/behavior/internal/ServerBehaviorBacking.ts:163-168,
// :230-262 at db021da0); Voltage, ActiveCurrent, ActivePower and
// Frequency are all of
// the quieter quality (packages/model/src/standard/elements/
// electrical-power-measurement.element.ts:54-55, :58-59, :70-71, :94-95).
// Review II, phase 4.
func TestElectricalPowerReportsItsServedReadings(t *testing.T) {
	t.Parallel()
	srv := NewElectricalPowerServerFromReadings(specReadings{})
	reportable := srv.MatterReportable()
	for _, id := range srv.MatterAttributes() {
		a := epmdef.Definition.Attribute(id)
		if a == nil || !a.Quality.Quieter {
			continue
		}
		if !slices.Contains(reportable, id) {
			t.Errorf("%s (0x%04X) is served and of the quieter quality, but not change-reported", a.Name, id)
		}
	}
}

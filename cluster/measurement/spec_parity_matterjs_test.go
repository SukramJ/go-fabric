// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	aqdef "github.com/SukramJ/go-fabric/cluster/spec/airquality"
	booldef "github.com/SukramJ/go-fabric/cluster/spec/booleanstate"
	co2def "github.com/SukramJ/go-fabric/cluster/spec/carbondioxideconcentrationmeasurement"
	eemdef "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	epmdef "github.com/SukramJ/go-fabric/cluster/spec/electricalpowermeasurement"
	flowdef "github.com/SukramJ/go-fabric/cluster/spec/flowmeasurement"
	illdef "github.com/SukramJ/go-fabric/cluster/spec/illuminancemeasurement"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	pm10def "github.com/SukramJ/go-fabric/cluster/spec/pm10concentrationmeasurement"
	pm25def "github.com/SukramJ/go-fabric/cluster/spec/pm25concentrationmeasurement"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	ptdef "github.com/SukramJ/go-fabric/cluster/spec/powertopology"
	prsdef "github.com/SukramJ/go-fabric/cluster/spec/pressuremeasurement"
	rhdef "github.com/SukramJ/go-fabric/cluster/spec/relativehumiditymeasurement"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	tmpdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturemeasurement"
	"github.com/SukramJ/go-fabric/contract"
)

// specReadings is a consolidated electrical group for the parity check.
type specReadings struct{ stubFloatSrc }

func (specReadings) ActivePower() (float64, bool) { return 1, true }
func (specReadings) Voltage() (float64, bool)     { return 230, true }
func (specReadings) Current() (float64, bool)     { return 100, true }
func (specReadings) Frequency() (float64, bool)   { return 50, true }
func (specReadings) Energy() (float64, bool)      { return 10, true }
func (specReadings) HasEnergy() bool              { return true }

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
		{"OccupancySensing", NewOccupancySensingServer(b), occdef.Definition, uint32(occdef.FeaturePassiveInfrared)},
		{"AirQuality", NewAirQualityServer(contract.MeasurementCO2, f), aqdef.Definition, 0},
		{"CO2", NewCO2ConcentrationServer(f), co2def.Definition, uint32(co2def.FeatureNumericMeasurement)},
		{"PM2.5", NewPM25ConcentrationServer(f), pm25def.Definition, uint32(pm25def.FeatureNumericMeasurement)},
		{"PM10", NewPM10ConcentrationServer(f), pm10def.Definition, uint32(pm10def.FeatureNumericMeasurement)},
		{"PowerSource bool", NewPowerSourceServer(b), psdef.Definition, uint32(psdef.FeatureBattery)},
		{"PowerSource float", NewPowerSourceServerFromFloat(f), psdef.Definition, uint32(psdef.FeatureBattery)},
		{"PowerTopology", NewPowerTopologyServer(), ptdef.Definition, uint32(ptdef.FeatureNodeTopology)},
		{"ElectricalPower", NewElectricalPowerServer(f), epmdef.Definition, uint32(epmdef.FeatureAlternatingCurrent)},
		{"ElectricalPower readings", NewElectricalPowerServerFromReadings(specReadings{f}), epmdef.Definition, uint32(epmdef.FeatureAlternatingCurrent)},
		{"ElectricalEnergy", NewElectricalEnergyServer(f), eemdef.Definition, uint32(eemdef.FeatureImportedEnergy | eemdef.FeatureCumulativeEnergy)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spectest.CheckServer(t, tc.srv, tc.def, tc.features)
		})
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
	if !ok || !isList || len(list) != 1 {
		t.Fatalf("ElectricalPowerMeasurement Accuracy = %T (%v)", v, ok)
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

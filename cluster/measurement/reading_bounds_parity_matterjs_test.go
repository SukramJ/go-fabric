// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package measurement

import (
	"math"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// floatReading is a FloatMeasurementSource with one fixed reading.
type floatReading float64

func (f floatReading) MatterFloatValue() (float64, bool) { return float64(f), true }

func (floatReading) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementFlow
}

// TestReadingBoundsParity: a NaN reading is no reading — MeasuredValue
// reads null — in every server over a float source (W3-club1-07), and a
// reading outside the advertised range reads as its bound, so MeasuredValue
// meets its "minMeasuredValue to maxMeasuredValue" constraint
// (pressure-measurement.element.ts:24-25, concentration-measurement.element.ts:32-33
// at 862ad75f; W3-club1-08).
func TestReadingBoundsParity(t *testing.T) {
	t.Parallel()
	servers := map[string]func(contract.FloatMeasurementSource) interface{ MatterRead(uint32) (any, bool) }{
		"Temperature": func(s contract.FloatMeasurementSource) interface{ MatterRead(uint32) (any, bool) } {
			return NewTemperatureServer(s)
		},
		"Humidity": func(s contract.FloatMeasurementSource) interface{ MatterRead(uint32) (any, bool) } {
			return NewHumidityServer(s)
		},
		"Illuminance": func(s contract.FloatMeasurementSource) interface{ MatterRead(uint32) (any, bool) } {
			return NewIlluminanceServer(s)
		},
		"Pressure": func(s contract.FloatMeasurementSource) interface{ MatterRead(uint32) (any, bool) } {
			return NewPressureServer(s)
		},
		"Flow": func(s contract.FloatMeasurementSource) interface{ MatterRead(uint32) (any, bool) } {
			return NewFlowServer(s)
		},
		"CO2": func(s contract.FloatMeasurementSource) interface{ MatterRead(uint32) (any, bool) } {
			return NewCO2ConcentrationServer(s)
		},
	}
	for name, build := range servers {
		if v, ok := build(floatReading(math.NaN())).MatterRead(0x0000); !ok || v != nil {
			t.Errorf("%s: MeasuredValue of NaN = %v, %v; want null", name, v, ok)
		}
	}
	for _, c := range []struct {
		name    string
		reading float64
		want    any
	}{
		{"Pressure below MinMeasuredValue", -5, int16(0)},
		{"Pressure far below", -1e6, int16(0)},
		{"Pressure above MaxMeasuredValue", 1e6, int16(32766)},
		{"CO2 above MaxMeasuredValue", 2e5, float32(100000)},
		{"CO2 below MinMeasuredValue", -1, float32(0)},
	} {
		var srv interface{ MatterRead(uint32) (any, bool) }
		if c.name[:3] == "CO2" {
			srv = NewCO2ConcentrationServer(floatReading(c.reading))
		} else {
			srv = NewPressureServer(floatReading(c.reading))
		}
		if v, _ := srv.MatterRead(0x0000); v != c.want {
			t.Errorf("%s: MeasuredValue %v (%T), want %v", c.name, v, v, c.want)
		}
	}
}

// electricalReading is an ElectricalReadingsSource that answers every
// parameter with one fixed reading.
type electricalReading float64

func (e electricalReading) MatterFloatValue() (float64, bool) { return float64(e), true }

func (electricalReading) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementFlow
}
func (e electricalReading) ActivePower() (float64, bool) { return float64(e), true }
func (e electricalReading) Voltage() (float64, bool)     { return float64(e), true }
func (e electricalReading) Current() (float64, bool)     { return float64(e), true }
func (e electricalReading) Frequency() (float64, bool)   { return float64(e), true }
func (e electricalReading) Energy() (float64, bool)      { return float64(e), true }
func (electricalReading) HasEnergy() bool                { return false }

// TestElectricalPowerReadingBoundsParity: ElectricalPowerMeasurement's
// readings follow the same two rules as TestReadingBoundsParity. A NaN
// reading is no reading — Voltage, ActiveCurrent and Frequency read null as
// ActivePower does (R2-P0-bound-B04, the residual of W3-club1-07) — and a
// reading outside the attribute's constraint, an infinite one included,
// reads as its bound: "-2^62 to 2^62" for Voltage, ActiveCurrent and
// ActivePower, "0 to 1000000" for Frequency
// (electrical-power-measurement.element.ts:54-55, :58-59, :70-71, :94-95 at
// db021da0). The definition this server is built from carries the same
// texts.
func TestElectricalPowerReadingBoundsParity(t *testing.T) {
	t.Parallel()
	const bound = int64(1) << 62
	for attr, text := range map[uint32]string{
		attrElPwrVoltage: "-2^62 to 2^62", attrElPwrActiveCurrnt: "-2^62 to 2^62",
		attrElPwrActivePower: "-2^62 to 2^62", attrElPwrFrequency: "0 to 1000000",
	} {
		if got := electricalPowerInst.Definition().Attribute(attr).Constraint.Text; got != text {
			t.Errorf("attribute 0x%04X: the definition's constraint is %q, the server holds %q", attr, got, text)
		}
	}
	inf := math.Inf(1)
	for _, c := range []struct {
		name    string
		attr    uint32
		reading float64
		want    any
	}{
		{"Voltage NaN", attrElPwrVoltage, math.NaN(), nil},
		{"ActiveCurrent NaN", attrElPwrActiveCurrnt, math.NaN(), nil},
		{"Frequency NaN", attrElPwrFrequency, math.NaN(), nil},
		{"ActivePower NaN", attrElPwrActivePower, math.NaN(), nil},

		{"Voltage +Inf", attrElPwrVoltage, inf, bound},
		{"Voltage -Inf", attrElPwrVoltage, -inf, -bound},
		{"ActiveCurrent +Inf", attrElPwrActiveCurrnt, inf, bound},
		{"ActiveCurrent -Inf", attrElPwrActiveCurrnt, -inf, -bound},
		{"ActivePower +Inf", attrElPwrActivePower, inf, bound},
		{"ActivePower -Inf", attrElPwrActivePower, -inf, -bound},
		{"Frequency +Inf", attrElPwrFrequency, inf, int64(1_000_000)},
		{"Frequency -Inf", attrElPwrFrequency, -inf, int64(0)},

		// 2^63 is the first float64 an int64 cannot hold; 1e300 is far
		// beyond it. Volts scale by 1000, milliamperes by 1.
		{"Voltage above 2^62 mV", attrElPwrVoltage, 1e300, bound},
		{"Voltage below -2^62 mV", attrElPwrVoltage, -1e300, -bound},
		{"ActiveCurrent 2^63 mA", attrElPwrActiveCurrnt, 0x1p63, bound},
		{"ActiveCurrent -2^63 mA", attrElPwrActiveCurrnt, -0x1p63, -bound},
		{"ActiveCurrent just above 2^62 mA", attrElPwrActiveCurrnt, 0x1p62 + 1024, bound},
		{"ActivePower 2^63 W", attrElPwrActivePower, 0x1p63, bound},
		{"Frequency below 0", attrElPwrFrequency, -1, int64(0)},
		{"Frequency above 1000000 mHz", attrElPwrFrequency, 1000.5, int64(1_000_000)},

		// Inside the constraint the reading is scaled and rounded.
		{"Voltage 230.4 V", attrElPwrVoltage, 230.4, int64(230400)},
		{"ActiveCurrent 1500 mA", attrElPwrActiveCurrnt, 1500, int64(1500)},
		{"ActiveCurrent -2^62 mA", attrElPwrActiveCurrnt, -0x1p62, -bound},
		{"ActivePower -12.5 W", attrElPwrActivePower, -12.5, int64(-12500)},
		{"Frequency 50 Hz", attrElPwrFrequency, 50, int64(50000)},
		{"Frequency 1000 Hz", attrElPwrFrequency, 1000, int64(1_000_000)},
	} {
		v, ok := NewElectricalPowerServerFromReadings(electricalReading(c.reading)).MatterRead(c.attr)
		if !ok || v != c.want {
			t.Errorf("%s: read %v (%T), %v; want %v", c.name, v, v, ok, c.want)
		}
	}
}

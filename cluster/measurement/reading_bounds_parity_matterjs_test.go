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

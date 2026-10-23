// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package thermostat is the generated definition of the Matter Thermostat
// cluster (definition_gen.go, server_gen.go) and the value helpers a host
// encoding its temperatures needs (values.go).
package thermostat

import "math"

// CelsiusToInt16 converts a Celsius temperature to the Matter wire
// encoding (int16 × 0.01 °C). Clamps to [−27315, 32766] rather than
// the raw int16 limits: 32767 is the TLV-null sentinel
// (Matter §2.3.5.1 / chip `kMaxMeasuredValueRange = 32766`) and
// −32768 falls below the physical absolute-zero floor
// (chip `kMinMeasuredValueRange = -27315 = -273.15 °C`).
// Mirrors matter.js `packages/model/src/standard/elements/
// temperature-measurement.element.ts` + chip
// `src/app/clusters/temperature-measurement-server/
// TemperatureMeasurementCluster.cpp:27-28`.
//
// One encoder for the thermostat setpoints and limits and the
// TemperatureMeasurement server (cluster/measurement calls it): two
// encoders for one wire format can round a boundary differently without
// anything failing. It lives in the generated package, beside the
// definition, so a declaration-only host reaches it without a server
// package.
func CelsiusToInt16(c float64) int16 {
	v := math.Round(c * 100)
	if v > 32766 { // 32767 is the Matter NULL sentinel — must not be emitted as a real value
		return 32766
	}
	if v < -27315 { // −273.15 °C absolute-zero floor per chip kMinMeasuredValueRange
		return -27315
	}
	return int16(v)
}

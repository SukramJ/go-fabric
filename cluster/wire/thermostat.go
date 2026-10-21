// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"errors"
)

// Thermostat command IDs per Matter §4.3.9.
const (
	ThermostatCmdSetpointRaiseLower  uint32 = 0x00
	ThermostatCmdSetWeeklySchedule   uint32 = 0x01
	ThermostatCmdGetWeeklySchedule   uint32 = 0x02
	ThermostatCmdClearWeeklySchedule uint32 = 0x03
)

// SetpointRaiseLower mode values per Matter §4.3.9.1.
const (
	ThermostatSetpointModeHeat uint8 = 0
	ThermostatSetpointModeCool uint8 = 1
	ThermostatSetpointModeBoth uint8 = 2
)

// ErrThermostatMalformed is returned for malformed Thermostat command
// payloads.
var ErrThermostatMalformed = errors.New("wire: Thermostat command malformed")

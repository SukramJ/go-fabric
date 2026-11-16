// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

// Thermostat command IDs per Matter §4.3.9.
const (
	// ThermostatCmdSetpointRaiseLower duplicates a generated constant.
	//
	// Deprecated: use CmdSetpointRaiseLower of cluster/spec/thermostat, the
	// generated id: one source of ids (review II, R2-P6-par-1262,
	// decision 140). Removable in the next minor release.
	ThermostatCmdSetpointRaiseLower uint32 = 0x00
	// ThermostatCmdSetWeeklySchedule names a command id.
	//
	// Deprecated: the command is not in the Matter model this module serves
	// (parity/schema.json has no command 0x01 on Thermostat); nothing
	// outside this package uses it (review II, R2-P6-api-1261, decision 140).
	// Removable in the next minor release.
	ThermostatCmdSetWeeklySchedule uint32 = 0x01
	// ThermostatCmdGetWeeklySchedule names a command id.
	//
	// Deprecated: the command is not in the Matter model this module serves
	// (parity/schema.json has no command 0x02 on Thermostat), where 0x02 is
	// AddThermostatSuggestionResponse; nothing outside this package uses it
	// (review II, R2-P6-api-1261, decision 140).
	// Removable in the next minor release.
	ThermostatCmdGetWeeklySchedule uint32 = 0x02
	// ThermostatCmdClearWeeklySchedule names a command id.
	//
	// Deprecated: the command is not in the Matter model this module serves
	// (parity/schema.json has no command 0x03 on Thermostat); nothing
	// outside this package uses it (review II, R2-P6-api-1261, decision 140).
	// Removable in the next minor release.
	ThermostatCmdClearWeeklySchedule uint32 = 0x03
)

// SetpointRaiseLower mode values per Matter §4.3.9.1.
const (
	ThermostatSetpointModeHeat uint8 = 0
	ThermostatSetpointModeCool uint8 = 1
	ThermostatSetpointModeBoth uint8 = 2
)

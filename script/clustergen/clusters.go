// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

// committed lists the clusters whose generated definitions are committed
// under cluster/spec/: each is the definition a server in this module is
// built on. A cluster joins when its server does; the migration list in
// docs/adr/0013-generated-cluster-definitions.md says which servers are
// next.
var committed = []string{
	// cluster/pump
	"PumpConfigurationAndControl",
	// cluster/modebase
	"LaundryWasherMode",
	"RvcRunMode",
	"RvcCleanMode",
	"DishwasherMode",
	// cluster/filter
	"HepaFilterMonitoring",
	"ActivatedCarbonFilterMonitoring",
	// cluster/light
	"ColorControl",
	// cluster/onoff
	"OnOff",
	// cluster/valve
	"ValveConfigurationAndControl",
	// cluster/modeselect
	"ModeSelect",
	// cluster/alarm
	"SmokeCoAlarm",
	// cluster/fan
	"FanControl",
	// cluster/opstate
	"OperationalState",
	"RvcOperationalState",
	// cluster/levelcontrol
	"LevelControl",
	// cluster/closure
	"ClosureControl",
	// cluster/cover
	"WindowCovering",
	// cluster/lock
	"DoorLock",
	// cluster/thermo
	"Thermostat",
}

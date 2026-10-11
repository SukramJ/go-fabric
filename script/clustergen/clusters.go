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
	"EnergyEvseMode",
	"WaterHeaterMode",
	"DeviceEnergyManagementMode",
	"MicrowaveOvenMode",
	"OvenMode",
	"RefrigeratorAndTemperatureControlledCabinetMode",
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
	// cluster/alarmbase
	"DishwasherAlarm",
	"RefrigeratorAlarm",
	"TemperatureAlarm",
	// cluster/appliance
	"LaundryWasherControls",
	"LaundryDryerControls",
	"MicrowaveOvenControl",
	// cluster/fan
	"FanControl",
	// cluster/opstate
	"OperationalState",
	"RvcOperationalState",
	"OvenCavityOperationalState",
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
	"TemperatureControl",
	// cluster/measurement
	"TemperatureMeasurement",
	"RelativeHumidityMeasurement",
	"IlluminanceMeasurement",
	"PressureMeasurement",
	"FlowMeasurement",
	"BooleanState",
	"OccupancySensing",
	"AirQuality",
	"CarbonDioxideConcentrationMeasurement",
	"Pm25ConcentrationMeasurement",
	"Pm10ConcentrationMeasurement",
	"CarbonMonoxideConcentrationMeasurement",
	"NitrogenDioxideConcentrationMeasurement",
	"OzoneConcentrationMeasurement",
	"FormaldehydeConcentrationMeasurement",
	"Pm1ConcentrationMeasurement",
	"TotalVolatileOrganicCompoundsConcentrationMeasurement",
	"RadonConcentrationMeasurement",
	"BooleanStateConfiguration",
	"PowerSource",
	"PowerTopology",
	"ElectricalPowerMeasurement",
	"ElectricalEnergyMeasurement",
	// cluster/energy
	"WaterHeaterManagement",
	"EnergyPreference",
	"MeterIdentification",
	"DeviceEnergyManagement",
	"EnergyEvse",
	// cluster/wire
	"Switch",
	"AdministratorCommissioning",
	"Groups",
	"ScenesManagement",
	// cluster/core
	"Identify",
	"Descriptor",
	"BasicInformation",
	"BridgedDeviceBasicInformation",
	"GeneralDiagnostics",
	"GeneralCommissioning",
	"TimeSynchronization",
	"Binding",
	"IcdManagement",
	"OtaSoftwareUpdateRequestor",
	"NetworkCommissioning",
	"DiagnosticLogs",
	"FixedLabel",
	"UserLabel",
	"LocalizationConfiguration",
	"TimeFormatLocalization",
	"UnitLocalization",
	"SoftwareDiagnostics",
	"EthernetNetworkDiagnostics",
}

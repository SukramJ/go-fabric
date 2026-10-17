// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// committed lists the clusters whose generated definitions are committed
// under cluster/spec/: each is the definition a server in this module is
// built on, or one the client role reads typed (the last group). A server's
// cluster joins when its server does; the migration list in
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
	"ClosureDimension",
	// cluster/servicearea
	"ServiceArea",
	// cluster/cover
	"WindowCovering",
	// cluster/lock
	"DoorLock",
	// cluster/thermo
	"Thermostat",
	"TemperatureControl",
	"ThermostatUserInterfaceConfiguration",
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
	"SoilMeasurement",
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
	"CommodityPrice",
	"CommodityTariff",
	"CommodityMetering",
	"ElectricalGridConditions",
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
	// cluster/core OtaSoftwareUpdateRequestor, client side: the requestor
	// invokes QueryImage, ApplyUpdateRequest and NotifyUpdateApplied on a
	// provider and decodes the responses with these codecs (ADR 0019);
	// cluster/core OTAProvider serves the cluster since ADR 0021.
	"OtaSoftwareUpdateProvider",
	"NetworkCommissioning",
	"DiagnosticLogs",
	"FixedLabel",
	"UserLabel",
	"LocalizationConfiguration",
	"TimeFormatLocalization",
	"UnitLocalization",
	"SoftwareDiagnostics",
	"EthernetNetworkDiagnostics",
	"WiFiNetworkDiagnostics",
	"PowerSourceConfiguration",
	"EcosystemInformation",
	"Actions",
	// cluster/media (ADR 0022)
	"MediaPlayback",
	"KeypadInput",
	"ContentLauncher",
	"ApplicationLauncher",
	"ApplicationBasic",
	"AccountLogin",
	"AudioOutput",
	"Channel",
	"TargetNavigator",
	"MediaInput",
	"LowPower",
	"WakeOnLan",
	"ContentControl",
	"ContentAppObserver",
	// cluster/core Messages (ADR 0022)
	"Messages",
	// cluster/core CommissionerControl (ADR 0020 stage 6, brief AS)
	"CommissionerControl",
	// controller/client: definitions without a server in this module, so
	// the client role reads their attributes, commands and events typed
	// (register line 441, brief AX §11). Left out: AccessControl,
	// OperationalCredentials and GroupKeyManagement (hand-written core
	// servers, ADR 0013's skip list), the camera clusters, Joint Fabric,
	// TLS and Thread (out of scope, docs/matterjs-comparison.md).
	"Groupcast",
	"WaterTankLevelMonitoring",
	"AmbientContextSensing",
	"WiFiNetworkManagement",
	"Chime",
}

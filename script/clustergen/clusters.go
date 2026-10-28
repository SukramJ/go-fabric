// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// committed lists the clusters whose generated definitions and servers
// are committed under cluster/spec/: each is the definition a server in
// this module is built on, one the client role reads typed, or one the
// generated server serves as matter.js's stub behaviour does (the last
// two groups). With definitionOnly it names every snapshot cluster
// (TestEverySnapshotClusterIsCommitted). A server's
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
	// cluster/netinfra, after connectedhomeip (brief 26-B; matter.js serves
	// both as the generated stub)
	"WiFiNetworkManagement",
	"ThreadNetworkDirectory",
	// cluster/threadbr, after connectedhomeip over an OTBR's REST API
	// (ADR 0027, brief 26a; matter.js serves both as the generated stub)
	"ThreadBorderRouterManagement",
	"ThreadNetworkDiagnostics",
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
	// cluster/camera (briefs 26-A1, 26-A2, 26-A3): WebRtcTransportRequestor
	// after matter.js's WebRtcTransportRequestorServer.ts; Chime,
	// CameraAvSettingsUserLevelManagement, CameraAvStreamManagement,
	// ZoneManagement, WebRtcTransportProvider and PushAvStreamTransport
	// after connectedhomeip.
	"WebRtcTransportRequestor",
	"Chime",
	"CameraAvSettingsUserLevelManagement",
	"CameraAvStreamManagement",
	"ZoneManagement",
	"WebRtcTransportProvider",
	"PushAvStreamTransport",
	// cluster/tls (brief 26-A3): after connectedhomeip.
	"TlsCertificateManagement",
	"TlsClientManagement",
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
	// (register line 441, brief AX §11).
	"Groupcast",
	"WaterTankLevelMonitoring",
	// cluster/sensing (brief 26-C): AmbientContextSensing after
	// connectedhomeip.
	"AmbientContextSensing",
	// cluster/jointfabric (brief 26-D): JointFabricDatastore and
	// JointFabricAdministrator after connectedhomeip.
	"JointFabricDatastore",
	"JointFabricAdministrator",
}

// definitionOnly lists the clusters whose definition is committed without
// a generated server: a hand-written server of this module serves each
// (ADR 0013's skip list — the access control, operational credentials and
// group key servers of cluster/core), so the package holds the definition
// and the typed state (State, Patch, StateFrom) the client role reads and
// writes the cluster through, and no Server, Wrap or Declare. Each
// package's hand-written core_parity_test.go holds the server's wire
// constants against the definition (ADR 0013, amendment of 2026-10-25).
var definitionOnly = []string{
	"AccessControl",
	"OperationalCredentials",
	"GroupKeyManagement",
}

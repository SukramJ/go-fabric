// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// committed lists the device types whose generated files are committed
// under device/: the device types in scope of
// docs/concept-matter-1.6.1-and-device-layer.md Appendix F.3 — every
// snapshot device type the appendix does not name, less the three whose
// mandatory clusters are the excluded camera and media families (owner's
// decision, 2026-10-11) — with OtaRequestor, which ADR 0018 ("Decisions
// taken at the start of wave 6", 1) takes in since the requestor joined in
// wave 5 (ADR 0019), and the eleven controller device types ADR 0020's
// stage 5 takes in (brief AO): their client clusters are what the binding
// manager selects for a Binding entry. Sorted by name.
// wave 5 (ADR 0019), SecondaryNetworkInterface, which concept v2 §B.4
// takes in with the WiFiNetworkDiagnostics server (brief AM), and
// OtaProvider, which ADR 0021 takes in with the OTA Software Update
// Provider server (brief AQ), and the three media device types
// (BasicVideoPlayer, CastingVideoPlayer, ContentApp), which ADR 0022 takes
// in with the media and content clusters (brief AR). Sorted by name.
var committed = []string{
	"Aggregator",
	"AirPurifier",
	"AirQualitySensor",
	"BasicVideoPlayer",
	"BatteryStorage",
	"BridgedNode",
	"CastingVideoPlayer",
	"Closure",
	"ClosureController",
	"ClosurePanel",
	"ColorDimmerSwitch",
	"ColorTemperatureLight",
	"ContactSensor",
	"ContentApp",
	"ControlBridge",
	"CookSurface",
	"Cooktop",
	"DeviceEnergyManagement",
	"DimmableLight",
	"DimmablePlugInUnit",
	"DimmerSwitch",
	"Dishwasher",
	"DoorLock",
	"DoorLockController",
	"Doorbell",
	"ElectricalEnergyTariff",
	"ElectricalMeter",
	"ElectricalSensor",
	"ElectricalUtilityMeter",
	"EnergyEvse",
	"ExtendedColorLight",
	"ExtractorHood",
	"Fan",
	"FlowSensor",
	"GenericSwitch",
	"HeatPump",
	"HumiditySensor",
	"IrrigationSystem",
	"LaundryDryer",
	"LaundryWasher",
	"LightSensor",
	"MeterReferencePoint",
	"MicrowaveOven",
	"ModeSelect",
	"MountedDimmableLoadControl",
	"MountedOnOffControl",
	"OccupancySensor",
	"OnOffLight",
	"OnOffLightSwitch",
	"OnOffPlugInUnit",
	"OnOffSensor",
	"OtaProvider",
	"OtaRequestor",
	"Oven",
	"PowerSource",
	"PressureSensor",
	"Pump",
	"PumpController",
	"RainSensor",
	"Refrigerator",
	"RoboticVacuumCleaner",
	"RoomAirConditioner",
	"RootNode",
	"SecondaryNetworkInterface",
	"SmokeCoAlarm",
	"SoilSensor",
	"SolarPower",
	"Speaker",
	"TemperatureControlledCabinet",
	"TemperatureSensor",
	"Thermostat",
	"ThermostatController",
	"VideoRemoteControl",
	"WaterFreezeDetector",
	"WaterHeater",
	"WaterLeakDetector",
	"WaterValve",
	"WindowCovering",
	"WindowCoveringController",
}

// cameraMedia is the group of the device types not named in Appendix F.3
// whose mandatory requirements are its excluded families.
const cameraMedia = "camera / media (Appendix F.3: their mandatory clusters are the excluded camera and media families)"

// excluded names every other snapshot device type with its group in
// docs/concept-matter-1.6.1-and-device-layer.md Appendix F.3.
var excluded = map[string]string{
	// Camera device types: they mandate the F.2 camera clusters.
	"Camera":           "F.3 camera device types",
	"SnapshotCamera":   "F.3 camera device types",
	"AudioDoorbell":    "F.3 camera device types",
	"Intercom":         "F.3 camera device types",
	"Chime":            "F.3 camera device types",
	"CameraController": "F.3 camera device types",
	// The media device types left the group with ADR 0022.
	// Infrastructure: F.2 Thread or Joint Fabric. OtaProvider left the
	// group with ADR 0021.
	"ThreadBorderRouter":           "F.3 infrastructure",
	"NetworkInfrastructureManager": "F.3 infrastructure",
	"JointFabricAdministrator":     "F.3 infrastructure",
	// Camera / media by their mandatory requirements (owner's decision,
	// 2026-10-11), read from parity/schema.json effective.requirements:
	// VideoDoorbell and FloodlightCamera mandate a Camera component
	// ("deviceType Camera, M, 1"), whose mandatory server clusters
	// CameraAvStreamManagement and WebRtcTransportProvider are the F.2
	// camera family; CastingVideoClient mandates the media client clusters
	// KeypadInput, ContentLauncher and ApplicationBasic ("M").
	"VideoDoorbell":      cameraMedia,
	"FloodlightCamera":   cameraMedia,
	"CastingVideoClient": cameraMedia,
}

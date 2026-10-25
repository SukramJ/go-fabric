// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// committed lists the device types whose generated files are committed
// under device/: the device types in scope of
// docs/history/concept-matter-1.6.1-and-device-layer.md Appendix F.3 — every
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
// in with the media and content clusters (brief AR), and the camera and
// infrastructure device types (Camera, SnapshotCamera, AudioDoorbell,
// Intercom, Chime, CameraController, ThreadBorderRouter,
// NetworkInfrastructureManager, JointFabricAdministrator), which brief CL
// takes in with the generated servers of their clusters (owner decision,
// 2026-10-25). Sorted by name.
var committed = []string{
	"Aggregator",
	"AirPurifier",
	"AirQualitySensor",
	"AudioDoorbell",
	"BasicVideoPlayer",
	"BatteryStorage",
	"BridgedNode",
	"Camera",
	"CameraController",
	"CastingVideoPlayer",
	"Chime",
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
	"Intercom",
	"IrrigationSystem",
	"JointFabricAdministrator",
	"LaundryDryer",
	"LaundryWasher",
	"LightSensor",
	"MeterReferencePoint",
	"MicrowaveOven",
	"ModeSelect",
	"MountedDimmableLoadControl",
	"MountedOnOffControl",
	"NetworkInfrastructureManager",
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
	"SnapshotCamera",
	"SoilSensor",
	"SolarPower",
	"Speaker",
	"TemperatureControlledCabinet",
	"TemperatureSensor",
	"Thermostat",
	"ThermostatController",
	"ThreadBorderRouter",
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
// docs/history/concept-matter-1.6.1-and-device-layer.md Appendix F.3.
var excluded = map[string]string{
	// The camera device types (Camera, SnapshotCamera, AudioDoorbell,
	// Intercom, Chime, CameraController) and the infrastructure ones
	// (ThreadBorderRouter, NetworkInfrastructureManager,
	// JointFabricAdministrator) left the group on 2026-10-25 (brief CL §1,
	// owner decision): their clusters have generated servers. The media
	// device types left it with ADR 0022, OtaProvider with ADR 0021.
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

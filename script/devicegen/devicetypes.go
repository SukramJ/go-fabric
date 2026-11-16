// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// committed lists the device types whose generated files are committed
// under device/, sorted by name. The scope is
// docs/history/concept-matter-1.6.1-and-device-layer.md Appendix F.3 —
// every snapshot device type the appendix does not name, less the three
// whose mandatory clusters are the excluded camera and media families
// (owner's decision, 2026-10-11) — and the types taken in since:
// OtaRequestor (ADR 0018, "Decisions taken at the start of wave 6", 1;
// the requestor joined in wave 5, ADR 0019); the eleven controller device
// types of ADR 0020's stage 5 (brief AO), whose client clusters are what
// the binding manager selects for a Binding entry;
// SecondaryNetworkInterface (concept v2 §B.4, with the
// WiFiNetworkDiagnostics server, brief AM); OtaProvider (ADR 0021, with
// the OTA Software Update Provider server, brief AQ); the three media
// device types BasicVideoPlayer, CastingVideoPlayer and ContentApp (ADR
// 0022, with the media and content clusters, brief AR); the camera and
// infrastructure device types Camera, SnapshotCamera, AudioDoorbell,
// Intercom, Chime, CameraController, ThreadBorderRouter,
// NetworkInfrastructureManager and JointFabricAdministrator (brief CL,
// with the generated servers of their clusters, owner decision
// 2026-10-25); and CastingVideoClient (wave 27-C2, owner decision
// 2026-10-29).
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
	"CastingVideoClient",
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
	"FloodlightCamera",
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
	"VideoDoorbell",
	"VideoRemoteControl",
	"WaterFreezeDetector",
	"WaterHeater",
	"WaterLeakDetector",
	"WaterValve",
	"WindowCovering",
	"WindowCoveringController",
}

// excluded names every other snapshot device type with its group in
// docs/history/concept-matter-1.6.1-and-device-layer.md Appendix F.3. It is
// empty since wave 27-C2: every snapshot device type is committed. The
// camera device types (Camera, SnapshotCamera, AudioDoorbell, Intercom,
// Chime, CameraController) and the infrastructure ones (ThreadBorderRouter,
// NetworkInfrastructureManager, JointFabricAdministrator) left the group on
// 2026-10-25 (brief CL §1, owner decision): their clusters have generated
// servers. The media device types left it with ADR 0022, OtaProvider with
// ADR 0021, and the composites VideoDoorbell and FloodlightCamera with wave
// 27-B (concept v10 0.5): their components are committed device types,
// which their generated constructors take as parts (gen.go
// compositeComponents). CastingVideoClient, excluded as camera / media by
// its mandatory media client clusters (owner's decision, 2026-10-11), left
// it with wave 27-C2 (owner decision, 2026-10-29): matter.js
// casting-video-client.ts carries the Binding server, the optional
// ContentAppObserver server and client behaviours only, which the
// generator emits as it does for VideoRemoteControl.
var excluded = map[string]string{}

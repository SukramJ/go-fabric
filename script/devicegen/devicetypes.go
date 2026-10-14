// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

// committed lists the device types whose generated files are committed
// under device/: the device types in scope of
// docs/concept-matter-1.6.1-and-device-layer.md Appendix F.3 — every
// snapshot device type the appendix does not name, less the three whose
// mandatory clusters are the excluded camera and media families (owner's
// decision, 2026-10-11) — with OtaRequestor, which ADR 0018 ("Decisions
// taken at the start of wave 6", 1) takes in since the requestor joined in
// wave 5 (ADR 0019), and SecondaryNetworkInterface, which concept v2 §B.4
// takes in with the WiFiNetworkDiagnostics server (brief AM). Sorted by
// name.
var committed = []string{
	"Aggregator",
	"AirPurifier",
	"AirQualitySensor",
	"BatteryStorage",
	"BridgedNode",
	"Closure",
	"ClosurePanel",
	"ColorTemperatureLight",
	"ContactSensor",
	"CookSurface",
	"Cooktop",
	"DeviceEnergyManagement",
	"DimmableLight",
	"DimmablePlugInUnit",
	"Dishwasher",
	"DoorLock",
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
	"OnOffPlugInUnit",
	"OtaRequestor",
	"Oven",
	"PowerSource",
	"PressureSensor",
	"Pump",
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
	"WaterFreezeDetector",
	"WaterHeater",
	"WaterLeakDetector",
	"WaterValve",
	"WindowCovering",
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
	// Media device types: they mandate the F.2 media clusters.
	"BasicVideoPlayer":   "F.3 media device types",
	"CastingVideoPlayer": "F.3 media device types",
	"ContentApp":         "F.3 media device types",
	// Infrastructure: F.2 Thread, Joint Fabric or OTA provider.
	"ThreadBorderRouter":           "F.3 infrastructure",
	"NetworkInfrastructureManager": "F.3 infrastructure",
	"JointFabricAdministrator":     "F.3 infrastructure",
	"OtaProvider":                  "F.3 infrastructure",
	// Controller device types: client clusters and Binding only, the
	// initiator role.
	"OnOffLightSwitch":         "F.3 controller device types",
	"DimmerSwitch":             "F.3 controller device types",
	"ColorDimmerSwitch":        "F.3 controller device types",
	"ControlBridge":            "F.3 controller device types",
	"OnOffSensor":              "F.3 controller device types",
	"DoorLockController":       "F.3 controller device types",
	"WindowCoveringController": "F.3 controller device types",
	"ThermostatController":     "F.3 controller device types",
	"PumpController":           "F.3 controller device types",
	"ClosureController":        "F.3 controller device types",
	"VideoRemoteControl":       "F.3 controller device types",
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

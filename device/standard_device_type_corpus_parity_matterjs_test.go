// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js
// packages/node/test/endpoint/validation/StandardDeviceTypeCorpusTest.ts at
// 862ad75f: every standard device type built with the state its server
// needs and judged, carrying only the violations the corpus pins.
//
// matter.js adds each generated endpoint type to a node's root (a
// BridgedNode under an Aggregator, RootNode as the node's own root) with
// the per-type state of its fixtures table (:52-172); here each committed
// kit type is built with the host's statements of the device package's
// fixtures (conformance_test.go, the same state in Go form) but without
// the parts those fixtures add — the corpus judges a type as generated,
// its components the developer's — assembled as a bridged device below
// the Aggregator, and judged (endpoint.ValidateDeviceTypes). The
// violations it may carry are matter.js's pinnedViolations (:314-364),
// a mandatory server the type cannot construct by itself because its
// mandated features are no legal selection (unimplementedMandatoryServers,
// :366-384), and the ones the Go fixture tolerates with its reason. A name
// of STANDARD_DEVICE_TYPE_NAMES the module does not commit is a skipped
// subcase with devicegen's reason; the types the kit does not build as a
// bridged device (the node's root and Aggregator, BridgedNode,
// OtaRequestor) are skipped with the fixture table's reason.
package device

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// standardDeviceTypeNames is STANDARD_DEVICE_TYPE_NAMES
// (StandardDeviceTypeCorpusTest.ts:433-526).
var standardDeviceTypeNames = []string{
	"Aggregator", "AirPurifier", "AirQualitySensor", "AudioDoorbell", "BasicVideoPlayer", "BatteryStorage",
	"BridgedNode", "Camera", "CameraController", "CastingVideoClient", "CastingVideoPlayer", "Chime", "Closure",
	"ClosureController", "ClosurePanel", "ColorDimmerSwitch", "ColorTemperatureLight", "ContactSensor",
	"ContentApp", "ControlBridge", "CookSurface", "Cooktop", "DeviceEnergyManagement", "DimmableLight",
	"DimmablePlugInUnit", "DimmerSwitch", "Dishwasher", "Doorbell", "DoorLock", "DoorLockController",
	"ElectricalEnergyTariff", "ElectricalMeter", "ElectricalSensor", "ElectricalUtilityMeter", "EnergyEvse",
	"ExtendedColorLight", "ExtractorHood", "Fan", "FloodlightCamera", "FlowSensor", "GenericSwitch", "HeatPump",
	"HumiditySensor", "Intercom", "IrrigationSystem", "JointFabricAdministrator", "LaundryDryer", "LaundryWasher",
	"LightSensor", "MeterReferencePoint", "MicrowaveOven", "ModeSelect", "MountedDimmableLoadControl",
	"MountedOnOffControl", "NetworkInfrastructureManager", "OccupancySensor", "OnOffLight", "OnOffLightSwitch",
	"OnOffPlugInUnit", "OnOffSensor", "OtaProvider", "OtaRequestor", "Oven", "PowerSource", "PressureSensor",
	"Pump", "PumpController", "RainSensor", "Refrigerator", "RoboticVacuumCleaner", "RoomAirConditioner",
	"RootNode", "SecondaryNetworkInterface", "SmokeCoAlarm", "SnapshotCamera", "SoilSensor", "SolarPower",
	"Speaker", "TemperatureControlledCabinet", "TemperatureSensor", "Thermostat", "ThermostatController",
	"ThreadBorderRouter", "VideoDoorbell", "VideoRemoteControl", "WaterFreezeDetector", "WaterHeater",
	"WaterLeakDetector", "WaterValve", "WindowCovering", "WindowCoveringController",
}

// pinnedViolations is the corpus's pinnedViolations
// (StandardDeviceTypeCorpusTest.ts:314-364), as "<kind> <deviceType>
// <requirement>": the components a developer adds.
var pinnedViolations = map[string][]string{
	"BatteryStorage":   {"instanceCount BatteryStorage device:ElectricalSensor", "instanceCount BatteryStorage device:PowerSource", "instanceCount BatteryStorage device:DeviceEnergyManagement"},
	"ElectricalMeter":  {"instanceCount ElectricalMeter device:ElectricalSensor"},
	"EnergyEvse":       {"instanceCount EnergyEvse device:PowerSource", "instanceCount EnergyEvse device:DeviceEnergyManagement", "instanceCount EnergyEvse device:ElectricalSensor"},
	"FloodlightCamera": {"instanceCount FloodlightCamera device:OnOffLight", "instanceCount FloodlightCamera device:Camera"},
	"HeatPump":         {"instanceCount HeatPump device:PowerSource", "instanceCount HeatPump device:DeviceEnergyManagement", "instanceCount HeatPump device:ElectricalSensor"},
	"Intercom":         {"instanceCount Intercom device:GenericSwitch"},
	"IrrigationSystem": {"instanceCount IrrigationSystem device:WaterValve"},
	"Oven":             {"instanceCount Oven device:TemperatureControlledCabinet", "instanceCount Oven condition:Heater"},
	"Refrigerator":     {"instanceCount Refrigerator device:TemperatureControlledCabinet", "instanceCount Refrigerator condition:Cooler"},
	"SmokeCoAlarm":     {"instanceCount SmokeCoAlarm device:PowerSource"},
	"SolarPower":       {"instanceCount SolarPower device:PowerSource", "instanceCount SolarPower device:ElectricalSensor"},
	"VideoDoorbell":    {"instanceCount VideoDoorbell device:Camera", "instanceCount VideoDoorbell device:Doorbell"},
}

// devicegenExcluded reads script/devicegen's excluded map — the snapshot
// device types the module does not commit, each with its reason — from the
// generator's source. The map may be empty (every snapshot device type
// committed); the test fails only when the declaration is not found.
func devicegenExcluded(t *testing.T) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "../script/devicegen/devicetypes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The string constants a reason may name.
	consts := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok {
			for i, id := range vs.Names {
				if i < len(vs.Values) {
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						consts[id.Name], _ = strconv.Unquote(lit.Value)
					}
				}
			}
		}
		return true
	})
	out := map[string]string{}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "excluded" || len(vs.Values) != 1 {
			return true
		}
		lit, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		found = true
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			k, ok := kv.Key.(*ast.BasicLit)
			if !ok {
				continue
			}
			name, _ := strconv.Unquote(k.Value)
			switch v := kv.Value.(type) {
			case *ast.BasicLit:
				out[name], _ = strconv.Unquote(v.Value)
			case *ast.Ident:
				out[name] = consts[v.Name]
			}
		}
		return false
	})
	if !found {
		t.Fatal("no excluded map read from script/devicegen")
	}
	return out
}

// needsFeatureSelection reports whether the cluster's features must be
// selected for a legal selection — matter.js requiresFeatureSelection
// (StandardDeviceTypeCorpusTest.ts:378-384, FeatureSelectionViolations of
// the behavior's own selection, none).
func needsFeatureSelection(clusterID uint32) bool {
	def := spec.Lookup(clusterID)
	return def == nil || spec.CheckFeatures(def, 0) != nil
}

func TestStandardDeviceTypeCorpusParityMatterJS(t *testing.T) {
	excluded := devicegenExcluded(t)
	committed := map[string]*Type{}
	for _, ty := range Types() {
		committed[ty.Name] = ty
	}

	t.Run("StandardDeviceTypeCorpus", func(t *testing.T) {
		t.Run("collects the standard device type corpus", func(t *testing.T) {
			for name := range committed {
				if !slices.Contains(standardDeviceTypeNames, name) {
					t.Errorf("committed type %s is not in the corpus", name)
				}
			}
			for _, name := range standardDeviceTypeNames {
				_, isCommitted := committed[name]
				_, isExcluded := excluded[name]
				if isCommitted == isExcluded {
					t.Errorf("%s: committed %v, excluded %v; want exactly one", name, isCommitted, isExcluded)
				}
			}
			for name := range pinnedViolations {
				if !slices.Contains(standardDeviceTypeNames, name) {
					t.Errorf("stale pin %s", name)
				}
			}
		})

		t.Run("refuses a Closure endpoint without the tags its Descriptor TagList requires", func(t *testing.T) {
			t.Skip("StandardDeviceTypeCorpusTest.ts:548: a Descriptor TagList declaration has no Go subject yet (the SemanticNamespace subject, milestone 5)")
		})

		for _, name := range standardDeviceTypeNames {
			title := "judges " + name
			if name == "RootNode" {
				title = "judges RootNode as the root endpoint of a node, which carries the standard RootNode type"
			}
			t.Run(title, func(t *testing.T) {
				if reason, ok := excluded[name]; ok {
					t.Skip("not committed (script/devicegen excluded): " + reason)
				}
				if reason, ok := noFixture[name]; ok {
					t.Skip(reason)
				}
				ty := committed[name]
				d, err := New(ty, testOptions(name), testWith(t, name)...)
				if err != nil {
					t.Fatalf("%s failed to build: %v", name, err)
				}
				judgeCorpusType(t, name, d)
			})
		}
	})
}

// judgeCorpusType holds the assembled device's violations to the corpus.
func judgeCorpusType(t *testing.T, name string, d *Device) {
	t.Helper()
	mounted := map[uint32]bool{}
	for _, srv := range d.Servers() {
		mounted[srv.MatterClusterID()] = true
	}
	tolerate := fixtures[name].tolerate
	pinned := pinnedViolations[name]
	vs := violationsOf(assemble(t, d))
	got := make([]string, 0, len(vs))
	for i := range vs {
		v := &vs[i]
		text := string(v.Kind) + " " + v.DeviceType + " " + v.Requirement
		got = append(got, text)
		switch {
		case slices.Contains(pinned, text):
		case tolerate[v.Key()] != "":
		case v.Kind == "missing" && v.HasCluster && v.Element == "" && !mounted[v.Cluster] && needsFeatureSelection(v.Cluster):
		default:
			t.Errorf("violation %s is neither pinned nor a server only the developer can complete", text)
		}
	}
	for _, want := range pinned {
		if !slices.Contains(got, want) {
			t.Errorf("matter.js pins %s; the Go type does not carry it (got %s)", want, strings.Join(got, "; "))
		}
	}
}

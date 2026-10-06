// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	lightcluster "github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/groups"
)

// What the module itself controls, held to its device types by the
// validator: every application device type the module's servers are built
// for (application_device_types_test.go), every built-in measurement kind,
// and the light and detector servers — each assembled as a host assembles
// it. A bridged endpoint's violations are its own; the root and the
// Aggregator carry no servers here.

// bridgedViolations assembles one bridged endpoint and returns its
// violations.
func bridgedViolations(t *testing.T, spec Spec) []DeviceTypeViolation {
	t.Helper()
	mgr, err := groups.NewManager(nopGroupStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(&oneEndpointStore{}, Config{VendorID: 1, ProductID: 1, NodeLabel: "x", Groups: mgr}, nil)
	if err != nil {
		t.Fatal(err)
	}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{spec}}})
	if err != nil {
		t.Fatal(err)
	}
	var out []DeviceTypeViolation
	all := ValidateDeviceTypes(topo)
	for i := range all {
		if all[i].Endpoint == topo.Bridged()[0].ID {
			out = append(out, all[i])
		}
	}
	return out
}

func TestApplicationDeviceTypesMeetTheirDeviceTypeRequirements(t *testing.T) {
	t.Parallel()
	for _, tc := range applicationDeviceCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bridgedViolations(t, Spec{
				StableKey: StringKey(tc.name), DeviceType: tc.deviceType, FriendlyName: tc.name,
				Source: tc.source, Measurement: tc.measurement,
			})
			var want []string
			if tc.name == "SmokeCoAlarm" {
				// SmokeCoAlarm requires a PowerSource component endpoint (M, min
				// 1); a bridged endpoint has no parts of its own, so no host
				// can satisfy it here. Recorded in
				// notes/parity/matter_behaviour_findings.md.
				want = []string{"instanceCount device:PowerSource"}
			}
			wantKinds(t, got, want...)
		})
	}
}

// measurementSource answers every measurement kind's source interfaces.
type measurementSource struct{ class contract.MeasurementClass }

func (m measurementSource) MatterMeasurementClass() contract.MeasurementClass { return m.class }
func (measurementSource) MatterFloatValue() (float64, bool)                   { return 1, true }
func (measurementSource) MatterBoolValue() (value, observed bool)             { return true, true }
func (measurementSource) ActivePower() (float64, bool)                        { return 1, true }
func (measurementSource) Voltage() (float64, bool)                            { return 230, true }
func (measurementSource) Current() (float64, bool)                            { return 1, true }
func (measurementSource) Frequency() (float64, bool)                          { return 50, true }
func (measurementSource) Energy() (float64, bool)                             { return 1, true }
func (measurementSource) HasEnergy() bool                                     { return true }
func (measurementSource) MatterSwitchPositions() uint8                        { return 2 }
func (measurementSource) MatterSwitchSupportsLongPress() bool                 { return true }

func TestMeasurementKindsMeetTheirDeviceTypeRequirements(t *testing.T) {
	t.Parallel()
	for class := contract.MeasurementClass(1); class < 64; class++ {
		kind, ok := contract.MeasurementKindFor(class)
		if !ok || kind.DeviceType == 0 {
			continue
		}
		t.Run(kind.Name, func(t *testing.T) {
			t.Parallel()
			wantKinds(t, bridgedViolations(t, Spec{
				StableKey: StringKey(kind.Name), DeviceType: kind.DeviceType, FriendlyName: kind.Name,
				Measurement: measurementSource{class}, PowerSource: measurementSource{contract.MeasurementBattery},
			}))
		})
	}
}

// lightingOnOff is a host's OnOff server with the Lighting feature.
func lightingOnOff() contract.ClusterServer {
	return dtServer{id: onoff.ClusterID, features: onoff.FeatureLighting, attrs: onoff.LightingAttributes(), cmds: onoff.LightingCommands()}
}

// TestLightServersMeetTheLightDeviceTypes: the module's LevelControl (with
// Lighting) and ColorControl servers carry ColorTemperatureLight; the
// ColorControl server serves CT only, so an ExtendedColorLight lacks XY.
func TestLightServersMeetTheLightDeviceTypes(t *testing.T) {
	t.Parallel()
	servers := func() []contract.ClusterServer {
		return []contract.ClusterServer{
			lightingOnOff(),
			levelcontrol.NewServer(levelcontrol.Config{Lighting: true}),
			lightcluster.NewColorControlServer(lightcluster.ColorControlServerConfig{MinMireds: 153, MaxMireds: 500, InitialMireds: 300}),
		}
	}
	for _, tc := range []struct {
		name string
		dt   uint16
		want []string
	}{
		{"ColorTemperatureLight", 0x010C, nil},
		// XY is mandatory for ExtendedColorLight; the server does not
		// serve it (package light doc; matter_behaviour_findings.md).
		{"ExtendedColorLight", 0x010D, []string{"missing ColorControl.XY"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wantKinds(t, bridgedViolations(t, Spec{
				StableKey: StringKey(tc.name), DeviceType: tc.dt, FriendlyName: tc.name,
				Source: deviceTypeSource{dt: tc.dt, servers: servers()},
			}), tc.want...)
		})
	}
}

// TestBooleanStateServerMeetsTheDetectorDeviceTypes: Water Leak Detector,
// Water Freeze Detector and Rain Sensor require BooleanState's StateChange
// event, which the module's BooleanState server lists.
func TestBooleanStateServerMeetsTheDetectorDeviceTypes(t *testing.T) {
	t.Parallel()
	for name, dt := range map[string]uint16{"WaterLeakDetector": 0x0043, "WaterFreezeDetector": 0x0041, "RainSensor": 0x0044} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := measurement.NewBooleanStateServer(measurementSource{contract.MeasurementContact})
			wantKinds(t, bridgedViolations(t, Spec{
				StableKey: StringKey(name), DeviceType: dt, FriendlyName: name,
				Source: deviceTypeSource{dt: dt, servers: []contract.ClusterServer{server}},
			}))
		})
	}
}

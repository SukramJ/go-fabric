// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	dem "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	evse "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/internal/paritytest"
	"github.com/SukramJ/go-fabric/schema"
)

// TestParityMatterJS_DeviceEnergyManagementServer builds every feature
// selection the definition allows and holds each against the matter.js
// conformance (spectest.CheckServer): FeatureMap, ClusterRevision, the
// attribute, command and event lists, the privileges and the read-only
// writes. A selection the definition refuses is refused by the server.
func TestParityMatterJS_DeviceEnergyManagementServer(t *testing.T) {
	t.Parallel()
	built := 0
	for features := range energy.DemFeature(1 << 7) {
		srv, err := energy.NewDeviceEnergyManagement(energy.DeviceEnergyConfig{Features: features, Manager: energy.NopDemManager{}})
		if ferr := spec.CheckFeatures(dem.Definition, uint32(features)); ferr != nil {
			if err == nil {
				t.Errorf("selection 0x%02X built, the definition refuses it: %v", features, ferr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("selection 0x%02X: %v", features, err)
		}
		spectest.CheckServer(t, srv, dem.Definition, uint32(features))
		built++
	}
	if built == 0 {
		t.Fatal("no selection built")
	}
}

// TestParityMatterJS_EnergyEvseServer does the same for EnergyEvse, with
// and without the optional attributes and StartDiagnostics.
func TestParityMatterJS_EnergyEvseServer(t *testing.T) {
	t.Parallel()
	for features := range energy.EvseFeature(1 << 5) {
		for _, optional := range []bool{false, true} {
			srv, err := energy.NewEnergyEvse(energy.EvseConfig{
				Features: features, UserMaximumChargeCurrent: optional, RandomizationDelayWindow: optional,
				ApproximateEvEfficiency: optional && features&energy.EvseFeatureChargingPreferences != 0, StartDiagnostics: optional,
			})
			if ferr := spec.CheckFeatures(evse.Definition, uint32(features)); ferr != nil {
				if err == nil {
					t.Errorf("selection 0x%02X built, the definition refuses it: %v", features, ferr)
				}
				continue
			}
			if err != nil {
				t.Fatalf("selection 0x%02X: %v", features, err)
			}
			spectest.CheckServer(t, srv, evse.Definition, uint32(features))
		}
	}
}

// TestParityMatterJS_EvseDemSnapshot pins the snapshot facts the servers
// rest on: revisions, the commands' responses, the events' priorities,
// the enum values chip's rules name, and the device types: EnergyEvse
// 0x050C mandates EnergyEvse and EnergyEvseMode and a DeviceEnergyManagement
// part with PFR; DeviceEnergyManagement 0x050D offers DeviceEnergyManagement.
func TestParityMatterJS_EvseDemSnapshot(t *testing.T) {
	t.Parallel()
	d := paritytest.ClusterSnapshot(t, energy.ClusterIDDeviceEnergyManagement)
	if d.Revision != dem.Revision {
		t.Errorf("DeviceEnergyManagement revision %d, generated %d", d.Revision, dem.Revision)
	}
	for name, conf := range map[string]string{
		"PowerAdjustRequest": "PA", "CancelPowerAdjustRequest": "PA", "StartTimeAdjustRequest": "STA",
		"PauseRequest": "PAU", "ResumeRequest": "PAU", "ModifyForecastRequest": "FA",
		"RequestConstraintBasedForecast": "CON", "CancelRequest": "STA | FA | CON",
	} {
		if c := d.Command(t, name); c.Response != "status" || c.Conformance != conf {
			t.Errorf("%s %+v, want status / %s", name, c, conf)
		}
	}
	for _, name := range []string{"PowerAdjustStart", "PowerAdjustEnd", "Paused", "Resumed"} {
		if e := d.Event(t, name); e.Priority != "info" {
			t.Errorf("%s %+v", name, e)
		}
	}
	e := paritytest.ClusterSnapshot(t, energy.ClusterIDEnergyEvse)
	if e.Revision != evse.Revision {
		t.Errorf("EnergyEvse revision %d, generated %d", e.Revision, evse.Revision)
	}
	if c := e.Command(t, "GetTargets"); c.Response != "GetTargetsResponse" {
		t.Errorf("GetTargets %+v", c)
	}
	if ev := e.Event(t, "Fault"); ev.Priority != "critical" {
		t.Errorf("Fault %+v", ev)
	}
	if a := e.Attribute(t, "UserMaximumChargeCurrent"); a.Access != "RW VM" || a.Conformance != "O" {
		t.Errorf("UserMaximumChargeCurrent %+v", a)
	}
	// The enum values chip's state machine names.
	for name, v := range map[string]uint8{
		"NotPluggedIn": uint8(energy.EvseNotPluggedIn), "PluggedInDemand": uint8(energy.EvsePluggedInDemand),
		"PluggedInCharging": uint8(energy.EvsePluggedInCharging), "Fault": uint8(energy.EvseFaulted),
	} {
		if !enumHas(evse.StateEnumDef, name, uint64(v)) {
			t.Errorf("StateEnum %s is not %d", name, v)
		}
	}
	for name, v := range map[string]uint8{
		"Disabled": uint8(energy.SupplyDisabled), "ChargingEnabled": uint8(energy.SupplyChargingEnabled),
		"DisabledError": uint8(energy.SupplyDisabledError), "DisabledDiagnostics": uint8(energy.SupplyDisabledDiagnostics),
		"Enabled": uint8(energy.SupplyEnabled),
	} {
		if !enumHas(evse.SupplyStateEnumDef, name, uint64(v)) {
			t.Errorf("SupplyStateEnum %s is not %d", name, v)
		}
	}
	for name, v := range map[string]uint8{
		"NoOptOut": uint8(energy.OptOutNone), "LocalOptOut": uint8(energy.OptOutLocal),
		"GridOptOut": uint8(energy.OptOutGrid), "OptOut": uint8(energy.OptOutAll),
	} {
		if !enumHas(dem.OptOutStateEnumDef, name, uint64(v)) {
			t.Errorf("OptOutStateEnum %s is not %d", name, v)
		}
	}
	for _, c := range []struct{ deviceType, cluster uint32 }{
		{0x050C, energy.ClusterIDEnergyEvse},
		{0x050C, 0x009D}, // EnergyEvseMode
		{0x050D, energy.ClusterIDDeviceEnergyManagement},
	} {
		if !schema.DeviceTypeRequiresServerCluster(c.deviceType, c.cluster) {
			t.Errorf("device type 0x%04X does not require 0x%04X", c.deviceType, c.cluster)
		}
	}
}

func enumHas(e *spec.Enum, name string, v uint64) bool {
	for i := range e.Values {
		if e.Values[i].Name == name {
			return e.Values[i].Value == v
		}
	}
	return false
}

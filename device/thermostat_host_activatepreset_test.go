// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"bytes"
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	thermostatdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/im"
)

// TestThermostatHostActivatePreset: the device facade reaches the kit's
// ActivatePreset through the typed server's Host (decision 356) — the
// SetActivePresetRequest command's path run as the host: a known handle
// becomes ActivePresetHandle and moves the setpoints, an unknown one is
// INVALID_COMMAND and changes nothing, nil clears the handle and leaves
// the setpoints where they are.
func TestThermostatHostActivatePreset(t *testing.T) { //nolint:tparallel // the subtests are ordered: the activation, then the refusal and the clearing on its state
	t.Parallel()
	ctx := context.Background()
	home := []byte("Home")
	heat := int16(2150)
	types := []thermostatdef.PresetTypeStruct{{PresetScenario: thermostatdef.PresetScenarioOccupied, NumberOfPresets: 1}}
	presets := []thermostatdef.PresetStruct{{PresetHandle: spec.ValueOf(home), PresetScenario: thermostatdef.PresetScenarioOccupied, HeatingSetpoint: &heat, BuiltIn: spec.ValueOf(true)}}
	count := uint8(1)
	decl := thermostatdef.Declare{
		Features: thermostatdef.FeatureHeating | thermostatdef.FeaturePresets,
		Initial:  thermostatdef.Patch{NumberOfPresets: &count, PresetTypes: &types, Presets: &presets},
	}
	d, err := device.NewThermostat(device.Options{StableKey: device.StringKey("thermo-host-preset"), Name: "thermo"}, decl)
	if err != nil {
		t.Fatal(err)
	}
	srv := d.Thermostat()
	host := srv.Host()
	if host == nil {
		t.Fatal("the kit's Thermostat has no Host")
	}
	before := srv.Get().OccupiedHeatingSetpoint
	if before == heat {
		t.Fatalf("the preset's setpoint %d is already in effect; the test proves nothing", heat)
	}

	t.Run("activates the preset with its setpoints", func(t *testing.T) {
		if err := host.ActivatePreset(ctx, home); err != nil {
			t.Fatal(err)
		}
		st := srv.Get()
		if st.ActivePresetHandle == nil || !bytes.Equal(*st.ActivePresetHandle, home) {
			t.Errorf("ActivePresetHandle %v, want %q", st.ActivePresetHandle, home)
		}
		if st.OccupiedHeatingSetpoint != heat {
			t.Errorf("OccupiedHeatingSetpoint %d, want the preset's %d", st.OccupiedHeatingSetpoint, heat)
		}
	})

	t.Run("an unknown handle is INVALID_COMMAND", func(t *testing.T) {
		err := host.ActivatePreset(ctx, []byte("Away"))
		if spec.StatusOf(err) != im.StatusInvalidCommand {
			t.Fatalf("unknown handle: %v, want INVALID_COMMAND", err)
		}
		if st := srv.Get(); st.ActivePresetHandle == nil || !bytes.Equal(*st.ActivePresetHandle, home) {
			t.Errorf("ActivePresetHandle %v after the refusal, want %q", st.ActivePresetHandle, home)
		}
	})

	t.Run("nil clears the handle, the setpoints stay", func(t *testing.T) {
		if err := host.ActivatePreset(ctx, nil); err != nil {
			t.Fatal(err)
		}
		st := srv.Get()
		if st.ActivePresetHandle != nil {
			t.Errorf("ActivePresetHandle %q, want null", *st.ActivePresetHandle)
		}
		if st.OccupiedHeatingSetpoint != heat {
			t.Errorf("OccupiedHeatingSetpoint %d, want %d untouched", st.OccupiedHeatingSetpoint, heat)
		}
	})
}

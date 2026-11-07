// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/surface"
)

// TestThermostatDeclaresSuggestions: ThermostatSuggestions is ahead of
// matter.js (thermo.Ahead), refused without its register row; with it the
// declaration's Initial gives MaxThermostatSuggestions, and the server
// reads the node's Matter time, which the node hands it as the device
// joins the topology: before that a suggestion answers INVALID_IN_STATE,
// with the node's time it is taken and followed (the node's own wiring is
// node's TestOneMatterTimePerNode).
func TestThermostatDeclaresSuggestions(t *testing.T) {
	t.Parallel()
	handle := []byte("Home")
	heat := int16(2100)
	types := []thermostatdef.PresetTypeStruct{{PresetScenario: thermostatdef.PresetScenarioOccupied, NumberOfPresets: 1}}
	presets := []thermostatdef.PresetStruct{{PresetHandle: spec.ValueOf(handle), PresetScenario: thermostatdef.PresetScenarioOccupied, HeatingSetpoint: &heat, BuiltIn: spec.ValueOf(true)}}
	count, limit := uint8(1), uint8(6)
	decl := thermostatdef.Declare{
		Features: thermostatdef.FeatureHeating | thermostatdef.FeaturePresets | thermostatdef.FeatureThermostatSuggestions,
		Initial:  thermostatdef.Patch{NumberOfPresets: &count, PresetTypes: &types, Presets: &presets, MaxThermostatSuggestions: &limit},
	}
	opts := device.Options{StableKey: device.StringKey("thermo-tsuggest"), Name: "thermo"}
	if _, err := device.NewThermostat(opts, decl); !errors.Is(err, surface.ErrExtensionNotEnabled) {
		t.Fatalf("without the row: %v, want ErrExtensionNotEnabled", err)
	}
	opts.Surface = surface.Policies{Extensions: surface.NewExtensions("BD-chip-Thermostat-Suggestions")}
	d, err := device.NewThermostat(opts, decl)
	if err != nil {
		t.Fatal(err)
	}
	srv := d.Thermostat()
	if got := srv.Get().MaxThermostatSuggestions; got != limit {
		t.Errorf("MaxThermostatSuggestions %d, want %d", got, limit)
	}
	add := func() (any, error) {
		return srv.Underlying().MatterInvoke(context.Background(), thermostatdef.CmdAddThermostatSuggestion,
			thermostatdef.AddThermostatSuggestionRequest{PresetHandle: handle, EffectiveTime: spec.Nullable[uint32]{Null: true}, ExpirationInMinutes: 30})
	}
	if _, err := add(); spec.StatusOf(err) != im.StatusInvalidInState {
		t.Fatalf("before the node's time: %v, want INVALID_IN_STATE", err)
	}
	participant, ok := srv.Underlying().(interface {
		AttachMatterTime(func() (uint32, bool))
	})
	if !ok {
		t.Fatal("the kit's Thermostat takes no Matter time")
	}
	participant.AttachMatterTime(func() (uint32, bool) { return 800_000_000, true })
	resp, err := add()
	if err != nil {
		t.Fatal(err)
	}
	st := srv.Get()
	if r, ok := resp.(thermostatdef.AddThermostatSuggestionResponse); !ok || st.CurrentThermostatSuggestion == nil || st.CurrentThermostatSuggestion.UniqueId != r.UniqueId {
		t.Errorf("response %+v, current %+v", resp, st.CurrentThermostatSuggestion)
	}
	if st.ActivePresetHandle == nil || !bytes.Equal(*st.ActivePresetHandle, handle) {
		t.Errorf("ActivePresetHandle %q, want %q", st.ActivePresetHandle, handle)
	}
}

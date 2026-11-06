// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
)

// TestServerMatchesTheGeneratedDefinition holds every selection the server
// serves against matter.js thermostat-cluster.element.ts
// (spectest.CheckServer): HEAT, COOL, HEAT+COOL and HEAT+COOL+AUTO, each
// with and without LTNE, OCC, PRES and MSCH (with PRES as well), with
// TEVT (with OCC, PRES and MSCH as well), and with PRES and TSUGGEST (with
// TEVT as well).
func TestServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	for _, base := range []uint32{
		thermo.ThermostatFeatureHEAT,
		thermo.ThermostatFeatureCOOL,
		thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL,
		thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL | thermo.ThermostatFeatureAUTO,
	} {
		for _, f := range []uint32{
			base, base | thermo.ThermostatFeatureLTNE, base | thermo.ThermostatFeatureOCC,
			base | thermo.ThermostatFeaturePRES, base | thermo.ThermostatFeatureOCC | thermo.ThermostatFeaturePRES,
			base | thermo.ThermostatFeatureMSCH, base | thermo.ThermostatFeaturePRES | thermo.ThermostatFeatureMSCH,
			base | thermo.ThermostatFeatureTEVT,
			base | thermo.ThermostatFeatureTEVT | thermo.ThermostatFeatureOCC | thermo.ThermostatFeaturePRES | thermo.ThermostatFeatureMSCH,
			base | thermo.ThermostatFeaturePRES | thermo.ThermostatFeatureTSUGGEST,
			base | thermo.ThermostatFeatureTEVT | thermo.ThermostatFeaturePRES | thermo.ThermostatFeatureTSUGGEST,
		} {
			cfg := thermo.DefaultThermostatConfig()
			cfg.Features = f
			cfg.NumberOfPresets = 1
			cfg.PresetTypes = []thermo.PresetType{{PresetScenario: tdef.PresetScenarioOccupied, NumberOfPresets: 1}}
			mode := tdef.SystemModeHeat
			if f&thermo.ThermostatFeatureHEAT == 0 {
				mode = tdef.SystemModeCool
			}
			cfg.ScheduleTypes = []thermo.ScheduleType{{SystemMode: mode, NumberOfSchedules: 1}}
			cfg.NumberOfSchedules, cfg.NumberOfScheduleTransitions = 1, 1
			srv, err := thermo.New(cfg)
			if err != nil {
				t.Fatalf("features 0x%X: %v", f, err)
			}
			spectest.CheckServer(t, srv, tdef.Definition, f)
		}
	}
}

// TestFeaturesTheServerDoesNotServeAreRefused pins the refusal of a
// feature whose elements the server does not serve — Setback, which
// matter.js refuses — and of the suggestions without Presets ("[PRES]",
// connectedhomeip's ThermostatCluster.h:80 static_assert at 6170af84) —
// New with ErrFeatures, NewThermostatServer with a panic.
// MatterScheduleConfiguration ("not yet implemented" in matter.js), the
// events (which matter.js does not emit) and the suggestions (which it
// does not implement) are served ahead of it (schedules.go, events.go,
// suggestions.go).
func TestFeaturesTheServerDoesNotServeAreRefused(t *testing.T) {
	t.Parallel()
	for _, f := range []tdef.Feature{tdef.FeatureSetback, tdef.FeatureThermostatSuggestions} {
		cfg := thermo.DefaultThermostatConfig()
		cfg.Features |= uint32(f)
		if _, err := thermo.New(cfg); !errors.Is(err, thermo.ErrFeatures) {
			t.Errorf("feature 0x%X: New error %v, want ErrFeatures", uint32(f), err)
		}
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewThermostatServer accepted a selection New refuses")
		}
	}()
	thermo.NewThermostatServer(thermo.ThermostatConfig{})
}

// TestWritesThroughTheDefinition pins what the definition answers ahead of
// the server's rules, and what the migration fixed: LocalTemperatureCalibration
// ("RW VM", plain state in matter.js) is writable; FanOnly is a SystemMode a
// heating-only thermostat accepts; SystemMode Auto needs AUTO; a setpoint of
// an absent feature is UNSUPPORTED_ATTRIBUTE; SetpointRaiseLower takes the
// generated request.
func TestWritesThroughTheDefinition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv := newHeatOnly()
	if err := srv.MatterWrite(ctx, tdef.AttrLocalTemperatureCalibration, int8(-5)); err != nil {
		t.Fatalf("LocalTemperatureCalibration: %v", err)
	}
	if v, _ := srv.MatterRead(tdef.AttrLocalTemperatureCalibration); v != int16(-5) {
		t.Errorf("LocalTemperatureCalibration = %v, want -5", v)
	}
	if err := srv.MatterWrite(ctx, tdef.AttrSystemMode, uint8(tdef.SystemModeFanOnly)); err != nil {
		t.Errorf("SystemMode FanOnly: %v", err)
	}
	for _, c := range []struct {
		attr  uint32
		value any
		want  im.StatusCode
	}{
		{tdef.AttrSystemMode, uint8(tdef.SystemModeAuto), im.StatusConstraintError},
		{tdef.AttrOccupiedCoolingSetpoint, int16(2500), im.StatusUnsupportedAttribute},
		{tdef.AttrAbsMinHeatSetpointLimit, int16(700), im.StatusUnsupportedWrite},
		{tdef.AttrControlSequenceOfOperation, uint8(9), im.StatusConstraintError},
	} {
		err := srv.MatterWrite(ctx, c.attr, c.value)
		if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != c.want {
			t.Errorf("write 0x%04X = %v: %v, want status 0x%02X", c.attr, c.value, err, c.want)
		}
	}
	for _, req := range []any{
		tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeHeat, Amount: 5},
		&tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeHeat, Amount: 5},
	} {
		if _, err := srv.MatterInvoke(ctx, tdef.CmdSetpointRaiseLower, req); err != nil {
			t.Fatalf("SetpointRaiseLower(%T): %v", req, err)
		}
	}
	if v, _ := srv.MatterRead(tdef.AttrOccupiedHeatingSetpoint); v != int16(2100) {
		t.Errorf("OccupiedHeatingSetpoint = %v, want 2100 after two raises of 0.5 °C", v)
	}
	_, err := srv.MatterInvoke(ctx, tdef.CmdSetpointRaiseLower, (*tdef.SetpointRaiseLowerRequest)(nil))
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusInvalidCommand {
		t.Errorf("SetpointRaiseLower(nil): %v, want INVALID_COMMAND", err)
	}
}

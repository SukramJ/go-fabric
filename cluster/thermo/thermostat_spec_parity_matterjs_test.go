// SPDX-License-Identifier: MIT
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
// with and without LTNE.
func TestServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	for _, base := range []uint32{
		thermo.ThermostatFeatureHEAT,
		thermo.ThermostatFeatureCOOL,
		thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL,
		thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL | thermo.ThermostatFeatureAUTO,
	} {
		for _, f := range []uint32{base, base | thermo.ThermostatFeatureLTNE} {
			cfg := thermo.DefaultThermostatConfig()
			cfg.Features = f
			srv, err := thermo.New(cfg)
			if err != nil {
				t.Fatalf("features 0x%X: %v", f, err)
			}
			spectest.CheckServer(t, srv, tdef.Definition, f)
		}
	}
}

// TestFeaturesTheServerDoesNotServeAreRefused pins the refusal of a
// feature whose elements the server does not serve — Occupancy's
// Unoccupied setpoints, the schedule and preset attributes and commands —
// New with ErrFeatures, NewThermostatServer with a panic.
func TestFeaturesTheServerDoesNotServeAreRefused(t *testing.T) {
	t.Parallel()
	for _, f := range []tdef.Feature{tdef.FeatureOccupancy, tdef.FeatureMatterScheduleConfiguration, tdef.FeaturePresets} {
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

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
)

// TestSetAttributesRefusedLimitStoresNoPreset (R2-P6-err-1560):
// SetAttributes is all or nothing. A limit the setpoint rules refuse,
// given with Presets, refuses the call before the presets are stored.
func TestSetAttributesRefusedLimitStoresNoPreset(t *testing.T) {
	t.Parallel()
	srv := mustNew(t, thermostatConfig(5, []thermo.Preset{setpointPreset(1, 2000, 2600)}))
	err := srv.SetAttributes(map[uint32]any{
		tdef.AttrPresets:                 []thermo.Preset{setpointPreset(1, 2100, 2600)},
		tdef.AttrAbsMinHeatSetpointLimit: int16(1500), // above the user limit MinHeat 700
	})
	if err == nil {
		t.Fatal("an absolute limit above the user limit was accepted")
	}
	v, _ := srv.MatterRead(tdef.AttrPresets)
	if got := *v.(spec.List[thermo.Preset])[0].HeatingSetpoint; got != 2000 {
		t.Errorf("preset 1 heats to %d after the refused call, want 2000", got)
	}
	if abs, _ := srv.MatterRead(tdef.AttrAbsMinHeatSetpointLimit); abs != int16(700) {
		t.Errorf("AbsMinHeatSetpointLimit %v after the refused call, want 700", abs)
	}
}

// TestSetAttributesJudgesASetpointAgainstTheCallsLimit (R2-P6-err-1561):
// a lower user limit and a setpoint inside it, given in one call, are
// accepted as they are in two calls.
func TestSetAttributesJudgesASetpointAgainstTheCallsLimit(t *testing.T) {
	t.Parallel()
	cfg := thermostatConfig(5, nil)
	minHeat := int16(1000)
	cfg.MinHeatSetpointLimit = &minHeat
	cfg.InitialHeatingSetpoint = 1200
	srv := mustNew(t, cfg)
	if err := srv.SetAttributes(map[uint32]any{
		tdef.AttrMinHeatSetpointLimit:    int16(900),
		tdef.AttrOccupiedHeatingSetpoint: int16(950),
	}); err != nil {
		t.Fatalf("a limit and a setpoint inside it in one call: %v", err)
	}
	for id, want := range map[uint32]int16{tdef.AttrMinHeatSetpointLimit: 900, tdef.AttrOccupiedHeatingSetpoint: 950} {
		if v, _ := srv.MatterRead(id); v != want {
			t.Errorf("attribute 0x%04X reads %v, want %d", id, v, want)
		}
	}
	// The setpoint is still held to the call's limit.
	if err := srv.SetAttributes(map[uint32]any{
		tdef.AttrMinHeatSetpointLimit:    int16(1100),
		tdef.AttrOccupiedHeatingSetpoint: int16(1000),
	}); err == nil {
		t.Error("a setpoint below the call's own limit was accepted")
	}
}

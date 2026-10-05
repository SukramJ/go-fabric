// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
)

// Attribute ids (thermostat-cluster.element.ts).
const (
	attrOccupiedCooling uint32 = 0x0011
	attrOccupiedHeating uint32 = 0x0012
	attrMinHeatLimit    uint32 = 0x0015
	attrMaxHeatLimit    uint32 = 0x0016
	attrMinCoolLimit    uint32 = 0x0017
	attrMaxCoolLimit    uint32 = 0x0018
)

func isConstraintError(err error) bool {
	var st interface{ MatterStatusCode() im.StatusCode }
	return errors.As(err, &st) && st.MatterStatusCode() == im.StatusConstraintError
}

// TestParityMatterJS_Thermostat_SetpointLimitWrites mirrors matter.js
// ThermostatServer.ts #assertLimitWithinAbs and #reconcileSetpoints
// (chip FixUserLimits / FixUserLimitDeadband / FixRange) on the default
// HEAT+COOL+AUTO thermostat: abs heat 700-3000, abs cool 1600-3200,
// deadband 2.0 °C, occupied 2000 / 2600 (TC-TSTAT-2.2).
func TestParityMatterJS_Thermostat_SetpointLimitWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a limit inside the absolute range is accepted", func(t *testing.T) {
		t.Parallel()
		s := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
		if err := s.MatterWrite(ctx, attrMinHeatLimit, int16(701)); err != nil {
			t.Fatalf("MinHeatSetpointLimit=701: %v", err)
		}
		if got := readInt16(t, s, attrMinHeatLimit); got != 701 {
			t.Fatalf("MinHeatSetpointLimit = %d, want 701", got)
		}
	})

	t.Run("a limit outside the absolute range is a ConstraintError", func(t *testing.T) {
		t.Parallel()
		s := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
		for _, w := range []struct {
			id uint32
			v  int16
		}{{attrMinHeatLimit, 699}, {attrMaxHeatLimit, 3001}, {attrMinCoolLimit, 1599}, {attrMaxCoolLimit, 3201}} {
			if err := s.MatterWrite(ctx, w.id, w.v); !isConstraintError(err) {
				t.Errorf("write 0x%04X=%d: %v, want ConstraintError", w.id, w.v, err)
			}
		}
	})

	t.Run("a min limit above the max limit drags the max along", func(t *testing.T) {
		t.Parallel()
		s := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
		if err := s.MatterWrite(ctx, attrMaxHeatLimit, int16(1500)); err != nil {
			t.Fatal(err)
		}
		if err := s.MatterWrite(ctx, attrMinHeatLimit, int16(1800)); err != nil {
			t.Fatal(err)
		}
		if got := readInt16(t, s, attrMaxHeatLimit); got != 1800 {
			t.Errorf("MaxHeatSetpointLimit = %d, want 1800 (raised to the new min)", got)
		}
	})

	t.Run("a raised max heat limit pushes the max cool limit by the deadband", func(t *testing.T) {
		t.Parallel()
		s := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
		if err := s.MatterWrite(ctx, attrMaxCoolLimit, int16(2800)); err != nil {
			t.Fatal(err)
		}
		if err := s.MatterWrite(ctx, attrMaxHeatLimit, int16(2700)); err != nil {
			t.Fatal(err)
		}
		if got := readInt16(t, s, attrMaxCoolLimit); got != 2900 {
			t.Errorf("MaxCoolSetpointLimit = %d, want 2900 (2700 + deadband)", got)
		}
	})

	t.Run("a heating setpoint inside the deadband pushes the cooling setpoint", func(t *testing.T) {
		t.Parallel()
		s := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
		var reported [][]uint32
		s.OnMatterAttributesChanged(func(ids []uint32) { reported = append(reported, ids) })
		if err := s.MatterWrite(ctx, attrOccupiedHeating, int16(2500)); err != nil {
			t.Fatal(err)
		}
		if got := readInt16(t, s, attrOccupiedCooling); got != 2700 {
			t.Errorf("OccupiedCoolingSetpoint = %d, want 2700 (2500 + deadband)", got)
		}
		if len(reported) != 1 || !slices.Equal(reported[0], []uint32{attrOccupiedCooling}) {
			t.Errorf("reported %v, want the pushed OccupiedCoolingSetpoint once", reported)
		}
	})

	t.Run("a lowered max heat limit crops the heating setpoint", func(t *testing.T) {
		t.Parallel()
		s := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
		if err := s.MatterWrite(ctx, attrMaxHeatLimit, int16(1800)); err != nil {
			t.Fatal(err)
		}
		if got := readInt16(t, s, attrOccupiedHeating); got != 1800 {
			t.Errorf("OccupiedHeatingSetpoint = %d, want 1800 (cropped to the new max)", got)
		}
	})

	t.Run("a setpoint outside its limits stays a ConstraintError", func(t *testing.T) {
		t.Parallel()
		s := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
		if err := s.MatterWrite(ctx, attrOccupiedHeating, int16(699)); !isConstraintError(err) {
			t.Errorf("OccupiedHeatingSetpoint=699: %v, want ConstraintError", err)
		}
	})
}

// TestParityMatterJS_Thermostat_ReconcileSequences drives the coupled
// repairs of matter.js #fixUserLimits, #fixLimitDeadband and
// #fixSetpointRange branch by branch: each case writes a sequence and reads
// back the six reconciled values.
func TestParityMatterJS_Thermostat_ReconcileSequences(t *testing.T) {
	t.Parallel()
	type write struct {
		id uint32
		v  int16
	}
	type want struct{ minHeat, maxHeat, minCool, maxCool, occHeat, occCool int16 }
	cases := []struct {
		name   string
		cfg    func(*thermo.ThermostatConfig)
		writes []write
		want   want
	}{
		{
			name:   "max heat below min heat drags min heat down",
			writes: []write{{attrMinHeatLimit, 1500}, {attrMaxHeatLimit, 1400}},
			want:   want{1400, 1400, 1700, 3200, 1400, 2600},
		},
		{
			name:   "lowered max cool pushes max heat down",
			writes: []write{{attrMaxCoolLimit, 2700}},
			want:   want{700, 2500, 1600, 2700, 2000, 2600},
		},
		{
			name:   "max heat past the cool range pins max cool and pulls max heat back",
			cfg:    func(c *thermo.ThermostatConfig) { c.AbsMaxCoolSetpointLimit = 3100 },
			writes: []write{{attrMaxHeatLimit, 3000}},
			want:   want{700, 2900, 1600, 3100, 2000, 2600},
		},
		{
			name:   "raised min heat pushes min cool up",
			writes: []write{{attrMinHeatLimit, 1500}},
			want:   want{1500, 3000, 1700, 3200, 2000, 2600},
		},
		{
			name:   "min cool below the heat range pins min heat and pushes min cool",
			cfg:    func(c *thermo.ThermostatConfig) { c.AbsMinHeatSetpointLimit = 1500 },
			writes: []write{{attrMinCoolLimit, 1600}},
			want:   want{1500, 3000, 1700, 3200, 2000, 2600},
		},
		{
			name:   "cooling setpoint inside the deadband pushes the heating setpoint",
			writes: []write{{attrOccupiedCooling, 2100}},
			want:   want{700, 3000, 1600, 3200, 1900, 2100},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cfg := thermo.DefaultThermostatConfig()
			if c.cfg != nil {
				c.cfg(&cfg)
			}
			s := thermo.NewThermostatServer(cfg)
			for _, w := range c.writes {
				if err := s.MatterWrite(context.Background(), w.id, w.v); err != nil {
					t.Fatalf("write 0x%04X=%d: %v", w.id, w.v, err)
				}
			}
			got := want{
				readInt16(t, s, attrMinHeatLimit), readInt16(t, s, attrMaxHeatLimit),
				readInt16(t, s, attrMinCoolLimit), readInt16(t, s, attrMaxCoolLimit),
				readInt16(t, s, attrOccupiedHeating), readInt16(t, s, attrOccupiedCooling),
			}
			if got != c.want {
				t.Errorf("after %v: %+v, want %+v", c.writes, got, c.want)
			}
		})
	}
}

// TestThermostat_SetpointWriteRefusals: a setpoint of an absent feature and
// a non-numeric value are refused before any reconcile.
func TestThermostat_SetpointWriteRefusals(t *testing.T) {
	t.Parallel()
	heatOnly := thermo.DefaultThermostatConfig()
	heatOnly.Features = thermo.ThermostatFeatureHEAT
	s := thermo.NewThermostatServer(heatOnly)
	if err := s.MatterWrite(context.Background(), attrMinCoolLimit, int16(1700)); err == nil {
		t.Error("a cool limit was written on a heat-only thermostat")
	}
	if err := s.MatterWrite(context.Background(), attrMinHeatLimit, "warm"); err == nil {
		t.Error("a non-numeric limit was accepted")
	}
}

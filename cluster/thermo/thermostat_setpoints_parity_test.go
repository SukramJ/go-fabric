// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"errors"
	"slices"
	"strings"
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

// TestParityMatterJS_Thermostat_IgnoredWrites: ControlSequenceOfOperation
// and MinSetpointDeadBand are "optionally writeable" and a write is
// silently ignored — matter.js #ensureControlSequenceOfOperationNotWritable
// / #ensureMinSetpointDeadBandNotWritable — while the type constraints
// still answer ConstraintError (TC-TSTAT-2.2 steps 11-12).
func TestParityMatterJS_Thermostat_IgnoredWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
	const csoo, deadband uint32 = 0x001B, 0x0019
	before, _ := s.MatterRead(csoo)
	if err := s.MatterWrite(ctx, csoo, uint8(4)); err != nil {
		t.Fatalf("write ControlSequenceOfOperation=4: %v", err)
	}
	if err := s.MatterWrite(ctx, csoo, uint8(2)); err != nil {
		t.Fatalf("write ControlSequenceOfOperation=2: %v", err)
	}
	if after, _ := s.MatterRead(csoo); after != before {
		t.Errorf("ControlSequenceOfOperation changed %v -> %v; the write must be ignored", before, after)
	}
	if err := s.MatterWrite(ctx, csoo, uint8(6)); !isConstraintError(err) {
		t.Errorf("ControlSequenceOfOperation=6: %v, want ConstraintError", err)
	}
	if err := s.MatterWrite(ctx, deadband, int16(5)); err != nil {
		t.Fatalf("write MinSetpointDeadBand=5: %v", err)
	}
	if v, _ := s.MatterRead(deadband); v != int8(20) {
		t.Errorf("MinSetpointDeadBand = %v after an ignored write, want 20", v)
	}
	for _, bad := range []int16{-1, 128} {
		if err := s.MatterWrite(ctx, deadband, bad); !isConstraintError(err) {
			t.Errorf("MinSetpointDeadBand=%d: %v, want ConstraintError", bad, err)
		}
	}
	heatOnly := thermo.DefaultThermostatConfig()
	heatOnly.Features = thermo.ThermostatFeatureHEAT
	if err := thermo.NewThermostatServer(heatOnly).MatterWrite(ctx, deadband, int16(5)); err == nil {
		t.Error("MinSetpointDeadBand written without AUTO")
	}
	if err := s.MatterWrite(ctx, csoo, "x"); err == nil {
		t.Error("a non-numeric ControlSequenceOfOperation was accepted")
	}
	if err := s.MatterWrite(ctx, deadband, "x"); err == nil {
		t.Error("a non-numeric MinSetpointDeadBand was accepted")
	}
}

// TestParityMatterJS_Thermostat_StartupReconciliation mirrors matter.js
// 0ceb202d packages/node/test/behaviors/thermostat/ThermostatBehaviorTest.ts
// "startup reconciliation" (:57-173) over ThermostatServer.ts:1015-1042
// (#reconcileInitialSetpoints): New repairs a configuration that violates
// the setpoint rules, preferring the heating side, and refuses one it
// cannot repair. The server's user limits start at the absolute ones and
// the deadband is the default 2.0 °C, so the cases that set user limits or
// another deadband are run on the absolute limits instead.
func TestParityMatterJS_Thermostat_StartupReconciliation(t *testing.T) {
	t.Parallel()
	read := func(t *testing.T, s *thermo.ThermostatServer, attrID uint32) int16 {
		t.Helper()
		v, ok := s.MatterRead(attrID)
		if !ok {
			t.Fatalf("attribute 0x%04X not served", attrID)
		}
		return v.(int16)
	}
	for _, tc := range []struct {
		name string
		edit func(*thermo.ThermostatConfig)
		want map[uint32]int16
	}{
		{
			// :83-93
			name: "moves the cooling setpoint when the setpoints violate the deadband",
			edit: func(c *thermo.ThermostatConfig) { c.InitialHeatingSetpoint, c.InitialCoolingSetpoint = 2100, 2200 },
			want: map[uint32]int16{attrOccupiedHeating: 2100, attrOccupiedCooling: 2300},
		},
		{
			// :95-102
			name: "clamps a setpoint outside its limits and moves the cooling setpoint to keep the deadband",
			edit: func(c *thermo.ThermostatConfig) { c.InitialHeatingSetpoint = 3100 },
			want: map[uint32]int16{attrOccupiedHeating: 3000, attrOccupiedCooling: 3200},
		},
		{
			// :74-81, on the absolute limit: the cool limit moves up.
			name: "raises the cool limit when it can keep the deadband within its absolute limits",
			edit: func(c *thermo.ThermostatConfig) { c.AbsMinHeatSetpointLimit = 1500 },
			want: map[uint32]int16{attrMinHeatLimit: 1500, attrMinCoolLimit: 1700},
		},
		{
			// :58-72, at the default deadband: the heat limit is pulled back.
			name: "pulls a heat limit back when the cool limit cannot keep the deadband",
			edit: func(c *thermo.ThermostatConfig) { c.AbsMaxHeatSetpointLimit = 3500 },
			want: map[uint32]int16{attrMaxCoolLimit: 3200, attrMaxHeatLimit: 3000},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := thermo.DefaultThermostatConfig()
			tc.edit(&cfg)
			s, err := thermo.New(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			for attrID, want := range tc.want {
				if got := read(t, s, attrID); got != want {
					t.Errorf("attribute 0x%04X = %d, want %d", attrID, got, want)
				}
			}
			if err := s.MatterWrite(context.Background(), attrOccupiedHeating, int16(2200)); err != nil {
				t.Errorf("a setpoint write after the repair: %v", err)
			}
		})
	}

	t.Run("a heating-only setpoint above its limit is cropped", func(t *testing.T) {
		t.Parallel()
		s, err := thermo.New(thermo.ThermostatConfig{
			Features:                thermo.ThermostatFeatureHEAT,
			AbsMinHeatSetpointLimit: 700, AbsMaxHeatSetpointLimit: 3000,
			InitialHeatingSetpoint: 3500,
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if got := read(t, s, attrOccupiedHeating); got != 3000 {
			t.Errorf("OccupiedHeatingSetpoint = %d, want 3000", got)
		}
	})

	t.Run("fails with the violated condition when the absolute limits leave no room for the deadband", func(t *testing.T) {
		t.Parallel()
		// :104-123
		cfg := thermo.DefaultThermostatConfig()
		cfg.AbsMinHeatSetpointLimit, cfg.AbsMaxCoolSetpointLimit = 2900, 3000
		cfg.InitialHeatingSetpoint, cfg.InitialCoolingSetpoint = 2900, 3000
		_, err := thermo.New(cfg)
		if !errors.Is(err, thermo.ErrSetpoints) {
			t.Fatalf("New = %v, want ErrSetpoints", err)
		}
		const want = "cannot be fixed: maxCoolSetpointLimit (3000) - maxHeatSetpointLimit (3000) is below the deadband (200); after adjusting,"
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	})

	t.Run("fails for unordered absolute limits", func(t *testing.T) {
		t.Parallel()
		// :164-173
		cfg := thermo.DefaultThermostatConfig()
		cfg.AbsMinHeatSetpointLimit, cfg.AbsMaxHeatSetpointLimit = 2500, 2000
		_, err := thermo.New(cfg)
		if !errors.Is(err, thermo.ErrSetpoints) {
			t.Fatalf("New = %v, want ErrSetpoints", err)
		}
		const want = "absMinHeatSetpointLimit (2500) is above absMaxHeatSetpointLimit (2000)"
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tc "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestTemperatureControlSetAttributesApplies: the device's own setpoint
// or level lands, a value outside the cluster's rules is refused and
// changes nothing.
func TestTemperatureControlSetAttributesApplies(t *testing.T) {
	t.Parallel()

	number := newTC(t, numberConfig(&setter{}))
	if err := number.SetAttributes(nil); err != nil {
		t.Errorf("SetAttributes(nil) = %v", err)
	}
	if err := number.SetAttributes(map[uint32]any{tc.AttrTemperatureSetpoint: int16(-1700)}); err != nil {
		t.Fatalf("SetAttributes: %v", err)
	}
	if v, _ := number.MatterRead(tc.AttrTemperatureSetpoint); v != int16(-1700) {
		t.Errorf("TemperatureSetpoint %v", v)
	}
	if err := number.SetAttributes(map[uint32]any{tc.AttrTemperatureSetpoint: int16(-1750)}); !errors.Is(err, thermo.ErrTemperatureControlValue) {
		t.Errorf("an off-step setpoint: %v, want ErrTemperatureControlValue", err)
	}
	if v, _ := number.MatterRead(tc.AttrTemperatureSetpoint); v != int16(-1700) {
		t.Errorf("a refused Set moved the setpoint to %v", v)
	}

	level := newTC(t, levelConfig(&setter{}))
	if err := level.SetAttributes(map[uint32]any{tc.AttrSelectedTemperatureLevel: uint8(2)}); err != nil {
		t.Fatalf("SetAttributes: %v", err)
	}
	if v, _ := level.MatterRead(tc.AttrSelectedTemperatureLevel); v != uint8(2) {
		t.Errorf("SelectedTemperatureLevel %v", v)
	}
	if err := level.SetAttributes(map[uint32]any{tc.AttrSelectedTemperatureLevel: uint8(9)}); !errors.Is(err, thermo.ErrTemperatureControlValue) {
		t.Errorf("a level beyond the list: %v, want ErrTemperatureControlValue", err)
	}
}

// TestTemperatureControlSetAttributesRefusals: an attribute the selection
// does not serve, a value of the wrong type and the configuration's
// attributes are refused.
func TestTemperatureControlSetAttributesRefusals(t *testing.T) {
	t.Parallel()

	number := newTC(t, numberConfig(&setter{}))
	if err := number.SetAttributes(map[uint32]any{tc.AttrSelectedTemperatureLevel: uint8(0)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("a level on a number server: %v, want ErrNotServed", err)
	}
	if err := number.SetAttributes(map[uint32]any{tc.AttrTemperatureSetpoint: "cold"}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("a setpoint of a string: %v, want ErrInvalidValue", err)
	}
	if err := number.SetAttributes(map[uint32]any{tc.AttrMinTemperature: int16(-1000)}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("MinTemperature: %v, want ErrNotHeld", err)
	}
}

// TestTemperatureControlHandleEmitAndVeto: a host handler takes
// SetTemperature, no event is defined, and the veto can be set and
// removed (every attribute is read-only, so it never runs).
func TestTemperatureControlHandleEmitAndVeto(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dev := &setter{}
	srv := newTC(t, numberConfig(dev))
	calls := 0
	if err := srv.Handle(tc.CmdSetTemperature, func(context.Context, any) (any, error) {
		calls++
		return nil, nil
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// An off-step request would be refused by the server's own checks.
	req := tc.SetTemperatureRequest{TargetTemperature: ptr(int16(-1750))}
	if _, err := srv.MatterInvoke(ctx, tc.CmdSetTemperature, req); err != nil || calls != 1 || len(dev.targets) != 0 {
		t.Errorf("host handler: err %v, calls %d, device saw %v; want the handler only", err, calls, dev.targets)
	}
	if err := srv.Handle(tc.CmdSetTemperature, nil); err != nil {
		t.Fatalf("Handle(nil): %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, tc.CmdSetTemperature, req); statusOf(err) != im.StatusConstraintError {
		t.Errorf("after nil: %v, want the server's ConstraintError", err)
	}
	if err := srv.Handle(0x42, func(context.Context, any) (any, error) { return nil, nil }); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("Handle(0x42) = %v, want ErrNotAccepted", err)
	}
	if err := srv.Emit(1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit = %v, want ErrNotEmitted", err)
	}

	ran := false
	srv.SetBeforeWrite(func(context.Context, uint32, any) error { ran = true; return nil })
	if err := srv.MatterWrite(ctx, tc.AttrTemperatureSetpoint, int16(-1500)); statusOf(err) != im.StatusUnsupportedWrite {
		t.Errorf("MatterWrite = %v, want UnsupportedWrite", err)
	}
	if ran {
		t.Error("the veto ran for a write the definition refuses")
	}
	srv.SetBeforeWrite(nil)
}

// thermostatServer is a self-held Thermostat server, the default heating and cooling one with mutate applied.
func thermostatServer(tb testing.TB, mutate func(*thermo.ThermostatConfig)) *thermo.ThermostatServer {
	tb.Helper()
	cfg := thermo.DefaultThermostatConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	srv, err := thermo.New(cfg)
	if err != nil {
		tb.Fatal(err)
	}
	return srv
}

// TestThermostatSetAttributesStoresTheRemainingAttributes: the
// calibration, the system mode and a null LocalTemperature are stored as
// given, and a value equal to the configuration's is accepted.
func TestThermostatSetAttributesStoresTheRemainingAttributes(t *testing.T) {
	t.Parallel()

	srv := thermostatServer(t, nil)
	s := tdef.Wrap(srv)
	cur, _ := srv.MatterRead(tdef.AttrControlSequenceOfOperation)
	if err := srv.SetAttributes(map[uint32]any{
		tdef.AttrLocalTemperatureCalibration: int8(-5),
		tdef.AttrLocalTemperature:            int16(2100),
		tdef.AttrControlSequenceOfOperation:  cur,
	}); err != nil {
		t.Fatalf("SetAttributes: %v", err)
	}
	if st := s.Get(); st.LocalTemperature == nil || *st.LocalTemperature != 2100 {
		t.Errorf("LocalTemperature %v", st.LocalTemperature)
	}
	if v, _ := srv.MatterRead(tdef.AttrLocalTemperatureCalibration); fmt.Sprint(v) != "-5" {
		t.Errorf("LocalTemperatureCalibration %v", v)
	}
	// Setting the same temperature again changes nothing.
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrLocalTemperature: int16(2100)}); err != nil {
		t.Errorf("SetAttributes(same) = %v", err)
	}
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrOccupancy: uint8(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("Occupancy without OCC: %v, want ErrNotServed", err)
	}
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrPresets: "none"}); err == nil {
		t.Error("Presets without PRES was accepted")
	}
}

// TestThermostatSetAttributesOccupancyAndUnoccupiedSetpoints: with OCC the
// occupancy and the unoccupied setpoints are the device's too, and a
// moved setpoint ends the active preset as a local change does.
func TestThermostatSetAttributesOccupancyAndUnoccupiedSetpoints(t *testing.T) {
	t.Parallel()

	srv := thermostatServer(t, func(cfg *thermo.ThermostatConfig) {
		cfg.Features |= thermo.ThermostatFeatureOCC
		cfg.InitialUnoccupiedHeatingSetpoint = ptr(int16(1600))
		cfg.InitialUnoccupiedCoolingSetpoint = ptr(int16(2800))
	})
	if err := srv.SetAttributes(map[uint32]any{
		tdef.AttrOccupancy:                   uint8(1),
		tdef.AttrUnoccupiedHeatingSetpoint:   int16(1500),
		tdef.AttrUnoccupiedCoolingSetpoint:   int16(2900),
		tdef.AttrOccupiedCoolingSetpoint:     int16(2700),
		tdef.AttrOccupiedHeatingSetpoint:     int16(2100),
		tdef.AttrLocalTemperatureCalibration: int8(0),
	}); err != nil {
		t.Fatalf("SetAttributes: %v", err)
	}
	st := tdef.Wrap(srv).Get()
	if st.OccupiedHeatingSetpoint != 2100 || st.OccupiedCoolingSetpoint != 2700 ||
		st.UnoccupiedHeatingSetpoint != 1500 ||
		st.UnoccupiedCoolingSetpoint != 2900 {
		t.Errorf("state %+v", st)
	}
	if v, _ := srv.MatterRead(tdef.AttrOccupancy); v != uint8(1) {
		t.Errorf("Occupancy %v", v)
	}
}

// TestThermostatSetAttributesHoldsTheLimits: the absolute limits, the user
// limits and the setpoints are held to one another, and a refusal stores
// nothing.
func TestThermostatSetAttributesHoldsTheLimits(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name   string
		values map[uint32]any
		want   error
	}{
		{"a user limit below the absolute one", map[uint32]any{tdef.AttrMinHeatSetpointLimit: int16(600)}, spec.ErrInvalidValue},
		{"a user limit above the absolute one", map[uint32]any{tdef.AttrMaxCoolSetpointLimit: int16(3300)}, spec.ErrInvalidValue},
		{"a setpoint above its limit", map[uint32]any{tdef.AttrOccupiedCoolingSetpoint: int16(3300)}, spec.ErrInvalidValue},
		{"an absolute heating limit inside the user limits", map[uint32]any{tdef.AttrAbsMinHeatSetpointLimit: int16(900)}, nil},
		{"an absolute cooling limit inside the user limits", map[uint32]any{tdef.AttrAbsMaxCoolSetpointLimit: int16(3000)}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			srv := thermostatServer(t, nil)
			before := tdef.Wrap(srv).Get()
			err := srv.SetAttributes(c.values)
			if err == nil {
				t.Fatal("SetAttributes succeeded")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Errorf("SetAttributes = %v, want %v", err, c.want)
			}
			after := tdef.Wrap(srv).Get()
			if after.OccupiedHeatingSetpoint != before.OccupiedHeatingSetpoint || after.OccupiedCoolingSetpoint != before.OccupiedCoolingSetpoint ||
				after.MinHeatSetpointLimit != before.MinHeatSetpointLimit {
				t.Errorf("a refused Set stored %+v", after)
			}
		})
	}
}

// TestThermostatSetAttributesMovesTheAbsoluteLimits: an absolute limit the
// user limits allow is stored and reported, the heating and the cooling
// pair apart.
func TestThermostatSetAttributesMovesTheAbsoluteLimits(t *testing.T) {
	t.Parallel()

	srv := thermostatServer(t, nil)
	var got []uint32
	srv.OnAttributeChange(func(c contract.Change) { got = append(got, c.Attributes...) })
	if err := srv.SetAttributes(map[uint32]any{
		tdef.AttrAbsMinHeatSetpointLimit: int16(500), tdef.AttrAbsMaxHeatSetpointLimit: int16(3100),
		tdef.AttrAbsMinCoolSetpointLimit: int16(1500), tdef.AttrAbsMaxCoolSetpointLimit: int16(3300),
	}); err != nil {
		t.Fatalf("SetAttributes: %v", err)
	}
	want := []uint32{
		tdef.AttrAbsMinHeatSetpointLimit, tdef.AttrAbsMaxHeatSetpointLimit,
		tdef.AttrAbsMinCoolSetpointLimit, tdef.AttrAbsMaxCoolSetpointLimit,
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("reported %v, want %v", got, want)
	}
}

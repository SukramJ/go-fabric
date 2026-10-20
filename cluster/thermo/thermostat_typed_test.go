// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
)

func ptrOf[T any](v T) *T { return &v }

// typedThermostat is the self-held Thermostat server (HEAT, COOL, AUTO;
// heating 20.00 °C, cooling 26.00 °C, deadband 2.0 °C) under its typed
// server.
func typedThermostat(t *testing.T) *tdef.Server {
	t.Helper()
	srv, err := thermo.New(thermo.DefaultThermostatConfig())
	if err != nil {
		t.Fatal(err)
	}
	return tdef.Wrap(srv)
}

func TestThermostatTypedSetGetOnChanged(t *testing.T) {
	t.Parallel()
	s := typedThermostat(t)
	var changes [][]uint32
	unsubscribe := s.OnChanged(func(_ tdef.State, changed []uint32) { changes = append(changes, changed) })
	defer unsubscribe()

	// A heating setpoint 1.00 °C below cooling: the reconcile moves the
	// cooling setpoint to keep the 2.0 °C deadband, as a write's does.
	if err := s.Set(tdef.Patch{
		OccupiedHeatingSetpoint: ptrOf(int16(2500)),
		LocalTemperature:        ptrOf(int16(2150)),
		SystemMode:              ptrOf(tdef.SystemModeHeat),
	}); err != nil {
		t.Fatal(err)
	}
	st := s.Get()
	if st.OccupiedHeatingSetpoint != 2500 || st.OccupiedCoolingSetpoint != 2700 || st.SystemMode != tdef.SystemModeHeat ||
		st.LocalTemperature == nil || *st.LocalTemperature != 2150 {
		t.Errorf("state %+v", st)
	}
	want := []uint32{tdef.AttrLocalTemperature, tdef.AttrOccupiedCoolingSetpoint, tdef.AttrOccupiedHeatingSetpoint, tdef.AttrSystemMode}
	if len(changes) != 1 || !slices.Equal(changes[0], want) {
		t.Errorf("changes %v, want [%v]", changes, want)
	}
	if err := s.Set(tdef.Patch{Null: []uint32{tdef.AttrLocalTemperature}}); err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got.LocalTemperature != nil {
		t.Errorf("LocalTemperature after null: %d", *got.LocalTemperature)
	}

	// Above MaxHeatSetpointLimit: nothing of the patch is stored.
	if err := s.Set(tdef.Patch{OccupiedHeatingSetpoint: ptrOf(int16(3500)), SystemMode: ptrOf(tdef.SystemModeCool)}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("a setpoint above its limit: %v", err)
	}
	if got := s.Get(); got.OccupiedHeatingSetpoint != 2500 || got.SystemMode != tdef.SystemModeHeat {
		t.Errorf("a refused patch stored %+v", got)
	}
	if err := s.Set(tdef.Patch{ControlSequenceOfOperation: ptrOf(tdef.ControlSequenceOfOperationEnum(0))}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("ControlSequenceOfOperation: %v", err)
	}
}

func TestThermostatTypedHandleReplacesTheCommand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := typedThermostat(t)
	var got []tdef.SetpointRaiseLowerRequest
	if err := s.HandleSetpointRaiseLower(func(_ context.Context, req tdef.SetpointRaiseLowerRequest) (struct{}, error) {
		got = append(got, req)
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeHeat, Amount: 10}
	if _, err := s.Underlying().MatterInvoke(ctx, tdef.CmdSetpointRaiseLower, req); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != req || s.Get().OccupiedHeatingSetpoint != 2000 {
		t.Errorf("the host's handler saw %v; heating %d", got, s.Get().OccupiedHeatingSetpoint)
	}
	if err := s.HandleSetpointRaiseLower(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Underlying().MatterInvoke(ctx, tdef.CmdSetpointRaiseLower, &req); err != nil {
		t.Fatal(err)
	}
	if h := s.Get().OccupiedHeatingSetpoint; h != 2100 || len(got) != 1 {
		t.Errorf("after nil: heating %d, host calls %d", h, len(got))
	}
	// SetActivePresetRequest needs the Presets feature.
	if err := s.HandleSetActivePresetRequest(func(context.Context, tdef.SetActivePresetRequestRequest) (struct{}, error) {
		return struct{}{}, nil
	}); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("a command the selection does not accept: %v", err)
	}
}

func TestThermostatTypedEmitAndOnWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := typedThermostat(t)
	// The server serves none of the event feature TEVT's events.
	if err := s.EmitSystemModeChange(1, tdef.SystemModeChangeEvent{}); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("an event outside EventList: %v", err)
	}
	refusal := errors.New("the boiler is in service")
	var seen []int16
	if err := s.OnWrite(func(_ context.Context, next tdef.State, attrID uint32) error {
		if attrID == tdef.AttrOccupiedHeatingSetpoint {
			seen = append(seen, next.OccupiedHeatingSetpoint)
			return refusal
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Underlying().MatterWrite(ctx, tdef.AttrOccupiedHeatingSetpoint, int64(2100)); !errors.Is(err, refusal) {
		t.Fatalf("a vetoed write: %v", err)
	}
	if len(seen) != 1 || seen[0] != 2100 || s.Get().OccupiedHeatingSetpoint != 2000 {
		t.Errorf("the veto saw %v; heating %d", seen, s.Get().OccupiedHeatingSetpoint)
	}
}

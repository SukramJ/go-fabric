// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

// The Events feature (TEVT) after connectedhomeip at 6170af84: the event
// cases of src/app/clusters/thermostat-server/tests (TestThermostatCluster.cpp
// TestLocalTemperatureNormalAndEvents :110-140, TestSystemModeValidationAndEvents
// :180-215, TestThermostatRunningModeAndRunningState :316-372;
// TestThermostatSetpoints.cpp TestOccupiedHeatingAndCoolingSetpointsAttributes
// :195-232; TestThermostatAtomicAndPresets.cpp TestSetActivePresetRequestCommand
// :318-360), ported by meaning, and the rules of ThermostatClusterEvents.cpp
// and its call sites (events.go).

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

type emitted struct {
	endpoint uint16
	id       uint32
	data     any
}

// payloadRecorder keeps every event with its endpoint and payload.
type payloadRecorder struct {
	mu     sync.Mutex
	events []emitted
}

func (r *payloadRecorder) MatterEmitEvent(endpoint uint16, _, event uint32, data any, _ contract.EventPriority) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, emitted{endpoint, event, data})
}

// take returns the events so far and forgets them.
func (r *payloadRecorder) take() []emitted {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

func tevtServer(t *testing.T, features uint32, edit ...func(*thermo.ThermostatConfig)) (*thermo.ThermostatServer, *payloadRecorder) {
	t.Helper()
	cfg := thermo.DefaultThermostatConfig()
	cfg.Features = features
	for _, e := range edit {
		e(&cfg)
	}
	srv := mustNew(t, cfg)
	rec := &payloadRecorder{}
	srv.SetMatterEventEmitter(rec)
	srv.SetEndpoint(3)
	return srv, rec
}

func wantIDs(t *testing.T, got []emitted, want ...uint32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("events %+v, want ids %v", got, want)
	}
	for i := range want {
		if got[i].id != want[i] {
			t.Fatalf("event %d is 0x%02X, want 0x%02X (%+v)", i, got[i].id, want[i], got)
		}
	}
}

const (
	featHeatTEVT     = thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureTEVT
	featHeatCoolTEVT = thermo.ThermostatFeatureHEAT | thermo.ThermostatFeatureCOOL | thermo.ThermostatFeatureTEVT
	featAutoTEVT     = featHeatCoolTEVT | thermo.ThermostatFeatureAUTO
)

// TestChipLocalTemperatureNormalAndEvents ports
// TestLocalTemperatureNormalAndEvents: a changed LocalTemperature emits
// LocalTemperatureChange with the current value.
func TestChipLocalTemperatureNormalAndEvents(t *testing.T) {
	t.Parallel()
	srv, rec := tevtServer(t, featHeatTEVT)
	v := int16(2150)
	srv.SetLocalTemperature(&v)
	_ = rec.take()
	w := int16(2200)
	srv.SetLocalTemperature(&w)
	got := rec.take()
	wantIDs(t, got, tdef.EventLocalTemperatureChange)
	e := got[0].data.(tdef.LocalTemperatureChangeEvent)
	if e.CurrentLocalTemperature.Null || e.CurrentLocalTemperature.Value != 2200 || got[0].endpoint != 3 {
		t.Errorf("event %+v on endpoint %d", e, got[0].endpoint)
	}
}

// TestChipSystemModeValidationAndEvents ports
// TestSystemModeValidationAndEvents: a written SystemMode emits
// SystemModeChange; Auto without AUTO is ConstraintError and emits nothing.
func TestChipSystemModeValidationAndEvents(t *testing.T) {
	t.Parallel()
	srv, rec := tevtServer(t, featHeatCoolTEVT)
	ctx := context.Background()
	if err := srv.MatterWrite(ctx, tdef.AttrSystemMode, uint8(tdef.SystemModeCool)); err != nil {
		t.Fatal(err)
	}
	got := rec.take()
	wantIDs(t, got, tdef.EventSystemModeChange)
	if e := got[0].data.(tdef.SystemModeChangeEvent); e.PreviousSystemMode == nil || *e.PreviousSystemMode != tdef.SystemModeHeat || e.CurrentSystemMode != tdef.SystemModeCool {
		t.Errorf("event %+v", e)
	}
	if err := srv.MatterWrite(ctx, tdef.AttrSystemMode, uint8(tdef.SystemModeAuto)); !isStatus(err, im.StatusConstraintError) {
		t.Errorf("Auto without AUTO: %v, want ConstraintError", err)
	}
	for _, m := range []tdef.SystemModeEnum{tdef.SystemModeEmergencyHeat, tdef.SystemModePrecooling} {
		if err := srv.MatterWrite(ctx, tdef.AttrSystemMode, uint8(m)); err != nil {
			t.Errorf("mode %d: %v", m, err)
		}
	}
	wantIDs(t, rec.take(), tdef.EventSystemModeChange, tdef.EventSystemModeChange)
}

// TestChipThermostatRunningModeAndRunningState ports
// TestThermostatRunningModeAndRunningState. chip serves ThermostatRunningMode
// as an optional attribute; its conformance is "TEVT & AUTO, [AUTO]", so
// the port runs with AUTO, where it is mandatory.
func TestChipThermostatRunningModeAndRunningState(t *testing.T) {
	t.Parallel()
	state := uint16(0)
	srv, rec := tevtServer(t, featAutoTEVT, func(c *thermo.ThermostatConfig) { c.ThermostatRunningState = &state })
	if v, ok := srv.MatterRead(tdef.AttrThermostatRunningMode); !ok || v != uint8(tdef.ThermostatRunningModeOff) {
		t.Fatalf("ThermostatRunningMode = %v %v, want Off", v, ok)
	}
	if err := srv.SetThermostatRunningMode(uint8(tdef.ThermostatRunningModeHeat)); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetThermostatRunningMode(uint8(tdef.ThermostatRunningModeCool)); err != nil {
		t.Fatal(err)
	}
	got := rec.take()
	wantIDs(t, got, tdef.EventRunningModeChange, tdef.EventRunningModeChange)
	if e := got[1].data.(tdef.RunningModeChangeEvent); *e.PreviousRunningMode != tdef.ThermostatRunningModeHeat || e.CurrentRunningMode != tdef.ThermostatRunningModeCool {
		t.Errorf("event %+v", e)
	}
	if err := srv.SetThermostatRunningMode(0xFF); !isStatus(err, im.StatusConstraintError) {
		t.Errorf("0xFF: %v, want ConstraintError", err)
	}
	srv.SetThermostatRunningState(uint16(tdef.RelayStateHeat))
	got = rec.take()
	wantIDs(t, got, tdef.EventRunningStateChange)
	if e := got[0].data.(tdef.RunningStateChangeEvent); *e.PreviousRunningState != 0 || e.CurrentRunningState != tdef.RelayStateHeat {
		t.Errorf("event %+v", e)
	}
}

// TestChipOccupiedSetpointsEmitSetpointChange ports
// TestOccupiedHeatingAndCoolingSetpointsAttributes: a written occupied
// setpoint emits SetpointChange (ThermostatClusterSetpointsBase.cpp:63-85).
func TestChipOccupiedSetpointsEmitSetpointChange(t *testing.T) {
	t.Parallel()
	srv, rec := tevtServer(t, featAutoTEVT)
	ctx := context.Background()
	if err := srv.MatterWrite(ctx, tdef.AttrOccupiedHeatingSetpoint, int16(2100)); err != nil {
		t.Fatal(err)
	}
	got := rec.take()
	wantIDs(t, got, tdef.EventSetpointChange)
	e := got[0].data.(tdef.SetpointChangeEvent)
	if e.SystemMode != tdef.SystemModeHeat || e.Occupancy == nil || *e.Occupancy != tdef.OccupancyOccupied || *e.PreviousSetpoint != 2000 || e.CurrentSetpoint != 2100 {
		t.Errorf("event %+v", e)
	}
	if err := srv.MatterWrite(ctx, tdef.AttrOccupiedCoolingSetpoint, int16(2500)); err != nil {
		t.Fatal(err)
	}
	got = rec.take()
	wantIDs(t, got, tdef.EventSetpointChange)
	if e := got[0].data.(tdef.SetpointChangeEvent); e.SystemMode != tdef.SystemModeCool || e.CurrentSetpoint != 2500 {
		t.Errorf("event %+v", e)
	}
}

// TestChipSetActivePresetRequestEmitsActivePresetChange ports the event
// part of TestSetActivePresetRequestCommand: an accepted request emits
// ActivePresetChange; an unknown handle is InvalidCommand and emits
// nothing; a null handle clears and emits.
func TestChipSetActivePresetRequestEmitsActivePresetChange(t *testing.T) {
	t.Parallel()
	cfg := thermostatConfig(5, []thermo.Preset{newPreset(withHandle([]byte("PRST")))})
	cfg.Features |= thermo.ThermostatFeatureTEVT
	srv := mustNew(t, cfg)
	rec := &payloadRecorder{}
	srv.SetMatterEventEmitter(rec)
	invoke := func(h spec.Nullable[[]byte]) error {
		ctx := im.WithCommandPath(context.Background(), im.ConcreteCommandPath{Endpoint: 4, Cluster: tdef.ClusterID, Command: tdef.CmdSetActivePresetRequest, HasEndpoint: true})
		_, err := srv.MatterInvoke(ctx, tdef.CmdSetActivePresetRequest, tdef.SetActivePresetRequestRequest{PresetHandle: h})
		return err
	}
	if err := invoke(spec.ValueOf([]byte("PRST"))); err != nil {
		t.Fatal(err)
	}
	got := rec.take()
	wantIDs(t, got, tdef.EventActivePresetChange)
	e := got[0].data.(tdef.ActivePresetChangeEvent)
	if got[0].endpoint != 4 || e.PreviousPresetHandle == nil || !e.PreviousPresetHandle.Null || !bytes.Equal(e.CurrentPresetHandle.Value, []byte("PRST")) {
		t.Errorf("event %+v on %d", e, got[0].endpoint)
	}
	if err := invoke(spec.ValueOf([]byte{0xFF, 0xFF, 0xFF, 0xFF})); !isStatus(err, im.StatusInvalidCommand) {
		t.Errorf("unknown handle: %v, want InvalidCommand", err)
	}
	wantIDs(t, rec.take())
	// The same handle again: chip emits on every accepted request.
	if err := invoke(spec.ValueOf([]byte("PRST"))); err != nil {
		t.Fatal(err)
	}
	wantIDs(t, rec.take(), tdef.EventActivePresetChange)
	if err := invoke(spec.NullOf[[]byte]()); err != nil {
		t.Fatal(err)
	}
	got = rec.take()
	wantIDs(t, got, tdef.EventActivePresetChange)
	if e := got[0].data.(tdef.ActivePresetChangeEvent); !e.CurrentPresetHandle.Null || !bytes.Equal(e.PreviousPresetHandle.Value, []byte("PRST")) {
		t.Errorf("event %+v", e)
	}
}

func isStatus(err error, code im.StatusCode) bool {
	var s interface{ MatterStatusCode() im.StatusCode }
	return errors.As(err, &s) && s.MatterStatusCode() == code
}

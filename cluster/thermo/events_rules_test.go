// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

// The rules of the TEVT events (events.go) beyond matter.js's
// ThermostatEventsTest.ts (events_parity_matterjs_test.go): the gate, the
// EventList per selection, no event for an unchanged value, the payloads
// of each event, the setpoint events of every path that moves a setpoint,
// the events of the start, and the endpoint.

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/cluster/thermo"
	"thingfabric.dev/go-fabric/im"
)

func TestEventsNeedTEVT(t *testing.T) {
	t.Parallel()
	srv, rec := tevtServer(t, thermo.ThermostatFeatureHEAT|thermo.ThermostatFeatureCOOL|thermo.ThermostatFeatureAUTO)
	if got := srv.MatterEvents(); len(got) != 0 {
		t.Fatalf("EventList %v without TEVT", got)
	}
	v := int16(2100)
	srv.SetLocalTemperature(&v)
	srv.SetSystemMode(uint8(tdef.SystemModeCool))
	if err := srv.MatterWrite(context.Background(), tdef.AttrOccupiedHeatingSetpoint, int16(2100)); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetThermostatRunningMode(uint8(tdef.ThermostatRunningModeHeat)); !isStatus(err, im.StatusUnsupportedAttribute) {
		t.Errorf("SetThermostatRunningMode without TEVT: %v, want UnsupportedAttribute", err)
	}
	wantIDs(t, rec.take())
}

func TestEventListFollowsTheSelection(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		features uint32
		want     []uint32
	}{
		"heat": {featHeatTEVT, []uint32{tdef.EventSystemModeChange, tdef.EventLocalTemperatureChange, tdef.EventSetpointChange, tdef.EventRunningStateChange}},
		"ltne": {featHeatTEVT | thermo.ThermostatFeatureLTNE, []uint32{tdef.EventSystemModeChange, tdef.EventSetpointChange, tdef.EventRunningStateChange}},
		"auto+occ": {featAutoTEVT | thermo.ThermostatFeatureOCC, []uint32{
			tdef.EventSystemModeChange, tdef.EventLocalTemperatureChange, tdef.EventOccupancyChange,
			tdef.EventSetpointChange, tdef.EventRunningStateChange, tdef.EventRunningModeChange,
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, _ := tevtServer(t, tc.features)
			got := srv.MatterEvents()
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("EventList %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEventsOnlyForAChange(t *testing.T) {
	t.Parallel()
	srv, rec := tevtServer(t, featAutoTEVT)
	srv.SetSystemMode(uint8(tdef.SystemModeAuto)) // the default
	if err := srv.MatterWrite(context.Background(), tdef.AttrOccupiedHeatingSetpoint, int16(2000)); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetThermostatRunningMode(uint8(tdef.ThermostatRunningModeOff)); err != nil {
		t.Fatal(err)
	}
	srv.SetLocalTemperature(nil) // null, as it starts
	wantIDs(t, rec.take())
	v := int16(1900)
	srv.SetLocalTemperature(&v)
	// The null waits for the end of the 60 s window (the ported cases).
	srv.SetLocalTemperature(nil)
	got := rec.take()
	wantIDs(t, got, tdef.EventLocalTemperatureChange)
	if e := got[0].data.(tdef.LocalTemperatureChangeEvent); e.CurrentLocalTemperature.Null || e.CurrentLocalTemperature.Value != 1900 {
		t.Errorf("event %+v, want 1900", e)
	}
}

// An unoccupied setpoint is reported without occupancy, a user limit not at
// all, and the setpoints a reconcile moves each.
func TestSetpointChangeEvents(t *testing.T) {
	t.Parallel()
	occ := false
	srv, rec := tevtServer(t, featAutoTEVT|thermo.ThermostatFeatureOCC, func(c *thermo.ThermostatConfig) { c.Occupied = &occ })
	ctx := context.Background()
	if err := srv.MatterWrite(ctx, tdef.AttrUnoccupiedHeatingSetpoint, int16(1800)); err != nil {
		t.Fatal(err)
	}
	got := rec.take()
	wantIDs(t, got, tdef.EventSetpointChange)
	if e := got[0].data.(tdef.SetpointChangeEvent); e.SystemMode != tdef.SystemModeHeat || e.Occupancy == nil || *e.Occupancy != 0 || *e.PreviousSetpoint != 2000 || e.CurrentSetpoint != 1800 {
		t.Errorf("event %+v", e)
	}
	if err := srv.MatterWrite(ctx, tdef.AttrMinHeatSetpointLimit, int16(800)); err != nil {
		t.Fatal(err)
	}
	wantIDs(t, rec.take())
	// The deadband (2.0 °C) pushes the occupied cooling setpoint up with
	// the heating one.
	if err := srv.MatterWrite(ctx, tdef.AttrOccupiedHeatingSetpoint, int16(2500)); err != nil {
		t.Fatal(err)
	}
	got = rec.take()
	wantIDs(t, got, tdef.EventSetpointChange, tdef.EventSetpointChange)
	if e := got[1].data.(tdef.SetpointChangeEvent); e.SystemMode != tdef.SystemModeCool || *e.Occupancy != tdef.OccupancyOccupied || *e.PreviousSetpoint != 2600 || e.CurrentSetpoint != 2700 {
		t.Errorf("companion event %+v", e)
	}
}

// The command paths and the host's SetAttributes emit too, on their
// endpoints.
func TestEventsOfCommandsAndHostSets(t *testing.T) {
	t.Parallel()
	srv, rec := tevtServer(t, featAutoTEVT)
	ctx := im.WithCommandPath(context.Background(), im.ConcreteCommandPath{Endpoint: 7, Cluster: tdef.ClusterID, Command: tdef.CmdSetpointRaiseLower, HasEndpoint: true})
	if _, err := srv.MatterInvoke(ctx, tdef.CmdSetpointRaiseLower, tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeHeat, Amount: 10}); err != nil {
		t.Fatal(err)
	}
	got := rec.take()
	wantIDs(t, got, tdef.EventSetpointChange)
	if got[0].endpoint != 7 || got[0].data.(tdef.SetpointChangeEvent).CurrentSetpoint != 2100 {
		t.Errorf("command event %+v", got[0])
	}
	if err := srv.SetAttributes(map[uint32]any{tdef.AttrSystemMode: uint8(tdef.SystemModeHeat), tdef.AttrLocalTemperature: int16(2050)}); err != nil {
		t.Fatal(err)
	}
	got = rec.take()
	// SystemMode Heat sets ThermostatRunningMode Heat (W3-clua1-01, the
	// rule followSystemModeLocked cites), so RunningModeChange follows.
	wantIDs(t, got, tdef.EventSystemModeChange, tdef.EventLocalTemperatureChange, tdef.EventRunningModeChange)
	if got[0].endpoint != 3 {
		t.Errorf("host event on endpoint %d, want the stamped 3", got[0].endpoint)
	}
	srv.SetOccupiedCoolingSetpoint(2900)
	got = rec.take()
	wantIDs(t, got, tdef.EventSetpointChange)
}

// A direct write of the setpoint in effect clears the active preset, and
// the cleared handle emits ActivePresetChange (events.go).
func TestDirectSetpointWriteClearsThePresetWithAnEvent(t *testing.T) {
	t.Parallel()
	cfg := thermostatConfig(5, []thermo.Preset{newPreset(withHandle([]byte{0x01}))})
	cfg.Features |= thermo.ThermostatFeatureTEVT
	srv := mustNew(t, cfg)
	rec := &payloadRecorder{}
	srv.SetMatterEventEmitter(rec)
	ctx := im.WithCommandPath(context.Background(), im.ConcreteCommandPath{Endpoint: 1, Cluster: tdef.ClusterID, Command: tdef.CmdSetActivePresetRequest, HasEndpoint: true})
	if _, err := srv.MatterInvoke(ctx, tdef.CmdSetActivePresetRequest, tdef.SetActivePresetRequestRequest{PresetHandle: spec.ValueOf([]byte{0x01})}); err != nil {
		t.Fatal(err)
	}
	_ = rec.take()
	if err := srv.MatterWrite(context.Background(), tdef.AttrOccupiedHeatingSetpoint, int16(2100)); err != nil {
		t.Fatal(err)
	}
	got := rec.take()
	wantIDs(t, got, tdef.EventSetpointChange, tdef.EventActivePresetChange)
	if e := got[1].data.(tdef.ActivePresetChangeEvent); !e.CurrentPresetHandle.Null || e.PreviousPresetHandle.Null {
		t.Errorf("event %+v", e)
	}
}

// The events of the start — a stale handle cleared, a setpoint the initial
// reconcile moved, the active preset that move ends — wait for the
// endpoint and go out once, at the first SetEndpoint.
func TestStartupEventsWaitForTheEndpoint(t *testing.T) {
	t.Parallel()
	stale := filled(16, 9)
	cfg := fullConfig(func(cfg *thermo.ThermostatConfig) {
		cfg.ActiveScheduleHandle = stale
		cfg.InitialCoolingSetpoint = 2025 // the deadband 25 moves it to 2250
	})
	srv := mustNew(t, cfg)
	rec := &payloadRecorder{}
	srv.SetMatterEventEmitter(rec)
	wantIDs(t, rec.take())
	srv.SetEndpoint(5)
	got := rec.take()
	wantIDs(t, got, tdef.EventActiveScheduleChange, tdef.EventSetpointChange)
	for _, e := range got {
		if e.endpoint != 5 {
			t.Errorf("start-up event %+v not on the stamped endpoint 5", e)
		}
	}
	if e := got[1].data.(tdef.SetpointChangeEvent); e.SystemMode != tdef.SystemModeCool || *e.PreviousSetpoint != 2025 || e.CurrentSetpoint != 2250 {
		t.Errorf("SetpointChange %+v, want Cool 2025 to 2250", e)
	}
	if e := got[0].data.(tdef.ActiveScheduleChangeEvent); !bytes.Equal(e.PreviousScheduleHandle.Value, stale) || !e.CurrentScheduleHandle.Null {
		t.Errorf("ActiveScheduleChange %+v, want the stale handle to null", e)
	}
	srv.SetEndpoint(5) // a reassembly: nothing again
	wantIDs(t, rec.take())

	presetHandle := []byte{0x01}
	pcfg := thermostatConfig(5, []thermo.Preset{newPreset(withHandle(presetHandle))})
	pcfg.Features |= thermo.ThermostatFeatureTEVT
	pcfg.ActivePresetHandle = presetHandle
	pcfg.InitialHeatingSetpoint = 3100
	psrv := mustNew(t, pcfg)
	prec := &payloadRecorder{}
	psrv.SetMatterEventEmitter(prec)
	psrv.SetEndpoint(2)
	recorded := prec.of(tdef.EventActivePresetChange)
	presets := make([]tdef.ActivePresetChangeEvent, 0, len(recorded))
	for _, d := range recorded {
		presets = append(presets, d.(tdef.ActivePresetChangeEvent))
	}
	if len(presets) != 1 || !bytes.Equal(presets[0].PreviousPresetHandle.Value, presetHandle) || !presets[0].CurrentPresetHandle.Null {
		t.Errorf("ActivePresetChange %+v, want the preset the reconcile ended to null", presets)
	}
}

// A host list that drops the active schedule clears the handle, and the
// clear emits; the command that keeps the handle emits nothing, one
// without a command path emits on the stamped endpoint.
func TestActiveScheduleChangeFromEverySource(t *testing.T) {
	t.Parallel()
	c := onlineEvents(t, fullConfig())
	if err := c.srv.SetSchedules([]thermo.Schedule{newSchedule(), newSchedule()}); err != nil {
		t.Fatal(err)
	}
	list := c.srv.Schedules()
	first := list[0].ScheduleHandle.Value
	c.record()
	invoke := func(h []byte) {
		t.Helper()
		if _, err := c.srv.MatterInvoke(context.Background(), tdef.CmdSetActiveScheduleRequest, tdef.SetActiveScheduleRequestRequest{ScheduleHandle: h}); err != nil {
			t.Fatal(err)
		}
	}
	invoke(first)
	invoke(first)
	got := c.rec.take()
	wantIDs(t, got, tdef.EventActiveScheduleChange)
	if got[0].endpoint != 1 {
		t.Errorf("event on endpoint %d, want the stamped 1", got[0].endpoint)
	}
	if err := c.srv.SetSchedules([]thermo.Schedule{list[1]}); err != nil {
		t.Fatal(err)
	}
	got = c.rec.take()
	wantIDs(t, got, tdef.EventActiveScheduleChange)
	if e := got[0].data.(tdef.ActiveScheduleChangeEvent); !bytes.Equal(e.PreviousScheduleHandle.Value, first) || !e.CurrentScheduleHandle.Null {
		t.Errorf("event %+v, want the dropped handle to null", e)
	}
	if h := c.srv.ActiveScheduleHandle(); h != nil {
		t.Errorf("ActiveScheduleHandle %x, want null", h)
	}
}

// The threshold is checked only when the selection has the event, as
// matter.js installs the check only then.
func TestLocalTemperatureChangeThresholdWithoutTheEvent(t *testing.T) {
	t.Parallel()
	zero := int16(0)
	srv := mustNew(t, configBase(featAutoTEVT|thermo.ThermostatFeatureLTNE, func(cfg *thermo.ThermostatConfig) { cfg.LocalTemperatureChangeThreshold = &zero }))
	if err := srv.SetLocalTemperatureChangeThreshold(-5); err != nil {
		t.Errorf("SetLocalTemperatureChangeThreshold under LTNE: %v, want accepted", err)
	}
}

// The host's SetAttributes of Occupancy emits OccupancyChange as SetOccupied.
func TestOccupancyChangeFromSetAttributes(t *testing.T) {
	t.Parallel()
	c := onlineEvents(t, fullConfig())
	c.record()
	if err := c.srv.SetAttributes(map[uint32]any{tdef.AttrOccupancy: uint8(0)}); err != nil {
		t.Fatal(err)
	}
	got := c.rec.take()
	wantIDs(t, got, tdef.EventOccupancyChange)
	if e := got[0].data.(tdef.OccupancyChangeEvent); *e.PreviousOccupancy != tdef.OccupancyOccupied || e.CurrentOccupancy != 0 {
		t.Errorf("event %+v", e)
	}
}

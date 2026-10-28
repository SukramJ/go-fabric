// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

// The rules of the TEVT events (events.go) beyond chip's tests: the gate,
// the EventList per selection, no event for an unchanged value, the
// payloads of each event, the setpoint events of every path that moves a
// setpoint, and the endpoint.

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
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
	srv.SetLocalTemperature(nil)
	got := rec.take()
	wantIDs(t, got, tdef.EventLocalTemperatureChange, tdef.EventLocalTemperatureChange)
	if e := got[1].data.(tdef.LocalTemperatureChangeEvent); !e.CurrentLocalTemperature.Null {
		t.Errorf("event %+v, want null", e)
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
	wantIDs(t, got, tdef.EventSystemModeChange, tdef.EventLocalTemperatureChange)
	if got[0].endpoint != 3 {
		t.Errorf("host event on endpoint %d, want the stamped 3", got[0].endpoint)
	}
	srv.SetOccupiedCoolingSetpoint(2900)
	got = rec.take()
	wantIDs(t, got, tdef.EventSetpointChange)
}

// A direct write of the setpoint in effect clears the active preset, and
// chip's SetActivePreset(null) on that path emits ActivePresetChange
// (ThermostatCluster.h:215-224).
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

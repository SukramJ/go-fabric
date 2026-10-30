// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/surface"
)

// switchingPort is a thermostatdef.SchedulePort that records the active
// schedules it is told.
type switchingPort struct{ active [][]byte }

func (p *switchingPort) SetActiveSchedule(_ context.Context, handle []byte) error {
	p.active = append(p.active, handle)
	return nil
}

func (*switchingPort) CommitSchedules(context.Context, []thermostatdef.ScheduleStruct) error {
	return nil
}

// TestThermostatDeclaresSchedules: a declaration with the
// MatterScheduleConfiguration feature takes the schedule types, counts,
// schedules and active handle from its Initial and its SchedulePort from
// the port field; a SetActiveScheduleRequest reaches the port.
func TestThermostatDeclaresSchedules(t *testing.T) {
	t.Parallel()
	types := []thermostatdef.ScheduleTypeStruct{{SystemMode: thermostatdef.SystemModeHeat, NumberOfSchedules: 1, ScheduleTypeFeatures: thermostatdef.ScheduleTypeFeaturesSupportsSetpoints}}
	heat := int16(2000)
	schedules := []thermostatdef.ScheduleStruct{{
		ScheduleHandle: spec.ValueOf([]byte{0x01}), SystemMode: thermostatdef.SystemModeHeat,
		Transitions: []thermostatdef.ScheduleTransitionStruct{{DayOfWeek: thermostatdef.ScheduleDayOfWeekMonday, HeatingSetpoint: &heat}},
		BuiltIn:     spec.NullOf[bool](),
	}}
	one, transitions := uint8(1), uint8(4)
	port := &switchingPort{}
	// MSCH is ahead of matter.js (thermo.Ahead): the row is enabled.
	schedulesOn := surface.Policies{Extensions: surface.NewExtensions("BD-chip-Thermostat-Schedules")}
	d, err := device.NewThermostat(device.Options{StableKey: device.StringKey("thermo-msch"), Name: "thermo", Surface: &schedulesOn}, thermostatdef.Declare{
		Features: thermostatdef.FeatureHeating | thermostatdef.FeatureMatterScheduleConfiguration,
		Initial: thermostatdef.Patch{
			ScheduleTypes: &types, NumberOfSchedules: &one, NumberOfScheduleTransitions: &transitions, Schedules: &schedules,
		},
		SchedulePort: port,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := d.Thermostat()
	st := srv.Get()
	if len(st.Schedules) != 1 || st.Schedules[0].BuiltIn.Null || !st.Schedules[0].BuiltIn.Value {
		t.Fatalf("schedules %+v, want the declared one, built in", st.Schedules)
	}
	ctx := im.WithCommandPath(context.Background(), im.ConcreteCommandPath{Endpoint: 1, Cluster: thermostatdef.ClusterID, Command: thermostatdef.CmdSetActiveScheduleRequest, HasEndpoint: true})
	if _, err := srv.Underlying().MatterInvoke(ctx, thermostatdef.CmdSetActiveScheduleRequest, thermostatdef.SetActiveScheduleRequestRequest{ScheduleHandle: []byte{0x01}}); err != nil {
		t.Fatal(err)
	}
	if len(port.active) != 1 || !bytes.Equal(port.active[0], []byte{0x01}) {
		t.Errorf("port calls %v, want [01]", port.active)
	}
}

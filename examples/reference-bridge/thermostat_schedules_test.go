// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/im"
)

// TestDemoThermostatSchedulesAreGated: the demo thermostat serves
// MatterScheduleConfiguration only with --thermostat-schedules — the host's
// flag, off by default (issue #273) — and with it the built-in week program
// is stored and active, and SetActiveScheduleRequest switches to it
// through the thermostat's SchedulePort.
func TestDemoThermostatSchedulesAreGated(t *testing.T) {
	t.Parallel()
	opts := device.Options{StableKey: device.StringKey("demo-thermostat"), Name: "Thermostat"}
	plain, err := newDemoThermostat("Thermostat").device(opts)
	if err != nil {
		t.Fatal(err)
	}
	if fm, _ := plain.Thermostat().Underlying().MatterRead(spec.AttrFeatureMap); fm.(uint32)&uint32(thermostatdef.FeatureMatterScheduleConfiguration) != 0 {
		t.Errorf("FeatureMap 0x%X serves MSCH without the flag", fm)
	}

	gated := newDemoThermostat("Thermostat")
	gated.schedules = true
	d, err := gated.device(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv := d.Thermostat()
	st := srv.Get()
	if len(st.Schedules) != 1 || st.ActiveScheduleHandle == nil || !bytes.Equal(*st.ActiveScheduleHandle, weekdaysHandle) {
		t.Fatalf("schedules %+v active %v, want the active built-in week program", st.Schedules, st.ActiveScheduleHandle)
	}
	ctx := im.WithCommandPath(context.Background(), im.ConcreteCommandPath{Endpoint: 1, Cluster: thermostatdef.ClusterID, Command: thermostatdef.CmdSetActiveScheduleRequest, HasEndpoint: true})
	if _, err := srv.Underlying().MatterInvoke(ctx, thermostatdef.CmdSetActiveScheduleRequest, thermostatdef.SetActiveScheduleRequestRequest{ScheduleHandle: weekdaysHandle}); err != nil {
		t.Errorf("SetActiveScheduleRequest: %v", err)
	}
}

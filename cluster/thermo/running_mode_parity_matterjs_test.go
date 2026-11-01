// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"testing"

	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
)

// TestParityMatterJS_SystemModeDrivesRunningMode: a SystemMode that
// changes to Off, Heat or Cool sets ThermostatRunningMode to the same
// mode when the attribute is served, by a controller's write and by the
// host's SetSystemMode alike, and emits RunningModeChange; Auto leaves it
// — matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts:1185,
// :1194-1206 at 862ad75f (#handleSystemModeChange; W3-clua1-01).
func TestParityMatterJS_SystemModeDrivesRunningMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srv, rec := tevtServer(t, featAutoTEVT)
	runningMode := func() uint8 {
		v, _ := srv.MatterRead(tdef.AttrThermostatRunningMode)
		return v.(uint8)
	}
	if err := srv.MatterWrite(ctx, tdef.AttrSystemMode, uint8(tdef.SystemModeHeat)); err != nil {
		t.Fatal(err)
	}
	if got := runningMode(); got != uint8(tdef.ThermostatRunningModeHeat) {
		t.Fatalf("ThermostatRunningMode %d after SystemMode Heat, want Heat", got)
	}
	got := rec.take()
	wantIDs(t, got, tdef.EventSystemModeChange, tdef.EventRunningModeChange)
	if e := got[1].data.(tdef.RunningModeChangeEvent); *e.PreviousRunningMode != tdef.ThermostatRunningModeOff || e.CurrentRunningMode != tdef.ThermostatRunningModeHeat {
		t.Errorf("RunningModeChange %+v", e)
	}

	srv.SetSystemMode(uint8(tdef.SystemModeCool))
	if got := runningMode(); got != uint8(tdef.ThermostatRunningModeCool) {
		t.Fatalf("ThermostatRunningMode %d after SetSystemMode Cool, want Cool", got)
	}
	wantIDs(t, rec.take(), tdef.EventSystemModeChange, tdef.EventRunningModeChange)

	if err := srv.MatterWrite(ctx, tdef.AttrSystemMode, uint8(tdef.SystemModeAuto)); err != nil {
		t.Fatal(err)
	}
	if got := runningMode(); got != uint8(tdef.ThermostatRunningModeCool) {
		t.Fatalf("ThermostatRunningMode %d after SystemMode Auto, want Cool kept", got)
	}
	wantIDs(t, rec.take(), tdef.EventSystemModeChange)

	srv.SetSystemMode(uint8(tdef.SystemModeOff))
	if got := runningMode(); got != uint8(tdef.ThermostatRunningModeOff) {
		t.Fatalf("ThermostatRunningMode %d after SetSystemMode Off, want Off", got)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/closure"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/servicearea"
	"github.com/SukramJ/go-fabric/cluster/spec"
	cdim "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	tsuic "github.com/SukramJ/go-fabric/cluster/spec/thermostatuserinterfaceconfiguration"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/schema"
)

// mountedServer returns the server of clusterID on ep.
func mountedServer(t *testing.T, ep *endpoint.Endpoint, clusterID uint32) contract.ClusterServer {
	t.Helper()
	for _, srv := range endpoint.ClusterServers(ep) {
		if srv != nil && srv.MatterClusterID() == clusterID {
			return srv
		}
	}
	t.Fatalf("endpoint %d mounts no cluster 0x%04X", ep.ID, clusterID)
	return nil
}

// TestVacuumServesItsAreas: the vacuum's endpoint mounts ServiceArea with
// one map and two areas; a selection while cleaning is InvalidInMode, one
// while idle is stored with its Progress, and the app pipe's Reset clears
// it.
func TestVacuumServesItsAreas(t *testing.T) {
	f, br := startFleetBridge(t)
	vac := mountedEndpoint(t, br, "demo-vacuum-1")
	if maps := readMounted(t, vac, sa.ClusterID, sa.AttrSupportedMaps).(spec.List[servicearea.Map]); len(maps) != 1 {
		t.Errorf("SupportedMaps %+v, want one map", maps)
	}
	if areas := readMounted(t, vac, sa.ClusterID, sa.AttrSupportedAreas).(spec.List[servicearea.Area]); len(areas) != 2 {
		t.Errorf("SupportedAreas %+v, want two areas", areas)
	}
	srv := mountedServer(t, vac, sa.ClusterID)
	ctx := context.Background()
	if err := f.vacuum.run.SetCurrentMode(rvcCleaning); err != nil {
		t.Fatal(err)
	}
	resp, err := srv.MatterInvoke(ctx, sa.CmdSelectAreas, sa.SelectAreasRequest{NewAreas: []uint32{areaKitchen}})
	if err != nil || resp.(sa.SelectAreasResponse).Status != servicearea.SelectAreasInvalidInMode {
		t.Errorf("select while cleaning: %+v %v", resp, err)
	}
	if err := f.vacuum.run.SetCurrentMode(rvcIdle); err != nil {
		t.Fatal(err)
	}
	resp, _ = srv.MatterInvoke(ctx, sa.CmdSelectAreas, sa.SelectAreasRequest{NewAreas: []uint32{areaKitchen, areaLivingRoom}})
	if resp.(sa.SelectAreasResponse).Status != servicearea.SelectAreasSuccess {
		t.Fatalf("select while idle: %+v", resp)
	}
	resp, _ = srv.MatterInvoke(ctx, sa.CmdSkipArea, sa.SkipAreaRequest{SkippedArea: areaKitchen})
	if resp.(sa.SkipAreaResponse).Status != servicearea.SkipAreaInvalidInMode {
		t.Errorf("skip while idle: %+v", resp)
	}
	if err := f.vacuum.run.SetCurrentMode(rvcCleaning); err != nil {
		t.Fatal(err)
	}
	resp, _ = srv.MatterInvoke(ctx, sa.CmdSkipArea, sa.SkipAreaRequest{SkippedArea: areaKitchen})
	if resp.(sa.SkipAreaResponse).Status != servicearea.SkipAreaSuccess {
		t.Errorf("skip while cleaning: %+v", resp)
	}
	// Back to Idle: every Progress entry Skipped.
	if err := f.vacuum.run.SetCurrentMode(rvcIdle); err != nil {
		t.Fatal(err)
	}
	for _, p := range readMounted(t, vac, sa.ClusterID, sa.AttrProgress).(spec.List[servicearea.Progress]) {
		if p.Status != servicearea.StatusSkipped {
			t.Errorf("Progress %+v after Idle, want Skipped", p)
		}
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "Reset"}); err != nil {
		t.Fatal(err)
	}
	if sel := readMounted(t, vac, sa.ClusterID, sa.AttrSelectedAreas).([]uint32); len(sel) != 0 {
		t.Errorf("SelectedAreas after Reset %v", sel)
	}
	if mode := readMounted(t, vac, modebase.ClusterIDRvcRunMode, modebase.AttrCurrentMode); mode != rvcIdle {
		t.Errorf("run mode after Reset %v", mode)
	}
}

// TestPanelUnlatchesAndMoves: the ClosurePanel endpoint mounts
// ClosureDimension; a SetTarget that unlatches and moves arrives at the
// target, a tick at a time.
func TestPanelUnlatchesAndMoves(t *testing.T) {
	if name, _ := schema.DeviceTypeName(uint32(closure.DeviceTypeClosurePanel)); name != "ClosurePanel" {
		t.Fatalf("device type 0x0231 is %q", name)
	}
	_, br := startFleetBridge(t)
	panel := mountedEndpoint(t, br, "demo-panel-1")
	if panel.DeviceType != closure.DeviceTypeClosurePanel {
		t.Fatalf("panel advertises 0x%04X", panel.DeviceType)
	}
	// The one departure is the Descriptor's TAGLIST, which ClosurePanel
	// marks mandatory (matter.js closure-panel-device.element.ts:16) and
	// the module's Descriptor does not serve — by_design.md
	// BD-Matter-ClosureWithoutTagList, the garage's Closure endpoint's
	// departure as well.
	for _, v := range endpoint.ValidateDeviceTypes(br.Topology()) {
		if v.Endpoint == panel.ID && v.Requirement != "Descriptor.TAGLIST" {
			t.Errorf("device type violation: %s", v)
		}
	}
	srv := mountedServer(t, panel, cdim.ClusterID)
	target, unlatch := uint16(8000), false
	if _, err := srv.MatterInvoke(context.Background(), cdim.CmdSetTarget, cdim.SetTargetRequest{Position: &target, Latch: &unlatch}); err != nil {
		t.Fatalf("SetTarget: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		cur := readMounted(t, panel, cdim.ClusterID, cdim.AttrCurrentState).(closure.DimensionState)
		if cur.Position.Value == target && !cur.Latch.Value {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("CurrentState %+v did not reach position %d unlatched", cur, target)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestThermostatHasItsUIConfiguration: the thermostat mounts
// ThermostatUserInterfaceConfiguration with matter.js's initial values.
func TestThermostatHasItsUIConfiguration(t *testing.T) {
	_, br := startFleetBridge(t)
	ts := mountedEndpoint(t, br, "demo-thermostat-1")
	if v := readMounted(t, ts, tsuic.ClusterID, tsuic.AttrTemperatureDisplayMode); v != uint8(tsuic.TemperatureDisplayModeCelsius) {
		t.Errorf("TemperatureDisplayMode %v", v)
	}
	if v := readMounted(t, ts, tsuic.ClusterID, tsuic.AttrKeypadLockout); v != uint8(tsuic.KeypadLockoutNoLockout) {
		t.Errorf("KeypadLockout %v", v)
	}
}

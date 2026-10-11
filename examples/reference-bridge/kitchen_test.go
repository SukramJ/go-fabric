// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/alarmbase"
	"github.com/SukramJ/go-fabric/cluster/appliance"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	mwo "github.com/SukramJ/go-fabric/cluster/spec/microwaveovencontrol"
	tcdef "github.com/SukramJ/go-fabric/cluster/spec/temperaturecontrol"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/schema"
)

// TestOvenHasItsHeatingCavityPart: the Oven's mandatory
// TemperatureControlledCabinet is a part of the oven's endpoint, states the
// Heater condition the Oven requires of a descendant, and serves OvenMode,
// OvenCavityOperationalState and TemperatureControl (TN); the device-type
// validator reports nothing for either endpoint.
func TestOvenHasItsHeatingCavityPart(t *testing.T) {
	if name, _ := schema.DeviceTypeName(uint32(deviceTypeOven)); name != "Oven" {
		t.Errorf("device type 0x%04X is %q in the snapshot", deviceTypeOven, name)
	}
	_, br := startFleetBridge(t)
	topo := br.Topology()
	oven := mountedEndpoint(t, br, "demo-oven-1")
	if oven.DeviceType != deviceTypeOven || len(oven.PartIDs) != 1 {
		t.Fatalf("oven %+v: want an Oven with one part", oven)
	}
	cavity := topo.FindByID(oven.PartIDs[0])
	if cavity == nil || cavity.DeviceType != deviceTypeTemperatureControlledCabinet ||
		!slices.Equal(cavity.DeviceConditions, []string{conditionHeater}) {
		t.Fatalf("the oven's part is %+v, want a Heater TemperatureControlledCabinet", cavity)
	}
	for _, v := range endpoint.ValidateDeviceTypes(topo) {
		if v.Endpoint == oven.ID || v.Endpoint == cavity.ID {
			t.Errorf("device type violation: %s", v)
		}
	}
	readMounted(t, oven, 0x0039, 0x0001) // BridgedDeviceBasicInformation VendorName
	if fm := readMounted(t, cavity, tcdef.ClusterID, cluster.AttrGlobalFeatureMap); fm != uint32(tcdef.FeatureTemperatureNumber) {
		t.Errorf("cavity TemperatureControl FeatureMap = %v, want TN", fm)
	}
	readMounted(t, cavity, modebase.ClusterIDOvenMode, modebase.AttrCurrentMode)
	if v := readMounted(t, cavity, opstate.ClusterIDOvenCavityOperationalState, opstate.AttrPhaseList); !slices.Equal(v.([]string), opstate.OvenCavityPhases) {
		t.Errorf("PhaseList %v", v)
	}
	ops := mountedServer(t, cavity, opstate.ClusterIDOvenCavityOperationalState)
	if _, err := ops.MatterInvoke(context.Background(), opstate.CmdStart, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := ops.MatterRead(opstate.AttrOperationalState); v != uint8(opstate.StateRunning) {
		t.Errorf("after Start: %v", v)
	}
}

// TestMicrowaveCooks: the MicrowaveOven serves OperationalState (with
// CountdownTime), MicrowaveOvenMode and MicrowaveOvenControl, and
// SetCookingParameters with StartAfterSetting starts it.
func TestMicrowaveCooks(t *testing.T) {
	_, br := startFleetBridge(t)
	topo := br.Topology()
	mw := mountedEndpoint(t, br, "demo-microwave-1")
	for _, v := range endpoint.ValidateDeviceTypes(topo) {
		if v.Endpoint == mw.ID {
			t.Errorf("device type violation: %s", v)
		}
	}
	readMounted(t, mw, modebase.ClusterIDMicrowaveOvenMode, modebase.AttrCurrentMode)
	readMounted(t, mw, opstate.ClusterIDOperationalState, opstate.AttrCountdownTime)
	control := mountedServer(t, mw, appliance.ClusterIDMicrowaveOvenControl)
	start, cook := true, uint32(90)
	if _, err := control.MatterInvoke(context.Background(), mwo.CmdSetCookingParameters,
		mwo.SetCookingParametersRequest{CookMode: new(microwaveDefrost), CookTime: &cook, StartAfterSetting: &start}); err != nil {
		t.Fatal(err)
	}
	if v := readMounted(t, mw, opstate.ClusterIDOperationalState, opstate.AttrOperationalState); v != uint8(opstate.StateRunning) {
		t.Errorf("OperationalState %v", v)
	}
	if v := readMounted(t, mw, modebase.ClusterIDMicrowaveOvenMode, modebase.AttrCurrentMode); v != microwaveDefrost {
		t.Errorf("CurrentMode %v", v)
	}
	if v, _ := control.MatterRead(mwo.AttrCookTime); v != cook {
		t.Errorf("CookTime %v", v)
	}
	// Running: a second SetCookingParameters is INVALID_IN_STATE.
	if _, err := control.MatterInvoke(context.Background(), mwo.CmdSetCookingParameters, mwo.SetCookingParametersRequest{}); err == nil {
		t.Error("SetCookingParameters while running accepted")
	}
	if _, err := control.MatterInvoke(context.Background(), mwo.CmdAddMoreTime, mwo.AddMoreTimeRequest{TimeToAdd: 30}); err != nil {
		t.Fatal(err)
	}
	if v, _ := control.MatterRead(mwo.AttrCookTime); v != cook+30 {
		t.Errorf("CookTime after AddMoreTime %v", v)
	}
}

// TestKitchenAlarmsAndControls: the dishwasher serves DishwasherAlarm, the
// fridge RefrigeratorAlarm (driven by CHIP's SetRefrigeratorDoorStatus),
// the washer LaundryWasherControls and the dryer LaundryDryerControls.
func TestKitchenAlarmsAndControls(t *testing.T) {
	f, br := startFleetBridge(t)
	topo := br.Topology()
	dish := mountedEndpoint(t, br, "demo-dishwasher-1")
	if v := readMounted(t, dish, alarmbase.ClusterIDDishwasherAlarm, alarmbase.AttrSupported); v != dishwasherAlarms {
		t.Errorf("DishwasherAlarm Supported %v", v)
	}
	washer := mountedEndpoint(t, br, "demo-washer-1")
	readMounted(t, washer, appliance.ClusterIDLaundryWasherControls, 0x0000) // SpinSpeeds
	dryer := mountedEndpoint(t, br, "demo-dryer-1")
	readMounted(t, dryer, appliance.ClusterIDLaundryDryerControls, 0x0001) // SelectedDrynessLevel
	fridge := mountedEndpoint(t, br, "demo-fridge-1")
	for _, ep := range []*endpoint.Endpoint{dish, washer, dryer, fridge} {
		for _, v := range endpoint.ValidateDeviceTypes(topo) {
			if v.Endpoint == ep.ID {
				t.Errorf("device type violation: %s", v)
			}
		}
	}
	one, zero := uint8(1), uint8(0)
	if err := f.applyPipeCommand(pipeCommand{Name: "SetRefrigeratorDoorStatus", EndpointID: fridge.ID, DoorOpen: &one}); err != nil {
		t.Fatal(err)
	}
	if v := readMounted(t, fridge, alarmbase.ClusterIDRefrigeratorAlarm, alarmbase.AttrState); v != fridgeDoorOpen {
		t.Errorf("State after the door opened %v", v)
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "SetRefrigeratorDoorStatus", EndpointID: fridge.ID, DoorOpen: &zero}); err != nil {
		t.Fatal(err)
	}
	if st, mask := readMounted(t, fridge, alarmbase.ClusterIDRefrigeratorAlarm, alarmbase.AttrState), readMounted(t, fridge, alarmbase.ClusterIDRefrigeratorAlarm, alarmbase.AttrMask); st != uint32(0) || mask != uint32(0) {
		t.Errorf("after the door closed: State %v Mask %v", st, mask)
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "SetRefrigeratorDoorStatus"}); err == nil {
		t.Error("SetRefrigeratorDoorStatus without DoorOpen applied")
	}
}

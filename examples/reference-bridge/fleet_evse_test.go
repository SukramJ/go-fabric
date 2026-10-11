// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/modebase"
	"github.com/SukramJ/go-fabric/cluster/spec"
	demdef "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	evsedef "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/schema"
)

// TestEvseHasItsParts: the charger advertises EnergyEvse (0x050C) with
// EnergyEvse and EnergyEvseMode, and three parts — PowerSource (wired),
// DeviceEnergyManagement (ControllableEsa, with its mode cluster and the
// app's PA|PFR|STA|PAU|FA|CON) and ElectricalSensor — and the device-type
// validator reports nothing for any of the four endpoints.
func TestEvseHasItsParts(t *testing.T) {
	for id, want := range map[uint16]string{
		deviceTypeEnergyEvse: "EnergyEvse", deviceTypeDeviceEnergyManagement: "DeviceEnergyManagement",
		deviceTypeElectricalSensor: "ElectricalSensor", deviceTypePowerSource: "PowerSource",
	} {
		if name, _ := schema.DeviceTypeName(uint32(id)); name != want {
			t.Errorf("device type 0x%04X is %q in the snapshot, want %q", id, name, want)
		}
	}
	_, br := startFleetBridge(t)
	topo := br.Topology()
	charger := mountedEndpoint(t, br, "demo-evse-1")
	if charger.DeviceType != deviceTypeEnergyEvse || len(charger.PartIDs) != 3 {
		t.Fatalf("charger %+v: want an EnergyEvse with three parts", charger)
	}
	parts := map[uint16]*endpoint.Endpoint{}
	for _, id := range charger.PartIDs {
		p := topo.FindByID(id)
		if p == nil {
			t.Fatalf("part %d not in the topology", id)
		}
		parts[p.DeviceType] = p
	}
	for _, v := range endpoint.ValidateDeviceTypes(topo) {
		if v.Endpoint == charger.ID || slices.Contains(charger.PartIDs, v.Endpoint) {
			t.Errorf("device type violation: %s", v)
		}
	}
	demPart := parts[deviceTypeDeviceEnergyManagement]
	if demPart == nil || !slices.Equal(demPart.DeviceConditions, []string{conditionControllableEsa}) {
		t.Fatalf("DeviceEnergyManagement part %+v", demPart)
	}
	if fm := readMounted(t, demPart, demdef.ClusterID, cluster.AttrGlobalFeatureMap); fm != uint32(0x7B) {
		t.Errorf("DeviceEnergyManagement FeatureMap = %v, want 0x7B", fm)
	}
	readMounted(t, demPart, modebase.ClusterIDDeviceEnergyManagementMode, modebase.AttrCurrentMode)
	if fm := readMounted(t, charger, evsedef.ClusterID, cluster.AttrGlobalFeatureMap); fm != uint32(0x1F) {
		t.Errorf("EnergyEvse FeatureMap = %v, want PREF|SOC|PNC|RFID|V2X", fm)
	}
	readMounted(t, charger, modebase.ClusterIDEnergyEvseMode, modebase.AttrCurrentMode)
	power := parts[deviceTypePowerSource]
	if power == nil {
		t.Fatal("no PowerSource part")
	}
	if fm := readMounted(t, power, psdef.ClusterID, cluster.AttrGlobalFeatureMap); fm != uint32(psdef.FeatureWired) {
		t.Errorf("PowerSource FeatureMap = %v, want WIRED", fm)
	}
	if v := readMounted(t, power, psdef.ClusterID, psdef.AttrEndpointList); !slices.Equal(v.([]uint16), []uint16{power.ID}) {
		t.Errorf("PowerSource EndpointList %v", v)
	}
	if parts[deviceTypeElectricalSensor] == nil {
		t.Fatal("no ElectricalSensor part")
	}
}

func fire(t *testing.T, e *demoEvse, trigger uint64) {
	t.Helper()
	if !e.testEventTrigger(trigger) {
		t.Fatalf("trigger 0x%016X not handled", trigger)
	}
}

// TestEvseTriggerSession replays TC_EEVSE_2_2.py's trigger and command
// sequence (connectedhomeip at the harness pin) against the daemon's
// charger: basic functionality, plug-in, enable charging, charge demand,
// the user limit, demand clear, unplug, basic functionality clear.
func TestEvseTriggerSession(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newDemoEvse("test charger")
		e.build()
		s := e.evse
		fire(t, e, triggerEvseBasicFunctionality)
		fire(t, e, triggerEvsePluggedIn|0x0000_0002_0000_0000) // an endpoint in the trigger is ignored
		if s.State() != energy.EvsePluggedInNoDemand {
			t.Fatalf("State %d after plug-in", s.State())
		}
		req := evsedef.EnableChargingRequest{ChargingEnabledUntil: spec.NullOf[uint32](), MinimumChargeCurrent: 6000, MaximumChargeCurrent: 60000}
		if _, err := s.MatterInvoke(context.Background(), evsedef.CmdEnableCharging, req); err != nil {
			t.Fatal(err)
		}
		fire(t, e, triggerEvseChargeDemand)
		if s.State() != energy.EvsePluggedInCharging || s.MaximumChargeCurrent() != 32000 {
			t.Errorf("charging: State %d, MaximumChargeCurrent %d", s.State(), s.MaximumChargeCurrent())
		}
		fire(t, e, triggerEvseChargeDemandClear)
		fire(t, e, triggerEvsePluggedInClear)
		if s.State() != energy.EvseNotPluggedIn {
			t.Errorf("State %d after unplug", s.State())
		}
		fire(t, e, triggerEvseBasicFunctionalityClear)
		if s.CircuitCapacity() != 0 || s.UserMaximumChargeCurrent() != 0 {
			t.Error("basic functionality clear did not restore")
		}
		for _, tr := range []uint64{
			triggerEvseGroundFault, triggerEvseOverTemperatureFault, triggerEvseFaultClear,
			triggerEvseSetSoCLow, triggerEvseSetSoCHigh, triggerEvseSetSoCClear, triggerEvseSetVehicleID,
			triggerEvseTriggerRFID, triggerEvseDiagnosticsComplete, triggerEvseTimeOfUseMode, triggerEvseTimeOfUseModeClear,
		} {
			fire(t, e, tr)
		}
		if e.testEventTrigger(0x0099_0000_0000_00FF) || e.testEventTrigger(0x1234_0000_0000_0000) {
			t.Error("an unknown trigger was handled")
		}
		s.Close()
	})
}

// TestEvseChargingSchedule: with targets set, the EV plugged in and
// charging enabled, ComputeChargingSchedule (EVSEManufacturerImpl.cpp:
// 335-423) fills NextChargeTargetTime and NextChargeStartTime; a target of
// TargetSoC 100 only leaves NextChargeRequiredEnergy null and
// NextChargeTargetSoC 100 when the vehicle reports no SoC
// (TC_EEVSE_2_3.py steps 11-12).
func TestEvseChargingSchedule(t *testing.T) {
	t.Parallel()
	e := newDemoEvse("test charger")
	e.now = func() time.Time { return time.Date(2026, 10, 12, 10, 0, 0, 0, time.Local) }
	e.build()
	s := e.evse
	fire(t, e, triggerEvseBasicFunctionality)
	fire(t, e, triggerEvsePluggedIn)
	hundred := uint8(100)
	if _, err := s.MatterInvoke(context.Background(), evsedef.CmdSetTargets, evsedef.SetTargetsRequest{ChargingTargetSchedules: []energy.ChargingTargetSchedule{
		{DayOfWeekForSequence: 0x7F, ChargingTargets: []energy.ChargingTarget{{TargetTimeMinutesPastMidnight: 1439, TargetSoC: &hundred}}},
	}}); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.MatterRead(evsedef.AttrNextChargeTargetTime); v != nil {
		t.Errorf("NextChargeTargetTime %v before charging is enabled", v)
	}
	req := evsedef.EnableChargingRequest{ChargingEnabledUntil: spec.NullOf[uint32](), MinimumChargeCurrent: 6000, MaximumChargeCurrent: 60000}
	if _, err := s.MatterInvoke(context.Background(), evsedef.CmdEnableCharging, req); err != nil {
		t.Fatal(err)
	}
	target, _ := s.MatterRead(evsedef.AttrNextChargeTargetTime)
	start, _ := s.MatterRead(evsedef.AttrNextChargeStartTime)
	now := uint32(e.now().Unix() - 946684800)                         //nolint:gosec // test clock
	if tt, ok := target.(uint32); !ok || tt != (now/60+1439-600)*60 { // 23:59 local, today
		t.Errorf("NextChargeTargetTime %v", target)
	}
	if st, ok := start.(uint32); !ok || st != now { // 1000 MWh at 32 A cannot finish: start now
		t.Errorf("NextChargeStartTime %v", start)
	}
	if v, _ := s.MatterRead(evsedef.AttrNextChargeTargetSoC); v != uint8(100) {
		t.Errorf("NextChargeTargetSoC %v", v)
	}
	if v, _ := s.MatterRead(evsedef.AttrNextChargeRequiredEnergy); v != nil {
		t.Errorf("NextChargeRequiredEnergy %v", v)
	}
	// With the vehicle's SoC known, the energy is the SoC gap.
	fire(t, e, triggerEvseSetSoCLow)
	e.computeChargingSchedule()
	if v, _ := s.MatterRead(evsedef.AttrNextChargeRequiredEnergy); v != nil {
		t.Errorf("NextChargeRequiredEnergy %v with SoC", v)
	}
	if st, _ := s.MatterRead(evsedef.AttrNextChargeStartTime); st == start {
		t.Errorf("NextChargeStartTime did not move with the SoC: %v", st)
	}
	if _, err := s.MatterInvoke(context.Background(), evsedef.CmdClearTargets, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.MatterRead(evsedef.AttrNextChargeTargetTime); v != nil {
		t.Errorf("NextChargeTargetTime %v after ClearTargets", v)
	}
	s.Close()
}

// TestDemTriggers drives the DEM test event triggers
// (DEMTestEventTriggers.cpp) through the commands TC_DEM_2_x send after
// them.
func TestDemTriggers(t *testing.T) {
	t.Parallel()
	e := newDemoEvse("test charger")
	e.build()
	d := e.dem.srv
	defer d.Close()
	invoke := func(cmd uint32, fields any) error {
		_, err := d.MatterInvoke(context.Background(), cmd, fields)
		return err
	}
	fire(t, e, triggerDemPowerAdjustment)
	if err := invoke(demdef.CmdPowerAdjustRequest, demdef.PowerAdjustRequestRequest{Power: 10000000, Duration: 30, Cause: demdef.AdjustmentCauseLocalOptimization}); err != nil {
		t.Errorf("PowerAdjustRequest: %v", err)
	}
	fire(t, e, triggerDemUserOptOutLocalOptimization)
	if d.EsaState() != energy.EsaStateOnline {
		t.Errorf("ESAState %d after the local opt-out", d.EsaState())
	}
	fire(t, e, triggerDemUserOptOutGridOptimization)
	if d.OptOutState() != energy.OptOutAll {
		t.Errorf("OptOutState %d", d.OptOutState())
	}
	fire(t, e, triggerDemUserOptOutClearAll)
	fire(t, e, triggerDemPowerAdjustmentClear)

	fire(t, e, triggerDemPausable)
	if f := d.Forecast(); f == nil || len(f.Slots) != 2 || *f.Slots[1].MinPauseDuration != 2 {
		t.Fatalf("pausable forecast %+v", f)
	}
	if err := invoke(demdef.CmdPauseRequest, demdef.PauseRequestRequest{Duration: 20, Cause: demdef.AdjustmentCauseLocalOptimization}); err != nil {
		t.Errorf("PauseRequest: %v", err)
	}
	if err := invoke(demdef.CmdResumeRequest, nil); err != nil {
		t.Errorf("ResumeRequest: %v", err)
	}
	fire(t, e, triggerDemPausableNextSlot)
	if f := d.Forecast(); f.ActiveSlotNumber.Value != 1 {
		t.Errorf("ActiveSlotNumber %v", f.ActiveSlotNumber)
	}
	fire(t, e, triggerDemPausableClear)

	fire(t, e, triggerDemForecastAdjustment)
	f := d.Forecast()
	power := int64(1000)
	if err := invoke(demdef.CmdModifyForecastRequest, demdef.ModifyForecastRequestRequest{
		ForecastId: f.ForecastId, SlotAdjustments: []energy.SlotAdjustment{{SlotIndex: 0, NominalPower: &power, Duration: 180}},
		Cause: demdef.AdjustmentCauseGridOptimization,
	}); err != nil {
		t.Errorf("ModifyForecastRequest: %v", err)
	}
	fire(t, e, triggerDemForecastAdjustmentNextSlot)
	fire(t, e, triggerDemForecastAdjustmentClear)

	fire(t, e, triggerDemStartTimeAdjustment)
	f = d.Forecast()
	if f.EarliestStartTime == nil || f.EarliestStartTime.Value != f.StartTime-60 || *f.LatestEndTime != f.EndTime+60 {
		t.Errorf("start-time forecast %+v", f)
	}
	if err := invoke(demdef.CmdStartTimeAdjustRequest, demdef.StartTimeAdjustRequestRequest{RequestedStartTime: f.StartTime + 30, Cause: demdef.AdjustmentCauseLocalOptimization}); err != nil {
		t.Errorf("StartTimeAdjustRequest: %v", err)
	}
	if err := invoke(demdef.CmdCancelRequest, nil); err != nil {
		t.Errorf("CancelRequest: %v", err)
	}
	fire(t, e, triggerDemStartTimeAdjustmentClear)

	fire(t, e, triggerDemConstraintBasedAdjustment)
	if f := d.Forecast(); len(f.Slots) != 4 {
		t.Errorf("constraint forecast has %d slots", len(f.Slots))
	}
	fire(t, e, triggerDemConstraintBasedAdjustmentClear)
	fire(t, e, triggerDemForecast)
	fire(t, e, triggerDemForecastClear)
	if f := d.Forecast(); f == nil || len(f.Slots) != 0 || !f.ActiveSlotNumber.Null {
		t.Errorf("cleared forecast %+v", f)
	}
	fire(t, e, triggerDemPowerRangeAdjustment)
	if d.AbsMinPower() != 1000000 {
		t.Errorf("AbsMinPower %d", d.AbsMinPower())
	}
	fire(t, e, triggerDemPowerRangeAdjustmentClear)
	if d.AbsMinPower() != 1200000 {
		t.Errorf("AbsMinPower %d after clear", d.AbsMinPower())
	}
	if e.testEventTrigger(0x0098_0000_0000_00FF) {
		t.Error("an unknown DEM trigger was handled")
	}
	if status, _, err := e.dem.ChangeToMode(context.Background(), demModeAllOptimization); err != nil || status != modebase.StatusSuccess {
		t.Errorf("DEM ChangeToMode %v %v", status, err)
	}
	if status, _, err := e.ChangeToMode(context.Background(), evseModeSolarCharging); err != nil || status != modebase.StatusSuccess {
		t.Errorf("EVSE ChangeToMode %v %v", status, err)
	}
}

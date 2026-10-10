// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	dem "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	evse "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	"github.com/SukramJ/go-fabric/im"
)

// TestDEMRefusals walks the remaining refusals of chip's DEM cluster and
// delegate (DeviceEnergyManagementCluster.cpp, DelegateImpl.cpp), each
// with the status chip answers.
func TestDEMRefusals(t *testing.T) {
	t.Parallel()
	host := &demHost{}
	srv, ev := newDEM(t, demPFRSet, host)
	if srv.MatterDataVersion() == 0 {
		t.Error("data version not started")
	}
	changed := 0
	unsub := srv.OnMatterAttributesChanged(func([]uint32) { changed++ })
	defer unsub()
	demWantStatus(t, "write", srv.MatterWrite(context.Background(), dem.AttrEsaState, uint64(1)), im.StatusUnsupportedWrite)
	now := demEpoch(time.Now())

	// Null forecast.
	demWantStatus(t, "start time, no forecast", demInvoke(srv, dem.CmdStartTimeAdjustRequest, dem.StartTimeAdjustRequestRequest{}), im.StatusFailure)
	demWantStatus(t, "cancel, no forecast", demInvoke(srv, dem.CmdCancelRequest, nil), im.StatusFailure)
	demWantStatus(t, "modify, no forecast", demInvoke(srv, dem.CmdModifyForecastRequest, dem.ModifyForecastRequestRequest{}), im.StatusFailure)
	demWantStatus(t, "constraints, no forecast", demInvoke(srv, dem.CmdRequestConstraintBasedForecast, dem.RequestConstraintBasedForecastRequest{}), im.StatusFailure)

	// A forecast without EarliestStartTime / LatestEndTime.
	f := triggerForecast(now)
	f.EarliestStartTime, f.LatestEndTime = nil, nil
	if err := srv.SetForecast(f); err != nil {
		t.Fatal(err)
	}
	demWantStatus(t, "start time, no window", demInvoke(srv, dem.CmdStartTimeAdjustRequest, dem.StartTimeAdjustRequestRequest{}), im.StatusFailure)
	// A null EarliestStartTime means now (Cluster.cpp:446-456).
	null, latest := spec.NullOf[uint32](), f.EndTime+600
	f.EarliestStartTime, f.LatestEndTime = &null, &latest
	if err := srv.SetForecast(f); err != nil {
		t.Fatal(err)
	}
	demWantStatus(t, "start time, before now", demInvoke(srv, dem.CmdStartTimeAdjustRequest, dem.StartTimeAdjustRequestRequest{RequestedStartTime: now - 100}), im.StatusConstraintError)
	host.fail = errors.New("refused")
	before := srv.Forecast()
	demWantStatus(t, "start time, host refusal", demInvoke(srv, dem.CmdStartTimeAdjustRequest, dem.StartTimeAdjustRequestRequest{RequestedStartTime: now + 10}), im.StatusFailure)
	if after := srv.Forecast(); after.StartTime != before.StartTime || after.ForecastId != before.ForecastId+1 ||
		after.ForecastUpdateReason != dem.ForecastUpdateReasonInternalOptimization {
		t.Errorf("after a refused start-time adjustment: %+v", after)
	}
	demWantStatus(t, "constraints, host refusal", demInvoke(srv, dem.CmdRequestConstraintBasedForecast, dem.RequestConstraintBasedForecastRequest{}), im.StatusFailure)
	demWantStatus(t, "pause, host refusal", demInvoke(srv, dem.CmdPauseRequest, dem.PauseRequestRequest{Duration: 20}), im.StatusFailure)
	if srv.EsaState() != energy.EsaStateOnline {
		t.Errorf("ESAState %d after a refused pause", srv.EsaState())
	}
	host.fail = nil

	// Grid opt-out: Grid refused, Local taken; the forecast adjusted for
	// Grid returns to InternalOptimization.
	if err := demInvoke(srv, dem.CmdRequestConstraintBasedForecast, dem.RequestConstraintBasedForecastRequest{Cause: dem.AdjustmentCauseGridOptimization}); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetOptOutState(energy.OptOutGrid); err != nil {
		t.Fatal(err)
	}
	if srv.Forecast().ForecastUpdateReason != dem.ForecastUpdateReasonInternalOptimization {
		t.Error("the grid opt-out left the forecast's Grid reason")
	}
	demWantStatus(t, "grid opted out", demInvoke(srv, dem.CmdPauseRequest, dem.PauseRequestRequest{Duration: 20, Cause: dem.AdjustmentCauseGridOptimization}), im.StatusConstraintError)
	if err := srv.SetOptOutState(energy.OptOutNone); err != nil {
		t.Fatal(err)
	}

	// A running Local pause ends with UserOptOut on a Local opt-out.
	if err := demInvoke(srv, dem.CmdPauseRequest, dem.PauseRequestRequest{Duration: 20, Cause: dem.AdjustmentCauseLocalOptimization}); err != nil {
		t.Fatal(err)
	}
	ev.take()
	if err := srv.SetOptOutState(energy.OptOutLocal); err != nil {
		t.Fatal(err)
	}
	evs := ev.take()
	if len(evs) != 1 || evs[0].data.(dem.ResumedEvent).Cause != dem.CauseUserOptOut {
		t.Errorf("events %+v, want Resumed(UserOptOut)", evs)
	}
	if err := srv.SetOptOutState(energy.OptOutNone); err != nil {
		t.Fatal(err)
	}

	// Pause preconditions.
	g := srv.Forecast()
	g.ActiveSlotNumber = spec.NullOf[uint16]()
	_ = srv.SetForecast(g)
	demWantStatus(t, "pause, no active slot", demInvoke(srv, dem.CmdPauseRequest, dem.PauseRequestRequest{Duration: 20}), im.StatusFailure)
	g.ActiveSlotNumber = spec.ValueOf(uint16(7))
	_ = srv.SetForecast(g)
	demWantStatus(t, "pause, active slot beyond", demInvoke(srv, dem.CmdPauseRequest, dem.PauseRequestRequest{Duration: 20}), im.StatusFailure)
	g.ActiveSlotNumber = spec.ValueOf(uint16(0))
	g.Slots[0].SlotIsPausable = nil
	_ = srv.SetForecast(g)
	demWantStatus(t, "pause, slot without pause fields", demInvoke(srv, dem.CmdPauseRequest, dem.PauseRequestRequest{Duration: 20}), im.StatusFailure)
	if err := srv.SetEsaState(energy.EsaStateFault); err != nil {
		t.Fatal(err)
	}
	demWantStatus(t, "pause, faulted", demInvoke(srv, dem.CmdPauseRequest, dem.PauseRequestRequest{Duration: 20}), im.StatusConstraintError)
	demWantStatus(t, "resume, not paused", demInvoke(srv, dem.CmdResumeRequest, nil), im.StatusInvalidInState)
	if err := srv.SetEsaState(energy.EsaStatePaused); err != nil {
		t.Fatal(err)
	}
	demWantStatus(t, "resume, no pause running", demInvoke(srv, dem.CmdResumeRequest, nil), im.StatusFailure)
	if err := srv.SetEsaState(energy.EsaStateOnline); err != nil {
		t.Fatal(err)
	}

	// Cancel refused by the host.
	if err := demInvoke(srv, dem.CmdRequestConstraintBasedForecast, dem.RequestConstraintBasedForecastRequest{Cause: dem.AdjustmentCauseLocalOptimization}); err != nil {
		t.Fatal(err)
	}
	cancelHost := &failingCancel{}
	srv3, err := energy.NewDeviceEnergyManagement(energy.DeviceEnergyConfig{Features: demPFRSet, EsaState: energy.EsaStateOnline, Manager: cancelHost})
	if err != nil {
		t.Fatal(err)
	}
	defer srv3.Close()
	_ = srv3.SetForecast(triggerForecast(now))
	_ = srv3.SetPowerAdjustmentCapability(triggerCapability())
	if err := demInvoke(srv3, dem.CmdRequestConstraintBasedForecast, dem.RequestConstraintBasedForecastRequest{Cause: dem.AdjustmentCauseLocalOptimization}); err != nil {
		t.Fatal(err)
	}
	demWantStatus(t, "cancel, host refusal", demInvoke(srv3, dem.CmdCancelRequest, nil), im.StatusFailure)
	if err := demInvoke(srv3, dem.CmdPowerAdjustRequest, dem.PowerAdjustRequestRequest{Power: 10000000, Duration: 20}); err != nil {
		t.Fatal(err)
	}
	demWantStatus(t, "cancel adjustment, host refusal", demInvoke(srv3, dem.CmdCancelPowerAdjustRequest, nil), im.StatusFailure)
	if changed == 0 {
		t.Error("no change reported")
	}
}

// failingCancel refuses the cancel hooks.
type failingCancel struct{ energy.NopDemManager }

func (failingCancel) CancelRequest(context.Context) error     { return errors.New("refused") }
func (failingCancel) CancelPowerAdjust(energy.DemCause) error { return errors.New("refused") }

// TestEvseTransitions covers the state machine's remaining transitions
// (EnergyEvseDelegateImpl.cpp:605-1160) and the enabled-until expiry of
// both directions (:197-335).
func TestEvseTransitions(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		srv, ev, _ := newEvse(t, energy.EvseFeatureChargingPreferences|energy.EvseFeatureV2X)
		if srv.MatterDataVersion() == 0 {
			t.Error("data version not started")
		}
		unsub := srv.OnMatterAttributesChanged(func([]uint32) {})
		defer unsub()
		if srv.IsPluggedIn() || srv.BatteryCapacity() != nil || srv.StateOfCharge() != nil {
			t.Error("initial readings")
		}
		basicFunctionality(t, srv)
		// Plug-in and demand at once: EVConnected, then PluggedInDemand.
		if err := srv.SetHardwareState(energy.EvsePluggedInDemand); err != nil {
			t.Fatal(err)
		}
		wantEvse(t, "demand", srv, energy.EvsePluggedInDemand, energy.SupplyDisabled)
		// Discharging enabled while demanding: Discharging supply, the limit
		// computed, State unchanged (:1072-1128).
		now := evseNow()
		dUntil := now + 10
		if err := demInvoke(srv, evse.CmdEnableDischarging, evse.EnableDischargingRequest{DischargingEnabledUntil: spec.ValueOf(dUntil), MaximumDischargeCurrent: 16000}); err != nil {
			t.Fatal(err)
		}
		wantEvse(t, "discharging", srv, energy.EvsePluggedInDemand, energy.SupplyDischargingEnabled)
		// Demand again with DischargingEnabled: PluggedInDischarging.
		if err := srv.SetHardwareState(energy.EvsePluggedInNoDemand); err != nil {
			t.Fatal(err)
		}
		if err := srv.SetHardwareState(energy.EvsePluggedInDemand); err != nil {
			t.Fatal(err)
		}
		wantEvse(t, "discharging demand", srv, energy.EvsePluggedInDischarging, energy.SupplyDischargingEnabled)
		cUntil := now + 5
		enableCharging(t, srv, &cUntil, 6000, 16000)
		wantEvse(t, "both", srv, energy.EvsePluggedInDischarging, energy.SupplyEnabled)
		// Charging expires first: back to DischargingEnabled; then
		// discharging expires: Disabled.
		time.Sleep(6 * time.Second)
		synctest.Wait()
		if srv.SupplyState() != energy.SupplyDischargingEnabled {
			t.Errorf("after the charging expiry: SupplyState %d", srv.SupplyState())
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		wantEvse(t, "expired", srv, energy.EvsePluggedInDemand, energy.SupplyDisabled)
		evs := ev.take()
		if last := evs[len(evs)-1]; last.id != evse.EventEnergyTransferStopped || last.data.(evse.EnergyTransferStoppedEvent).EnergyDischarged == nil {
			t.Errorf("last event %+v, want EnergyTransferStopped with EnergyDischarged", last)
		}
		// Discharging expiring first: back to ChargingEnabled (:233-252).
		now = evseNow()
		cUntil, dUntil = now+10, now+5
		enableCharging(t, srv, &cUntil, 6000, 16000)
		if err := demInvoke(srv, evse.CmdEnableDischarging, evse.EnableDischargingRequest{DischargingEnabledUntil: spec.ValueOf(dUntil), MaximumDischargeCurrent: 16000}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(6 * time.Second)
		synctest.Wait()
		if srv.SupplyState() != energy.SupplyChargingEnabled {
			t.Errorf("after the discharging expiry: SupplyState %d", srv.SupplyState())
		}
		if err := demInvoke(srv, evse.CmdDisable, nil); err != nil {
			t.Fatal(err)
		}
		// Enabled with demand: charging (:966-977).
		enableCharging(t, srv, nil, 6000, 16000)
		if err := demInvoke(srv, evse.CmdEnableDischarging, evse.EnableDischargingRequest{DischargingEnabledUntil: spec.NullOf[uint32](), MaximumDischargeCurrent: 16000}); err != nil {
			t.Fatal(err)
		}
		if err := srv.SetHardwareState(energy.EvsePluggedInNoDemand); err != nil {
			t.Fatal(err)
		}
		if err := srv.SetHardwareState(energy.EvsePluggedInDemand); err != nil {
			t.Fatal(err)
		}
		wantEvse(t, "enabled demand", srv, energy.EvsePluggedInCharging, energy.SupplyEnabled)
		// Unplugging while charging stops the transfer with Other.
		ev.take()
		if err := srv.SetHardwareState(energy.EvseNotPluggedIn); err != nil {
			t.Fatal(err)
		}
		evs = ev.take()
		if len(evs) != 2 || evs[0].data.(evse.EnergyTransferStoppedEvent).Reason != evse.EnergyTransferStoppedReasonOther || evs[1].id != evse.EventEvNotDetected {
			t.Errorf("unplug events %+v", evs)
		}
		// Repeated inputs change nothing.
		for _, st := range []energy.EvseState{energy.EvseNotPluggedIn, energy.EvsePluggedInNoDemand, energy.EvsePluggedInNoDemand, energy.EvsePluggedInDemand, energy.EvsePluggedInDemand} {
			if err := srv.SetHardwareState(st); err != nil {
				t.Fatal(err)
			}
		}
		// A charging window already passed disables at once (:316-332).
		past := evseNow() - 1
		if err := demInvoke(srv, evse.CmdDisable, nil); err != nil {
			t.Fatal(err)
		}
		enableCharging(t, srv, &past, 6000, 16000)
		if srv.SupplyState() != energy.SupplyDisabled {
			t.Errorf("SupplyState %d after a past ChargingEnabledUntil", srv.SupplyState())
		}
		demWantStatus(t, "malformed discharging", demInvoke(srv, evse.CmdEnableDischarging, 1), im.StatusInvalidCommand)
		demWantStatus(t, "malformed targets", demInvoke(srv, evse.CmdSetTargets, 1), im.StatusInvalidCommand)
		// A write to another writable attribute, and an unchanged user limit.
		if err := srv.MatterWrite(context.Background(), evse.AttrRandomizationDelayWindow, uint64(60)); err != nil {
			t.Error(err)
		}
		if err := srv.MatterWrite(context.Background(), evse.AttrUserMaximumChargeCurrent, int64(32000)); err != nil {
			t.Error(err)
		}
		if err := srv.SetUserMaximumChargeCurrent(32000); err != nil {
			t.Error(err)
		}
	})
}

// TestEvseSoCTargets: with SoCReporting a target needs a TargetSoC of at
// most 100 (EnergyEvseCluster.cpp:540-547).
func TestEvseSoCTargets(t *testing.T) {
	t.Parallel()
	srv, _, _ := newEvse(t, energy.EvseFeatureChargingPreferences|energy.EvseFeatureSoCReporting)
	added, over := int64(1000), uint8(101)
	set := func(tg energy.ChargingTarget) error {
		return demInvoke(srv, evse.CmdSetTargets, evse.SetTargetsRequest{ChargingTargetSchedules: []energy.ChargingTargetSchedule{{DayOfWeekForSequence: 1, ChargingTargets: []energy.ChargingTarget{tg}}}})
	}
	demWantStatus(t, "no SoC", set(target(60, nil, &added)), im.StatusInvalidCommand)
	demWantStatus(t, "SoC above 100", set(target(60, &over, nil)), im.StatusConstraintError)
	fifty := uint8(50)
	if err := set(target(60, &fifty, nil)); err != nil {
		t.Fatal(err)
	}
	if got := srv.Targets(); len(got) != 1 || !slices.Equal(got[0].ChargingTargets, []energy.ChargingTarget{target(60, &fifty, nil)}) {
		t.Errorf("targets %+v", got)
	}
}

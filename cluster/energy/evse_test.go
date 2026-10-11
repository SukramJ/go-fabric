// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/cluster/spec"
	evse "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	"github.com/SukramJ/go-fabric/im"
)

// evseHost is an EvseHost with a settable meter that records the changes
// and reads the server back from EvseChanged, as chip's app does.
type evseHost struct {
	mu      sync.Mutex
	meter   int64
	changes []energy.EvseChange
	srv     *energy.EvseServer
	seen    []int64
}

func (h *evseHost) EvseEnergyMeter(discharging bool) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if discharging {
		return 0
	}
	return h.meter
}

func (h *evseHost) EvseChanged(c energy.EvseChange) {
	// Calls back into the server: safe, the lock is released.
	limit := h.srv.MaximumChargeCurrent()
	h.mu.Lock()
	h.changes = append(h.changes, c)
	h.seen = append(h.seen, limit)
	h.mu.Unlock()
}

func newEvse(t *testing.T, features energy.EvseFeature) (*energy.EvseServer, *energyEvents, *evseHost) {
	t.Helper()
	host := &evseHost{}
	srv, err := energy.NewEnergyEvse(energy.EvseConfig{
		Features: features, UserMaximumChargeCurrent: true, RandomizationDelayWindow: true,
		ApproximateEvEfficiency: true, StartDiagnostics: true, Host: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	host.srv = srv
	ev := &energyEvents{}
	srv.SetMatterEventEmitter(ev)
	srv.SetEndpoint(2)
	t.Cleanup(srv.Close)
	return srv, ev, host
}

// basicFunctionality is the EVSE Basic Functionality trigger
// (examples/evse-app/evse-common/src/EnergyEvseEventTriggers.cpp:53-70).
func basicFunctionality(t *testing.T, srv *energy.EvseServer) {
	t.Helper()
	for _, err := range []error{
		srv.SetMaxHardwareChargeCurrent(32000), srv.SetMaxHardwareDischargeCurrent(32000),
		srv.SetCircuitCapacity(32000), srv.SetUserMaximumChargeCurrent(32000),
		srv.SetHardwareState(energy.EvseNotPluggedIn),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// pluggedIn is the EV Plugged-in trigger (:83-92): a 63 A cable.
func pluggedIn(t *testing.T, srv *energy.EvseServer) {
	t.Helper()
	if err := srv.SetCableAssemblyLimit(63000); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetHardwareState(energy.EvsePluggedInNoDemand); err != nil {
		t.Fatal(err)
	}
}

func evseNow() uint32 { return demEpoch(time.Now()) }

func enableCharging(t *testing.T, srv *energy.EvseServer, until *uint32, lo, hi int64) {
	t.Helper()
	req := evse.EnableChargingRequest{ChargingEnabledUntil: spec.NullOf[uint32](), MinimumChargeCurrent: lo, MaximumChargeCurrent: hi}
	if until != nil {
		req.ChargingEnabledUntil = spec.ValueOf(*until)
	}
	if err := demInvoke(srv, evse.CmdEnableCharging, req); err != nil {
		t.Fatal(err)
	}
}

func wantEvse(t *testing.T, step string, srv *energy.EvseServer, st energy.EvseState, sup energy.SupplyState) {
	t.Helper()
	if srv.State() != st || srv.SupplyState() != sup {
		t.Errorf("step %s: State %d SupplyState %d, want %d %d", step, srv.State(), srv.SupplyState(), st, sup)
	}
}

// TestEvseChargingSession replays TC_EEVSE_2_2.py (connectedhomeip at the
// harness pin, steps 3-17) against the server's state machine.
func TestEvseChargingSession(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		srv, ev, host := newEvse(t, energy.EvseFeatureChargingPreferences)
		basicFunctionality(t, srv)
		wantEvse(t, "3", srv, energy.EvseNotPluggedIn, energy.SupplyDisabled)
		pluggedIn(t, srv)
		evs := ev.take()
		if len(evs) != 1 || evs[0].id != evse.EventEvConnected || evs[0].data.(evse.EvConnectedEvent).SessionId != 0 {
			t.Fatalf("step 4: events %+v, want EVConnected(0)", evs)
		}
		wantEvse(t, "4", srv, energy.EvsePluggedInNoDemand, energy.SupplyDisabled)

		until := evseNow() + 5
		enableCharging(t, srv, &until, 6000, 60000)
		if err := srv.SetHardwareState(energy.EvsePluggedInDemand); err != nil {
			t.Fatal(err)
		}
		evs = ev.take()
		if len(evs) != 1 || evs[0].data.(evse.EnergyTransferStartedEvent).MaximumCurrent != 32000 {
			t.Fatalf("step 6: events %+v, want EnergyTransferStarted(32000)", evs)
		}
		wantEvse(t, "6", srv, energy.EvsePluggedInCharging, energy.SupplyChargingEnabled)
		if srv.MaximumChargeCurrent() != 32000 {
			t.Errorf("step 6: MaximumChargeCurrent %d", srv.MaximumChargeCurrent())
		}

		time.Sleep(7 * time.Second) // ChargingEnabledUntil passes
		synctest.Wait()
		evs = ev.take()
		if len(evs) != 1 || evs[0].data.(evse.EnergyTransferStoppedEvent).Reason != evse.EnergyTransferStoppedReasonEvseStopped {
			t.Fatalf("step 7: events %+v, want EnergyTransferStopped(EVSEStopped)", evs)
		}
		wantEvse(t, "7", srv, energy.EvsePluggedInDemand, energy.SupplyDisabled)

		enableCharging(t, srv, nil, 6000, 12000)
		if evs = ev.take(); len(evs) != 1 || evs[0].data.(evse.EnergyTransferStartedEvent).MaximumCurrent != 12000 {
			t.Fatalf("step 8: events %+v", evs)
		}
		if err := srv.MatterWrite(context.Background(), evse.AttrUserMaximumChargeCurrent, int64(6000)); err != nil {
			t.Fatal(err)
		}
		if srv.MaximumChargeCurrent() != 6000 {
			t.Errorf("step 9a: MaximumChargeCurrent %d, want 6000", srv.MaximumChargeCurrent())
		}
		host.mu.Lock()
		host.meter = 5000
		host.mu.Unlock()
		if err := srv.SetHardwareState(energy.EvsePluggedInNoDemand); err != nil {
			t.Fatal(err)
		}
		evs = ev.take()
		if len(evs) != 1 || evs[0].data.(evse.EnergyTransferStoppedEvent).Reason != evse.EnergyTransferStoppedReasonEvStopped ||
			evs[0].data.(evse.EnergyTransferStoppedEvent).EnergyTransferred != 5000 {
			t.Fatalf("step 10: events %+v, want EnergyTransferStopped(EVStopped, 5000)", evs)
		}
		wantEvse(t, "10", srv, energy.EvsePluggedInNoDemand, energy.SupplyChargingEnabled)

		if err := srv.SetHardwareState(energy.EvseNotPluggedIn); err != nil {
			t.Fatal(err)
		}
		evs = ev.take()
		if len(evs) != 1 || evs[0].id != evse.EventEvNotDetected {
			t.Fatalf("step 14: events %+v, want EVNotDetected", evs)
		}
		if nd := evs[0].data.(evse.EvNotDetectedEvent); nd.State != energy.EvsePluggedInNoDemand || nd.SessionDuration < 7 {
			t.Errorf("step 14: EVNotDetected %+v", nd)
		}
		wantEvse(t, "14", srv, energy.EvseNotPluggedIn, energy.SupplyChargingEnabled)

		pluggedIn(t, srv)
		if err := srv.SetHardwareState(energy.EvsePluggedInDemand); err != nil {
			t.Fatal(err)
		}
		if id := srv.SessionID(); id == nil || *id != 1 {
			t.Errorf("step 15: SessionID %v, want 1", id)
		}
		if got := ev.ids(); !slices.Equal(got, []uint32{evse.EventEvConnected, evse.EventEnergyTransferStarted}) {
			t.Errorf("step 15: events %v", got)
		}
		if err := demInvoke(srv, evse.CmdDisable, evse.DisableRequest{}); err != nil {
			t.Fatal(err)
		}
		evs = ev.take()
		if len(evs) != 1 || evs[0].data.(evse.EnergyTransferStoppedEvent).Reason != evse.EnergyTransferStoppedReasonEvseStopped {
			t.Fatalf("step 16: events %+v", evs)
		}
		wantEvse(t, "16", srv, energy.EvsePluggedInDemand, energy.SupplyDisabled)
		host.mu.Lock()
		defer host.mu.Unlock()
		if !slices.Contains(host.changes, energy.EvseStateChanged) || !slices.Contains(host.changes, energy.EvseChargeCurrentChanged) {
			t.Errorf("host changes %v", host.changes)
		}
	})
}

// TestEvseFaultAndDiagnostics follows HwSetFault, HandleFaultRaised /
// Cleared, StartDiagnostics and HwDiagnosticsComplete
// (EnergyEvseDelegateImpl.cpp:356-373, :692-724, :818-833, :1169-1214).
func TestEvseFaultAndDiagnostics(t *testing.T) {
	t.Parallel()
	srv, ev, _ := newEvse(t, energy.EvseFeatureChargingPreferences)
	basicFunctionality(t, srv)
	pluggedIn(t, srv)
	ev.take()
	if err := srv.SetFault(evse.FaultStateGroundFault); err != nil {
		t.Fatal(err)
	}
	evs := ev.take()
	if len(evs) != 1 {
		t.Fatalf("events %+v", evs)
	}
	if f := evs[0].data.(evse.FaultEvent); f.State != energy.EvsePluggedInNoDemand || f.FaultStatePreviousState != energy.EvseFaultNone ||
		f.FaultStateCurrentState != evse.FaultStateGroundFault || f.SessionId.Null {
		t.Errorf("Fault %+v", f)
	}
	wantEvse(t, "fault", srv, energy.EvseFaulted, energy.SupplyDisabledError)
	demWantStatus(t, "same fault", srv.SetFault(evse.FaultStateGroundFault), im.StatusFailure)
	demWantStatus(t, "charging while faulted", demInvoke(srv, evse.CmdEnableCharging, evse.EnableChargingRequest{ChargingEnabledUntil: spec.NullOf[uint32](), MaximumChargeCurrent: 6000}), im.StatusFailure)
	if err := srv.SetFault(evse.FaultStateOverTemperature); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetFault(energy.EvseFaultNone); err != nil {
		t.Fatal(err)
	}
	wantEvse(t, "cleared", srv, energy.EvsePluggedInNoDemand, energy.SupplyDisabled)

	if err := demInvoke(srv, evse.CmdStartDiagnostics, evse.StartDiagnosticsRequest{}); err != nil {
		t.Fatal(err)
	}
	wantEvse(t, "diagnostics", srv, energy.EvsePluggedInNoDemand, energy.SupplyDisabledDiagnostics)
	demWantStatus(t, "diagnostics twice", demInvoke(srv, evse.CmdStartDiagnostics, nil), im.StatusFailure)
	demWantStatus(t, "disable in diagnostics", demInvoke(srv, evse.CmdDisable, nil), im.StatusFailure)
	if err := srv.DiagnosticsComplete(); err != nil {
		t.Fatal(err)
	}
	demWantStatus(t, "complete twice", srv.DiagnosticsComplete(), im.StatusFailure)
	wantEvse(t, "done", srv, energy.EvsePluggedInNoDemand, energy.SupplyDisabled)
}

// TestEvseCommandChecks: the field checks of HandleEnableCharging /
// EnableDischarging (EnergyEvseCluster.cpp:482-498), the
// UserMaximumChargeCurrent write (:135-143, :356-361) and the Hw* inputs'
// limits.
func TestEvseCommandChecks(t *testing.T) {
	t.Parallel()
	srv, ev, _ := newEvse(t, energy.EvseFeatureChargingPreferences|energy.EvseFeatureV2X|energy.EvseFeatureRfid|energy.EvseFeaturePlugAndCharge|energy.EvseFeatureSoCReporting)
	null := spec.NullOf[uint32]()
	for name, req := range map[string]evse.EnableChargingRequest{
		"min above max": {ChargingEnabledUntil: null, MinimumChargeCurrent: 7000, MaximumChargeCurrent: 6000},
		"negative min":  {ChargingEnabledUntil: null, MinimumChargeCurrent: -1, MaximumChargeCurrent: 6000},
		"negative max":  {ChargingEnabledUntil: null, MaximumChargeCurrent: -1},
	} {
		demWantStatus(t, name, demInvoke(srv, evse.CmdEnableCharging, req), im.StatusConstraintError)
	}
	demWantStatus(t, "negative discharge", demInvoke(srv, evse.CmdEnableDischarging, evse.EnableDischargingRequest{DischargingEnabledUntil: null, MaximumDischargeCurrent: -1}), im.StatusConstraintError)
	demWantStatus(t, "negative user max", srv.MatterWrite(context.Background(), evse.AttrUserMaximumChargeCurrent, int64(-1)), im.StatusConstraintError)
	demWantStatus(t, "malformed", demInvoke(srv, evse.CmdEnableCharging, 3), im.StatusInvalidCommand)
	for name, err := range map[string]error{
		"hw charge": srv.SetMaxHardwareChargeCurrent(-1), "hw discharge": srv.SetMaxHardwareDischargeCurrent(-1),
		"circuit": srv.SetCircuitCapacity(-1), "cable": srv.SetCableAssemblyLimit(-1),
		"voltage": srv.SetNominalMainsVoltage(99999), "user": srv.SetUserMaximumChargeCurrent(-1),
	} {
		demWantStatus(t, name, err, im.StatusConstraintError)
	}
	demWantStatus(t, "hw state", srv.SetHardwareState(energy.EvsePluggedInCharging), im.StatusFailure)

	// V2X: discharging, then charging, is Enabled.
	basicFunctionality(t, srv)
	if err := demInvoke(srv, evse.CmdEnableDischarging, evse.EnableDischargingRequest{DischargingEnabledUntil: null, MaximumDischargeCurrent: 16000}); err != nil {
		t.Fatal(err)
	}
	if srv.SupplyState() != energy.SupplyDischargingEnabled {
		t.Errorf("SupplyState %d after EnableDischarging", srv.SupplyState())
	}
	enableCharging(t, srv, nil, 6000, 16000)
	if srv.SupplyState() != energy.SupplyEnabled {
		t.Errorf("SupplyState %d after both", srv.SupplyState())
	}
	pluggedIn(t, srv)
	if err := srv.SetHardwareState(energy.EvsePluggedInDemand); err != nil {
		t.Fatal(err)
	}
	if srv.MaximumDischargeCurrent() != 16000 || srv.State() != energy.EvsePluggedInCharging {
		t.Errorf("discharge limit %d state %d", srv.MaximumDischargeCurrent(), srv.State())
	}

	// RFID, vehicle id, SoC.
	ev.take()
	if err := srv.ReportRFID([]byte{0, 0x11}); err != nil {
		t.Fatal(err)
	}
	if evs := ev.take(); len(evs) != 1 || evs[0].id != evse.EventRfid {
		t.Errorf("RFID events %+v", evs)
	}
	if err := srv.SetVehicleID("Test-Vehicle-ID-012345789-ABCDEF"); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(evse.AttrVehicleId); v != "Test-Vehicle-ID-012345789-ABCDEF" {
		t.Errorf("VehicleID %v", v)
	}
	demWantStatus(t, "long vehicle id", srv.SetVehicleID("Test-Vehicle-ID-012345789-ABCDEFG"), im.StatusFailure)
	if err := srv.SetVehicleID(""); err != nil {
		t.Fatal(err)
	}
	soc, capacity := uint8(20), int64(70000000)
	if err := srv.SetStateOfCharge(&soc); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetBatteryCapacity(&capacity); err != nil {
		t.Fatal(err)
	}
	if *srv.StateOfCharge() != 20 || *srv.BatteryCapacity() != 70000000 {
		t.Error("SoC readback")
	}
}

func target(minutes uint16, soc *uint8, added *int64) energy.ChargingTarget {
	return energy.ChargingTarget{TargetTimeMinutesPastMidnight: minutes, TargetSoC: soc, AddedEnergy: added}
}

// TestEvseTargets follows ValidateTargets (EnergyEvseCluster.cpp:509-572)
// and the store's merge (EnergyEvseTargetsStore.cpp:245-389), with
// TC_EEVSE_2_3.py's steps 13, 14 and 18.
func TestEvseTargets(t *testing.T) {
	t.Parallel()
	srv, _, host := newEvse(t, energy.EvseFeatureChargingPreferences)
	hundred, fifty, energyMWh, neg := uint8(100), uint8(50), int64(25000000), int64(-1)
	set := func(s ...energy.ChargingTargetSchedule) error {
		return demInvoke(srv, evse.CmdSetTargets, evse.SetTargetsRequest{ChargingTargetSchedules: s})
	}
	for name, c := range map[string]struct {
		s      []energy.ChargingTargetSchedule
		status im.StatusCode
	}{
		"no days":   {[]energy.ChargingTargetSchedule{{ChargingTargets: []energy.ChargingTarget{target(60, nil, &energyMWh)}}}, im.StatusConstraintError},
		"same day":  {[]energy.ChargingTargetSchedule{{DayOfWeekForSequence: 1, ChargingTargets: []energy.ChargingTarget{target(60, &hundred, &energyMWh)}}, {DayOfWeekForSequence: 1, ChargingTargets: []energy.ChargingTarget{target(60, &hundred, &energyMWh)}}}, im.StatusConstraintError},
		"minutes":   {[]energy.ChargingTargetSchedule{{DayOfWeekForSequence: 1, ChargingTargets: []energy.ChargingTarget{target(1440, nil, &energyMWh)}}}, im.StatusConstraintError},
		"soc":       {[]energy.ChargingTargetSchedule{{DayOfWeekForSequence: 1, ChargingTargets: []energy.ChargingTarget{target(60, &fifty, nil)}}}, im.StatusConstraintError},
		"neither":   {[]energy.ChargingTargetSchedule{{DayOfWeekForSequence: 1, ChargingTargets: []energy.ChargingTarget{target(60, nil, nil)}}}, im.StatusFailure},
		"negative":  {[]energy.ChargingTargetSchedule{{DayOfWeekForSequence: 1, ChargingTargets: []energy.ChargingTarget{target(60, nil, &neg)}}}, im.StatusConstraintError},
		"too many":  {[]energy.ChargingTargetSchedule{{DayOfWeekForSequence: 0x40, ChargingTargets: slices.Repeat([]energy.ChargingTarget{target(60, nil, &energyMWh)}, 11)}}, im.StatusResourceExhausted},
		"malformed": {nil, im.StatusSuccess},
	} {
		err := set(c.s...)
		if c.status == im.StatusSuccess {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		demWantStatus(t, name, err, c.status)
	}

	// Step 13: Mon-Fri at 23:40; step 14: Sat and Sun take theirs.
	if err := set(energy.ChargingTargetSchedule{DayOfWeekForSequence: 0x3E, ChargingTargets: []energy.ChargingTarget{target(1420, nil, &energyMWh)}}); err != nil {
		t.Fatal(err)
	}
	if err := set(
		energy.ChargingTargetSchedule{DayOfWeekForSequence: 0x40, ChargingTargets: []energy.ChargingTarget{target(540, nil, &energyMWh)}},
		energy.ChargingTargetSchedule{DayOfWeekForSequence: 0x01, ChargingTargets: []energy.ChargingTarget{target(540, nil, &energyMWh)}},
	); err != nil {
		t.Fatal(err)
	}
	got, err := srv.MatterInvoke(context.Background(), evse.CmdGetTargets, evse.GetTargetsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var days []energy.TargetDays
	for _, s := range got.(evse.GetTargetsResponse).ChargingTargetSchedules {
		days = append(days, s.DayOfWeekForSequence)
	}
	if !slices.Equal(days, []energy.TargetDays{0x3E, 0x40, 0x01}) {
		t.Errorf("schedules %v", days)
	}
	// Monday alone replaces Mon-Fri's targets? No: Mon-Fri is not inside
	// Monday, so it loses Monday and Monday is appended.
	if err := set(energy.ChargingTargetSchedule{DayOfWeekForSequence: 0x02, ChargingTargets: []energy.ChargingTarget{target(60, nil, &energyMWh)}}); err != nil {
		t.Fatal(err)
	}
	days = days[:0]
	for _, s := range srv.Targets() {
		days = append(days, s.DayOfWeekForSequence)
	}
	if !slices.Equal(days, []energy.TargetDays{0x3C, 0x40, 0x01, 0x02}) {
		t.Errorf("after Monday: %v", days)
	}
	if err := demInvoke(srv, evse.CmdClearTargets, evse.ClearTargetsRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(srv.Targets()) != 0 {
		t.Error("ClearTargets left targets")
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if !slices.Contains(host.changes, energy.EvseChargingPreferencesChanged) {
		t.Errorf("host changes %v", host.changes)
	}
}

// TestEvseTargetsSevenDays: a schedule per day fills the store; a
// schedule over two days replaces both (EnergyEvseTargetsStore.cpp:
// 298-314).
func TestEvseTargetsSevenDays(t *testing.T) {
	t.Parallel()
	srv, _, _ := newEvse(t, energy.EvseFeatureChargingPreferences)
	added := int64(1000)
	for day := range 7 {
		if err := demInvoke(srv, evse.CmdSetTargets, evse.SetTargetsRequest{ChargingTargetSchedules: []energy.ChargingTargetSchedule{
			{DayOfWeekForSequence: energy.TargetDays(1 << day), ChargingTargets: []energy.ChargingTarget{target(60, nil, &added)}},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	// Sunday and Monday each lie inside the new days: both take its targets.
	if err := demInvoke(srv, evse.CmdSetTargets, evse.SetTargetsRequest{ChargingTargetSchedules: []energy.ChargingTargetSchedule{
		{DayOfWeekForSequence: 0x03, ChargingTargets: []energy.ChargingTarget{target(120, nil, &added)}},
	}}); err != nil {
		t.Fatal(err)
	}
	if n := len(srv.Targets()); n != 7 {
		t.Errorf("%d schedules, want 7", n)
	}
	// The stored schedules stay pairwise disjoint and non-empty, so the
	// merge never reaches an eighth: chip's RESOURCE_EXHAUSTED for it
	// (:345) cannot be provoked through SetTargets.
}

// TestEvseScheduleAndConfig covers the device-side schedule setter and the
// construction defaults (EnergyEvseCluster.h:198-224).
func TestEvseScheduleAndConfig(t *testing.T) {
	t.Parallel()
	srv, _, _ := newEvse(t, energy.EvseFeatureChargingPreferences)
	if v, _ := srv.MatterRead(evse.AttrMinimumChargeCurrent); v != int64(6000) {
		t.Errorf("MinimumChargeCurrent %v", v)
	}
	if v, _ := srv.MatterRead(evse.AttrRandomizationDelayWindow); v != uint32(600) {
		t.Errorf("RandomizationDelayWindow %v", v)
	}
	start, target, soc := uint32(100), uint32(200), uint8(100)
	if err := srv.SetChargeSchedule(energy.ChargeSchedule{StartTime: &start, TargetTime: &target, TargetSoC: &soc}); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(evse.AttrNextChargeTargetTime); v != uint32(200) {
		t.Errorf("NextChargeTargetTime %v", v)
	}
	if v, _ := srv.MatterRead(evse.AttrNextChargeRequiredEnergy); v != nil {
		t.Errorf("NextChargeRequiredEnergy %v", v)
	}
	if err := srv.SetNominalMainsVoltage(230000); err != nil || srv.NominalMainsVoltage() != 230000 {
		t.Errorf("voltage %v", err)
	}
	basicFunctionality(t, srv)
	pluggedIn(t, srv)
	if srv.HardwareState() != energy.EvsePluggedInNoDemand || srv.CableAssemblyLimit() != 63000 || srv.MaxHardwareChargeCurrent() != 32000 ||
		srv.MaxHardwareDischargeCurrent() != 32000 || srv.UserMaximumChargeCurrent() != 32000 || srv.CircuitCapacity() != 32000 || !srv.IsPluggedIn() {
		t.Error("readback")
	}
	if srv.FaultState() != energy.EvseFaultNone {
		t.Error("FaultState")
	}
}

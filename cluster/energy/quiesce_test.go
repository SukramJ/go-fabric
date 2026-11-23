// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster/energy"
	"thingfabric.dev/go-fabric/cluster/spec"
	ct "thingfabric.dev/go-fabric/cluster/spec/commoditytariff"
	dem "thingfabric.dev/go-fabric/cluster/spec/deviceenergymanagement"
)

// TestDEMQuiesceStopsItsTimers holds contract.ClusterQuiescer for
// DeviceEnergyManagement: after MatterQuiesce neither the power-adjustment
// nor the pause timer is armed, and advancing the clock past both ends
// changes no attribute, emits no event and tells the host nothing. The
// SetEndpoint of a reassembly that puts the server back arms both again
// for the time left to their deadlines, as EvseServer re-arms its own.
// Before, MatterQuiesce left both armed and only the uncalled Close
// stopped them (review II, R2-P6-conc-1605, N-clumisc-4).
func TestDEMQuiesceStopsItsTimers(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1700000000, 0))
	host := &demHost{}
	srv, err := energy.NewDeviceEnergyManagement(energy.DeviceEnergyConfig{
		Features: demPFRSet, EsaType: dem.ESATypeEvse, EsaState: energy.EsaStateOnline,
		AbsMinPower: 1200000, AbsMaxPower: 7600000, Manager: host, Clock: clk,
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := &energyEvents{}
	srv.SetMatterEventEmitter(ev)
	srv.SetEndpoint(3)
	t.Cleanup(srv.Close)
	if err := srv.SetPowerAdjustmentCapability(triggerCapability()); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetForecast(triggerForecast(demEpoch(clk.Now()))); err != nil {
		t.Fatal(err)
	}
	if err := demInvoke(srv, dem.CmdPowerAdjustRequest, dem.PowerAdjustRequestRequest{Power: 10000000, Duration: 20, Cause: dem.AdjustmentCauseLocalOptimization}); err != nil {
		t.Fatal(err)
	}
	if err := demInvoke(srv, dem.CmdPauseRequest, dem.PauseRequestRequest{Duration: 20, Cause: dem.AdjustmentCauseLocalOptimization}); err != nil {
		t.Fatal(err)
	}
	if n := clk.Pending(); n != 2 {
		t.Fatalf("%d timers armed, want the adjustment's and the pause's", n)
	}
	clk.Advance(5 * time.Second)
	ev.take()

	srv.MatterQuiesce()
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d timers armed after MatterQuiesce", n)
	}
	clk.Advance(5 * time.Second)
	if ids := ev.ids(); len(ids) != 0 || srv.EsaState() != energy.EsaStatePaused {
		t.Fatalf("while quiesced: events %v, ESAState %d", ids, srv.EsaState())
	}

	// Put back: both deadlines stand, 10 s are left.
	srv.SetEndpoint(3)
	clk.Advance(9 * time.Second)
	if ids := ev.ids(); len(ids) != 0 {
		t.Fatalf("expired before the time left: events %v", ids)
	}
	clk.Advance(time.Second)
	if ids := ev.ids(); len(ids) != 2 || srv.EsaState() != energy.EsaStateOnline {
		t.Fatalf("after the remount: events %v, ESAState %d", ids, srv.EsaState())
	}
}

// TestTariffRemountResumesTheTimeSync: a CommodityTariff quiesced and put
// back (SetEndpoint) derives its clock-driven attributes again at once and
// on the clock afterwards: a delayed tariff whose StartDate passed while
// the server was out activates at the remount. Before, the sync stayed
// stopped until the host's next UpdateTariff (review II, R2-P6-fsm-1607,
// R2-P1-dr-rest-03 rest).
func TestTariffRemountResumesTheTimeSync(t *testing.T) {
	t.Parallel()
	const interval = 30 * time.Second
	clk := clocktest.New(time.Unix(1748736000, 0))
	srv, err := energy.NewCommodityTariff(energy.CommodityTariffConfig{Features: allTariffFeatures, Clock: clk, SyncInterval: interval})
	if err != nil {
		t.Fatal(err)
	}
	delayed := sample2()
	start := uint32(1748736000-946684800) + 45
	delayed.StartDate = &start
	delayed.CalendarPeriods = []energy.TariffCalendarPeriod{{StartDate: spec.ValueOf(start), DayPatternIds: []uint32{0}}}
	if err := srv.UpdateTariff(delayed); err != nil {
		t.Fatal(err)
	}
	srv.MatterQuiesce()
	clk.Advance(2 * interval)
	if v := read(t, srv, ct.AttrStartDate); v != nil {
		t.Fatalf("StartDate = %v while quiesced, want the update pending", v)
	}
	srv.SetEndpoint(1)
	if v := read(t, srv, ct.AttrStartDate); v != start {
		t.Errorf("StartDate = %v at the remount, want %d", v, start)
	}
	if n := clk.Pending(); n != 1 {
		t.Errorf("armed timers = %d after the remount, want the sync", n)
	}
	srv.SetEndpoint(1) // a second remount arms no second sync
	if n := clk.Pending(); n != 1 {
		t.Errorf("armed timers = %d after a second SetEndpoint", n)
	}
}

// TestDEMCommandAfterQuiesceArmsNoTimer (R2-P6-conc-1606 follow-on): a
// command in flight across MatterQuiesce — here one that arms after it —
// keeps its deadline but arms no timer; the SetEndpoint of a reassembly
// arms it for the time left. Before, the PowerAdjustRequest and
// PauseRequest armed their timers on the quiesced server.
func TestDEMCommandAfterQuiesceArmsNoTimer(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1700000000, 0))
	srv, err := energy.NewDeviceEnergyManagement(energy.DeviceEnergyConfig{
		Features: demPFRSet, EsaType: dem.ESATypeEvse, EsaState: energy.EsaStateOnline,
		AbsMinPower: 1200000, AbsMaxPower: 7600000, Manager: &demHost{}, Clock: clk,
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := &energyEvents{}
	srv.SetMatterEventEmitter(ev)
	srv.SetEndpoint(3)
	t.Cleanup(srv.Close)
	if err := srv.SetPowerAdjustmentCapability(triggerCapability()); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetForecast(triggerForecast(demEpoch(clk.Now()))); err != nil {
		t.Fatal(err)
	}
	srv.MatterQuiesce()
	if err := demInvoke(srv, dem.CmdPowerAdjustRequest, dem.PowerAdjustRequestRequest{Power: 10000000, Duration: 20, Cause: dem.AdjustmentCauseLocalOptimization}); err != nil {
		t.Fatal(err)
	}
	if err := demInvoke(srv, dem.CmdPauseRequest, dem.PauseRequestRequest{Duration: 20, Cause: dem.AdjustmentCauseLocalOptimization}); err != nil {
		t.Fatal(err)
	}
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d timers armed on the quiesced server", n)
	}
	clk.Advance(5 * time.Second)
	ev.take()
	srv.SetEndpoint(3)
	if n := clk.Pending(); n != 2 {
		t.Fatalf("%d timers armed after the remount, want 2", n)
	}
	clk.Advance(15 * time.Second)
	if ids := ev.ids(); len(ids) != 2 || srv.EsaState() != energy.EsaStateOnline {
		t.Fatalf("after the time left: events %v, ESAState %d", ids, srv.EsaState())
	}
}

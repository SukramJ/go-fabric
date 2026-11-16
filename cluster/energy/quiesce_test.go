// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/energy"
	dem "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
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

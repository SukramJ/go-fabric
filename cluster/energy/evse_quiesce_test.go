// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/energy"
	"github.com/SukramJ/go-fabric/contract"
)

// TestEvseMatterQuiesceStopsTheEnabledTimer: the bridge quiesces a server
// that leaves the topology through contract.ClusterQuiescer
// (bridge.quiesceServers). Afterwards the enabled-until timer is not
// armed — neither the deadline timer nor the 30-s re-check a node without
// a time re-arms for ever — and advancing the clock past the deadline
// leaves the supply enabled, tells the host nothing and emits no event
// (review II, R2-P1-clu-02).
func TestEvseMatterQuiesceStopsTheEnabledTimer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		synced bool
	}{
		{"deadline", true},
		{"no time, the 30-s re-check", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clk := clocktest.New(time.Unix(1700000000, 0))
			host := &evseHost{}
			srv, err := energy.NewEnergyEvse(energy.EvseConfig{
				Features: energy.EvseFeatureChargingPreferences, UserMaximumChargeCurrent: true,
				RandomizationDelayWindow: true, ApproximateEvEfficiency: true, StartDiagnostics: true,
				Host: host, Clock: clk,
			})
			if err != nil {
				t.Fatal(err)
			}
			host.srv = srv
			ev := &energyEvents{}
			srv.SetMatterEventEmitter(ev)
			srv.SetEndpoint(2)
			t.Cleanup(srv.Close)
			basicFunctionality(t, srv)
			pluggedIn(t, srv)
			until := demEpoch(clk.Now()) + 60
			if !tc.synced {
				srv.AttachMatterTime(func() (uint32, bool) { return 0, false })
			}
			enableCharging(t, srv, &until, 6000, 60000)
			wantEvse(t, "enabled", srv, energy.EvsePluggedInNoDemand, energy.SupplyChargingEnabled)
			if n := clk.Pending(); n != 1 {
				t.Fatalf("%d timers armed, want the enabled-until timer", n)
			}
			ev.take()
			host.mu.Lock()
			host.changes = nil
			host.mu.Unlock()

			q, ok := any(srv).(contract.ClusterQuiescer)
			if !ok {
				t.Fatal("EvseServer is not a contract.ClusterQuiescer: the bridge cannot stop its enabled-until timer")
			}
			q.MatterQuiesce()
			if n := clk.Pending(); n != 0 {
				t.Fatalf("%d timers armed after MatterQuiesce, want 0", n)
			}
			if !tc.synced {
				// The time arrives later: nothing may act on the old deadline.
				srv.AttachMatterTime(func() (uint32, bool) { return demEpoch(clk.Now()), true })
			}
			clk.Advance(5 * time.Minute)
			wantEvse(t, "after MatterQuiesce", srv, energy.EvsePluggedInNoDemand, energy.SupplyChargingEnabled)
			if evs := ev.take(); len(evs) != 0 {
				t.Errorf("events %+v after MatterQuiesce", evs)
			}
			host.mu.Lock()
			defer host.mu.Unlock()
			if len(host.changes) != 0 {
				t.Errorf("host told %+v after MatterQuiesce", host.changes)
			}
		})
	}
}

// TestEvseReAddedAfterMatterQuiesceEnforcesTheDeadline (review II,
// R2-P1-dr-rest-03): a reassembly drops the server (MatterQuiesce) and a
// later one puts the same object back, stamping its endpoint
// (bridge.Reassemble → SetEndpoint) as node.Aggregator's Remove and Add of
// the same Endpoint do. The enabled-until deadline the controller set
// before is enforced again: a deadline still ahead re-arms the timer, one
// that passed while the server was out disables at once.
func TestEvseReAddedAfterMatterQuiesceEnforcesTheDeadline(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		out  time.Duration // how long the server is out of the topology
	}{
		{"deadline ahead", 10 * time.Second},
		{"deadline passed while out", 2 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clk := clocktest.New(time.Unix(1700000000, 0))
			host := &evseHost{}
			srv, err := energy.NewEnergyEvse(energy.EvseConfig{
				Features: energy.EvseFeatureChargingPreferences, UserMaximumChargeCurrent: true,
				RandomizationDelayWindow: true, ApproximateEvEfficiency: true, StartDiagnostics: true,
				Host: host, Clock: clk,
			})
			if err != nil {
				t.Fatal(err)
			}
			host.srv = srv
			ev := &energyEvents{}
			srv.SetMatterEventEmitter(ev)
			srv.SetEndpoint(2)
			t.Cleanup(srv.Close)
			basicFunctionality(t, srv)
			pluggedIn(t, srv)
			until := demEpoch(clk.Now()) + 60
			enableCharging(t, srv, &until, 6000, 60000)

			srv.MatterQuiesce()
			clk.Advance(tc.out)
			if got := srv.SupplyState(); got != energy.SupplyChargingEnabled {
				t.Fatalf("SupplyState %v while out of the topology; want it untouched", got)
			}
			srv.SetMatterEventEmitter(ev) // the later reassembly
			srv.SetEndpoint(2)
			clk.Advance(5 * time.Minute)
			wantEvse(t, "past the deadline after the re-add", srv, energy.EvsePluggedInNoDemand, energy.SupplyDisabled)
		})
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package opstate_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/contract"
)

// TestMatterQuiesceStopsTheCountdownThrottle (R2-P6-conc-1506): the
// bridge's MatterQuiesce disarms the CountdownTime throttle's deferred
// report, the server's one timer, so nothing is reported after the server
// left the topology.
func TestMatterQuiesceStopsTheCountdownThrottle(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(5000, 0))
	srv, err := opstate.NewServer(opstate.Config{States: []opstate.StateEntry{{ID: opstate.StateStopped}, {ID: opstate.StateError}}, CountdownTime: true, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	q, ok := any(srv).(contract.ClusterQuiescer)
	if !ok {
		t.Fatal("OperationalState has no MatterQuiesce: its CountdownTime throttle outlives the bridge's use of it")
	}
	reports := 0
	srv.OnAttributeReport(func(contract.Change) { reports++ })
	for _, v := range []uint32{600, 599} { // from null at once, the second held back
		if err := srv.SetCountdownTime(&v); err != nil {
			t.Fatal(err)
		}
	}
	if n := clk.Pending(); n != 1 {
		t.Fatalf("%d timers armed, want the throttle's", n)
	}
	q.MatterQuiesce()
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d timers armed after MatterQuiesce, want 0", n)
	}
	clk.Advance(time.Minute)
	if reports != 1 {
		t.Errorf("CountdownTime reported %d times, want once (the change from null)", reports)
	}
}

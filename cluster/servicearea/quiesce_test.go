// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package servicearea_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
	"github.com/SukramJ/go-fabric/contract"
)

// TestMatterQuiesceStopsTheEndTimeThrottle: the bridge's MatterQuiesce
// disarms the EstimatedEndTime throttle's deferred report, the server's
// one timer, so nothing is reported after the server left the topology;
// the pattern of matter.js SwitchServer.ts:379-384 at 862ad75f
// (W3-club2-02).
func TestMatterQuiesceStopsTheEndTimeThrottle(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_800_000_000, 0))
	cfg := vacuum(&device{})
	cfg.Clock = clk
	srv := newServer(t, cfg)
	var q contract.ClusterQuiescer = srv
	seen := notifications(srv)
	for _, v := range []uint32{100, 200} { // from null at once, the increase held back
		if err := srv.SetEstimatedEndTime(&v); err != nil {
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
	count := 0
	for _, id := range *seen {
		if id == sa.AttrEstimatedEndTime {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("EstimatedEndTime reported %d times, want once (the change from null)", count)
	}
}

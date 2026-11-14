// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// TestProjectedQuieterAttributeIsThrottled: a measurement-projected
// attribute of quality Q — ElectricalPowerMeasurement.ActivePower
// (packages/model/src/standard/elements/electrical-power-measurement.element.ts:70-71
// at db021da0) — is reported as matter.js reports it: its changes leave
// the bulk change handler, and the attribute is reported, with its
// cluster's DataVersion advanced, only when its QuietEvent emits
// (packages/node/src/behavior/internal/ServerBehaviorBacking.ts:224-263):
// at once when the last report is older than a second, else once with the
// latest value when the second is up (packages/general/src/util/Observable.ts:1238-1253,
// the default interval :1383-1387). A change from or to null is reported
// at once (packages/node/src/behavior/Events.ts:281-290). Review II,
// R2-P4-nv-05b.
func TestProjectedQuieterAttributeIsThrottled(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Unix(1_700_000_000, 0))
	src := &fakeMeter{}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("meter"), DeviceAddress: "M", DeviceType: 0x0510,
			FriendlyName: "Meter", ChannelAddress: "M:1", Measurement: src,
		}}}})
	}
	b, err := New(snap, nil, Config{Listen: "127.0.0.1:0", Clock: clk}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Stop(context.WithoutCancel(ctx)) })
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{Clock: clk}, nil, nil))
	ep := b.Topology().Bridged()[0]
	version := func() uint32 { return ep.ClusterDataVersion(measurement.ClusterElectricalPower) }

	v0 := version()
	src.setPower(10) // null → value: at once
	if got := version(); got != v0+1 {
		t.Fatalf("a change from null: DataVersion %d, want %d (reported at once)", got, v0+1)
	}
	clk.Advance(100 * time.Millisecond)
	src.setPower(20) // within the second: held
	src.setPower(30) // still held, reported once
	if got := version(); got != v0+1 {
		t.Fatalf("changes within a second of the last report: DataVersion %d, want %d (held)", got, v0+1)
	}
	clk.Advance(900 * time.Millisecond)
	if got := version(); got != v0+2 {
		t.Fatalf("after the second: DataVersion %d, want %d (the held changes reported once)", got, v0+2)
	}
	clk.Advance(cluster.QuietMinimumInterval + time.Millisecond)
	src.setPower(40) // more than a second later: at once
	if got := version(); got != v0+3 {
		t.Fatalf("a change after a quiet second: DataVersion %d, want %d", got, v0+3)
	}
}

// TestPowerSourceBatteryQuietInterval: PowerSource's BatPercentRemaining,
// BatTimeRemaining and BatTimeToFullCharge are throttled to ten seconds,
// every other quieter attribute to [cluster.QuietMinimumInterval]
// (packages/node/src/behaviors/power-source/PowerSourceServer.ts:25-34 at
// db021da0). Review II, R2-P4-nv-05b.
func TestPowerSourceBatteryQuietInterval(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		cluster, attr uint32
		want          time.Duration
	}{
		{0x002F, 0x000C, 10 * time.Second}, // BatPercentRemaining
		{0x002F, 0x000D, 10 * time.Second}, // BatTimeRemaining
		{0x002F, 0x001B, 10 * time.Second}, // BatTimeToFullCharge
		{0x0090, 0x0008, cluster.QuietMinimumInterval},
		{0x0091, 0x0001, cluster.QuietMinimumInterval},
	} {
		if got := quietIntervalOf(c.cluster, c.attr); got != c.want {
			t.Errorf("quiet interval of %#04x/%#04x = %v, want %v", c.cluster, c.attr, got, c.want)
		}
	}
}

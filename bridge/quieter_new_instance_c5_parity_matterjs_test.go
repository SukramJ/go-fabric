// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster/measurement"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/endpoint/endpointtest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
)

// newQuietMeterRigA7 is [newQuietMeterRig] whose meter endpoint names the
// instance inst holds ([endpoint.Spec.Instance]) at each assembly.
func newQuietMeterRigA7(t *testing.T, attr uint32, inst *atomic.Int64) *quietMeterRig {
	t.Helper()
	r := &quietMeterRig{
		clk:      clocktest.New(time.Unix(1_700_000_000, 0)),
		src:      &fakeMeter{},
		reported: make(chan []im.ConcreteAttributePath, 16),
	}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r.snap = func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("meter"), DeviceAddress: "M", DeviceType: 0x0510,
			FriendlyName: "Meter", ChannelAddress: "M:1", Measurement: r.src, Instance: inst.Load(),
		}}}})
	}
	r.b, err = New(r.snap, nil, Config{Listen: "127.0.0.1:0", Clock: r.clk}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := r.b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.b.Stop(context.WithoutCancel(t.Context())) })
	r.mgr = subscription.NewManager(subscription.Config{Clock: r.clk}, func(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
		r.reported <- paths
	}, nil)
	r.b.AttachSubscriptionManager(r.mgr)
	r.path = im.ConcreteAttributePath{
		Endpoint: r.ep().ID, Cluster: measurement.ClusterElectricalPower, Attribute: attr,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}
	sub, err := r.mgr.Subscribe(subscription.SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 0x10, SessionID: 1, MaxIntervalCeiling: 3600,
		AttributePaths: []im.ConcreteAttributePath{r.path},
	})
	if err != nil {
		t.Fatal(err)
	}
	sub.EndPriming(r.clk.Now())
	return r
}

// TestNewInstanceStartsAFreshQuietThrottle (R5-par-5109): a quieter path
// whose endpoint is served by a new instance after a reassembly (a
// same-key Replace) does not take up the throttle the old instance's
// wiring parked: a change within the old throttle's interval is reported
// at once, and the change the old one held back is dropped with it. A
// re-wiring of the same instance keeps its throttle
// (TestHeldQuieterChangeSurvivesReassemble).
//
// Mirrors matter.js: the quieter throttle belongs to the Endpoint object.
// QuietEvent owns its QuietObservable (packages/node/src/behavior/Events.ts:274-306),
// whose last-emit time and deferred payload are its fields
// (packages/general/src/util/Observable.ts:1061-1063); a new Endpoint builds
// new Behaviors, backings and event emitters
// (packages/node/src/endpoint/Endpoint.ts:631,
// packages/node/src/endpoint/properties/Behaviors.ts:56-57, :781-803), so its
// first quieter change is emitted at once (Observable.ts:1238-1246) and a
// change the old instance held back is dropped with it (Observable.ts:1276-1280,
// Behaviors.ts:471-474) — at b25ed9604f.
func TestNewInstanceStartsAFreshQuietThrottle(t *testing.T) {
	t.Parallel()
	var inst atomic.Int64
	inst.Store(1)
	r := newQuietMeterRigA7(t, epmActivePower, &inst)
	v0 := r.version()
	r.src.setPower(10)
	r.step(t, "a change from null", v0, 1, true)
	r.clk.Advance(100 * time.Millisecond)
	r.src.setPower(20) // held by the old instance's throttle
	r.step(t, "a change within the second", v0, 2, false)

	inst.Store(2) // a same-key Replace: a new instance behind the key
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := r.b.Reassemble(ctx); err != nil {
		t.Fatal(err)
	}
	// The new instance is announced, at its own DataVersion.
	if !r.reportsPath(t) {
		t.Fatal("the new instance's path was not announced")
	}
	v1 := r.version()
	r.clk.Advance(100 * time.Millisecond)
	r.src.setPower(30) // within the old throttle's second
	r.step(t, "the new instance's first quieter change", v1, 1, true)
	r.clk.Advance(time.Second)
	r.step(t, "after the old throttle's second", v1, 1, false)
}

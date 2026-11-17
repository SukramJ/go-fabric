// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// gatedMeter is a fakeMeter that counts its live registrations and runs
// onRegister, once, inside the next registration.
type gatedMeter struct {
	*fakeMeter
	gmu        sync.Mutex
	live       int
	onRegister func()
}

func (g *gatedMeter) OnMatterValueChanged(cb func()) func() {
	g.gmu.Lock()
	hook := g.onRegister
	g.onRegister = nil
	g.live++
	g.gmu.Unlock()
	if hook != nil {
		hook()
	}
	g.fakeMeter.OnMatterValueChanged(cb)
	var once sync.Once
	return func() {
		once.Do(func() {
			g.gmu.Lock()
			g.live--
			g.gmu.Unlock()
		})
	}
}

func (g *gatedMeter) registrations() int {
	g.gmu.Lock()
	defer g.gmu.Unlock()
	return g.live
}

// TestRewiringsRunOneAfterTheOther (review II, N4-rwsub-2): a re-wiring
// that starts while another registers waits for it, so the change a
// quieter path's throttle holds back survives both — the later wiring
// takes up the path the earlier one parks — and the host's notifier holds
// the listeners of one wiring, not of both. Before, the later wiring made
// a fresh throttle, the earlier one parked the held change where nothing
// took it up, and both wirings' listeners were registered meanwhile.
func TestRewiringsRunOneAfterTheOther(t *testing.T) {
	t.Parallel()
	g := &gatedMeter{fakeMeter: &fakeMeter{}}
	r := &quietMeterRig{
		clk:      clocktest.New(time.Unix(1_700_000_000, 0)),
		src:      g.fakeMeter,
		reported: make(chan []im.ConcreteAttributePath, 16),
	}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r.snap = func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("meter"), DeviceAddress: "M", DeviceType: 0x0510,
			FriendlyName: "Meter", ChannelAddress: "M:1", Measurement: g,
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
		Endpoint: r.ep().ID, Cluster: measurement.ClusterElectricalPower, Attribute: epmActivePower,
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
	oneWiring := g.registrations()

	v0 := r.version()
	r.src.setPower(10)
	r.step(t, "a change from null", v0, 1, true)
	r.clk.Advance(100 * time.Millisecond)
	r.src.setPower(20) // held by the throttle
	r.step(t, "a change within the second", v0, 2, false)

	// The second re-wiring starts inside the first one's registration;
	// the first goes on once the second finished or waits for it.
	second := make(chan struct{})
	queued := make(chan struct{}, 1)
	r.b.rewireQueuedForTest = func() { queued <- struct{}{} }
	g.gmu.Lock()
	g.onRegister = func() {
		go func() {
			r.b.rewireMeasurementListeners()
			close(second)
		}()
		select {
		case <-second:
		case <-queued:
		}
	}
	g.gmu.Unlock()
	r.b.rewireMeasurementListeners()
	<-second

	if got := g.registrations(); got != oneWiring {
		t.Errorf("the meter holds %d registrations after two re-wirings, want one wiring's %d", got, oneWiring)
	}
	r.clk.Advance(900 * time.Millisecond)
	r.step(t, "after the second, the held change", v0, 2, true)
}

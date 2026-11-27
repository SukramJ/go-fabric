// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/measurement"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/endpoint/endpointtest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
)

// quietMeterRig is a started bridge with one metering endpoint, on a fake
// clock, and a subscription to one of its quieter paths whose reports the
// test takes by ticking the manager itself.
type quietMeterRig struct {
	b        *Bridge
	clk      *clocktest.Fake
	src      *fakeMeter
	snap     func(context.Context) (*endpoint.Topology, error)
	mgr      *subscription.Manager
	path     im.ConcreteAttributePath
	reported chan []im.ConcreteAttributePath
}

func newQuietMeterRig(t *testing.T, attr uint32) *quietMeterRig {
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
			FriendlyName: "Meter", ChannelAddress: "M:1", Measurement: r.src,
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

func (r *quietMeterRig) ep() *endpoint.Endpoint { return r.b.Topology().Bridged()[0] }

func (r *quietMeterRig) version() uint32 {
	return r.ep().ClusterDataVersion(measurement.ClusterElectricalPower)
}

// reportsPath ticks the manager at the clock's now and reports whether a
// report of the rig's path went out.
func (r *quietMeterRig) reportsPath(t *testing.T) bool {
	t.Helper()
	r.mgr.Tick(t.Context(), r.clk.Now())
	for {
		select {
		case paths := <-r.reported:
			if slices.Contains(paths, r.path) {
				return true
			}
		default:
			return false
		}
	}
}

// step checks the DataVersion's advance since v0 and whether the path was
// reported.
func (r *quietMeterRig) step(t *testing.T, when string, v0, advance uint32, report bool) {
	t.Helper()
	if got := r.version(); got != v0+advance {
		t.Fatalf("%s: DataVersion %d, want %d", when, got, v0+advance)
	}
	if got := r.reportsPath(t); got != report {
		t.Fatalf("%s: path reported %v, want %v", when, got, report)
	}
}

// TestProjectedQuieterAttributeIsThrottled: a measurement-projected
// attribute of quality Q — ElectricalPowerMeasurement.ActivePower
// (packages/model/src/standard/elements/electrical-power-measurement.element.ts:70-71
// at db021da0) — is reported as matter.js reports it: each change advances
// the cluster's DataVersion at once, as the commit that changed the value
// does (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457,
// #noteIfChanged :1476-1493, no exception for a quieter property), and
// the attribute is reported only when its QuietEvent emits
// (packages/node/src/behavior/internal/ServerBehaviorBacking.ts:224-263):
// at once when the last report is older than a second, else once with the
// latest value when the second is up (packages/general/src/util/Observable.ts:1238-1253,
// the default interval :1383-1387). A change from or to null is reported
// at once (packages/node/src/behavior/Events.ts:281-290). Review II,
// R2-P4-nv-05b, R2-P4-dr-sub-03.
func TestProjectedQuieterAttributeIsThrottled(t *testing.T) {
	t.Parallel()
	r := newQuietMeterRig(t, epmActivePower)
	v0 := r.version()
	r.src.setPower(10) // null → value: at once
	r.step(t, "a change from null", v0, 1, true)
	r.clk.Advance(100 * time.Millisecond)
	r.src.setPower(20) // within the second: held
	r.src.setPower(30) // still held, reported once
	r.step(t, "changes within a second of the last report", v0, 3, false)
	r.clk.Advance(900 * time.Millisecond)
	r.step(t, "after the second", v0, 3, true)
	r.clk.Advance(cluster.QuietMinimumInterval + time.Millisecond)
	r.src.setPower(40) // more than a second later: at once
	r.step(t, "a change after a quiet second", v0, 4, true)
}

// TestHeldQuieterChangeSurvivesReassemble: a change of a projected
// quieter attribute the throttle holds back is reported when its interval
// is up though the bridge re-wired its listeners meanwhile: the throttle
// of a path outlives a wiring that serves the path again, as matter.js's
// quieter throttle lives with the Endpoint's event emitter: QuietEvent
// owns its QuietObservable (packages/node/src/behavior/Events.ts:274-306),
// whose last-emit time and deferred payload are its fields
// (packages/general/src/util/Observable.ts:1061-1063), and the backing only
// registers an observer on it
// (packages/node/src/behavior/internal/ServerBehaviorBacking.ts:258-266).
// The emitters are disposed in Behaviors.close alone
// (packages/node/src/endpoint/properties/Behaviors.ts:471-474), which the
// endpoint's own reset and destructor call
// (packages/node/src/endpoint/Endpoint.ts:934, :1013) — at b25ed9604f. The
// re-wiring restarts no interval: the change after it is still held.
// Review II, R2-P4-dr-sub-01. "alone" rests on the two callers of
// Behaviors.close in packages/node/src at the pin
// (this.behaviors.close() :934, this.#behaviors?.close() :1013, the two
// Endpoint.ts lines above), not a read of every caller of reset or close.
func TestHeldQuieterChangeSurvivesReassemble(t *testing.T) {
	t.Parallel()
	r := newQuietMeterRig(t, epmActivePower)
	v0 := r.version()
	r.src.setPower(10)
	r.step(t, "a change from null", v0, 1, true)
	r.clk.Advance(100 * time.Millisecond)
	r.src.setPower(20) // held
	r.step(t, "a change within the second", v0, 2, false)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := r.b.Reassemble(ctx); err != nil {
		t.Fatal(err)
	}
	r.step(t, "after the re-wiring", v0, 2, false)
	r.clk.Advance(900 * time.Millisecond)
	r.step(t, "after the second", v0, 2, true)
	r.clk.Advance(100 * time.Millisecond)
	r.src.setPower(30) // within the second of the last report: held
	r.step(t, "a change after the re-wiring within the second", v0, 3, false)
}

// TestQuieterChangeInTheRewiringGapIsReportedAtAttach: a change of a
// projected quieter attribute that no live wiring saw — it came while the
// path was parked between two wirings — is compared when the next wiring
// takes the path up: the DataVersion advances and the throttle is told,
// as the change's commit would have done (matter.js
// packages/node/src/behavior/state/managed/Datasource.ts:1446-1457 at
// db021da0), without waiting for the source's next notification. Review
// II, R2-P4-rr-im-04.
func TestQuieterChangeInTheRewiringGapIsReportedAtAttach(t *testing.T) {
	t.Parallel()
	r := newQuietMeterRig(t, epmActivePower)
	v0 := r.version()
	r.src.setPower(10)
	r.step(t, "a change from null", v0, 1, true)
	r.clk.Advance(cluster.QuietMinimumInterval + time.Millisecond)
	// The change no live wiring sees: the source's notification reached
	// the path while it was parked.
	r.src.mu.Lock()
	r.src.power = 20
	r.src.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := r.b.Reassemble(ctx); err != nil {
		t.Fatal(err)
	}
	r.step(t, "the re-wiring that takes the path up", v0, 2, true)
	if err := r.b.Reassemble(ctx); err != nil {
		t.Fatal(err)
	}
	r.step(t, "a re-wiring with nothing changed", v0, 2, false)
}

// TestQuieterChangesInTheRewiringGapAdvanceTheirClusterOnce: two quieter
// paths of one cluster that changed while no wiring served them —
// ActivePower and Voltage — advance the cluster's DataVersion once when the
// next wiring takes them up, as one notification of both does and as one
// matter.js commit advances a cluster's version once, for all the
// properties it changed
// (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457 at
// db021da0). Review II, R2-P4-fr-im-04.
func TestQuieterChangesInTheRewiringGapAdvanceTheirClusterOnce(t *testing.T) {
	t.Parallel()
	r := newQuietMeterRig(t, epmActivePower)
	v0 := r.version()
	r.src.mu.Lock()
	r.src.power, r.src.powered = 10, true
	r.src.voltage, r.src.volted = 230, true
	r.src.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := r.b.Reassemble(ctx); err != nil {
		t.Fatal(err)
	}
	if got := r.version(); got != v0+1 {
		t.Fatalf("the re-wiring that takes up two changed quieter paths of one cluster: DataVersion %d, want %d", got, v0+1)
	}
}

// TestStoppedBridgeQuieterPathIsSilent: once the bridge stopped, a
// notification that still reaches the listener of its last wiring — a
// host that delivers one while the bridge detaches — moves neither the
// DataVersion nor the subscription: nothing is reported into a wiring the
// bridge tore down. Review II, R2-P4-dr-sub-05.
func TestStoppedBridgeQuieterPathIsSilent(t *testing.T) {
	t.Parallel()
	r := newQuietMeterRig(t, epmActivePower)
	v0 := r.version()
	r.src.setPower(10)
	r.step(t, "a change from null", v0, 1, true)
	if err := r.b.Stop(context.WithoutCancel(t.Context())); err != nil {
		t.Fatal(err)
	}
	r.clk.Advance(2 * cluster.QuietMinimumInterval)
	r.src.setPower(20) // fakeMeter's unsubscribe detaches nothing
	r.step(t, "a notification after Stop", v0, 1, false)
	r.clk.Advance(2 * cluster.QuietMinimumInterval)
	r.step(t, "an interval after it", v0, 1, false)
}

// epmActivePower and epmVoltage are ElectricalPowerMeasurement's
// ActivePower and Voltage.
const (
	epmActivePower = 0x0008
	epmVoltage     = 0x0004
)

// TestPowerSourceBatteryQuietInterval: PowerSource's BatPercentRemaining,
// BatTimeRemaining and BatTimeToFullCharge are throttled to ten seconds,
// every other quieter attribute to [cluster.QuietMinimumInterval]
// (packages/node/src/behaviors/power-source/PowerSourceServer.ts:25-34 at
// db91886c). Review II, R2-P4-nv-05b.
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

// TestProjectedElectricalReadingsAreReported: the projected
// ElectricalPowerMeasurement reports Voltage, ActiveCurrent and Frequency
// on change as it serves them, and as each has the quieter quality
// (packages/model/src/standard/elements/electrical-power-measurement.element.ts:54-55,
// :58-59, :94-95 at db021da0) through its throttle, as matter.js
// broadcasts every conformant quieter property when its QuietEvent emits
// (packages/node/src/behavior/internal/ServerBehaviorBacking.ts:230-262):
// a change from null at once,
// changes within the second held and reported once. One change advances
// the cluster's DataVersion once, though the endpoint's source and the
// cluster server that forwards its notifier both cover the path.
func TestProjectedElectricalReadingsAreReported(t *testing.T) {
	t.Parallel()
	r := newQuietMeterRig(t, epmVoltage)
	for _, attr := range []uint32{0x0004, 0x0005, 0x000E} { // Voltage, ActiveCurrent, Frequency
		if !slices.Contains(endpoint.ReportablePaths(r.ep()), im.ConcreteAttributePath{
			Endpoint: r.ep().ID, Cluster: measurement.ClusterElectricalPower, Attribute: attr,
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
		}) {
			t.Errorf("attribute 0x%04X of ElectricalPowerMeasurement is not change-reported", attr)
		}
	}
	v0 := r.version()
	r.src.setVoltage(230) // null → value: at once
	r.step(t, "Voltage from null", v0, 1, true)
	r.clk.Advance(100 * time.Millisecond)
	r.src.setVoltage(231) // within the second: held, the version advanced once
	r.step(t, "Voltage within a second of the last report", v0, 2, false)
	r.clk.Advance(900 * time.Millisecond)
	r.step(t, "after the second", v0, 2, true)
}

// fakeLowBat is a battery's LOWBAT reading that notifies its changes.
type fakeLowBat struct {
	mu  sync.Mutex
	cbs []func()
	low bool
}

func (*fakeLowBat) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementBattery
}

func (f *fakeLowBat) MatterBoolValue() (value, observed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.low, true
}

func (f *fakeLowBat) OnMatterValueChanged(cb func()) func() {
	f.mu.Lock()
	f.cbs = append(f.cbs, cb)
	f.mu.Unlock()
	return func() {}
}

func (f *fakeLowBat) set(low bool) {
	f.mu.Lock()
	f.low = low
	cbs := append([]func(){}, f.cbs...)
	f.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

// TestProjectedChangeAdvancesItsVersionOnce: a measurement endpoint's
// source notifies the bridge twice for one change when a cluster server
// it materialised forwards its notifier — the PowerSource of a battery
// reading — and the change is still one commit: BatChargeLevel, which has
// no quieter quality, advances PowerSource's DataVersion once, as one
// matter.js commit advances a cluster's version once
// (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457 at
// db021da0, advanceVersion in the commit's first phase).
func TestProjectedChangeAdvancesItsVersionOnce(t *testing.T) {
	t.Parallel()
	src := &fakeLowBat{}
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{{
			StableKey: endpoint.StringKey("bat"), DeviceAddress: "B", DeviceType: 0x0011,
			FriendlyName: "Battery", ChannelAddress: "B:1", Measurement: src,
		}}}})
	}
	b, err := New(snap, nil, Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Stop(context.WithoutCancel(ctx)) })
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, nil, nil))
	ep := b.Topology().Bridged()[0]
	const powerSource = 0x002F
	v0 := ep.ClusterDataVersion(powerSource)
	src.set(true)
	if got := ep.ClusterDataVersion(powerSource); got != v0+1 {
		t.Fatalf("one change of BatChargeLevel: DataVersion %d, want %d (one advance)", got, v0+1)
	}
	src.set(true) // no change
	if got := ep.ClusterDataVersion(powerSource); got != v0+1 {
		t.Fatalf("a notification without a change: DataVersion %d, want %d", got, v0+1)
	}
}

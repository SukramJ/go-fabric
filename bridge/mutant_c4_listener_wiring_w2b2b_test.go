// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Kill tests for the C4 survivors in subscribe.go's change-notifier
// wiring: wireMeasurementListeners, wireAttributeNotifiers,
// detachListenersOfGeneration, reportServerChange and pathMemory.refresh
// (phase 8 wave 2b, #602, decision 692). The wiring is called directly on
// an assembled topology; every assertion counts registrations or reads a
// DataVersion, synchronously.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/endpoint/endpointtest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/mdns"
)

// plainServerW2B2b is a cluster server that notifies nothing; attribute 0
// reads 1.
type plainServerW2B2b struct {
	id         uint32
	reportable []uint32
}

func (s plainServerW2B2b) MatterClusterID() uint32 { return s.id }

func (plainServerW2B2b) MatterRead(attrID uint32) (any, bool) { return uint8(1), attrID == 0 }

func (plainServerW2B2b) MatterWrite(context.Context, uint32, any) error         { return nil }
func (plainServerW2B2b) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (s plainServerW2B2b) MatterReportable() []uint32                           { return s.reportable }

// registrationsW2B2b counts the live OnMatterValueChanged and
// OnAttributeChange registrations made on it.
type registrationsW2B2b struct {
	mu          sync.Mutex
	value, attr int
}

func (r *registrationsW2B2b) OnMatterValueChanged(func()) func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.value++
	return func() {}
}

func (r *registrationsW2B2b) OnAttributeChange(func(contract.Change)) func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attr++
	return func() {}
}

func (r *registrationsW2B2b) counts() (value, attr int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.value, r.attr
}

// notifierSourceW2B2b is an endpoint source that notifies and is no
// cluster server itself: its notifier covers every reportable path of the
// endpoint.
type notifierSourceW2B2b struct {
	*registrationsW2B2b
	servers []contract.ClusterServer
}

func (notifierSourceW2B2b) MatterDeviceType() uint16 { return 0x0073 }

func (s notifierSourceW2B2b) MatterClusterServers() []contract.ClusterServer { return s.servers }

// clusterNotifierSourceW2B2b is a notifying source that is also the
// server of a cluster its endpoint does not serve: its notifier covers no
// path of the endpoint.
type clusterNotifierSourceW2B2b struct {
	plainServerW2B2b
	notifierSourceW2B2b
}

// notifyingServerW2B2b is a mounted cluster server that notifies.
type notifyingServerW2B2b struct {
	plainServerW2B2b
	*registrationsW2B2b
}

// unknownMeasurementW2B2b is a measurement source of a class nothing
// materialises, and no change notifier.
type unknownMeasurementW2B2b struct{}

func (unknownMeasurementW2B2b) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementClass(0xFFFF)
}

// assembleW2B2b assembles one bridged endpoint per source.
func assembleW2B2b(t *testing.T, sources ...contract.EndpointSource) *endpoint.Topology {
	t.Helper()
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatalf("endpoint.New: %v", err)
	}
	specs := make([]endpoint.Spec, len(sources))
	for i, src := range sources {
		specs[i] = endpoint.Spec{StableKey: endpoint.StringKey(string(rune('a' + i))), DeviceType: src.MatterDeviceType(), FriendlyName: "Device", Source: src}
	}
	topo, err := asm.Assemble(context.Background(), []endpoint.Snapshot{{Scope: "app", Endpoints: specs}})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if got := len(topo.Bridged()); got != len(sources) {
		t.Fatalf("assembled %d bridged endpoints, want %d", got, len(sources))
	}
	return topo
}

// unstartedBridgeW2B2b is a bridge that never started: the wiring under
// test is called on it directly.
func unstartedBridgeW2B2b(t *testing.T) *Bridge {
	t.Helper()
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

// TestMeasurementWiringPicksTheNotifierThatCoversPathsW2B2b: a notifying
// source is wired even when its endpoint also carries a measurement that
// does not notify; a notifier — the source's or a mounted server's —
// that covers none of the endpoint's reportable paths is not wired.
// Kills subscribe.go:2410 (!= → ==), :2413 (&& → ||), :2434 (> → >=) and
// :2465 (guard off).
func TestMeasurementWiringPicksTheNotifierThatCoversPathsW2B2b(t *testing.T) {
	t.Parallel()
	covering := notifierSourceW2B2b{registrationsW2B2b: &registrationsW2B2b{}}
	pathless := &registrationsW2B2b{}
	covering.servers = []contract.ClusterServer{
		plainServerW2B2b{id: 0xFFF1, reportable: []uint32{0}},
		notifyingServerW2B2b{plainServerW2B2b{id: 0xFFF3}, pathless}, // reports nothing
	}
	foreign := clusterNotifierSourceW2B2b{
		plainServerW2B2b:    plainServerW2B2b{id: 0xFFF2}, // served nowhere on its endpoint
		notifierSourceW2B2b: notifierSourceW2B2b{registrationsW2B2b: &registrationsW2B2b{}, servers: []contract.ClusterServer{plainServerW2B2b{id: 0xFFF1, reportable: []uint32{0}}}},
	}
	topo := assembleW2B2b(t, covering, foreign)
	for _, ep := range topo.Bridged() {
		if s, ok := ep.Source.(notifierSourceW2B2b); ok && s.registrationsW2B2b == covering.registrationsW2B2b {
			ep.Measurement = unknownMeasurementW2B2b{}
		}
	}

	b := unstartedBridgeW2B2b(t)
	b.wireMeasurementListeners(subscription.NewManager(subscription.Config{}, nil, nil), topo)

	if v, _ := covering.counts(); v != 1 {
		t.Errorf("the notifying source beside a silent measurement was registered %d times, want 1", v)
	}
	if v, _ := pathless.counts(); v != 0 {
		t.Errorf("a mounted notifier without a reportable path was registered %d times, want 0", v)
	}
	if v, _ := foreign.counts(); v != 0 {
		t.Errorf("a source notifier of a cluster its endpoint does not serve was registered %d times, want 0", v)
	}
}

// TestDetachOfAnOvertakenGenerationKeepsTheListenersW2B2b: a detach for a
// generation a later wiring moved past leaves that wiring's listeners;
// the current generation's detach runs them. Kills subscribe.go:2599
// (guard off, != → ==).
func TestDetachOfAnOvertakenGenerationKeepsTheListenersW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	ran := 0
	b.mu.Lock()
	b.listenerGeneration = 5
	b.measurementUnsubscribers = []func(){func() { ran++ }}
	b.mu.Unlock()

	b.detachListenersOfGeneration(4)
	if ran != 0 {
		t.Fatalf("a detach of generation 4 ran generation 5's unsubscribers")
	}
	b.detachListenersOfGeneration(5)
	if ran != 1 {
		t.Fatalf("the detach of the current generation ran %d unsubscribers, want 1", ran)
	}
}

// TestRootChangeNotifierIsNotWiredTwiceW2B2b: a root server that is both
// a ChangeNotifier and an AttributeChangeNotifier is left to
// wireRootNotifiers; the same server on a bare endpoint is wired here.
// Kills subscribe.go:2734 (guard off).
func TestRootChangeNotifierIsNotWiredTwiceW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	onRoot, onBare := &registrationsW2B2b{}, &registrationsW2B2b{}
	root := &endpoint.Endpoint{ID: 0}
	root.PublishClusterServers([]contract.ClusterServer{notifyingServerW2B2b{plainServerW2B2b{id: 0xFFF4, reportable: []uint32{0}}, onRoot}})
	bare := &endpoint.Endpoint{ID: 5}
	bare.PublishClusterServers([]contract.ClusterServer{notifyingServerW2B2b{plainServerW2B2b{id: 0xFFF4, reportable: []uint32{0}}, onBare}})

	b.wireAttributeNotifiers(&listenerSet{}, subscription.NewManager(subscription.Config{}, nil, nil), &endpoint.Topology{Endpoints: []*endpoint.Endpoint{root, bare}})
	if _, a := onRoot.counts(); a != 0 {
		t.Errorf("the root's change notifier was subscribed for attribute changes %d times, want 0", a)
	}
	if _, a := onBare.counts(); a != 1 {
		t.Errorf("the bare endpoint's server was subscribed for attribute changes %d times, want 1", a)
	}
}

// TestReportServerChangeMarksAndAdvancesW2B2b: a change naming no
// attribute does nothing; a controller's write outside any write hold is
// marked without an advance; a host change while a write to the endpoint
// is applied advances the version once when the write ends, a report
// view's mark advances nothing. Kills subscribe.go:2772 (guard off),
// :2783 (drop !) and :2784 (&& → ||).
func TestReportServerChangeMarksAndAdvancesW2B2b(t *testing.T) {
	t.Parallel()
	const cluster = uint32(0xFFF1)
	src := notifierSourceW2B2b{registrationsW2B2b: &registrationsW2B2b{}, servers: []contract.ClusterServer{plainServerW2B2b{id: cluster, reportable: []uint32{0}}}}
	ep := assembleW2B2b(t, src).Bridged()[0]
	b := unstartedBridgeW2B2b(t)
	hooks := 0
	b.SetOnAttributeChange(func(uint16, uint32, contract.Change) { hooks++ })
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 0x10, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{{HasEndpoint: true, Endpoint: ep.ID, HasCluster: true, Cluster: cluster}},
	}); err != nil {
		t.Fatal(err)
	}

	v0 := ep.ClusterDataVersion(cluster)
	b.reportServerChange(mgr, ep, cluster, contract.Change{Origin: contract.OriginHostSet}, true)
	if hooks != 0 || ep.ClusterDataVersion(cluster) != v0 {
		t.Fatalf("a change naming no attribute told the hook %d times and moved the version %d → %d", hooks, v0, ep.ClusterDataVersion(cluster))
	}

	b.reportServerChange(mgr, ep, cluster, contract.Change{Attributes: []uint32{0}, Origin: contract.OriginControllerWrite}, true)
	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	if got := spy.reported(cluster); !slices.Equal(got, []uint32{0}) {
		t.Errorf("a controller's write outside a write hold reported %v, want [0]", got)
	}
	if v := ep.ClusterDataVersion(cluster); v != v0 {
		t.Errorf("a controller's write's own change advanced the version %d → %d", v0, v)
	}

	scope := writeScope{endpoints: []uint16{ep.ID}}
	b.holdWrite(scope, func() []im.WriteResult {
		b.reportServerChange(mgr, ep, cluster, contract.Change{Attributes: []uint32{0}, Origin: contract.OriginHostSet}, true)
		return nil
	})
	if v := ep.ClusterDataVersion(cluster); v != v0+1 {
		t.Errorf("a host change held by a write moved the version %d → %d, want one advance", v0, v)
	}
	b.holdWrite(scope, func() []im.WriteResult {
		b.reportServerChange(mgr, ep, cluster, contract.Change{Attributes: []uint32{0}, Origin: contract.OriginHostSet}, false)
		return nil
	})
	if v := ep.ClusterDataVersion(cluster); v != v0+1 {
		t.Errorf("a report view's mark held by a write moved the version %d → %d, want none", v0+1, v)
	}
}

// TestPathMemoryRefreshLeavesAnUnheldPathUnheldW2B2b: refresh reads again
// only a path the memory holds a value of; an unheld path stays unheld and
// its next settle counts as moved. Kills subscribe.go:3771 (guard off,
// || → &&).
func TestPathMemoryRefreshLeavesAnUnheldPathUnheldW2B2b(t *testing.T) {
	t.Parallel()
	const cluster = uint32(0xFFF5)
	src := notifierSourceW2B2b{registrationsW2B2b: &registrationsW2B2b{}, servers: []contract.ClusterServer{plainServerW2B2b{id: cluster, reportable: []uint32{0}}}}
	ep := assembleW2B2b(t, src).Bridged()[0]
	p := im.ConcreteAttributePath{Endpoint: ep.ID, Cluster: cluster, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}

	m := &pathMemory{ep: ep, last: map[im.ConcreteAttributePath]attributeReading{}}
	m.refresh(p)
	if !m.settle(p) {
		t.Fatal("a path refresh found unheld settles as unmoved: refresh stored a reading of it")
	}
}

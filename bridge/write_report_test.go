// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	core "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	hepadef "github.com/SukramJ/go-fabric/cluster/spec/hepafiltermonitoring"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/store"
)

// TestWrittenAttributeReachesASubscriber pins that a successful write is
// reported like any other state change: the written path is marked dirty
// for the subscription that covers it, while a failed write path is not. Mirrors matter.js,
// whose Datasource advances the version and broadcasts every committed
// change, written or not (Datasource.ts). Found by the CHIP Python harness
// (TC-IDM-2.3 step 4: subscribe, write NodeLabel, expect the report).
func TestWrittenAttributeReachesASubscriber(t *testing.T) {
	t.Parallel()
	ep := &endpointpkg.Endpoint{ID: 7, DeviceType: 0x010A, SourceKey: endpointpkg.StringKey("w"), Scope: "s", DeviceAddress: "d"}
	b := newReachabilityBridge(t, ep)

	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	nodeLabel := im.ConcreteAttributePath{
		Endpoint: 7, Cluster: core.BridgedDeviceBasicInformationClusterID, Attribute: 0x0005,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{nodeLabel},
	}); err != nil {
		t.Fatal(err)
	}
	b.AttachSubscriptionManager(mgr)

	failed := nodeLabel
	failed.Attribute = 0x0006
	b.reportWrittenAttributes([]im.WriteResult{
		{Path: nodeLabel, Status: im.StatusSuccess},
		{Path: failed, Status: im.StatusConstraintError},
	})

	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.calls) != 1 || len(spy.calls[0]) != 1 || spy.calls[0][0] != nodeLabel {
		t.Fatalf("reported %+v, want exactly the written NodeLabel path", spy.calls)
	}
}

// labelSource is a bridged endpoint serving one cluster server.
type labelSource struct{ srv contract.ClusterServer }

func (labelSource) MatterDeviceType() uint16 { return 0x010A }
func (s labelSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{s.srv}
}

// TestUnchangedWrittenAttributeIsNotReported: a write whose server says it
// changed nothing (contract.ChangeReportingWriter) is not reported to a
// subscriber; a write that changed the value is. matter.js broadcasts only
// the properties a commit changed
// (packages/node/src/behavior/state/managed/Datasource.ts:1319 at
// db021da0, onChange with the change list :1484 fills; an equal value no
// change at :1479).
func TestUnchangedWrittenAttributeIsNotReported(t *testing.T) {
	t.Parallel()
	labels, err := core.NewUserLabel(core.UserLabelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ep := &endpointpkg.Endpoint{ID: 7, DeviceType: 0x010A, SourceKey: endpointpkg.StringKey("u"), Scope: "s", DeviceAddress: "d", Source: labelSource{labels}}
	b := newReachabilityBridge(t, ep)
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	labelList := im.ConcreteAttributePath{
		Endpoint: 7, Cluster: userlabeldef.ClusterID, Attribute: userlabeldef.AttrLabelList,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{labelList},
	}); err != nil {
		t.Fatal(err)
	}
	b.AttachSubscriptionManager(mgr)
	d := endpointpkg.NewTopologyDispatcher(b.topology)

	now := time.Now()
	reports := func(name string) int {
		t.Helper()
		value := im.AttributeValue{Value: spec.List[userlabeldef.LabelStruct]{{Label: "room", Value: "hall"}}}
		results := d.Write(context.Background(), labelList, value)
		if len(results) != 1 || results[0].Status != im.StatusSuccess {
			t.Fatalf("%s: %+v", name, results)
		}
		b.reportWrittenAttributes(results)
		now = now.Add(2 * time.Second)
		mgr.Tick(context.Background(), now)
		spy.mu.Lock()
		defer spy.mu.Unlock()
		n := len(spy.calls)
		spy.calls = nil
		return n
	}
	if n := reports("first write"); n != 1 {
		t.Fatalf("a changed LabelList reported %d times, want 1", n)
	}
	if n := reports("the same list again"); n != 0 {
		t.Fatalf("an unchanged LabelList reported %d times, want none", n)
	}
}

// TestUnchangedWriteOverTheWireIsNotReported: a WriteRequest that writes
// the value the server holds advances no DataVersion and reaches no
// subscriber, and one that writes a new value advances it once and is
// reported once — the write's result (im.WriteResult.Commit) carries the
// server's verdict from the dispatcher to the report. matter.js advances the version only for a
// commit that changed a value and broadcasts only the changed properties
// (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457, the
// changed names noted at :1484 and handed on at :1319, an equal value no
// change at :1479, at db021da0).
func TestUnchangedWriteOverTheWireIsNotReported(t *testing.T) {
	t.Parallel()
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, Bridged: true})
	if err != nil {
		t.Fatal(err)
	}
	h := newAppHarness(t, appDevice{deviceType: 0x0100, servers: []contract.ClusterServer{srv}})
	epID := h.endpoints[0x0100]
	w := watchCluster(t, h, epID, onoff.ClusterID)
	write := func(v uint64) func() {
		return func() {
			if st := h.writeAttribute(epID, onoff.ClusterID, onoff.AttrOnTime, uintValue(v)); st != 0 {
				t.Fatalf("OnTime write: status 0x%02X", st)
			}
		}
	}
	w.expect("a new OnTime", write(50), onoff.AttrOnTime, 1, 1)
	w.expect("the OnTime the server holds", write(50), onoff.AttrOnTime, 0, 0)
	w.expect("another new OnTime", write(60), onoff.AttrOnTime, 1, 1)
}

// invokeChangeDispatcher is an im.Dispatcher whose Breadcrumb moves when
// ArmFailSafe runs, the way GeneralCommissioning's does.
type invokeChangeDispatcher struct {
	breadcrumb uint64
}

func (d *invokeChangeDispatcher) Read(_ context.Context, p im.ConcreteAttributePath) []im.ReadResult {
	mk := func(attr uint32, v any) im.ReadResult {
		return im.ReadResult{Path: im.ConcreteAttributePath{
			Endpoint: p.Endpoint, Cluster: p.Cluster, Attribute: attr,
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
		}, Value: im.AttributeValue{Value: v}, Status: im.StatusSuccess}
	}
	return []im.ReadResult{mk(0x0000, d.breadcrumb), mk(0x0002, uint8(0)), mk(0xFFFD, uint16(2))}
}

func (d *invokeChangeDispatcher) Write(context.Context, im.ConcreteAttributePath, im.AttributeValue) []im.WriteResult {
	return nil
}

func (d *invokeChangeDispatcher) Invoke(_ context.Context, p im.ConcreteCommandPath, _ any) im.InvokeResult {
	d.breadcrumb++
	return im.InvokeResult{Path: p, Status: im.StatusSuccess}
}

// TestCommandChangedAttributeReachesASubscriber pins that an attribute a
// command changed is reported — and only that one. Mirrors matter.js, whose
// Datasource reports every property a command handler assigned. Found by
// the CHIP Python harness (TC-IDM-1.5: ArmFailSafe moves Breadcrumb, the
// subscriber waits for it).
func TestCommandChangedAttributeReachesASubscriber(t *testing.T) {
	t.Parallel()
	ep := &endpointpkg.Endpoint{ID: 7, DeviceType: 0x010A, SourceKey: endpointpkg.StringKey("i"), Scope: "s", DeviceAddress: "d"}
	b := newReachabilityBridge(t, ep)
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	breadcrumb := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0030, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	regulatory := breadcrumb
	regulatory.Attribute = 0x0002
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{breadcrumb, regulatory},
	}); err != nil {
		t.Fatal(err)
	}
	b.AttachSubscriptionManager(mgr)

	d := &invokeChangeDispatcher{}
	req := im.InvokeRequest{Invokes: []im.CommandInvocation{{
		Path: im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x0030, Command: 0x00, HasEndpoint: true, HasCluster: true, HasCommand: true},
	}}}
	before := b.snapshotInvokedClusters(context.Background(), d, req)
	_ = im.HandleInvokeRequest(im.WithAuthModePASE(context.Background()), d, req)
	b.reportInvokeChanges(context.Background(), d, before)

	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.calls) != 1 || len(spy.calls[0]) != 1 || spy.calls[0][0] != breadcrumb {
		t.Fatalf("reported %+v, want exactly Breadcrumb (the only attribute the command changed)", spy.calls)
	}
}

// siblingMovingOnOff is a hand-written OnOff server (Lighting) whose
// OnTime write leaves OnTime as it holds it and moves OffWaitTime, and
// names that change ([contract.ChangeNamingWriter]).
type siblingMovingOnOff struct {
	mu          sync.Mutex
	offWaitTime uint16
}

func (*siblingMovingOnOff) MatterClusterID() uint32 { return onoff.ClusterID }

func (s *siblingMovingOnOff) MatterRead(attrID uint32) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch attrID {
	case onoff.AttrOnOff:
		return false, true
	case onoff.AttrOnTime:
		return uint16(0), true
	case onoff.AttrOffWaitTime:
		return s.offWaitTime, true
	case 0xFFFC: // FeatureMap: Lighting
		return uint32(onoff.FeatureLT), true
	case 0xFFFD:
		return uint16(6), true
	}
	return nil, false
}

func (s *siblingMovingOnOff) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	_, err := s.MatterWriteCommit(ctx, attrID, value)
	return err
}

func (s *siblingMovingOnOff) MatterWriteCommit(_ context.Context, attrID uint32, _ any) ([]uint32, error) {
	if attrID != onoff.AttrOnTime {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offWaitTime++
	return []uint32{onoff.AttrOffWaitTime}, nil
}

func (*siblingMovingOnOff) MatterInvoke(context.Context, uint32, any) (any, error) {
	return nil, nil
}

func (*siblingMovingOnOff) MatterReportable() []uint32 {
	return []uint32{onoff.AttrOnOff, onoff.AttrOnTime, onoff.AttrOffWaitTime}
}

// TestWriteReportsTheAttributesItsCommitNames: a write that leaves the
// written attribute as it was and moves a sibling advances the cluster's
// DataVersion once and reports the sibling alone — the attributes its
// server names (contract.ChangeNamingWriter, im.WriteResult.Commit), not
// the written path. matter.js notes a name in a commit's change list only
// when its value differs and hands the list on
// (packages/node/src/behavior/state/managed/Datasource.ts:1476-1484 and
// :1319 at db021da0), and advances the version once for a commit that
// changed something (:1456-1457) (review II, R2-P5-write-02).
func TestWriteReportsTheAttributesItsCommitNames(t *testing.T) {
	t.Parallel()
	srv := &siblingMovingOnOff{}
	h := newAppHarness(t, appDevice{deviceType: 0x0100, servers: []contract.ClusterServer{srv}})
	epID := h.endpoints[0x0100]
	w := watchCluster(t, h, epID, onoff.ClusterID)
	write := func() {
		if st := h.writeAttribute(epID, onoff.ClusterID, onoff.AttrOnTime, uintValue(0)); st != 0 {
			t.Fatalf("OnTime write: status 0x%02X", st)
		}
	}
	advanced, onTime := w.step(write, onoff.AttrOnTime)
	if advanced != 1 || onTime != 0 {
		t.Errorf("DataVersion advanced %d, OnTime reported %d times; want 1 and 0", advanced, onTime)
	}
	// The sibling: one more write, counted on OffWaitTime.
	w.expect("OffWaitTime, moved by the OnTime write", write, onoff.AttrOffWaitTime, 1, 1)
}

// conditionSink is a host's write port that moves Condition when
// LastChangedTime is written, reporting it with the host's origin from
// inside the controller's write.
type conditionSink struct {
	srv *spec.Server
	// after, when set, runs once the Sink moved Condition, still inside
	// the write.
	after func()
}

func (s *conditionSink) MatterWriteAttribute(_ context.Context, attrID uint32, _ any) error {
	if attrID != hepadef.AttrLastChangedTime {
		return nil
	}
	v, _ := s.srv.MatterRead(hepadef.AttrCondition)
	c, _ := v.(uint8)
	err := s.srv.Set(hepadef.AttrCondition, c+1)
	if s.after != nil {
		s.after()
	}
	return err
}

// TestHostChangeInsideAWriteAdvancesOnce: a host that moves a sibling
// attribute from inside a controller's write, and reports it with its own
// origin through the server's change notifier, is held with the write: the
// cluster's DataVersion advances once — the write's commit — and the
// written attribute and the sibling are each reported once (review II,
// R2-P5-write-03, F-db021da0-45). matter.js advances a datasource's
// version once per commit (Datasource.ts:1456-1457 at db021da0).
func TestHostChangeInsideAWriteAdvancesOnce(t *testing.T) {
	t.Parallel()
	sink := &conditionSink{}
	srv, err := spec.NewServer(hepadef.Definition, spec.Options{
		Features:   uint32(hepadef.FeatureCondition),
		Attributes: []uint32{hepadef.AttrLastChangedTime},
	}, spec.ServerConfig{Sink: sink, Initial: map[uint32]any{hepadef.AttrCondition: uint8(40)}})
	if err != nil {
		t.Fatal(err)
	}
	sink.srv = srv
	h := newAppHarness(t, appDevice{deviceType: 0x002D, servers: []contract.ClusterServer{srv}})
	epID := h.endpoints[0x002D]
	w := watchCluster(t, h, epID, hepadef.ClusterID)
	for i, v := range []uint64{1000, 2000} {
		if st := h.writeAttribute(epID, hepadef.ClusterID, hepadef.AttrLastChangedTime, uintValue(v)); st != 0 {
			t.Fatalf("write %d: status 0x%02X", i+1, st)
		}
		w.now = w.now.Add(time.Minute)
		w.mgr.Tick(context.Background(), w.now)
		now := w.ep.ClusterDataVersion(hepadef.ClusterID)
		advanced := now - w.version
		w.version = now
		reports := map[uint32]int{}
		w.spy.mu.Lock()
		for _, call := range w.spy.calls {
			for _, p := range call {
				reports[p.Attribute]++
			}
		}
		w.spy.calls = nil
		w.spy.mu.Unlock()
		if advanced != 1 || reports[hepadef.AttrLastChangedTime] != 1 || reports[hepadef.AttrCondition] != 1 {
			t.Errorf("write %d: DataVersion advanced %d, LastChangedTime reported %d times, Condition %d; want 1, 1, 1",
				i+1, advanced, reports[hepadef.AttrLastChangedTime], reports[hepadef.AttrCondition])
		}
	}
}

// rootNotifyingServer is a host's root server of a manufacturer cluster
// whose write of attribute 0 moves attribute 1 and fires its change
// notifier (contract.ChangeNotifier) from inside the write.
type rootNotifyingServer struct {
	hostNotifier
	mu   sync.Mutex
	vals [2]uint16
}

const rootNotifyingCluster = uint32(0xFFF1FC03)

func (*rootNotifyingServer) MatterClusterID() uint32 { return rootNotifyingCluster }

func (s *rootNotifyingServer) MatterRead(attrID uint32) (any, bool) {
	if attrID > 1 {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vals[attrID], true
}
func (*rootNotifyingServer) MatterAttributes() []uint32 { return []uint32{0, 1} }
func (*rootNotifyingServer) MatterReportable() []uint32 { return []uint32{0, 1} }

func (s *rootNotifyingServer) MatterWrite(_ context.Context, attrID uint32, value any) error {
	v, _ := value.(uint64)
	s.mu.Lock()
	s.vals[0] = uint16(v)
	s.vals[1]++
	s.mu.Unlock()
	s.fire()
	return nil
}

func (*rootNotifyingServer) MatterInvoke(context.Context, uint32, any) (any, error) {
	return nil, nil
}

// TestRootNotifierInsideAWriteAdvancesOnce: a host's root server whose
// change notifier fires from inside a controller's write is held with the
// write: the cluster's DataVersion advances once and each attribute is
// reported once (review II, R2-P5-write-03, F-db021da0-45).
func TestRootNotifierInsideAWriteAdvancesOnce(t *testing.T) {
	t.Parallel()
	srv := &rootNotifyingServer{}
	sh := newSecureHarnessWith(t, func(*store.Store, uint8) (Snapshotter, []contract.ClusterServer) {
		asm, err := endpointpkg.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
		if err != nil {
			t.Fatalf("endpoint.New: %v", err)
		}
		return func(ctx context.Context) (*endpointpkg.Topology, error) {
			return asm.Assemble(ctx, []endpointpkg.Snapshot{{Scope: "app"}})
		}, []contract.ClusterServer{srv}
	})
	sh.allowAll()
	h := &appHarness{secureHarness: sh, endpoints: map[uint16]uint16{}}
	w := watchCluster(t, h, 0, rootNotifyingCluster)
	for i, v := range []uint64{5, 6} {
		if st := h.writeAttribute(0, rootNotifyingCluster, 0, uintValue(v)); st != 0 {
			t.Fatalf("write %d: status 0x%02X", i+1, st)
		}
		w.now = w.now.Add(time.Minute)
		w.mgr.Tick(context.Background(), w.now)
		now := w.ep.ClusterDataVersion(rootNotifyingCluster)
		advanced := now - w.version
		w.version = now
		reports := map[uint32]int{}
		w.spy.mu.Lock()
		for _, call := range w.spy.calls {
			for _, p := range call {
				reports[p.Attribute]++
			}
		}
		w.spy.calls = nil
		w.spy.mu.Unlock()
		if advanced != 1 || reports[0] != 1 || reports[1] != 1 {
			t.Errorf("write %d: DataVersion advanced %d, attribute 0 reported %d times, 1 %d; want 1, 1, 1", i+1, advanced, reports[0], reports[1])
		}
	}
}

// TestHostChangeInsideARootWriteAdvancesOnce: on the root, where a
// generated server's own tracker is the DataVersion the wire carries, a
// Sink that moves a sibling inside a controller's write is part of the
// write's commit: the server names both attributes in the write's result,
// its version advances once per write, and each attribute is reported
// once (review II, Fable read 5b-write F1). matter.js advances a
// datasource's version once per transaction for every field it noted and
// hands the list on (Datasource.ts:1456-1457, :1476-1484, :1319 at
// db021da0).
func TestHostChangeInsideARootWriteAdvancesOnce(t *testing.T) {
	t.Parallel()
	sink := &conditionSink{}
	srv, err := spec.NewServer(hepadef.Definition, spec.Options{
		Features:   uint32(hepadef.FeatureCondition),
		Attributes: []uint32{hepadef.AttrLastChangedTime},
	}, spec.ServerConfig{Sink: sink, Initial: map[uint32]any{hepadef.AttrCondition: uint8(40)}})
	if err != nil {
		t.Fatal(err)
	}
	sink.srv = srv
	sh := newSecureHarnessWith(t, func(*store.Store, uint8) (Snapshotter, []contract.ClusterServer) {
		asm, err := endpointpkg.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
		if err != nil {
			t.Fatalf("endpoint.New: %v", err)
		}
		return func(ctx context.Context) (*endpointpkg.Topology, error) {
			return asm.Assemble(ctx, []endpointpkg.Snapshot{{Scope: "app"}})
		}, []contract.ClusterServer{srv}
	})
	sh.allowAll()
	h := &appHarness{secureHarness: sh, endpoints: map[uint16]uint16{}}
	w := watchCluster(t, h, 0, hepadef.ClusterID)
	// A report built while the write runs — after the Sink moved
	// Condition, before the write's commit advanced the version — carries
	// nothing: the sibling's mark waits for the write's end (Fable read
	// 5b-write R3).
	var inside int
	sink.after = func() {
		w.now = w.now.Add(time.Minute)
		w.mgr.Tick(context.Background(), w.now)
		w.spy.mu.Lock()
		for _, call := range w.spy.calls {
			inside += len(call)
		}
		w.spy.calls = nil
		w.spy.mu.Unlock()
	}
	version := srv.MatterDataVersion()
	for i, v := range []uint64{1000, 2000} {
		if st := h.writeAttribute(0, hepadef.ClusterID, hepadef.AttrLastChangedTime, uintValue(v)); st != 0 {
			t.Fatalf("write %d: status 0x%02X", i+1, st)
		}
		w.now = w.now.Add(time.Minute)
		w.mgr.Tick(context.Background(), w.now)
		now := srv.MatterDataVersion()
		advanced := now - version
		version = now
		reports := map[uint32]int{}
		w.spy.mu.Lock()
		for _, call := range w.spy.calls {
			for _, p := range call {
				reports[p.Attribute]++
			}
		}
		w.spy.calls = nil
		w.spy.mu.Unlock()
		if advanced != 1 || reports[hepadef.AttrLastChangedTime] != 1 || reports[hepadef.AttrCondition] != 1 {
			t.Errorf("write %d: the server's DataVersion advanced %d, LastChangedTime reported %d times, Condition %d; want 1, 1, 1",
				i+1, advanced, reports[hepadef.AttrLastChangedTime], reports[hepadef.AttrCondition])
		}
		if inside != 0 {
			t.Errorf("write %d: %d path(s) reported while the write ran, before its commit advanced the version", i+1, inside)
		}
	}
}

// TestWrittenAttributeIsNotReportedBeforeTheWriteAdvances (Fable read
// 5b-write R3, the remaining window): on a bridged endpoint the
// dispatcher advances the cluster's version after the server returned; a
// report built while the server tells its own write's change — here from
// the host hook, which runs right after the mark — carries nothing, and
// the written attribute is reported once, after the advance.
func TestWrittenAttributeIsNotReportedBeforeTheWriteAdvances(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(hepadef.Definition, spec.Options{
		Features:   uint32(hepadef.FeatureCondition),
		Attributes: []uint32{hepadef.AttrLastChangedTime},
	}, spec.ServerConfig{Initial: map[uint32]any{hepadef.AttrCondition: uint8(40)}})
	if err != nil {
		t.Fatal(err)
	}
	h := newAppHarness(t, appDevice{deviceType: 0x002D, servers: []contract.ClusterServer{srv}})
	epID := h.endpoints[0x002D]
	w := watchCluster(t, h, epID, hepadef.ClusterID)
	var inside int
	h.bridge.SetOnAttributeChange(func(_ uint16, _ uint32, change contract.Change) {
		if change.Origin != contract.OriginControllerWrite {
			return
		}
		w.now = w.now.Add(time.Minute)
		w.mgr.Tick(context.Background(), w.now)
		w.spy.mu.Lock()
		for _, call := range w.spy.calls {
			inside += len(call)
		}
		w.spy.calls = nil
		w.spy.mu.Unlock()
	})
	if st := h.writeAttribute(epID, hepadef.ClusterID, hepadef.AttrLastChangedTime, uintValue(1000)); st != 0 {
		t.Fatalf("write: status 0x%02X", st)
	}
	if inside != 0 {
		t.Errorf("%d path(s) reported while the server told its write, before the dispatcher advanced the version", inside)
	}
	if advanced, n := w.step(func() {}, hepadef.AttrLastChangedTime); advanced != 1 || n != 1 {
		t.Errorf("DataVersion advanced %d, LastChangedTime reported %d times; want 1 and 1", advanced, n)
	}
}

// reportViewHidden shadows the embedded server's OnAttributeReport in
// [noReportViewServer] with a method of another signature, so the wrapper
// is a server with a change view and no report view.
type reportViewHidden struct{}

func (reportViewHidden) OnAttributeReport() {}

// noReportViewServer is a generated server seen without its report view
// ([contract.AttributeReportNotifier]): its changes, a write's own
// included, reach the bridge through OnAttributeChange alone.
type noReportViewServer struct {
	*spec.Server
	reportViewHidden
}

// TestWrittenAttributeOfAServerWithoutReportViewWaitsForTheAdvance
// (F-db021da0-59): the sibling of
// TestWrittenAttributeIsNotReportedBeforeTheWriteAdvances for a server
// wired on its change view alone. The write's own change is not marked
// while the server tells it — before the dispatcher advanced the version —
// but after the advance, once.
func TestWrittenAttributeOfAServerWithoutReportViewWaitsForTheAdvance(t *testing.T) {
	t.Parallel()
	inner, err := spec.NewServer(hepadef.Definition, spec.Options{
		Features:   uint32(hepadef.FeatureCondition),
		Attributes: []uint32{hepadef.AttrLastChangedTime},
	}, spec.ServerConfig{Initial: map[uint32]any{hepadef.AttrCondition: uint8(40)}})
	if err != nil {
		t.Fatal(err)
	}
	srv := noReportViewServer{Server: inner}
	if _, reports := any(srv).(contract.AttributeReportNotifier); reports {
		t.Fatal("the wrapper still has a report view")
	}
	if _, changes := any(srv).(contract.AttributeChangeNotifier); !changes {
		t.Fatal("the wrapper lost its change view")
	}
	h := newAppHarness(t, appDevice{deviceType: 0x002D, servers: []contract.ClusterServer{srv}})
	epID := h.endpoints[0x002D]
	w := watchCluster(t, h, epID, hepadef.ClusterID)
	var told, inside int
	h.bridge.SetOnAttributeChange(func(_ uint16, _ uint32, change contract.Change) {
		if change.Origin != contract.OriginControllerWrite {
			return
		}
		told++
		w.now = w.now.Add(time.Minute)
		w.mgr.Tick(context.Background(), w.now)
		w.spy.mu.Lock()
		for _, call := range w.spy.calls {
			inside += len(call)
		}
		w.spy.calls = nil
		w.spy.mu.Unlock()
	})
	if st := h.writeAttribute(epID, hepadef.ClusterID, hepadef.AttrLastChangedTime, uintValue(1000)); st != 0 {
		t.Fatalf("write: status 0x%02X", st)
	}
	if told == 0 {
		t.Fatal("the server told no change of its write; the test proves nothing")
	}
	if inside != 0 {
		t.Errorf("%d path(s) reported while the server told its write, before the dispatcher advanced the version", inside)
	}
	if advanced, n := w.step(func() {}, hepadef.AttrLastChangedTime); advanced != 1 || n != 1 {
		t.Errorf("DataVersion advanced %d, LastChangedTime reported %d times; want 1 and 1", advanced, n)
	}
}

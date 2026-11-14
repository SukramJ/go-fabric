// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	core "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec"
	userlabeldef "github.com/SukramJ/go-fabric/cluster/spec/userlabel"
	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
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
	b.reportWrittenAttributes(im.WriteResponse{Responses: []im.AttributeStatus{
		{Path: nodeLabel, Status: im.StatusIB{Status: im.StatusSuccess}},
		{Path: failed, Status: im.StatusIB{Status: im.StatusConstraintError}},
	}}, nil)

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
		changes := &endpointpkg.WriteChanges{}
		ctx := endpointpkg.WithWriteChanges(context.Background(), changes)
		value := im.AttributeValue{Value: spec.List[userlabeldef.LabelStruct]{{Label: "room", Value: "hall"}}}
		results := d.Write(ctx, labelList, value)
		resp := im.WriteResponse{}
		for _, r := range results {
			resp.Responses = append(resp.Responses, im.AttributeStatus{Path: r.Path, Status: im.StatusIB{Status: r.Status}})
		}
		if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != im.StatusSuccess {
			t.Fatalf("%s: %+v", name, resp.Responses)
		}
		b.reportWrittenAttributes(resp, changes)
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
// reported once — the record the write handler attaches
// (endpoint.WithWriteChanges) carries the server's verdict from the
// dispatcher to the report. matter.js advances the version only for a
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
	_ = im.HandleInvokeRequest(context.Background(), d, req)
	b.reportInvokeChanges(context.Background(), d, before)

	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.calls) != 1 || len(spy.calls[0]) != 1 || spy.calls[0][0] != breadcrumb {
		t.Fatalf("reported %+v, want exactly Breadcrumb (the only attribute the command changed)", spy.calls)
	}
}

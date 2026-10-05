// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	core "github.com/SukramJ/go-fabric/cluster/core"
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
	}})

	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.calls) != 1 || len(spy.calls[0]) != 1 || spy.calls[0][0] != nodeLabel {
		t.Fatalf("reported %+v, want exactly the written NodeLabel path", spy.calls)
	}
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

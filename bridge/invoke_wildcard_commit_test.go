// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// wildcardInvoker is invokeChangeDispatcher on endpoint 7 that also runs
// a wildcard-endpoint command there and names its targets, as
// endpoint.TopologyDispatcher does.
type wildcardInvoker struct {
	invokeChangeDispatcher
	targets []uint16
}

func (d *wildcardInvoker) InvokeAuthorized(ctx context.Context, p im.ConcreteCommandPath, fields any, authorize im.CommandAuthorizer) []im.InvokeResult {
	if p.HasEndpoint {
		return []im.InvokeResult{d.Invoke(ctx, p, fields)}
	}
	var out []im.InvokeResult
	for _, e := range d.targets {
		if authorize != nil && !authorize(e, p.Cluster, p.Command).IsSuccess() {
			continue
		}
		q := p
		q.Endpoint, q.HasEndpoint = e, true
		out = append(out, d.Invoke(ctx, q, fields))
	}
	return out
}

// CheckACL grants every group access: the access check is not this
// test's subject.
func (d *wildcardInvoker) CheckACL(context.Context, uint8, uint64, []uint32, uint16, uint32, uint8) im.StatusCode {
	return im.StatusSuccess
}

func (d *wildcardInvoker) CommandTargets(im.ConcreteCommandPath, im.CommandAuthorizer) []uint16 {
	return d.targets
}

// TestWildcardAndGroupInvokeChangesReachASubscriber (R2-P6-par-0401): a
// command whose path names no endpoint — a unicast wildcard-endpoint
// invoke, and a group invoke — is one commit like a concrete one: the
// attribute its server changed without a notification of its own is
// reported to the subscriber and the cluster's DataVersion advances.
func TestWildcardAndGroupInvokeChangesReachASubscriber(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"concrete", "wildcard", "group"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ep := &endpointpkg.Endpoint{ID: 7, DeviceType: 0x010A, SourceKey: endpointpkg.StringKey("i"), Scope: "s", DeviceAddress: "d"}
			b := newReachabilityBridge(t, ep)
			spy := &reachAttrReporterSpy{}
			mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
			breadcrumb := im.ConcreteAttributePath{Endpoint: 7, Cluster: 0x0030, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
			if _, err := mgr.Subscribe(subscription.SubscribeArgs{
				PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
				AttributePaths: []im.ConcreteAttributePath{breadcrumb},
			}); err != nil {
				t.Fatal(err)
			}
			b.AttachSubscriptionManager(mgr)
			topoEP := b.Topology().FindByID(7)
			if topoEP == nil {
				t.Fatal("endpoint 7 not in the topology")
			}
			v0 := topoEP.ClusterDataVersion(0x0030)
			d := &wildcardInvoker{targets: []uint16{7}}
			path := im.ConcreteCommandPath{Endpoint: 7, Cluster: 0x0030, Command: 0x00, HasEndpoint: mode == "concrete", HasCluster: true, HasCommand: true}
			req := im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: path}}}
			ctx := im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), 1, nil)
			switch mode {
			case "group":
				gctx := im.WithGroupSubject(im.WithFabricFilter(ctx, false, 1), im.GroupSubject{GroupID: 1, HasValidMapping: true, Endpoints: []uint16{7}})
				_ = b.handleGroupInvoke(gctx, d, req)
			default:
				before := b.snapshotInvokedClusters(ctx, d, req)
				_ = im.HandleInvokeRequest(ctx, d, req)
				b.reportInvokeChanges(ctx, d, before)
			}
			if d.breadcrumb != 1 {
				t.Fatalf("the command ran %d times", d.breadcrumb)
			}
			mgr.Tick(ctx, time.Now().Add(2*time.Second))
			spy.mu.Lock()
			calls := len(spy.calls)
			spy.mu.Unlock()
			if calls != 1 {
				t.Errorf("reports to the subscriber: %d, want 1", calls)
			}
			if v := topoEP.ClusterDataVersion(0x0030); v == v0 {
				t.Errorf("DataVersion stayed %d", v)
			}
		})
	}
}

// TestInvokeCommitAdvancesWithoutAManager (N-findev-2): a subscription
// manager detached while the command runs still lets the commit advance
// the cluster's DataVersion; only the marks need a manager.
func TestInvokeCommitAdvancesWithoutAManager(t *testing.T) {
	t.Parallel()
	ep := &endpointpkg.Endpoint{ID: 7, DeviceType: 0x010A, SourceKey: endpointpkg.StringKey("i"), Scope: "s", DeviceAddress: "d"}
	b := newReachabilityBridge(t, ep)
	b.AttachSubscriptionManager(subscription.NewManager(subscription.Config{}, (&reachAttrReporterSpy{}).report, nil))
	topoEP := b.Topology().FindByID(7)
	v0 := topoEP.ClusterDataVersion(0x0030)
	d := &invokeChangeDispatcher{}
	req := im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: im.ConcreteCommandPath{Endpoint: 7, Cluster: 0x0030, HasEndpoint: true, HasCluster: true, HasCommand: true}}}}
	before := b.snapshotInvokedClusters(context.Background(), d, req)
	_ = im.HandleInvokeRequest(context.Background(), d, req)
	b.AttachSubscriptionManager(nil)
	b.reportInvokeChanges(context.Background(), d, before)
	if v := topoEP.ClusterDataVersion(0x0030); v == v0 {
		t.Errorf("DataVersion stayed %d with the manager detached", v)
	}
}

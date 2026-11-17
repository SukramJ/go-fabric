// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// fencedCounter serves attribute 0, which its command 0 advances without
// a change notification, and admits a request's read only from fabric 1.
type fencedCounter struct {
	mu sync.Mutex
	n  uint8
}

func (*fencedCounter) MatterClusterID() uint32 { return 0xFFF1FC08 }
func (s *fencedCounter) MatterRead(attr uint32) (any, bool) {
	if attr != 0 {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n, true
}
func (*fencedCounter) MatterAttributes() []uint32                     { return []uint32{0} }
func (*fencedCounter) MatterWrite(context.Context, uint32, any) error { return nil }
func (s *fencedCounter) MatterInvoke(context.Context, uint32, any) (any, error) {
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
	return nil, nil
}
func (*fencedCounter) MatterReportable() []uint32 { return nil }
func (*fencedCounter) MatterReadAllowed(ctx context.Context, _ uint32) bool {
	fabric, _, _ := contract.AccessingFabric(ctx)
	return fabric == 1
}

type fencedSource struct{ srv contract.ClusterServer }

func (fencedSource) MatterDeviceType() uint16 { return 0x010A }
func (s fencedSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{s.srv}
}

// TestInvokeCommitSeesAChangeTheInvokerMayNotRead (fable-read-6a-gate §6):
// the invoke commit's before/after snapshot is the bridge's own and is
// never delivered, so it reads without the requester's refusals — a
// command of a requester a ReadAccessDeclarer refuses still advances the
// DataVersion and reaches the subscribers the declarer admits.
func TestInvokeCommitSeesAChangeTheInvokerMayNotRead(t *testing.T) {
	t.Parallel()
	srv := &fencedCounter{}
	ep := &endpointpkg.Endpoint{ID: 7, DeviceType: 0x010A, SourceKey: endpointpkg.StringKey("f"), Scope: "s", DeviceAddress: "d", Source: fencedSource{srv: srv}}
	b := newReachabilityBridge(t, ep)
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	attr := im.ConcreteAttributePath{Endpoint: 7, Cluster: 0xFFF1FC08, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{attr}}); err != nil {
		t.Fatal(err)
	}
	b.AttachSubscriptionManager(mgr)
	topoEP := b.Topology().FindByID(7)
	v0 := topoEP.ClusterDataVersion(0xFFF1FC08)
	d := endpointpkg.NewTopologyDispatcher(b.Topology())
	refused := im.WithSubject(im.WithFabricFilter(context.Background(), false, 2), 0x2222, nil)
	path := im.ConcreteCommandPath{Endpoint: 7, Cluster: 0xFFF1FC08, Command: 0, HasEndpoint: true, HasCluster: true, HasCommand: true}
	req := im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: path}}}
	before := b.snapshotInvokedClusters(refused, d, req)
	if res := d.Invoke(refused, path, nil); res.Status != im.StatusSuccess {
		t.Fatalf("invoke: %+v", res)
	}
	b.reportInvokeChanges(refused, d, before)
	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	spy.mu.Lock()
	calls := len(spy.calls)
	spy.mu.Unlock()
	if calls != 1 {
		t.Errorf("reports to the subscriber: %d, want 1", calls)
	}
	if v := topoEP.ClusterDataVersion(0xFFF1FC08); v == v0 {
		t.Errorf("DataVersion stayed %d", v)
	}
}

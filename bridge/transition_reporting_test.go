// SPDX-License-Identifier: MIT
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

// transitioningServer is a LevelControl-shaped server that reports
// CurrentLevel itself (contract.SelfReportedAttributeLister) and runs a
// timer of its own (contract.ClusterQuiescer).
type transitioningServer struct {
	mu       sync.Mutex
	quiesced int
}

func (*transitioningServer) MatterClusterID() uint32 { return 0x0008 }

func (*transitioningServer) MatterRead(uint32) (any, bool)                          { return uint8(0), true }
func (*transitioningServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*transitioningServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*transitioningServer) MatterReportable() []uint32                             { return nil }
func (*transitioningServer) MatterSelfReportedAttributes() []uint32                 { return []uint32{0x0000} }

func (s *transitioningServer) MatterQuiesce() {
	s.mu.Lock()
	s.quiesced++
	s.mu.Unlock()
}

func (s *transitioningServer) quiesceCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quiesced
}

// serverSource is an endpoint source carrying fixed servers.
type serverSource struct{ servers []contract.ClusterServer }

func (*serverSource) MatterDeviceType() uint16                         { return 0x0101 }
func (s *serverSource) MatterClusterServers() []contract.ClusterServer { return s.servers }

// levelDispatcher answers a read of endpoint 7's LevelControl with a
// CurrentLevel and an OnLevel that both move when a command runs.
type levelDispatcher struct{ level, onLevel uint8 }

func (d *levelDispatcher) Read(_ context.Context, p im.ConcreteAttributePath) []im.ReadResult {
	mk := func(attr uint32, v any) im.ReadResult {
		return im.ReadResult{Path: im.ConcreteAttributePath{
			Endpoint: p.Endpoint, Cluster: p.Cluster, Attribute: attr,
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
		}, Value: im.AttributeValue{Value: v}, Status: im.StatusSuccess}
	}
	return []im.ReadResult{mk(0x0000, d.level), mk(0x0011, d.onLevel)}
}

func (d *levelDispatcher) Write(context.Context, im.ConcreteAttributePath, im.AttributeValue) []im.WriteResult {
	return nil
}

func (d *levelDispatcher) Invoke(_ context.Context, p im.ConcreteCommandPath, _ any) im.InvokeResult {
	d.level++
	d.onLevel++
	return im.InvokeResult{Path: p, Status: im.StatusSuccess}
}

// TestCommandLeavesSelfReportedAttributesToTheServer: a command's
// before/after comparison reports every attribute it changed except the
// ones the server reports itself — CurrentLevel moves by the quieter rules
// of its "Q" quality, which matter.js ServerBehaviorBacking applies to a
// command's change too.
func TestCommandLeavesSelfReportedAttributesToTheServer(t *testing.T) {
	t.Parallel()
	srv := &transitioningServer{}
	ep := &endpointpkg.Endpoint{
		ID: 7, DeviceType: 0x0101, SourceKey: endpointpkg.StringKey("lamp"), Scope: "s", DeviceAddress: "d",
		Source: &serverSource{servers: []contract.ClusterServer{srv}},
	}
	b := newReachabilityBridge(t, ep)
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	currentLevel := im.ConcreteAttributePath{Endpoint: 7, Cluster: 0x0008, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	onLevel := currentLevel
	onLevel.Attribute = 0x0011
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{currentLevel, onLevel},
	}); err != nil {
		t.Fatal(err)
	}
	b.AttachSubscriptionManager(mgr)

	d := &levelDispatcher{}
	req := im.InvokeRequest{Invokes: []im.CommandInvocation{{
		Path: im.ConcreteCommandPath{Endpoint: 7, Cluster: 0x0008, Command: 0x00, HasEndpoint: true, HasCluster: true, HasCommand: true},
	}}}
	before := b.snapshotInvokedClusters(context.Background(), d, req)
	_ = im.HandleInvokeRequest(context.Background(), d, req)
	b.reportInvokeChanges(context.Background(), d, before)

	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.calls) != 1 || len(spy.calls[0]) != 1 || spy.calls[0][0] != onLevel {
		t.Fatalf("reported %+v, want OnLevel alone (CurrentLevel is the server's to report)", spy.calls)
	}
}

// TestQuiescedOnRemovalAndStop: a server the new topology no longer
// carries stops its timers, a server it keeps does not, and Stop stops
// every one — as matter.js closes a removed endpoint's, and an offline
// node's, behaviors.
func TestQuiescedOnRemovalAndStop(t *testing.T) {
	t.Parallel()
	removed, kept := &transitioningServer{}, &transitioningServer{}
	keptEP := &endpointpkg.Endpoint{
		ID: 8, DeviceType: 0x0101, SourceKey: endpointpkg.StringKey("kept"), Scope: "s", DeviceAddress: "k",
		Source: &serverSource{servers: []contract.ClusterServer{kept}},
	}
	ep := &endpointpkg.Endpoint{
		ID: 7, DeviceType: 0x0101, SourceKey: endpointpkg.StringKey("gone"), Scope: "s", DeviceAddress: "g",
		Source: &serverSource{servers: []contract.ClusterServer{removed}},
	}
	b := newReachabilityBridge(t, ep)
	b.mu.Lock()
	b.topology.Endpoints = append(b.topology.Endpoints, keptEP)
	b.snapshotter = func(context.Context) (*endpointpkg.Topology, error) {
		return &endpointpkg.Topology{Endpoints: []*endpointpkg.Endpoint{
			{ID: 0, DeviceType: 0x0016}, {ID: 1, DeviceType: 0x000E}, keptEP,
		}}, nil
	}
	b.mu.Unlock()

	if err := b.Reassemble(context.Background()); err != nil {
		t.Fatal(err)
	}
	if removed.quiesceCount() != 1 || kept.quiesceCount() != 0 {
		t.Fatalf("after the reassembly: removed quiesced %d, kept %d; want 1, 0", removed.quiesceCount(), kept.quiesceCount())
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := b.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	if kept.quiesceCount() != 1 {
		t.Errorf("Stop quiesced the kept server %d times, want 1", kept.quiesceCount())
	}
}

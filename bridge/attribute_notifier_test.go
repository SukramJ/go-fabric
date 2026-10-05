// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// statefulServer is a bridged cluster server that keeps its own state and
// names the attributes that moved.
type statefulServer struct {
	mu  sync.Mutex
	cbs []func([]uint32)
}

func (*statefulServer) MatterClusterID() uint32 { return 0xFFF1_FC01 }

func (*statefulServer) MatterRead(attrID uint32) (any, bool)                   { return uint8(attrID), attrID < 3 }
func (*statefulServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*statefulServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*statefulServer) MatterReportable() []uint32                             { return []uint32{0, 1, 2} }

func (s *statefulServer) OnMatterAttributesChanged(cb func([]uint32)) func() {
	s.mu.Lock()
	s.cbs = append(s.cbs, cb)
	s.mu.Unlock()
	return func() {}
}

func (s *statefulServer) fire(attrs ...uint32) {
	s.mu.Lock()
	cbs := slices.Clone(s.cbs)
	s.mu.Unlock()
	for _, cb := range cbs {
		cb(attrs)
	}
}

var _ contract.AttributeChangeNotifier = (*statefulServer)(nil)

// TestAttributeChangeNotifierMarksOnlyTheNamedAttributes: a bridged server
// that names its changed attributes gets them — and only them — marked
// dirty, with the cluster's DataVersion advanced, although the host's
// source is no ChangeNotifier. An empty fire changes nothing.
func TestAttributeChangeNotifierMarksOnlyTheNamedAttributes(t *testing.T) {
	t.Parallel()
	srv := &statefulServer{}
	h := newAppHarness(t, appDevice{deviceType: 0x0073, servers: []contract.ClusterServer{srv}})
	ep := h.endpoints[0x0073]
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		FabricIndex: h.fabric, PeerNodeID: harnessControllerNodeID, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{{HasEndpoint: true, HasCluster: true, Endpoint: ep, Cluster: srv.MatterClusterID()}},
	}); err != nil {
		t.Fatal(err)
	}
	h.bridge.AttachSubscriptionManager(mgr)
	endpoint := h.bridge.Topology().FindByID(ep)
	before := endpoint.ClusterDataVersion(srv.MatterClusterID())

	srv.fire()
	if endpoint.ClusterDataVersion(srv.MatterClusterID()) != before {
		t.Error("an empty fire advanced the DataVersion")
	}
	srv.fire(1)
	if endpoint.ClusterDataVersion(srv.MatterClusterID()) == before {
		t.Error("a fire did not advance the DataVersion")
	}
	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	spy.mu.Lock()
	defer spy.mu.Unlock()
	var dirty []uint32
	for _, call := range spy.calls {
		for _, p := range call {
			if p.Cluster == srv.MatterClusterID() {
				dirty = append(dirty, p.Attribute)
			}
		}
	}
	if !slices.Equal(dirty, []uint32{1}) {
		t.Errorf("dirty attributes %v, want [1]", dirty)
	}
}

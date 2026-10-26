// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/store"
)

// notifyingServer is a bridged cluster server that notifies its own
// changes ([contract.ChangeNotifier]) and counts its live subscriptions.
type notifyingServer struct {
	id uint32

	mu     sync.Mutex
	value  uint8
	cbs    map[int]func()
	next   int
	active int
}

func newNotifyingServer(id uint32) *notifyingServer {
	return &notifyingServer{id: id, cbs: map[int]func(){}}
}

func (s *notifyingServer) MatterClusterID() uint32 { return s.id }

func (s *notifyingServer) MatterRead(attrID uint32) (any, bool) {
	if attrID != 0 {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, true
}

func (*notifyingServer) MatterWrite(context.Context, uint32, any) error         { return nil }
func (*notifyingServer) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (*notifyingServer) MatterReportable() []uint32                             { return []uint32{0} }

func (s *notifyingServer) OnMatterValueChanged(cb func()) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.next
	s.next++
	s.cbs[k] = cb
	s.active++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.cbs[k]; ok {
			delete(s.cbs, k)
			s.active--
		}
	}
}

// set changes the value and fires.
func (s *notifyingServer) set(v uint8) {
	s.mu.Lock()
	s.value = v
	cbs := make([]func(), 0, len(s.cbs))
	for _, cb := range s.cbs {
		cbs = append(cbs, cb)
	}
	s.mu.Unlock()
	for _, cb := range cbs {
		cb()
	}
}

func (s *notifyingServer) subscriptions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// selfServingSource is an endpoint source that is its own — and only —
// cluster server, and notifies.
type selfServingSource struct{ *notifyingServer }

func (selfServingSource) MatterDeviceType() uint16 { return 0x0073 }

func (s selfServingSource) MatterClusterServers() []contract.ClusterServer {
	return []contract.ClusterServer{s.notifyingServer}
}

// newSourceHarness is a secure harness over one bridged endpoint whose
// source is src.
func newSourceHarness(t *testing.T, src contract.EndpointSource) (h *secureHarness, ep uint16) {
	t.Helper()
	h = newSecureHarnessWith(t, func(*store.Store, uint8) (Snapshotter, []contract.ClusterServer) {
		asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
		if err != nil {
			t.Fatalf("endpoint.New: %v", err)
		}
		specs := []endpoint.Spec{{StableKey: endpoint.StringKey("dev"), DeviceType: src.MatterDeviceType(), FriendlyName: "Device", Source: src}}
		return func(ctx context.Context) (*endpoint.Topology, error) {
			return asm.Assemble(ctx, []endpoint.Snapshot{{Scope: "app", Endpoints: specs}})
		}, nil
	})
	h.allowAll()
	bridged := h.bridge.Topology().Bridged()
	if len(bridged) != 1 {
		t.Fatalf("assembled %d endpoints, want 1", len(bridged))
	}
	return h, bridged[0].ID
}

// subscribeTo attaches a subscription manager whose one subscription
// covers the cluster on ep and returns the spy it reports to.
func subscribeTo(t *testing.T, h *secureHarness, ep uint16, clusterID uint32) (*subscription.Manager, *reachAttrReporterSpy) {
	t.Helper()
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		FabricIndex: h.fabric, PeerNodeID: harnessControllerNodeID, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{{HasEndpoint: true, HasCluster: true, Endpoint: ep, Cluster: clusterID}},
	}); err != nil {
		t.Fatal(err)
	}
	h.bridge.AttachSubscriptionManager(mgr)
	return mgr, spy
}

// reported is every attribute of clusterID the spy saw.
func (s *reachAttrReporterSpy) reported(clusterID uint32) []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []uint32
	for _, call := range s.calls {
		for _, p := range call {
			if p.Cluster == clusterID {
				out = append(out, p.Attribute)
			}
		}
	}
	return out
}

// TestServerChangeNotifierIsHeardWithoutASourceNotifier: a mounted server
// that notifies its own change reaches a subscriber although the
// endpoint's source notifies nothing — matter.js reports each behavior's
// change through its own Datasource (Datasource.ts:776-796).
func TestServerChangeNotifierIsHeardWithoutASourceNotifier(t *testing.T) {
	t.Parallel()
	srv := newNotifyingServer(0xFFF1_FC02)
	h := newAppHarness(t, appDevice{deviceType: 0x0073, servers: []contract.ClusterServer{srv}})
	ep := h.endpoints[0x0073]
	mgr, spy := subscribeTo(t, h.secureHarness, ep, srv.id)
	endpointOf := h.bridge.Topology().FindByID(ep)
	before := endpointOf.ClusterDataVersion(srv.id)

	srv.set(7)
	if endpointOf.ClusterDataVersion(srv.id) == before {
		t.Error("the change did not advance the cluster's DataVersion")
	}
	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	if got := spy.reported(srv.id); !slices.Equal(got, []uint32{0}) {
		t.Errorf("reported attributes %v, want [0]", got)
	}
}

// TestSourceThatIsAServerIsSubscribedOnce: a source that is also its
// endpoint's cluster server is subscribed once, not again as a server.
func TestSourceThatIsAServerIsSubscribedOnce(t *testing.T) {
	t.Parallel()
	srv := newNotifyingServer(0xFFF1_FC03)
	h, ep := newSourceHarness(t, selfServingSource{srv})
	mgr, spy := subscribeTo(t, h, ep, srv.id)
	if n := srv.subscriptions(); n != 1 {
		t.Fatalf("the source is subscribed %d times, want 1", n)
	}
	srv.set(9)
	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	if got := spy.reported(srv.id); !slices.Equal(got, []uint32{0}) {
		t.Errorf("reported attributes %v, want [0]", got)
	}
}

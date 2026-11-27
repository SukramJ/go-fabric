// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/onoff"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
)

// TestAnnounceIgnoresTheReassemblyCallersRequest (R5-par-5106 =
// C5-589-n3): an endpoint a reassembly adds is announced to every
// subscriber whatever request the reassembly's caller runs under — a host
// that reassembles from a command handler passes the handler's ctx, which
// carries the invoking subject's access check. Here that check refuses
// every path; the subscriber still hears the new endpoint's OnOff. matter.js
// packages/node/src/node/integration/ProtocolService.ts:290-296 at
// db91886c takes the list from cluster.type.attributes and emits
// attrsChanged — no requester decides it.
func TestAnnounceIgnoresTheReassemblyCallersRequest(t *testing.T) {
	t.Parallel()
	srv, err := onoff.New(onoff.Config{})
	if err != nil {
		t.Fatal(err)
	}
	plug := &endpoint.Endpoint{ID: 5}
	plug.PublishClusterServers([]contract.ClusterServer{srv})
	var mu sync.Mutex
	withPlug := false
	snap := func(context.Context) (*endpoint.Topology, error) {
		mu.Lock()
		defer mu.Unlock()
		eps := []*endpoint.Endpoint{{ID: 0}, {ID: 2, Aggregator: true}}
		if withPlug {
			eps = append(eps, plug)
		}
		return &endpoint.Topology{Endpoints: eps}, nil
	}
	b := newStartedBridgeWithSnapshotter(t, snap)
	var reported []im.ConcreteAttributePath
	mgr := subscription.NewManager(subscription.Config{}, func(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
		mu.Lock()
		defer mu.Unlock()
		reported = append(reported, paths...)
	}, nil)
	b.AttachSubscriptionManager(mgr)
	onOff := im.ConcreteAttributePath{Endpoint: 5, Cluster: onoff.ClusterID, Attribute: onoff.AttrOnOff, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{onOff},
	}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	withPlug = true
	mu.Unlock()
	refusing := im.WithAuthority(t.Context(), func(uint16, uint32, uint8) im.StatusCode { return im.StatusUnsupportedAccess })
	if err := b.Reassemble(refusing); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	mgr.Tick(t.Context(), time.Now().Add(time.Minute))
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(reported, onOff) {
		t.Errorf("the added endpoint's OnOff was not announced under a caller's refusing access check (reported: %v)", reported)
	}
}

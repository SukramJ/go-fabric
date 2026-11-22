// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
)

// TestReassembleRemovingAnEndpointEndsNoSubscription: a Reassemble that
// removes an endpoint ends no subscription, the paths naming it select
// nothing from then on, and the subscription's other paths go on being
// reported. matter.js drops a removed endpoint from the protocol
// collections only (packages/node/src/node/integration/ProtocolService.ts
// :216-220 at db021da0, deleteEndpoint, called at :314); a subscription is
// closed by the peer, its session or its failed updates, never by the
// removal of an endpoint it names.
func TestReassembleRemovingAnEndpointEndsNoSubscription(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	withFive := true
	snap := func(context.Context) (*endpoint.Topology, error) {
		mu.Lock()
		defer mu.Unlock()
		eps := []*endpoint.Endpoint{
			{ID: 0},
			{ID: 2, Aggregator: true},
			{ID: 6, HasParentEndpointID: true, ParentEndpointID: 2},
		}
		if withFive {
			eps = append(eps, &endpoint.Endpoint{ID: 5, HasParentEndpointID: true, ParentEndpointID: 2})
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
	onOff := func(ep uint16) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Endpoint: ep, Cluster: 0x0006, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	sub, err := mgr.Subscribe(subscription.SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{onOff(5), onOff(6)},
	})
	if err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	withFive = false
	mu.Unlock()
	if err := b.Reassemble(t.Context()); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	if _, err := mgr.Get(sub.ID); err != nil {
		t.Fatalf("subscription ended by the removal of an endpoint it names: %v", err)
	}

	mgr.OnAttributeChanged(onOff(6))
	mgr.Tick(t.Context(), time.Now().Add(time.Minute))
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(reported, onOff(6)) {
		t.Errorf("a change on the remaining endpoint 6 was not reported (reported: %v)", reported)
	}
}

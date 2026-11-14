// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// TestReassembleMarksEveryChangedPartsList (review II, R2-P1-req2-13):
// after a reassembly Descriptor.PartsList is reported on the root, on the
// Aggregator wherever it sits — it is a role, not endpoint 1 — and on
// every endpoint whose parts changed; an endpoint whose parts did not
// change is not marked.
func TestReassembleMarksEveryChangedPartsList(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	withPart := false
	snap := func(context.Context) (*endpoint.Topology, error) {
		mu.Lock()
		defer mu.Unlock()
		eps := []*endpoint.Endpoint{
			{ID: 0},
			{ID: 2, Aggregator: true},
			{ID: 3, HasParentEndpointID: true, ParentEndpointID: 2},
			{ID: 5, HasParentEndpointID: true, ParentEndpointID: 2},
		}
		if withPart {
			eps = append(eps, &endpoint.Endpoint{ID: 4, Part: true, HasParentEndpointID: true, ParentEndpointID: 3})
		}
		return &endpoint.Topology{Endpoints: eps}, nil
	}
	b := newStartedBridgeWithSnapshotter(t, snap)
	var got []uint16
	mgr := subscription.NewManager(subscription.Config{}, func(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
		mu.Lock()
		defer mu.Unlock()
		for _, p := range paths {
			got = append(got, p.Endpoint)
		}
	}, nil)
	b.AttachSubscriptionManager(mgr)
	partsList := im.ConcreteAttributePath{Cluster: 0x001D, Attribute: 0x0003, HasCluster: true, HasAttribute: true}
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{partsList}}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	withPart = true
	mu.Unlock()
	if err := b.Reassemble(t.Context()); err != nil {
		t.Fatalf("Reassemble: %v", err)
	}
	mgr.Tick(t.Context(), time.Now().Add(time.Minute))
	mu.Lock()
	defer mu.Unlock()
	slices.Sort(got)
	got = slices.Compact(got)
	for _, want := range []uint16{0, 2, 3} {
		if !slices.Contains(got, want) {
			t.Errorf("PartsList of endpoint %d not reported after its parts changed (reported: %v)", want, got)
		}
	}
	if slices.Contains(got, 5) {
		t.Errorf("PartsList of endpoint 5 reported though its parts did not change (reported: %v)", got)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"reflect"
	"testing"

	"thingfabric.dev/go-fabric/im"
)

// TestTakeDirtyOrderFollowsThePrimingReport (review II, R2-P4-imread-10):
// the changed paths of one report come in the priming report's order —
// by the first request path that selects them, then ascending endpoint
// and cluster, then a cluster's attribute order ([im.CompareAttributeIDs]) —
// every time, whatever order they changed in. Mirrors matter.js
// packages/node/src/node/server/ServerSubscription.ts:860-868 at db021da0,
// which runs the subscription's request through AttributeSubscriptionResponse
// with the changed paths as a filter: the request's paths in order
// (AttributeReadResponse.ts:88-100), each walked as the priming report walks it.
func TestTakeDirtyOrderFollowsThePrimingReport(t *testing.T) {
	t.Parallel()
	at := func(ep uint16, cluster, attr uint32) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	want := []im.ConcreteAttributePath{
		// First request path: endpoint 3's ColorControl.
		at(3, 0x0300, 0x0000), at(3, 0x0300, 0x0001), at(3, 0x0300, 0x0002),
		// Second request path, the full wildcard: the rest, ascending.
		at(1, 0x0006, 0x0000), at(2, 0x0006, 0x0000), at(2, 0x0008, 0x0000), at(3, 0x0006, 0x0000),
	}
	m := newTestManager(Config{})
	sub, err := m.Subscribe(SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{{Endpoint: 3, Cluster: 0x0300, HasEndpoint: true, HasCluster: true}, {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for round := range 20 {
		for i := len(want) - 1; i >= 0; i-- {
			sub.markDirty(want[(i+round)%len(want)])
		}
		sub.mu.Lock()
		got := sub.takeDirtyLocked()
		sub.mu.Unlock()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d: changed paths in order %v, want %v", round, got, want)
		}
	}
}

// BenchmarkTakeDirtyOfALargeSubscription is the cost guard of review II
// R2-P6-bound-0507: 200 changed paths of a subscription with 10 000
// concrete request paths are sorted under s.mu, which every markDirty
// waits on. Each path's request-path index is taken once per drain; a
// scan per comparison cost about 28 ms a drain.
func BenchmarkTakeDirtyOfALargeSubscription(b *testing.B) {
	paths := make([]im.ConcreteAttributePath, 10_000)
	for i := range paths {
		paths[i] = im.ConcreteAttributePath{Endpoint: uint16(i / 100), Cluster: 6, Attribute: uint32(i % 100), HasEndpoint: true, HasCluster: true, HasAttribute: true} //nolint:gosec // i < 10 000
	}
	sub := &Subscription{AttributePaths: paths}
	for b.Loop() {
		for i := range 200 {
			sub.markDirty(paths[len(paths)-1-i*7])
		}
		sub.mu.Lock()
		_ = sub.takeDirtyLocked()
		sub.mu.Unlock()
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/transport/message"
)

// TestKeepSubscriptionsFalseTearsDownOnlyWithAFabric: a SubscribeRequest
// with KeepSubscriptions=false cancels the peer's subscriptions on a
// session with a fabric, and nothing on a session without one (PASE).
// Mirrors matter.js packages/node/src/node/server/InteractionServer.ts:647
// at db021da0, `if (fabric !== undefined && !keepSubscriptions)`; the walk
// over the peer's sessions is :648-658.
func TestKeepSubscriptionsFalseTearsDownOnlyWithAFabric(t *testing.T) {
	t.Parallel()
	path := im.ConcreteAttributePath{HasEndpoint: true, Endpoint: 0, HasCluster: true, Cluster: 0x001D, HasAttribute: true, Attribute: 0}
	for _, tc := range []struct {
		name     string
		fabric   uint8
		peer     uint64
		tornDown bool
	}{
		{"a session with a fabric", 1, 0xAAAA, true},
		{"a session without a fabric", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const sess uint16 = 12
			b := newStartedBridge(t)
			lookup := identityTestLookup{fabric: map[uint16]uint8{sess: tc.fabric}, subject: map[uint16]uint64{}}
			if tc.peer != 0 {
				lookup.subject[sess] = tc.peer
			}
			b.AttachSessionLookup(lookup)
			mgr := subscription.NewManager(subscription.Config{}, nil, nil)
			b.AttachSubscriptionManager(mgr)
			earlier, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: tc.fabric, PeerNodeID: tc.peer, SessionID: sess, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{path}})
			if err != nil {
				t.Fatal(err)
			}

			b.teardownForSubscribe(loopbackSrc(), &message.Header{SessionID: sess}, im.SubscribeRequest{KeepSubscriptions: false, MaxIntervalCeiling: 60, AttributeRequests: []im.ConcreteAttributePath{path}})

			_, getErr := mgr.Get(earlier.ID)
			if gone := getErr != nil; gone != tc.tornDown {
				t.Fatalf("earlier subscription gone=%v after KeepSubscriptions=false, want %v", gone, tc.tornDown)
			}
		})
	}
}

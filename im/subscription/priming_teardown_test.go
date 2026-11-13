// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"testing"
	"time"
)

// TestKeepSubscriptionsFalseLeavesAPrimingSubscription: the teardown a
// SubscribeRequest with KeepSubscriptions=false runs — ClosePeer, and the
// replacement of the session's subscriptions in Subscribe — ends the
// peer's established subscriptions, not one still priming. matter.js adds
// a subscription to its session only when it activates, after its
// SubscribeResponse (ServerSubscription.ts:411-421 at db021da0), and the
// teardown walks the sessions' subscriptions (InteractionServer.ts:647-662):
// a controller that subscribes twice at once keeps the first.
func TestKeepSubscriptionsFalseLeavesAPrimingSubscription(t *testing.T) {
	t.Parallel()
	m := NewManager(Config{}, nil, nil)
	established, err := m.Subscribe(SubscribeArgs{FabricIndex: 1, PeerNodeID: 0xABCD, SessionID: 7, MaxIntervalCeiling: 60})
	if err != nil {
		t.Fatal(err)
	}
	priming, err := m.Subscribe(SubscribeArgs{FabricIndex: 1, PeerNodeID: 0xABCD, SessionID: 7, MaxIntervalCeiling: 60, Priming: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := m.ClosePeer(1, 0xABCD); n != 1 {
		t.Errorf("ClosePeer cleared %d subscriptions, want the established one", n)
	}
	if _, err := m.Get(priming.ID); err != nil {
		t.Fatalf("ClosePeer closed the subscription still priming: %v", err)
	}
	if _, err := m.Get(established.ID); err == nil {
		t.Error("ClosePeer left the established subscription")
	}

	priming.EndPriming(time.Now())
	second, err := m.Subscribe(SubscribeArgs{FabricIndex: 1, PeerNodeID: 0xABCD, SessionID: 7, MaxIntervalCeiling: 60, Priming: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Subscribe(SubscribeArgs{FabricIndex: 1, PeerNodeID: 0xABCD, SessionID: 7, MaxIntervalCeiling: 60, Priming: true, ReplaceSessionDuplicate: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(second.ID); err != nil {
		t.Fatalf("the session's replacement closed a subscription still priming: %v", err)
	}
	if _, err := m.Get(priming.ID); err == nil {
		t.Error("the session's replacement left the established subscription")
	}
}

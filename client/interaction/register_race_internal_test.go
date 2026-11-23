// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"log/slog"
	"testing"

	"thingfabric.dev/go-fabric/client/exchange"
)

// TestRegisterCloseBeforeAddLeavesNothingRegistered: a Close landing
// between register's release of closeMu and add — the interaction's Close
// here, which snapshots the established set and closes s — leaves s in
// neither the peer index nor the active set, for each add register is
// given (review II, R2-C2-conc-2101).
func TestRegisterCloseBeforeAddLeavesNothingRegistered(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.DiscardHandler)
	peer := exchange.PeerAddress{FabricIndex: 1, NodeID: 7}
	for _, tc := range []struct {
		name string
		add  func(r *Subscriptions) func(*Subscription)
	}{
		{"peer", func(r *Subscriptions) func(*Subscription) { return r.AddPeer }},
		{"active", func(r *Subscriptions) func(*Subscription) { return r.addActive }},
		{"peer and active", func(r *Subscriptions) func(*Subscription) {
			return func(s *Subscription) { r.AddPeer(s); r.addActive(s) }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := NewSubscriptions(SubscriptionsConfig{Logger: log})
			c := Bind(nil, Config{Subscriptions: r, Logger: log})
			sub := r.newSubscription(peer, SubscribeRequest{}, log)
			sub.id = 5
			add := tc.add(r)
			if err := c.register(sub, func(s *Subscription) {
				c.Close(nil)
				add(s)
			}); err != nil {
				t.Fatalf("register: %v", err)
			}
			if !sub.isClosedNow() {
				t.Fatal("the interaction's Close did not close the subscription")
			}
			if r.GetPeer(peer, 5) != nil {
				t.Fatal("a closed subscription stays registered under (peer, id)")
			}
			for _, s := range r.Active() {
				if s == sub {
					t.Fatal("a closed subscription stays in the active set")
				}
			}
		})
	}
}

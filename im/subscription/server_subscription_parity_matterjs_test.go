// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

// A port of matter.js packages/node/test/node/ServerSubscriptionTest.ts at
// 862ad75f, the cases of the subscription's own state: which endpoints it
// selects by its event paths, and close (terminating) against a close that
// does not terminate. matter.js's closeForShutdown — the node going offline,
// the subscription kept for resumption — is Manager.Release in Go; close is
// Manager.Close. The wire cases are in node/server_subscription_parity_matterjs_test.go.

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

func TestServerSubscriptionParity(t *testing.T) {
	t.Parallel()

	t.Run("selects endpoints by its event paths when it has no attribute paths", func(t *testing.T) {
		t.Parallel()
		m := NewManager(Config{}, nil, nil)
		sub, err := m.Subscribe(SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
			EventPaths: []im.ConcreteEventPath{{Endpoint: 0, Cluster: 0x0028, HasEndpoint: true, HasCluster: true}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if sub.selectsAnyEndpoint(map[uint16]bool{1: true}) {
			t.Fatal("selects endpoint 1")
		}
		if !sub.selectsAnyEndpoint(map[uint16]bool{0: true}) {
			t.Fatal("does not select endpoint 0")
		}
	})

	t.Run("terminates on close() but not on closeForShutdown()", func(t *testing.T) {
		t.Parallel()
		m := NewManager(Config{}, nil, nil)
		terminated := map[uint32]bool{}
		m.SetOnSubscriptionTerminated(func(id uint32) { terminated[id] = true })
		args := SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60, KeepSubscriptions: true}
		closed, err := m.Subscribe(args)
		if err != nil {
			t.Fatal(err)
		}
		shutDown, err := m.Subscribe(args)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Close(closed.ID); err != nil {
			t.Fatal(err)
		}
		if err := m.Release(shutDown.ID); err != nil {
			t.Fatal(err)
		}
		if !terminated[closed.ID] || terminated[shutDown.ID] {
			t.Fatalf("terminated %v, want only the closed subscription %d", terminated, closed.ID)
		}
		if !closed.IsClosed() || !shutDown.IsClosed() {
			t.Fatal("a subscription stayed open")
		}
	})
}

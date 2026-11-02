// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/mdns"
)

// subscribeOn opens a subscription of fabricIndex on sessionID in m.
func subscribeOn(t *testing.T, m *subscription.Manager, fabricIndex uint8, peerNodeID uint64, sessionID uint16) *subscription.Subscription {
	t.Helper()
	sub, err := m.Subscribe(subscription.SubscribeArgs{
		FabricIndex: fabricIndex, PeerNodeID: peerNodeID, SessionID: sessionID,
		MinIntervalFloor: 1, MaxIntervalCeiling: 60, KeepSubscriptions: true,
		AttributePaths: []im.ConcreteAttributePath{{HasEndpoint: true, Endpoint: 1}},
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	return sub
}

// TestSubscriptionTerminated_StartsTheFabricsReconnect pins matter.js
// packages/protocol/src/protocol/DeviceAdvertiser.ts:174-179 at 862ad75f:
// a subscription that ends for good (isTerminated) starts the "reconnect"
// broadcast of its session's fabric — the peer's error status
// (closeSubscriptionByExchange) and the ack pump's abandonment
// (closeSubscriptionByCounter) each start one for that fabric's
// operational service only. A released subscription (Release, not
// terminated) starts none, and neither does the CloseFabric of a fabric
// whose service was already withdrawn.
func TestSubscriptionTerminated_StartsTheFabricsReconnect(t *testing.T) {
	t.Parallel()
	op := func(instance string) []string { return []string{instance + "|" + mdns.ServiceTypeOperational} }

	t.Run("closed by the peer's error status", func(t *testing.T) {
		t.Parallel()
		b, adv, _, op2 := startedEventBridge(t)
		m := subscription.NewManager(subscription.Config{}, nil, nil)
		b.AttachSubscriptionManager(m)
		sub := subscribeOn(t, m, 2, 0xC2, 0x20)
		b.rememberReportExchange(sub.ID, 0x20, 5)
		b.closeSubscriptionByExchange(0x20, 5, im.StatusFailure)
		b.reannounces.Wait()
		if d, _ := adv.events(); !slices.Equal(d, op(op2)) {
			t.Errorf("disconnected events %v, want %v", d, op(op2))
		}
	})

	t.Run("abandoned by the ack pump", func(t *testing.T) {
		t.Parallel()
		b, adv, op1, _ := startedEventBridge(t)
		m := subscription.NewManager(subscription.Config{}, nil, nil)
		b.AttachSubscriptionManager(m)
		sub := subscribeOn(t, m, 1, 0xC1, 0x10)
		b.reportCounterOwner.Store(reportCounterKey(0x10, 77), sub.ID)
		b.closeSubscriptionByCounter(0x10, 77)
		b.reannounces.Wait()
		if d, _ := adv.events(); !slices.Equal(d, op(op1)) {
			t.Errorf("disconnected events %v, want %v", d, op(op1))
		}
	})

	t.Run("released is not terminated", func(t *testing.T) {
		t.Parallel()
		b, adv, _, _ := startedEventBridge(t)
		m := subscription.NewManager(subscription.Config{}, nil, nil)
		b.AttachSubscriptionManager(m)
		sub := subscribeOn(t, m, 1, 0xC1, 0x10)
		if err := m.Release(sub.ID); err != nil {
			t.Fatalf("Release: %v", err)
		}
		b.reannounces.Wait()
		if d, _ := adv.events(); len(d) != 0 {
			t.Errorf("disconnected events %v, want none", d)
		}
	})

	t.Run("a removed fabric has no service left", func(t *testing.T) {
		t.Parallel()
		b, adv, _, _ := startedEventBridge(t)
		m := subscription.NewManager(subscription.Config{}, nil, nil)
		b.AttachSubscriptionManager(m)
		subscribeOn(t, m, 2, 0xC2, 0x20)
		b.WithdrawFabric(t.Context(), 2, [8]byte{0x02}, 0x22)
		m.CloseFabric(2)
		b.reannounces.Wait()
		if d, _ := adv.events(); len(d) != 0 {
			t.Errorf("disconnected events %v, want none", d)
		}
	})
}

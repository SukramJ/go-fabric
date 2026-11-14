// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// TestStopFlushesTheSubscriptionsBeforeTheSessionsEnd (review II,
// R2-P2-sess-03): Stop reports what every subscription owes before the
// sessions end, and the sessions' end closes the subscriptions without
// terminating them, so they stay persisted for the re-establishment on the
// next start (ADR 0008). The subscription engine is stopped first, as
// Node.Stop does. Mirrors matter.js's shutdown at db021da0:
// SessionManager.ts:1097-1113 closeAllSessions runs each session's
// closeSubscriptionsForShutdown (NodeSession.ts:326-329, a flush through the
// session, closeForShutdown leaving the subscription not terminated,
// ServerSubscription.ts:772-774) before its initiateForceClose.
func TestStopFlushesTheSubscriptionsBeforeTheSessionsEnd(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	opMgr := operational.NewManager(nil)
	b.AttachSessionRegistry(opMgr)
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)

	var (
		mu         sync.Mutex
		steps      []string
		terminated []uint32
	)
	record := func(step string) {
		mu.Lock()
		steps = append(steps, step)
		mu.Unlock()
	}
	opMgr.SetOnSessionClose(func(sessionID uint16) {
		record("session closed")
		mgr.CloseSession(sessionID)
	})
	mgr.SetOnSubscriptionTerminated(func(id uint32, _ uint8) {
		mu.Lock()
		terminated = append(terminated, id)
		mu.Unlock()
	})

	fabrics := []uint8{1, 2}
	subIDs := make([]uint32, 0, len(fabrics))
	path := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: 0, Cluster: 0x0028, Attribute: 0x0005}
	for i, fabric := range fabrics {
		entry, err := opMgr.OpenFromPase(0xB0B, uint64(0xA10+i), uint16(0x4320+i), []byte("stop-flush-shared-secret"))
		if err != nil {
			t.Fatalf("OpenFromPase: %v", err)
		}
		sub, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: fabric, PeerNodeID: uint64(0xA10 + i), SessionID: entry.SessionID, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{path}})
		if err != nil {
			t.Fatal(err)
		}
		subIDs = append(subIDs, sub.ID)
	}
	mgr.OnAttributeChanged(path)
	flushed := map[uint32]bool{}
	mgr.SetFlushReporter(func(_ context.Context, sub *subscription.Subscription, _ []im.EventReport, paths []im.ConcreteAttributePath) {
		mu.Lock()
		flushed[sub.ID] = len(paths) > 0
		mu.Unlock()
		record("flush")
	})
	mgr.Stop()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := b.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, id := range subIDs {
		if !flushed[id] {
			t.Errorf("subscription %d was not flushed with its owed attribute at Stop", id)
		}
	}
	want := []string{"flush", "flush", "session closed", "session closed"}
	if !slices.Equal(steps, want) {
		t.Errorf("Stop ran %v, want %v: every flush before the sessions end", steps, want)
	}
	if len(terminated) != 0 {
		t.Errorf("Stop terminated subscriptions %v, want none: a shutdown keeps them for re-establishment", terminated)
	}
}

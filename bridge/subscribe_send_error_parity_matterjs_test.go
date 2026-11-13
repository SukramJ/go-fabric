// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// A failed ongoing report owes the subscriber the same data again: matter.js
// packages/node/src/node/server/ServerSubscription.ts:566-581 puts the
// attribute filter back and restores #outstandingEventsMinNumber while
// #sendUpdateErrorCounter <= 2 (review 2026-11 W2-conc-01). A report that
// carried both owes both again (review II, W2-conc-04).

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

func TestFailedReportRetriesTheSameDataParity(t *testing.T) {
	t.Parallel()
	attrPath := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: 1, Cluster: 0x0006, Attribute: 0x0000}
	// The subscriber's isUrgent flag: its events ask for a report.
	eventPath := im.ConcreteEventPath{HasEndpoint: true, HasCluster: true, HasEvent: true, Endpoint: 1, Cluster: 0x003B, Event: 0x01, IsUrgent: true}

	type rig struct {
		b      *Bridge
		m      *subscription.Manager
		sub    *subscription.Subscription
		mu     sync.Mutex
		paths  [][]im.ConcreteAttributePath
		events [][]im.EventReport
	}
	setup := func(t *testing.T, sessionID uint16) *rig {
		t.Helper()
		r := &rig{b: newStartedBridge(t)}
		_, _, peerAddr := chunkTestPeer(t, r.b, sessionID)
		r.m = subscription.NewManager(subscription.Config{}, func(ctx context.Context, sub *subscription.Subscription, paths []im.ConcreteAttributePath) {
			r.mu.Lock()
			r.paths = append(r.paths, paths)
			r.mu.Unlock()
			r.b.reportSubscription(ctx, sub, paths)
		}, nil)
		r.m.SetEventReporter(func(ctx context.Context, sub *subscription.Subscription, events []im.EventReport) {
			r.mu.Lock()
			r.events = append(r.events, events)
			r.mu.Unlock()
			r.b.reportSubscriptionEvents(ctx, sub, events)
		})
		r.b.AttachSubscriptionManager(r.m)
		sub, err := r.m.Subscribe(subscription.SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 0xBBBB4444, SessionID: sessionID,
			MaxIntervalCeiling: 60,
			AttributePaths:     []im.ConcreteAttributePath{attrPath},
			EventPaths:         []im.ConcreteEventPath{eventPath},
		})
		if err != nil {
			t.Fatal(err)
		}
		r.sub = sub
		// Routed to a session the lookup does not know: every send fails
		// before a datagram leaves.
		r.b.routing.subTargets.Store(sub.ID, subTarget{
			src: peerAddr, hasPeerSourceNodeID: true, peerSourceNodeID: 0xBBBB4444,
			exchangeID: 21, sessionID: sessionID + 100, peerInitiator: true, pase: true,
		})
		return r
	}

	t.Run("attribute paths", func(t *testing.T) {
		t.Parallel()
		r := setup(t, 75)
		t0 := time.Now()
		r.m.OnAttributeChanged(attrPath)
		r.m.Tick(context.Background(), t0)
		r.m.Tick(context.Background(), t0.Add(time.Second))
		r.mu.Lock()
		defer r.mu.Unlock()
		if len(r.paths) != 2 {
			t.Fatalf("reporter called %d times, want 2: the failed report's paths are not retried", len(r.paths))
		}
		if len(r.paths[1]) != 1 || r.paths[1][0] != attrPath {
			t.Errorf("retry reported %v, want [%v]", r.paths[1], attrPath)
		}
	})

	t.Run("events", func(t *testing.T) {
		t.Parallel()
		r := setup(t, 76)
		t0 := time.Now()
		r.b.EmitEvent(1, 0x003B, 0x01, uint8(1), contract.EventPriority(im.EventPriorityInfo))
		r.m.Tick(context.Background(), t0)
		r.m.Tick(context.Background(), t0.Add(time.Second))
		r.mu.Lock()
		defer r.mu.Unlock()
		if len(r.events) != 2 {
			t.Fatalf("event reporter called %d times, want 2: the failed report's events are not retried", len(r.events))
		}
		if len(r.events[1]) != 1 || r.events[1][0].Number != r.events[0][0].Number {
			t.Errorf("retry reported %+v, want event %d again", r.events[1], r.events[0][0].Number)
		}
	})

	t.Run("an event and an attribute path in one report", func(t *testing.T) {
		t.Parallel()
		r := setup(t, 77)
		t0 := time.Now()
		r.m.OnAttributeChanged(attrPath)
		r.b.EmitEvent(1, 0x003B, 0x01, uint8(1), contract.EventPriority(im.EventPriorityInfo))
		r.m.Tick(context.Background(), t0)
		// The failed report took the path with its event; both are owed
		// again and leave together in the retry.
		r.m.Tick(context.Background(), t0.Add(time.Second))
		// What the failed retry owes again, read through a flush.
		var owedEvents []im.EventReport
		var owedPaths []im.ConcreteAttributePath
		r.m.SetFlushReporter(func(_ context.Context, _ *subscription.Subscription, events []im.EventReport, paths []im.ConcreteAttributePath) {
			owedEvents, owedPaths = events, paths
		})
		r.m.FlushFabric(context.Background(), 1, 0)
		r.mu.Lock()
		defer r.mu.Unlock()
		if len(owedEvents) != 1 || len(owedPaths) != 1 || owedPaths[0] != attrPath {
			t.Fatalf("owed after the failed retry: events %+v, paths %+v; want the event and %+v", owedEvents, owedPaths, attrPath)
		}
		if len(r.events) != 2 || len(r.paths) != 0 {
			t.Fatalf("event reporter called %d times, attribute reporter %d times; want 2 and 0: one report carries both, and so does its retry", len(r.events), len(r.paths))
		}
		if len(r.events[1]) != 1 || r.events[1][0].Number != r.events[0][0].Number {
			t.Errorf("retry reported %+v, want event %d again", r.events[1], r.events[0][0].Number)
		}
		if r.sub.TakeDuePaths() != nil {
			t.Error("paths left on the subscription after the reports")
		}
	})
}

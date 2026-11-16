// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// A report whose events are all filtered for the subscriber — the access
// check, or a fabric-sensitive event of another fabric — goes out empty
// and counts as the subscription's report, as an attribute report whose
// changes are all filtered does: one rule (owner decision 169, which
// replaced decision 145; review II, F-db021da0-8). Before decision 145's
// rule, the pass went to the event reporter, which sent nothing, and
// counted as the subscription's last report, so the keep-alive of the send
// interval never left (R2-P0-rv-A03); after decision 145 it sent nothing
// and did not count.
//
// matter.js at db021da0 sends a DataReport in that pass, an empty one:
// #sendUpdateMessage takes the data branch whenever an attribute filter or
// an event number is outstanding
// (packages/node/src/node/server/ServerSubscription.ts:909, :921-936),
// with `suppressEmptyReport: onlyWithData` — false on every pass but a
// flush's and a follow-up's without a keep-alive owed (:520, :622, :757) —
// and the messenger sends the report unless that flag is set and the
// report is empty (packages/protocol/src/interaction/InteractionMessenger.ts:794-796).
// connectedhomeip at 6170af84 sends the empty ReportData for a dirtied
// subscription whose pass encodes nothing and restarts the min and the max
// interval (src/app/reporting/Engine.cpp:818-853, src/app/ReadHandler.cpp:352-362,
// src/app/reporting/ReportScheduler.h:141-147).

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

func TestReportWhoseEventsAreAllFilteredGoesOutEmpty(t *testing.T) {
	t.Parallel()
	const switchCluster, initialPress = 0x003B, 0x01
	// A CASE subscriber of fabric 1 that no ACL entry grants anything.
	rig := func(t *testing.T, sessionID uint16, eventPath im.ConcreteEventPath) (m *subscription.Manager, sub *subscription.Subscription, next func(what string) im.ReportData, t0 time.Time) {
		t.Helper()
		b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
		peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)
		m = subscription.NewManager(subscription.Config{}, b.SubscriptionReporter(), nil)
		m.SetEventReporter(b.SubscriptionEventReporter())
		b.AttachSubscriptionManager(m)
		sub, err := m.Subscribe(subscription.SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 0xBBBB5555, SessionID: sessionID,
			MinIntervalFloor: 1, MaxIntervalCeiling: 60,
			EventPaths: []im.ConcreteEventPath{eventPath},
		})
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		target := subTarget{
			fabricIndex: 1, subjectNodeID: 0xBBBB5555,
			src: peerAddr, hasPeerSourceNodeID: true, peerSourceNodeID: 0xBBBB5555,
			exchangeID: 31, sessionID: sessionID, peerInitiator: true,
		}
		b.routing.subTargets.Store(sub.ID, target)
		t0 = time.Now()
		sub.TouchLastReport(t0)
		b.EmitEvent(1, switchCluster, initialPress, uint8(1), contract.EventPriority(im.EventPriorityInfo))
		// The test's premise: the subscriber may not read the event.
		owed := []im.EventReport{{Path: im.ConcreteEventPath{
			HasEndpoint: true, HasCluster: true, HasEvent: true, Endpoint: 1, Cluster: switchCluster, Event: initialPress,
		}, Number: 1}}
		if left := b.authorizedEventReports(context.Background(), target, owed); len(left) != 0 {
			t.Fatal("the event passes the subscriber's access check: the test filters nothing")
		}
		next = func(what string) im.ReportData {
			t.Helper()
			report, ok := readReportData(t, peerConn, peerSess, 3*time.Second)
			if !ok {
				t.Fatalf("%s: nothing reached the subscriber", what)
			}
			if !report.HasSubscription || report.SubscriptionID != sub.ID {
				t.Fatalf("%s: a report of subscription %d (present %v), want %d", what, report.SubscriptionID, report.HasSubscription, sub.ID)
			}
			if len(report.Reports) != 0 || len(report.EventReports) != 0 {
				t.Fatalf("%s: %d attribute and %d event reports reached a subscriber that may read neither", what, len(report.Reports), len(report.EventReports))
			}
			return report
		}
		return m, sub, next, t0
	}

	t.Run("at the send interval, with a non-urgent event owed, the keep-alive is sent", func(t *testing.T) {
		t.Parallel()
		m, sub, next, t0 := rig(t, 81, im.ConcreteEventPath{})
		m.Tick(context.Background(), t0.Add(sub.SendInterval()))
		next("the keep-alive at the send interval")
	})

	t.Run("an urgent event's report goes out empty and counts", func(t *testing.T) {
		t.Parallel()
		m, sub, next, t0 := rig(t, 82, im.ConcreteEventPath{IsUrgent: true})
		// The floor ends: the urgent event's report is due, and goes out
		// empty for this subscriber.
		m.Tick(context.Background(), t0.Add(time.Second))
		next("the urgent event's report, empty")
		// The keep-alive follows a send interval after it.
		m.Tick(context.Background(), t0.Add(time.Second).Add(sub.SendInterval()))
		next("the keep-alive a send interval after the empty report")
	})
}

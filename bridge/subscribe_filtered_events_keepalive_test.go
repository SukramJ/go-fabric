// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// A report whose events are all filtered for the subscriber — the access
// check, or a fabric-sensitive event of another fabric — is no report: it
// sends nothing and does not move the subscription's clock, and the
// keep-alive that is due with it is sent all the same (review II,
// R2-P0-rv-A03). Before, the pass went to the event reporter, which sent
// nothing, and counted as the subscription's last report: with a
// non-urgent event owed, the keep-alive of the send interval never left,
// and the subscriber heard nothing for two send intervals — 96 s on a
// 60 s max interval.
//
// matter.js at db021da0 sends a DataReport in that pass, an empty one:
// #sendUpdateMessage takes the data branch whenever an attribute filter or
// an event number is outstanding
// (packages/node/src/node/server/ServerSubscription.ts:909, :921-936),
// with `suppressEmptyReport: onlyWithData` — false on every pass but a
// flush's and a follow-up's without a keep-alive owed (:520, :622, :757) —
// and the messenger sends the report unless that flag is set and the
// report is empty (packages/protocol/src/interaction/InteractionMessenger.ts:794-796);
// its own TODO names the case (ServerSubscription.ts:923-925). This module
// sends the keep-alive when one is due and nothing otherwise.

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

func TestReportWhoseEventsAreAllFilteredLeavesTheKeepAliveDue(t *testing.T) {
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

	t.Run("an urgent event's report that sends nothing does not postpone the keep-alive", func(t *testing.T) {
		t.Parallel()
		m, sub, next, t0 := rig(t, 82, im.ConcreteEventPath{IsUrgent: true})
		// The floor ends: the urgent event's report is due, and comes to
		// nothing for this subscriber.
		m.Tick(context.Background(), t0.Add(time.Second))
		// Counted as a report, it would put the keep-alive a send interval
		// after itself; it is due a send interval after the last report
		// that was sent.
		m.Tick(context.Background(), t0.Add(sub.SendInterval()))
		next("the keep-alive a send interval after the last report sent")
	})
}

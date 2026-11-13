// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// reportRecordsOf counts the bookkeeping an ongoing report leaves for
// subID: its report exchange (both directions) and its report counters.
func reportRecordsOf(b *Bridge, subID uint32) (exchanges, owners, counters int) {
	if _, ok := b.subReportExchange.Load(subID); ok {
		exchanges++
	}
	b.reportExchangeOwner.Range(func(_, v any) bool {
		if owner, ok := v.(uint32); ok && owner == subID {
			owners++
		}
		return true
	})
	b.reportCounterOwner.Range(func(_, v any) bool {
		if owner, ok := v.(uint32); ok && owner == subID {
			counters++
		}
		return true
	})
	return exchanges, owners, counters
}

// TestReportOfAClosedSubscriptionLeavesNoRecords pins the end of a report
// that lost the race with its subscription's close (review II,
// W2-conc-11). A reporter loads the routing target once and records the
// report's exchange and counters after the send; a close landing in
// between has run its release (releaseSubscriptionRouting) already, and
// nothing forgot the records the reporter then wrote. The reporters now
// look at the subscription once more after their stores. The interleaving
// is laid out by hand here: the subscription is closed, and the target a
// reporter would have loaded before the close is put back.
func TestReportOfAClosedSubscriptionLeavesNoRecords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		report func(b *Bridge, sub *subscription.Subscription, paths []im.ConcreteAttributePath)
	}{
		{"attributes", func(b *Bridge, sub *subscription.Subscription, paths []im.ConcreteAttributePath) {
			b.reportSubscription(context.Background(), sub, paths)
		}},
		{"events", func(b *Bridge, sub *subscription.Subscription, _ []im.ConcreteAttributePath) {
			b.reportSubscriptionEvents(context.Background(), sub, oneEventReport())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const sessionID uint16 = 94
			b := newStartedBridge(t)
			b.AttachAckTracker(mrp.NewAckTracker(0))
			mgr := subscription.NewManager(subscription.Config{}, nil, nil)
			b.AttachSubscriptionManager(mgr)
			peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

			paths := []im.ConcreteAttributePath{
				{Endpoint: 0, Cluster: 0x001D, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true},
			}
			sub, err := mgr.Subscribe(subscription.SubscribeArgs{
				SessionID: sessionID, MinIntervalFloor: 1, MaxIntervalCeiling: 60, AttributePaths: paths,
			})
			if err != nil {
				t.Fatalf("Subscribe: %v", err)
			}
			if err := mgr.Close(sub.ID); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if !sub.IsClosed() {
				t.Fatal("the subscription is not closed")
			}
			// The target the reporter loaded before the close.
			b.routing.subTargets.Store(sub.ID, subTarget{
				pase: true, // a PASE subscriber: the report is not ACL-gated
				src:  peerAddr, hasPeerSourceNodeID: true, peerSourceNodeID: 0xBBBB4444,
				exchangeID: 24, sessionID: sessionID, peerInitiator: true,
			})

			tc.report(b, sub, paths)
			if counts, _ := drainReportDataChunks(t, peerConn, peerSess); len(counts) == 0 {
				t.Fatal("no report reached the peer; the test needs a report that was sent")
			}
			if exchanges, owners, counters := reportRecordsOf(b, sub.ID); exchanges+owners+counters != 0 {
				t.Errorf("records of a closed subscription's report: subReportExchange %d, reportExchangeOwner %d, reportCounterOwner %d — want none",
					exchanges, owners, counters)
			}
			if _, ok := b.routing.subTargets.Load(sub.ID); ok {
				t.Error("the routing target of a closed subscription is still stored")
			}
		})
	}
}

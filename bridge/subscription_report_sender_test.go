// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// The single report hook (review II, R2-P6-api-0508, W2-conc-04, owner
// decision 138): attaching a manager is the whole wiring. The manager the
// host built without a reporter reports events, attribute changes and the
// fabric's flush through the bridge's sender, which
// [Bridge.AttachSubscriptionManager] installs; before it, a host that
// forgot SetEventReporter / SetFlushReporter lost every event and every
// flush without a word.

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

func TestAttachedManagerReportsWithoutAFurtherHook(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 83
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

	var attrPath im.ConcreteAttributePath
	for _, ep := range b.Topology().Endpoints {
		if ep != nil && !ep.IsRoot() && !ep.IsAggregator() {
			if paths := endpointpkg.ReportablePaths(ep); len(paths) > 0 {
				attrPath = paths[0]
				break
			}
		}
	}
	if !attrPath.HasAttribute {
		t.Fatal("the topology has no reportable attribute")
	}

	// No reporter, no SetEventReporter, no SetFlushReporter: the attach
	// is the wiring.
	m := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(m)
	sub, err := m.Subscribe(subscription.SubscribeArgs{
		FabricIndex: 1, PeerNodeID: 0xBBBB5555, SessionID: sessionID,
		MinIntervalFloor: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{attrPath},
		EventPaths:     []im.ConcreteEventPath{{IsUrgent: true}},
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	b.routing.subTargets.Store(sub.ID, subTarget{
		pase: true, // a PASE subscriber: the report is not ACL-gated
		src:  peerAddr, hasPeerSourceNodeID: true, peerSourceNodeID: 0xBBBB5555,
		exchangeID: 33, sessionID: sessionID, peerInitiator: true,
	})
	t0 := time.Now()
	sub.TouchLastReport(t0)

	// The ongoing report: the event and the attribute change in one
	// ReportData.
	b.EmitEvent(attrPath.Endpoint, 0x003B, 0x01, uint8(1), contract.EventPriority(im.EventPriorityCritical))
	m.OnAttributeChanged(attrPath)
	ticked := make(chan struct{})
	go func() {
		defer close(ticked)
		m.Tick(context.Background(), t0.Add(time.Second))
	}()
	got, ok := readReportData(t, peerConn, peerSess, 5*time.Second)
	if !ok {
		t.Fatal("no report reached the subscriber: the attached manager has no way out")
	}
	answerOngoingReport(t, b, sessionID, im.StatusSuccess)
	<-ticked
	if len(got.EventReports) != 1 || len(got.Reports) != 1 {
		t.Fatalf("report carried %d event and %d attribute reports, want 1 and 1", len(got.EventReports), len(got.Reports))
	}

	// The flush of a fabric that leaves goes out through the same sender.
	b.EmitEvent(attrPath.Endpoint, 0x003B, 0x01, uint8(2), contract.EventPriority(im.EventPriorityInfo))
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		m.FlushFabric(ctx, 1, 0)
	}()
	got, ok = readReportData(t, peerConn, peerSess, 5*time.Second)
	if !ok {
		t.Fatal("FlushFabric sent nothing: the attached manager has no flush")
	}
	<-flushed
	if len(got.EventReports) != 1 {
		t.Fatalf("flush carried %d event reports, want the one owed", len(got.EventReports))
	}
}

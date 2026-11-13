// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// TestManagerStopDoesNotWaitOutAnUnansweredReportChunk pins the shutdown
// of the subscription engine while an ongoing report is parked on a chunk
// its peer never answers (review II, W2-conc-06). Manager.Stop waits for
// the report goroutines; the chunk wait knew only its answer and its
// timer — the MRP-derived bound, about 14 s at the defaults — so
// Node.Stop, which stops the engine before the bridge, stood still that
// long whatever its context said. The reporter's context now ends when
// the engine stops, and the chunk wait of an ongoing report ends with it.
// The bound here is 30 s, so only the stop can end the wait in time.
func TestManagerStopDoesNotWaitOutAnUnansweredReportChunk(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 93
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	b.chunkStatusResponseTimeoutOverride = 30 * time.Second
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

	mgr := subscription.NewManager(subscription.Config{TickInterval: 10 * time.Millisecond}, b.SubscriptionReporter(), nil)
	b.AttachSubscriptionManager(mgr)
	var paths []im.ConcreteAttributePath
	for _, ep := range b.Topology().Endpoints {
		if ep != nil && !ep.IsRoot() && !ep.IsAggregator() {
			paths = append(paths, endpointpkg.ReportablePaths(ep)...)
		}
	}
	sub, err := mgr.Subscribe(subscription.SubscribeArgs{
		SessionID: sessionID, MaxIntervalCeiling: 60, AttributePaths: paths,
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	b.routing.subTargets.Store(sub.ID, subTarget{
		pase: true, // a PASE subscriber: the report is not ACL-gated
		src:  peerAddr, hasPeerSourceNodeID: true, peerSourceNodeID: 0xBBBB4444,
		exchangeID: 23, sessionID: sessionID, peerInitiator: true,
	})
	for _, p := range paths {
		mgr.OnAttributeChanged(p)
	}
	mgr.Start(context.Background())

	// The first chunk of the report; the peer never answers it.
	counts, more := drainReportDataChunks(t, peerConn, peerSess)
	if len(counts) != 1 || !more[0] {
		t.Fatalf("the report went out in %d chunk(s) (more flags %v); the test needs a first chunk waiting for its answer", len(counts), more)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		mgr.Stop()
	}()
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("Manager.Stop still waits on a report parked on an unanswered chunk after 5 s")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/mdns"
)

// TestFlushChunkWaitEndsWithItsContext (review II, R2-P2-dr-req-01): the
// real flush reporter's wait for the answer to a non-final chunk ends
// with the flush's ctx — the one Stop bounds with stopSessionCloseBudget
// and fabric removal hands down — not only with the chunk's own
// flushStatusResponseTimeout. The peer never answers chunk 0 and the
// bridge's clock does not move: only ctx can end the flush.
func TestFlushChunkWaitEndsWithItsContext(t *testing.T) {
	t.Parallel()
	const sessionID uint16 = 92
	fake := clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	b, err := New(manyTempSensorsSnapshotterForTest(), mdns.NewNoop(), Config{Listen: "127.0.0.1:0", Clock: fake}, nil)
	if err != nil {
		t.Fatal(err)
	}
	testCtx, testCancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer testCancel()
	if err := b.Start(testCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = b.Stop(context.Background()) })
	peerConn, peerSess, peerAddr := chunkTestPeer(t, b, sessionID)

	var paths []im.ConcreteAttributePath
	for _, ep := range b.Topology().Endpoints {
		if ep == nil || ep.IsRoot() || ep.IsAggregator() {
			continue
		}
		paths = append(paths, endpointpkg.ReportablePaths(ep)...)
	}
	if len(paths) < 100 {
		t.Fatalf("topology yielded %d reportable paths, want >=100 so the flush cannot fit one chunk", len(paths))
	}
	const subID uint32 = 0x1201
	b.routing.subTargets.Store(subID, subTarget{
		pase:                true, // not ACL-gated
		src:                 peerAddr,
		hasPeerSourceNodeID: true,
		peerSourceNodeID:    0xBBBB4444,
		exchangeID:          12,
		sessionID:           sessionID,
		peerInitiator:       true,
	})

	flushCtx, flushCancel := context.WithCancel(testCtx)
	defer flushCancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.reportSubscriptionFlush(flushCtx, &subscription.Subscription{ID: subID}, nil, paths)
	}()

	if _, more, _ := readFlushChunk(t, peerConn, peerSess); !more {
		t.Fatal("chunk 0 announces no more chunks, want a multi-chunk flush")
	}
	// The flush now waits for chunk 0's answer on its 500 ms timer.
	for !slices.Contains(fake.Armed(), flushStatusResponseTimeout) {
		select {
		case <-done:
			t.Fatal("the flush returned before chunk 0's answer")
		case <-testCtx.Done():
			t.Fatal("the flush never waited for chunk 0's answer")
		default:
			runtime.Gosched()
		}
	}
	flushCancel()
	select {
	case <-done:
	case <-testCtx.Done():
		t.Fatal("the flush's wait for a non-final chunk did not end with its ctx")
	}
}

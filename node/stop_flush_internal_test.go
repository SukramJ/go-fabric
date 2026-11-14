// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"sync"
	"testing"
	"time"

	bidef "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/mdns"
)

// TestStopFlushesTheShutDownEventToItsSubscribers (review II,
// R2-P2-sess-03): the ShutDown event Stop emits reaches a subscription
// whose event path is not urgent — the event waits for the subscription's
// next report, and Stop's flush is that report — before the sessions end.
// matter.js emits ShutDown when the node goes offline
// (BasicInformationServer.ts:178-179, on lifecycle.goingOffline, Node.ts:222)
// before the runtime closes and its closeAllSessions flushes every
// subscription (ServerNetworkRuntime.ts:456-459, ServerNode.ts:152-155,
// SessionManager.ts:1097-1113 at db021da0).
func TestStopFlushesTheShutDownEventToItsSubscribers(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	n, _, idx := commissionedKeeperNode(t, &lateCloseAdvertiser{Noop: mdns.NewNoop()})
	shutDown := im.ConcreteEventPath{HasEndpoint: true, HasCluster: true, HasEvent: true, Endpoint: 0, Cluster: bidef.ClusterID, Event: bidef.EventShutDown}
	if _, err := n.sec.subs.Subscribe(subscription.SubscribeArgs{
		FabricIndex: idx, PeerNodeID: 0x77, SessionID: 0x1234, MaxIntervalCeiling: 60,
		EventPaths: []im.ConcreteEventPath{shutDown},
	}); err != nil {
		t.Fatal(err)
	}
	var (
		mu      sync.Mutex
		flushed []im.ConcreteEventPath
	)
	n.sec.subs.SetFlushReporter(func(_ context.Context, _ *subscription.Subscription, events []im.EventReport, _ []im.ConcreteAttributePath) {
		mu.Lock()
		defer mu.Unlock()
		for _, ev := range events {
			flushed = append(flushed, ev.Path)
		}
	})

	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, p := range flushed {
		if p.Endpoint == shutDown.Endpoint && p.Cluster == shutDown.Cluster && p.Event == shutDown.Event {
			return
		}
	}
	t.Fatalf("Stop flushed the events %v, want the ShutDown event among them", flushed)
}

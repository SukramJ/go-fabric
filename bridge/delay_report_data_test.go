// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// TestInvokeDelayReportDataDefersSubscriptions: an InvokeRequest's
// DelayReportData holds off the reports of the subscriptions that select
// the command's endpoint by DelayMinMs (matter.js InteractionServer
// #deferReports, TC-IDM-1.5), and leaves the others alone.
func TestInvokeDelayReportDataDefersSubscriptions(t *testing.T) {
	t.Parallel()
	b := newReachabilityBridge(t, &endpointpkg.Endpoint{ID: 7, DeviceType: 0x0100})
	var mu sync.Mutex
	reported := map[uint32]int{}
	mgr := subscription.NewManager(subscription.Config{}, func(_ context.Context, sub *subscription.Subscription, _ []im.ConcreteAttributePath) {
		mu.Lock()
		reported[sub.ID]++
		mu.Unlock()
	}, nil)
	path := func(ep uint16) im.ConcreteAttributePath {
		return im.ConcreteAttributePath{Endpoint: ep, Cluster: 0x0006, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	held, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{path(7)}})
	if err != nil {
		t.Fatal(err)
	}
	free, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: 2, PeerNodeID: 2, SessionID: 2, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{path(8)}})
	if err != nil {
		t.Fatal(err)
	}
	b.AttachSubscriptionManager(mgr)

	start := time.Now()
	b.deferReportsFor(im.InvokeRequest{
		Invokes:         []im.CommandInvocation{{Path: im.ConcreteCommandPath{Endpoint: 7, Cluster: 0x0006, Command: 2, HasEndpoint: true, HasCluster: true, HasCommand: true}}},
		DelayReportData: &im.DelayReportData{DelayMinMs: 1000},
	})
	mgr.OnAttributeChanged(path(7))
	mgr.OnAttributeChanged(path(8))
	mgr.Tick(context.Background(), start.Add(500*time.Millisecond))
	mu.Lock()
	if reported[held.ID] != 0 || reported[free.ID] != 1 {
		t.Errorf("at 0.5 s: reports %v, want only the other endpoint's subscription", reported)
	}
	mu.Unlock()
	mgr.Tick(context.Background(), start.Add(1100*time.Millisecond))
	mu.Lock()
	defer mu.Unlock()
	if reported[held.ID] != 1 {
		t.Errorf("at 1.1 s: the deferred subscription reported %d times, want 1", reported[held.ID])
	}
}

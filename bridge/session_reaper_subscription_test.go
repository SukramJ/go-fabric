// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// TestIdleReaperSparesASessionWithASubscription: the idle session reaper
// leaves a session that carries a live subscription, whose subscriber's
// MinIntervalFloor (here 10 min) can keep the session silent beyond
// [operational.SessionIdleTimeout]; an idle session without one is reaped
// in the same sweep. The bridge wires the exemption when the session
// registry is attached (review II, R2-P2-sess-02, owner decision 43).
func TestIdleReaperSparesASessionWithASubscription(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
	b := newUnstartedBridge(t)
	subs := subscription.NewManager(subscription.Config{Clock: clk}, nil, nil)
	b.AttachSubscriptionManager(subs)
	sessions := operational.NewManager(nopResumptionStore{})
	sessions.SetClock(clk)
	b.AttachSessionRegistry(sessions)

	var (
		mu     sync.Mutex
		closed []uint16
	)
	sessions.SetOnSessionClose(func(id uint16) {
		subs.CloseSession(id)
		mu.Lock()
		closed = append(closed, id)
		mu.Unlock()
	})
	snapshot := func() []uint16 {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(closed)
	}

	held, err := sessions.OpenFromPase(0xB0B, 0xA11, 0x2222, []byte("shared-secret"))
	if err != nil {
		t.Fatal(err)
	}
	control, err := sessions.OpenFromPase(0xB0C, 0xA12, 0x2223, []byte("shared-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := subs.Subscribe(subscription.SubscribeArgs{
		FabricIndex:        1,
		PeerNodeID:         0xA11,
		SessionID:          held.SessionID,
		MinIntervalFloor:   600, // above the 5-minute idle timeout
		MaxIntervalCeiling: 3600,
		AttributePaths:     []im.ConcreteAttributePath{{Endpoint: 1, Cluster: 0x0006, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}},
	}); err != nil {
		t.Fatal(err)
	}

	sessions.StartReaper(context.Background(), operational.SessionIdleTimeout, time.Minute)
	t.Cleanup(sessions.StopReaper)
	// One step is a minute of fake time; the bound is only reached when the
	// reaper goroutine does not run at all.
	for range 10_000 {
		if slices.Contains(snapshot(), control.SessionID) {
			break
		}
		clk.Advance(time.Minute)
		clk.Yield()
	}
	got := snapshot()
	if !slices.Contains(got, control.SessionID) {
		t.Fatalf("the idle session without a subscription was not reaped: closed %v", got)
	}
	if slices.Contains(got, held.SessionID) {
		t.Fatalf("the idle reaper closed session %d, which carries a live subscription", held.SessionID)
	}
	if _, err := sessions.Get(held.SessionID); err != nil {
		t.Fatalf("session with a subscription gone after the sweep: %v", err)
	}
}

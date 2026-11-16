// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/mdns"
)

// TestFabricLeavingFlushesTheInvokingSession (review II, R2-P1-req2-04):
// the flush of a fabric that is leaving reaches the subscriptions of the
// session that carries RemoveFabric too — the controller removing its own
// fabric hears the Leave event — and the command's turn on that session is
// handed on before, so the answer to the flushed report is not queued
// behind the command. matter.js's leave closes every session's
// subscriptions with a flush, the invoking one included (Fabric.ts:447-457
// at db021da0: one session after another, :450-454, each one's shutdown
// logic awaited at Session.ts:313).
func TestFabricLeavingFlushesTheInvokingSession(t *testing.T) {
	t.Parallel()
	const invoking, other uint16 = 41, 42
	b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	path := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: 0, Cluster: 0x0028, Attribute: 0x0005}
	subs := map[uint16]uint32{}
	for _, sid := range []uint16{invoking, other} {
		sub, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: 1, PeerNodeID: uint64(sid), SessionID: sid, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{path}})
		if err != nil {
			t.Fatal(err)
		}
		subs[sid] = sub.ID
	}
	mgr.OnAttributeChanged(path)

	var mu sync.Mutex
	released := false
	flushed := map[uint32]bool{}
	turnReleasedFirst := true
	mgr.SetFlushReporter(func(_ context.Context, sub *subscription.Subscription, _ []im.EventReport, _ []im.ConcreteAttributePath) { //nolint:staticcheck // the deprecated hook, held for its window (R2-P6-api-0508)
		mu.Lock()
		defer mu.Unlock()
		flushed[sub.ID] = true
		if sub.SessionID == invoking && !released {
			turnReleasedFirst = false
		}
	})
	ctx := context.WithValue(context.Background(), sessionTurnKey{}, func() {
		mu.Lock()
		released = true
		mu.Unlock()
	})
	ctx = mattercore.WithInvokeSessionID(ctx, invoking)
	b.EmitFabricLeavingContext(ctx, 1)

	mu.Lock()
	defer mu.Unlock()
	if !flushed[subs[other]] {
		t.Error("the other session's subscription was not flushed")
	}
	if !flushed[subs[invoking]] {
		t.Fatal("the invoking session's subscription was not flushed: its controller never hears the Leave event")
	}
	if !turnReleasedFirst {
		t.Error("the invoking session's subscription was flushed while the command held the session's turn")
	}
}

// TestFabricLeavingKeepsTheTurnWithoutSubscriptions: the leaving flush
// hands the invoking session's turn on only when that session has a
// subscription to flush; a session without one, or a removal outside a
// command, keeps it (R2-P6-test-0819).
func TestFabricLeavingKeepsTheTurnWithoutSubscriptions(t *testing.T) {
	t.Parallel()
	const invoking, other uint16 = 41, 42
	for _, c := range []struct {
		name    string
		session uint16
	}{{"invoking session without subscriptions", invoking}, {"outside a command", 0}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b, err := New(wbEmptySnapshotter, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			mgr := subscription.NewManager(subscription.Config{}, nil, nil)
			b.AttachSubscriptionManager(mgr)
			path := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: 0, Cluster: 0x0028, Attribute: 0x0005}
			if _, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: 1, PeerNodeID: uint64(other), SessionID: other, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{path}}); err != nil {
				t.Fatal(err)
			}
			var released atomic.Bool
			ctx := context.WithValue(context.Background(), sessionTurnKey{}, func() { released.Store(true) })
			if c.session != 0 {
				ctx = mattercore.WithInvokeSessionID(ctx, c.session)
			}
			b.EmitFabricLeavingContext(ctx, 1)
			if released.Load() {
				t.Fatal("the turn was handed on although the invoking session had nothing to flush")
			}
		})
	}
}

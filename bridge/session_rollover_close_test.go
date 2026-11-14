// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestSecureSessionClosedWhenItsCounterNearsExhaustion — a send that takes
// the session's outbound counter to within 100 000 of the maximum ends the
// session through the registry, as matter.js winds it down
// (packages/protocol/src/session/NodeSession.ts:398-412); a session whose
// counter is already used up is ended by the send that fails.
func TestSecureSessionClosedWhenItsCounterNearsExhaustion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		initial uint32
		sendErr bool
	}{
		{name: "near exhaustion", initial: 0xFFFFFFFF - 100_000},
		{name: "used up", initial: 0xFFFFFFFF, sendErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			reg := &fakeSessionRegistry{}
			b.AttachSessionRegistry(reg)
			sess, err := channel.New(channel.Config{
				EncryptKey: make([]byte, 16), DecryptKey: make([]byte, 16),
				LocalNodeID: 0xB0B, PeerNodeID: 0xA11, PeerSessionID: 0x1234,
				InitialCounter: tc.initial,
			})
			if err != nil {
				t.Fatal(err)
			}
			const sid uint16 = 7
			_, err = b.encryptSecureOutbound(sess, sid, &message.Header{SessionID: 0x1234}, []byte{0x01})
			if (err != nil) != tc.sendErr {
				t.Fatalf("encryptSecureOutbound err = %v, want error %v", err, tc.sendErr)
			}
			b.rolloverCloses.Wait()
			if got := reg.closedSessions(); !slices.Equal(got, []uint16{sid}) {
				t.Fatalf("registry Close calls = %v, want [%d]", got, sid)
			}
			// A second send does not close it again.
			_, _ = b.encryptSecureOutbound(sess, sid, &message.Header{SessionID: 0x1234}, []byte{0x01})
			b.rolloverCloses.Wait()
			if got := reg.closedSessions(); len(got) != 1 {
				t.Fatalf("registry Close calls after a second send = %v, want one", got)
			}
		})
	}
}

// TestRolloverCloseFlushesTheSessionsSubscriptions (review II,
// R2-P2-dr-req-07): the close of a session whose counter nears exhaustion
// first reports what the session's subscriptions owe, as matter.js's
// #scheduleRolloverClose runs `closeSubscriptions(true)` — a flush through
// the session — before the session closes
// (packages/protocol/src/session/NodeSession.ts:406-410, :312-323). A
// subscription of another session is not flushed.
func TestRolloverCloseFlushesTheSessionsSubscriptions(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	reg := &fakeSessionRegistry{}
	b.AttachSessionRegistry(reg)
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	const sid, otherSID uint16 = 7, 8
	path := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: 0, Cluster: 0x0028, Attribute: 0x0005}
	mine, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: 1, PeerNodeID: 0xA11, SessionID: sid, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{path}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{FabricIndex: 1, PeerNodeID: 0xA12, SessionID: otherSID, MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{path}}); err != nil {
		t.Fatal(err)
	}
	mgr.OnAttributeChanged(path)
	var (
		mu      sync.Mutex
		flushed []uint32
		before  []uint16 // the registry's closes when the flush ran
	)
	mgr.SetFlushReporter(func(_ context.Context, sub *subscription.Subscription, _ []im.EventReport, paths []im.ConcreteAttributePath) {
		mu.Lock()
		defer mu.Unlock()
		if len(paths) > 0 {
			flushed = append(flushed, sub.ID)
		}
		before = reg.closedSessions()
	})
	mgr.Stop()

	sess, err := channel.New(channel.Config{
		EncryptKey: make([]byte, 16), DecryptKey: make([]byte, 16),
		LocalNodeID: 0xB0B, PeerNodeID: 0xA11, PeerSessionID: 0x1234,
		InitialCounter: 0xFFFFFFFF - 100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.encryptSecureOutbound(sess, sid, &message.Header{SessionID: 0x1234}, []byte{0x01}); err != nil {
		t.Fatal(err)
	}
	b.rolloverCloses.Wait()
	if got := reg.closedSessions(); !slices.Equal(got, []uint16{sid}) {
		t.Fatalf("registry Close calls = %v, want [%d]", got, sid)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(flushed, []uint32{mine.ID}) {
		t.Fatalf("flushed subscriptions %v, want the closing session's [%d] with its owed attribute", flushed, mine.ID)
	}
	if len(before) != 0 {
		t.Fatalf("the flush ran after the registry closed %v, want it before the session closes", before)
	}
}

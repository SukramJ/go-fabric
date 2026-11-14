// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/transport/message"
)

// subTargetCount counts the bridge's stored subscription routing targets.
func subTargetCount(b *Bridge) int {
	n := 0
	b.routing.subTargets.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// closeSessionOnFirstAdd makes m close the session of the first
// subscription it admits as soon as it is admitted — the session's close
// landing between the admission and the bridge storing the subscription's
// routing target (another goroutine closing the session: the idle
// reaper, a fabric removal, a per-peer eviction, Stop).
func closeSessionOnFirstAdd(m *subscription.Manager) {
	var fired atomic.Bool // the close fires the hook again
	m.SetOnSessionSubscriptionsChanged(func(id uint16) {
		if fired.CompareAndSwap(false, true) {
			m.CloseSession(id)
		}
	})
}

// TestSubscribeClosedBeforeItsTargetLeavesNoTarget: a subscription whose
// session closes between its admission and the storing of its routing
// target leaves no target behind — the close hook ran before the target
// existed, and nothing would delete it later (review II, R2-P2-sess-07).
func TestSubscribeClosedBeforeItsTargetLeavesNoTarget(t *testing.T) {
	t.Parallel()
	const (
		bridgeNode, peerNode uint64 = 0x1111, 0xAAAA
		sess                 uint16 = 10
	)
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	bs, _ := identityTestPair(t, bridgeNode, peerNode, 0x10, 0x20)
	b.AttachSessionLookup(identityTestLookup{
		sess:    map[uint16]*channel.Session{sess: bs},
		fabric:  map[uint16]uint8{sess: 1},
		subject: map[uint16]uint64{sess: peerNode},
	})
	mgr := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(mgr)
	closeSessionOnFirstAdd(mgr)

	peerConn, peerAddr := openPeerSocket(t)
	t.Cleanup(func() { _ = peerConn.Close() })
	path := im.ConcreteAttributePath{HasEndpoint: true, Endpoint: 0, HasCluster: true, Cluster: 0x001D, HasAttribute: true, Attribute: 0}
	proto := message.ProtocolHeader{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeSubscribeRequest, ExchangeID: 77, Initiator: true, NeedsAck: true}
	req := im.SubscribeRequest{KeepSubscriptions: true, MaxIntervalCeiling: 60, AttributeRequests: []im.ConcreteAttributePath{path}}
	_ = b.handleSubscribeRequest(context.Background(), peerAddr, &message.Header{SessionID: sess}, proto, req)

	if n := len(mgr.Snapshot()); n != 0 {
		t.Fatalf("%d subscriptions live; the session's close should have taken it", n)
	}
	if n := subTargetCount(b); n != 0 {
		t.Fatalf("%d routing targets left for a subscription closed before its target was stored", n)
	}
}

// TestReestablishClosedBeforeItsTargetLeavesNoTarget: the same for a
// former subscription re-established after a restart (review II,
// R2-P2-sess-07).
func TestReestablishClosedBeforeItsTargetLeavesNoTarget(t *testing.T) {
	fab := newResumeFabric(t)
	st := newMemSubscriptionStore()
	ctrl := newFakeController(t, fab)
	run1 := startResumeDevice(t, st, nil)
	ctrl.setBridge(run1.addr(t))
	subscribeOverCASE(t, run1, ctrl)
	waitFor(t, "persist", func() bool { return len(st.ids()) == 1 })
	run1.stop()

	run2 := startResumeDevice(t, st, func(br *Bridge, sessions *operational.Manager) {
		br.AttachCaseInitiatorProvider(resumeInitiatorProvider(fab, sessions))
		br.AttachOperationalResolver(staticResolver{addr: ctrl.addr})
	})
	closeSessionOnFirstAdd(run2.subs)
	ctrl.setBridge(run2.addr(t))
	res := run2.br.ReestablishFormerSubscriptions(context.Background())
	if len(res.Reestablished) != 0 {
		t.Fatalf("re-established %v on a session that closed under it", res.Reestablished)
	}
	if n := subTargetCount(run2.br); n != 0 {
		t.Fatalf("%d routing targets left for a subscription closed before its target was stored", n)
	}
}

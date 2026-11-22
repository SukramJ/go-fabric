// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// Ports by meaning of matter.js
// packages/node/test/node/ServerSubscriptionTest.ts at b25ed960, the
// reporter-side rows of the session supersession cases: the count of failed
// updates a move starts again (:1663) and a failure on the session the
// subscription left, which is not counted and is resent at once on the new
// one (:1735, :1827). The move itself is im/subscription's
// (supersession_parity_matterjs_test.go there).

import (
	"context"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/secure/channel"
)

func TestServerSubscriptionSupersessionReporter(t *testing.T) {
	t.Parallel()
	attrPath := im.ConcreteAttributePath{HasEndpoint: true, HasCluster: true, HasAttribute: true, Endpoint: 1, Cluster: 0x0006, Attribute: 0x0000}
	const former, newer uint16 = 40, 41
	// inMove marks, per bridge, a SessionSuperseded call made from within a
	// lookup (supersedeQuietly).
	var (
		inMoveMu sync.Mutex
		inMove   = map[*Bridge]*atomic.Bool{}
	)
	inMoveOf := func(b *Bridge) *atomic.Bool {
		inMoveMu.Lock()
		defer inMoveMu.Unlock()
		return inMove[b]
	}
	supersedeQuietly := func(b *Bridge) {
		flag := inMoveOf(b)
		flag.Store(true)
		defer flag.Store(false)
		b.SessionSuperseded(former, newer)
	}
	peerAddr := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}

	// setup attaches a manager to a started bridge with one subscription
	// on former, routed there. The lookup knows every session, but each one
	// it hands out is closed, so every send fails before a datagram leaves;
	// onLookup sees each session a send looks up — and the lookup of the
	// newer session SessionSuperseded makes after a move, which
	// [supersedeQuietly] keeps out of the record.
	setup := func(t *testing.T, onLookup func(id uint16)) (*Bridge, *subscription.Manager, *subscription.Subscription) {
		t.Helper()
		b := newStartedBridge(t)
		closed, err := channel.New(channel.Config{EncryptKey: make([]byte, 16), DecryptKey: make([]byte, 16)})
		if err != nil {
			t.Fatal(err)
		}
		closed.Close()
		quiet := &atomic.Bool{}
		inMoveMu.Lock()
		inMove[b] = quiet
		inMoveMu.Unlock()
		b.AttachSessionLookup(sessionLookupFunc(func(id uint16) (*channel.Session, bool) {
			if onLookup != nil && !quiet.Load() {
				onLookup(id)
			}
			return closed, true
		}))
		m := subscription.NewManager(subscription.Config{}, nil, nil)
		b.AttachSubscriptionManager(m)
		sub, err := m.Subscribe(subscription.SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 0xBBBB4444, SessionID: former,
			MaxIntervalCeiling: 60, AttributePaths: []im.ConcreteAttributePath{attrPath},
		})
		if err != nil {
			t.Fatal(err)
		}
		b.routing.subTargets.Store(sub.ID, subTarget{
			src: peerAddr, sessionID: former, exchangeID: 21, peerInitiator: true,
			fabricIndex: 1, subjectNodeID: 0xBBBB4444,
		})
		return b, m, sub
	}
	failures := func(b *Bridge, subID uint32) int {
		b.subUpdateMu.Lock()
		defer b.subUpdateMu.Unlock()
		raw, ok := b.subSendErrorCount.Load(subID)
		if !ok {
			return 0
		}
		n, _ := raw.(int)
		return n
	}
	routedOn := func(b *Bridge, subID uint32) uint16 {
		raw, ok := b.routing.subTargets.Load(subID)
		if !ok {
			return 0
		}
		target, _ := raw.(subTarget)
		return target.sessionID
	}

	t.Run("counts failed updates anew on a newer session", func(t *testing.T) {
		t.Parallel()
		b, m, sub := setup(t, nil)
		t0 := time.Now()
		for i := range sendErrorRetryLimit {
			m.OnAttributeChanged(attrPath)
			m.Tick(context.Background(), t0.Add(time.Duration(i+1)*sub.SendInterval()))
		}
		if n := failures(b, sub.ID); n != sendErrorRetryLimit {
			t.Fatalf("%d failed updates counted on the former session; want %d", n, sendErrorRetryLimit)
		}
		b.SessionSuperseded(former, newer)
		if got := routedOn(b, sub.ID); got != newer {
			t.Fatalf("routed on session %d; want %d", got, newer)
		}
		if n := failures(b, sub.ID); n != 0 {
			t.Fatalf("%d failed updates counted after the move; want 0", n)
		}
		m.OnAttributeChanged(attrPath)
		m.Tick(context.Background(), t0.Add(10*sub.SendInterval()))
		if sub.IsClosed() || failures(b, sub.ID) != 1 {
			t.Fatalf("after a failure on the newer session: closed %v, %d counted; want open, 1", sub.IsClosed(), failures(b, sub.ID))
		}
	})

	t.Run("resends at once on its new session when a send on the session it left fails", func(t *testing.T) {
		t.Parallel()
		var (
			mu      sync.Mutex
			lookups []uint16
			b       *Bridge
		)
		// The subscriber sets up a newer session while the first send is
		// under way on the former one.
		b, m, sub := setup(t, func(id uint16) {
			mu.Lock()
			first := len(lookups) == 0
			if len(lookups) == 0 || lookups[len(lookups)-1] != id {
				lookups = append(lookups, id)
			}
			mu.Unlock()
			if first {
				supersedeQuietly(b)
			}
		})
		m.OnAttributeChanged(attrPath)
		m.Tick(context.Background(), time.Now().Add(time.Second))

		mu.Lock()
		got := slices.Clone(lookups)
		mu.Unlock()
		if !slices.Equal(got, []uint16{former, newer}) {
			t.Fatalf("sends looked up sessions %v in one pass; want [%d %d]: the resend at once on the newer session", got, former, newer)
		}
		// The failure on the former session is not counted, the one on the
		// newer session is.
		if n := failures(b, sub.ID); n != 1 || sub.IsClosed() {
			t.Fatalf("%d failed updates counted, closed %v; want 1, open", n, sub.IsClosed())
		}
	})

	t.Run("does not count a failure on the session it left towards giving up (characterization)", func(t *testing.T) {
		t.Parallel()
		var (
			b     *Bridge
			mu    sync.Mutex
			sends []uint16
		)
		b, m, sub := setup(t, func(id uint16) {
			mu.Lock()
			sends = append(sends, id)
			first := len(sends) == 1
			mu.Unlock()
			if first {
				supersedeQuietly(b)
			}
		})
		sent := func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(sends)
		}
		// The first send fails after the subscriber set up a newer session,
		// the next two on that one: three failures, of which two count —
		// one short of giving up.
		t0 := time.Now()
		for i := 0; i < 5 && sent() < 3; i++ {
			m.OnAttributeChanged(attrPath)
			m.Tick(context.Background(), t0.Add(time.Duration(i+1)*sub.SendInterval()))
		}
		mu.Lock()
		got := slices.Clone(sends)
		mu.Unlock()
		if !slices.Equal(got, []uint16{former, newer, newer}) {
			t.Fatalf("sends on sessions %v; want [%d %d %d]", got, former, newer, newer)
		}
		if sub.IsClosed() {
			t.Fatal("the subscription gave up although only its failures on the newer session count")
		}
		if n := failures(b, sub.ID); n != sendErrorRetryLimit {
			t.Fatalf("%d failed updates counted; want %d, the newer session's", n, sendErrorRetryLimit)
		}
	})

	t.Run("follows a move that came before its route was captured", func(t *testing.T) {
		t.Parallel()
		// Not a matter.js row: the route is stored after the manager's
		// Subscribe, so a supersession in between reaches the manager only.
		b, m, sub := setup(t, nil)
		b.routing.subTargets.Delete(sub.ID)
		b.SessionSuperseded(former, newer)
		b.routing.subTargets.Store(sub.ID, subTarget{src: peerAddr, sessionID: former, fabricIndex: 1, subjectNodeID: 0xBBBB4444})
		b.followMovedSession(m, sub.ID)
		if got := routedOn(b, sub.ID); got != newer {
			t.Fatalf("routed on session %d; want %d", got, newer)
		}
	})
}

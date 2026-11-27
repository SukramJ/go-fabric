// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// A port of matter.js packages/protocol/test/peer/RebootResubscribeArmerTest.ts
// at db91886c. matter.js drives the armer on a real SessionManager and
// ClientSubscriptions; the port drives it on a session table and a
// subscription set that implement the armer's context the way those two do
// (handlePeerShutdown with a session closes the sessions registered before
// it, sessionsRegisteredSince lists those registered at or after it,
// handlePeerLoss gives up every session, closeForPeer closes the peer's
// subscriptions), on the fake clock, whose AfterFunc callbacks run inside
// Advance — so the MockTime.macrotasks and MockTime.resolve waits of the
// original have no counterpart. A session the table registers is handed to
// SessionAdded once established, as the controller feeds the armer.

import (
	"slices"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/clock/clocktest"
)

var armerPeer = exchange.PeerAddress{FabricIndex: 0, NodeID: 0x44c}

type armerTestSession struct {
	peer      exchange.PeerAddress
	initiator bool
	createdAt time.Time
	// position is the registration position in the fixture's table.
	position int
}

func (s *armerTestSession) Peer() exchange.PeerAddress { return s.peer }
func (s *armerTestSession) Initiator() bool            { return s.initiator }
func (s *armerTestSession) CreatedAt() time.Time       { return s.createdAt }

type armerFixture struct {
	clock      *clocktest.Fake
	armer      *RebootResubscribeArmer
	mu         sync.Mutex
	sessions   []*armerTestSession
	registered int
	subs       map[int]bool
	nextSub    int
	peerLosts  int
}

func newArmerFixture(t *testing.T) *armerFixture {
	t.Helper()
	f := &armerFixture{clock: clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), subs: map[int]bool{}}
	f.armer = NewRebootResubscribeArmer(RebootArmerContext{
		Clock: f.clock,
		ClosePeerSessionsRegisteredBefore: func(peer exchange.PeerAddress, keep ArmerSession) {
			k, _ := keep.(*armerTestSession)
			f.mu.Lock()
			defer f.mu.Unlock()
			f.sessions = slices.DeleteFunc(f.sessions, func(s *armerTestSession) bool {
				return s.peer == peer && (k == nil || s.position < k.position)
			})
		},
		SessionsRegisteredSince: func(of ArmerSession) []ArmerSession {
			o, _ := of.(*armerTestSession)
			f.mu.Lock()
			defer f.mu.Unlock()
			var out []ArmerSession
			for _, s := range f.sessions {
				if o != nil && s.peer == o.peer && s.position >= o.position {
					out = append(out, s)
				}
			}
			return out
		},
		PeerLost: func(peer exchange.PeerAddress) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.peerLosts++
			f.sessions = slices.DeleteFunc(f.sessions, func(s *armerTestSession) bool { return s.peer == peer })
		},
		CloseSubscriptions: func(exchange.PeerAddress) {
			f.mu.Lock()
			defer f.mu.Unlock()
			clear(f.subs)
		},
	})
	t.Cleanup(f.armer.Close)
	return f
}

// createSession is the test's createSession(isInitiator, establishing): it
// registers a session and, unless it is establishing, establishes it.
func (f *armerFixture) createSession(initiator bool) *armerTestSession {
	s := f.createEstablishing(initiator)
	f.armer.SessionAdded(s)
	return s
}

// createEstablishing registers a session whose handshake still runs.
func (f *armerFixture) createEstablishing(initiator bool) *armerTestSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := &armerTestSession{peer: armerPeer, initiator: initiator, createdAt: f.clock.Now(), position: f.registered}
	f.registered++
	f.sessions = append(f.sessions, s)
	return s
}

// completeEstablishment establishes s.
func (f *armerFixture) completeEstablishment(s *armerTestSession) { f.armer.SessionAdded(s) }

// forceClose closes s, a handshake that failed.
func (f *armerFixture) forceClose(s *armerTestSession) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = slices.DeleteFunc(f.sessions, func(x *armerTestSession) bool { return x == s })
}

func (f *armerFixture) registerSubscription() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextSub++
	f.subs[f.nextSub] = true
	return f.nextSub
}

func (f *armerFixture) reportOver(s *armerTestSession) { f.armer.ReportStarted(s) }

func (f *armerFixture) isSubscribed(id int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subs[id]
}

func (f *armerFixture) sessionLives(s *armerTestSession) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.sessions, s)
}

func (f *armerFixture) peerLossCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.peerLosts
}

func TestRebootResubscribeArmerParity(t *testing.T) {
	t.Parallel()
	t.Run("RebootResubscribeArmer", func(t *testing.T) {
		t.Parallel()
		t.Run("gives a device the apply delay, capped at a day, plus three minutes to return", func(t *testing.T) {
			t.Parallel()
			for _, c := range []struct{ in, want time.Duration }{
				{0, 3 * time.Minute},
				{10 * time.Minute, 13 * time.Minute},
				{48 * time.Hour, 24*time.Hour + 3*time.Minute},
				{-5 * time.Millisecond, 3 * time.Minute},
			} {
				if got := RebootReturnTimeout(c.in); got != c.want {
					t.Errorf("RebootReturnTimeout(%v) = %v, want %v", c.in, got, c.want)
				}
			}
		})
		t.Run("closes older sessions when the armed device returns", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			older := f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			f.clock.Advance(time.Second)
			returned := f.createSession(false)
			if f.sessionLives(older) || !f.sessionLives(returned) {
				t.Fatalf("older %v returned %v", f.sessionLives(older), f.sessionLives(returned))
			}
		})
		t.Run("closes an older session created in the same instant as the returning session", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			older := f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			returned := f.createSession(false)
			if !returned.createdAt.Equal(older.createdAt) {
				t.Fatal("created at different times")
			}
			if f.sessionLives(older) || !f.sessionLives(returned) {
				t.Fatalf("older %v returned %v", f.sessionLives(older), f.sessionLives(returned))
			}
		})
		t.Run("does not take a session whose handshake fails as the return", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			older := f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			f.clock.Advance(time.Second)
			failed := f.createEstablishing(false)
			f.forceClose(failed)
			if !f.sessionLives(older) {
				t.Fatal("older closed")
			}
		})
		t.Run("re-subscribes when no report arrives within the grace window", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			f.createSession(false)
			if !f.isSubscribed(sub) {
				t.Fatal("closed at once")
			}
			f.clock.Advance(30 * time.Second)
			if f.isSubscribed(sub) {
				t.Fatal("still subscribed")
			}
		})
		t.Run("keeps the subscription when a report arrives over the returning session", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			returned := f.createSession(false)
			f.reportOver(returned)
			f.clock.Advance(30 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("re-subscribed")
			}
		})
		t.Run("keeps the subscription when a report arrives over a session opened after the return", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			f.createSession(false)
			later := f.createSession(true)
			f.reportOver(later)
			f.clock.Advance(30 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("re-subscribed")
			}
		})
		t.Run("keeps the subscription when the priming report arrives before the returning session is established", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			returning := f.createEstablishing(false)
			f.reportOver(returning)
			f.completeEstablishment(returning)
			f.clock.Advance(30 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("re-subscribed")
			}
		})
		t.Run("keeps the subscription when a report arrives over a session still establishing after the return", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			f.createSession(false)
			f.reportOver(f.createEstablishing(false))
			f.clock.Advance(30 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("re-subscribed")
			}
		})
		t.Run("keeps the subscription when a report arrives over a session that was establishing when the peer returned", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			returning := f.createEstablishing(false)
			second := f.createEstablishing(false)
			f.completeEstablishment(returning)
			f.reportOver(second)
			f.clock.Advance(30 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("re-subscribed")
			}
		})
		t.Run("re-subscribes when the only report arrives over a handshake that started before the returning session was created", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			stale := f.createEstablishing(false)
			f.clock.Advance(time.Second)
			f.reportOver(stale)
			f.createSession(false)
			f.clock.Advance(30 * time.Second)
			if f.isSubscribed(sub) {
				t.Fatal("still subscribed")
			}
		})
		t.Run("keeps the subscription when the report arrives over our session created after a returning session still establishing", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			returning := f.createEstablishing(false)
			f.clock.Advance(time.Second)
			initiated := f.createSession(true)
			f.reportOver(initiated)
			f.completeEstablishment(returning)
			f.clock.Advance(30 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("re-subscribed")
			}
		})
		t.Run("characterization: re-subscribes when the only report arrives over a session whose handshake fails", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			failed := f.createEstablishing(false)
			f.reportOver(failed)
			f.forceClose(failed)
			f.createSession(false)
			f.clock.Advance(30 * time.Second)
			if f.isSubscribed(sub) {
				t.Fatal("still subscribed")
			}
		})
		t.Run("re-subscribes when the only report arrives over a pre-reboot session", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			preReboot := f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			returned := f.createSession(false)
			f.reportOver(preReboot)
			if !returned.createdAt.Equal(preReboot.createdAt) {
				t.Fatal("created at different times")
			}
			f.clock.Advance(10 * time.Second)
			f.reportOver(preReboot)
			f.clock.Advance(20 * time.Second)
			if f.isSubscribed(sub) {
				t.Fatal("still subscribed")
			}
		})
		t.Run("re-subscribes when the only report arrives before the peer returns", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			preReboot := f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			f.reportOver(preReboot)
			f.createSession(false)
			f.clock.Advance(30 * time.Second)
			if f.isSubscribed(sub) {
				t.Fatal("still subscribed")
			}
		})
		t.Run("keeps the subscription when the peer opens a second session after reporting", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			returned := f.createSession(false)
			f.reportOver(returned)
			f.clock.Advance(10 * time.Second)
			f.createSession(false)
			f.clock.Advance(30 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("re-subscribed")
			}
		})
		t.Run("characterization: a second reboot inside the grace window keeps the first return's evidence", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			firstReturn := f.createSession(false)
			f.reportOver(firstReturn)
			f.clock.Advance(10 * time.Second)
			f.createSession(false)
			f.clock.Advance(30 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("re-subscribed")
			}
		})
		t.Run("keeps the subscription when a report arrives over a session we opened after the return", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.createSession(false) // pre-reboot session
			f.armer.Arm(armerPeer, 0)
			f.createSession(false)
			ourReconnect := f.createSession(true)
			f.reportOver(ourReconnect)
			f.clock.Advance(30 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("re-subscribed")
			}
		})
		t.Run("discards the previous cycle's returning session as evidence after a re-arm", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			firstReturn := f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			f.createSession(false)
			f.reportOver(firstReturn)
			f.clock.Advance(30 * time.Second)
			if f.isSubscribed(sub) {
				t.Fatal("still subscribed")
			}
		})
		t.Run("re-arming replaces the previous return deadline rather than leaving it to fire", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			f.registerSubscription()
			f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			f.clock.Advance(2 * time.Minute)
			f.armer.Arm(armerPeer, 0)
			f.clock.Advance(2 * time.Minute)
			if n := f.peerLossCount(); n != 0 {
				t.Fatalf("%d peer losses", n)
			}
		})
		t.Run("ignores sessions for peers that are not armed", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			older := f.createSession(false)
			f.clock.Advance(time.Second)
			f.createSession(false)
			if !f.sessionLives(older) {
				t.Fatal("older closed")
			}
		})
		t.Run("disarm cancels a pending re-subscribe", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			f.createSession(false)
			f.armer.Disarm(armerPeer)
			f.clock.Advance(30 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("re-subscribed")
			}
		})
		t.Run("still handles a late session if the device returns before the deadline", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			older := f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			f.clock.Advance(2 * time.Minute)
			if !f.sessionLives(older) {
				t.Fatal("older closed while waiting")
			}
			returned := f.createSession(false)
			if f.sessionLives(older) || !f.sessionLives(returned) {
				t.Fatalf("older %v returned %v", f.sessionLives(older), f.sessionLives(returned))
			}
		})
		t.Run("recovers the peer when no session returns within the return deadline", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			stale := f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			f.clock.Advance(5 * time.Minute)
			if f.sessionLives(stale) || f.isSubscribed(sub) {
				t.Fatalf("stale %v subscribed %v", f.sessionLives(stale), f.isSubscribed(sub))
			}
		})
		t.Run("extends the return deadline by the delay the device waits before it applies", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 3*time.Minute)
			f.clock.Advance(5 * time.Minute)
			if !f.isSubscribed(sub) {
				t.Fatal("recovered at the plain deadline")
			}
			f.clock.Advance(2 * time.Minute)
			if f.isSubscribed(sub) {
				t.Fatal("not recovered")
			}
		})
		t.Run("waits at most a day for the restart, as a requestor may cap a longer delay to that", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 30*24*time.Hour)
			f.clock.Advance(24*time.Hour + 4*time.Minute)
			if f.isSubscribed(sub) {
				t.Fatal("not recovered")
			}
		})
		t.Run("cancels the return deadline once the device returns", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			f.clock.Advance(time.Second)
			f.createSession(false)
			f.clock.Advance(5 * time.Minute)
			if n := f.peerLossCount(); n != 0 {
				t.Fatalf("%d peer losses", n)
			}
		})
		t.Run("disarm before the deadline cancels the recovery", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			f.armer.Disarm(armerPeer)
			f.clock.Advance(5 * time.Minute)
			if n := f.peerLossCount(); n != 0 {
				t.Fatalf("%d peer losses", n)
			}
		})
		t.Run("ignores a controller-initiated session (isInitiator true) even for an armed peer", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			older := f.createSession(false)
			f.armer.Arm(armerPeer, 0)
			f.clock.Advance(time.Second)
			f.createSession(true)
			if !f.sessionLives(older) {
				t.Fatal("older closed")
			}
			f.clock.Advance(30 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("re-subscribed")
			}
		})
		t.Run("re-arming drops the prior grace timer so it cannot fire closeForPeer on its own schedule", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			f.createSession(false)
			f.clock.Advance(10 * time.Second)
			f.armer.Arm(armerPeer, 0)
			f.clock.Advance(20 * time.Second)
			if !f.isSubscribed(sub) {
				t.Fatal("the first cycle's grace fired")
			}
			f.createSession(false)
			f.clock.Advance(30 * time.Second)
			if f.isSubscribed(sub) {
				t.Fatal("the second cycle's grace did not fire")
			}
		})
		t.Run("re-arming requires fresh evidence for the new cycle", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			sub := f.registerSubscription()
			f.armer.Arm(armerPeer, 0)
			firstReturn := f.createSession(false)
			f.reportOver(firstReturn)
			f.armer.Arm(armerPeer, 0)
			f.createSession(false)
			f.clock.Advance(30 * time.Second)
			if f.isSubscribed(sub) {
				t.Fatal("still subscribed")
			}
		})
		t.Run("disarm of an un-armed peer is a no-op", func(t *testing.T) {
			t.Parallel()
			f := newArmerFixture(t)
			f.armer.Disarm(armerPeer)
		})
	})
}

// TestRebootResubscribeArmerDeprecatedTimeClose: a context that sets only
// the deprecated ClosePeerSessionsBefore keeps its behaviour for the
// deprecation window — the sessions created before the returning one
// close, one created in the same instant stays (matter.js's deprecated
// Timestamp form, SessionManager.ts:719-721 at db91886c).
func TestRebootResubscribeArmerDeprecatedTimeClose(t *testing.T) {
	t.Parallel()
	clk := clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var closedBefore []time.Time
	armer := NewRebootResubscribeArmer(RebootArmerContext{
		Clock: clk,
		ClosePeerSessionsBefore: func(_ exchange.PeerAddress, before time.Time) { //nolint:staticcheck // the deprecated form, held for its window
			closedBefore = append(closedBefore, before)
		},
	})
	t.Cleanup(armer.Close)
	armer.Arm(armerPeer, 0)
	clk.Advance(time.Second)
	returned := &armerTestSession{peer: armerPeer, createdAt: clk.Now()}
	armer.SessionAdded(returned)
	if len(closedBefore) != 1 || !closedBefore[0].Equal(returned.createdAt) {
		t.Fatalf("ClosePeerSessionsBefore called with %v; want once with the returning session's creation time", closedBefore)
	}
}

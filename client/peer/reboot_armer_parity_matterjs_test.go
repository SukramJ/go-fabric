// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// A port of matter.js packages/protocol/test/peer/RebootResubscribeArmerTest.ts
// at 862ad75f. matter.js drives the armer on a real SessionManager and
// ClientSubscriptions; the port drives it on a session table and a
// subscription set that implement the armer's context the way those two do
// (handlePeerShutdown closes the sessions created before the time,
// handlePeerLoss every session, closeForPeer the peer's subscriptions), on
// the fake clock, whose AfterFunc callbacks run inside Advance — so the
// MockTime.macrotasks and MockTime.resolve waits of the original have no
// counterpart.

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/clock/clocktest"
)

var armerPeer = exchange.PeerAddress{FabricIndex: 0, NodeID: 0x44c}

type armerTestSession struct {
	peer      exchange.PeerAddress
	initiator bool
	createdAt time.Time
}

func (s *armerTestSession) Peer() exchange.PeerAddress { return s.peer }
func (s *armerTestSession) Initiator() bool            { return s.initiator }
func (s *armerTestSession) CreatedAt() time.Time       { return s.createdAt }

type armerFixture struct {
	clock     *clocktest.Fake
	armer     *RebootResubscribeArmer
	mu        sync.Mutex
	sessions  []*armerTestSession
	subs      map[int]bool
	nextSub   int
	peerLosts int
}

func newArmerFixture(t *testing.T) *armerFixture {
	t.Helper()
	f := &armerFixture{clock: clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), subs: map[int]bool{}}
	f.armer = NewRebootResubscribeArmer(RebootArmerContext{
		Clock: f.clock,
		ClosePeerSessionsBefore: func(peer exchange.PeerAddress, before time.Time) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.sessions = slices.DeleteFunc(f.sessions, func(s *armerTestSession) bool {
				return s.peer == peer && s.createdAt.Before(before)
			})
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

func (f *armerFixture) createSession(initiator bool) *armerTestSession {
	s := &armerTestSession{peer: armerPeer, initiator: initiator, createdAt: f.clock.Now()}
	f.mu.Lock()
	f.sessions = append(f.sessions, s)
	f.mu.Unlock()
	f.armer.SessionAdded(s)
	return s
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

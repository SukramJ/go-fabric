// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/fabrictestseam"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// errDroppedOnController is the cause the controller drops its sessions
// with — matter.js's dropSessionsOnController (ServerSubscriptionSessionTest.ts
// at b25ed960), which closes them without telling the device.
var errDroppedOnController = errors.New("test: dropped on the controller")

// dropSessionsOnController closes the controller's sessions with the
// device without a CloseSession on the wire: the device keeps them.
func dropSessionsOnController(p *fabrictest.Pair) {
	for _, s := range fabrictestseam.Peer(p).Sessions() {
		s.HandlePeerLoss(errDroppedOnController)
	}
}

// deviceSessions is the device's CASE sessions, by id, with their
// subscriptions.
func deviceSessions(t *testing.T, p *fabrictest.Pair) map[uint16][]uint32 {
	t.Helper()
	out := map[uint16][]uint32{}
	if err := p.Device.Act(func(n *node.Node) error {
		sessions := n.SessionDiagnostics().Sessions
		for i := range sessions {
			if !sessions[i].PASE {
				out[sessions[i].ID] = slices.Clone(sessions[i].SubscriptionIDs)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// subscriptionSession is the device session that holds subscription id,
// or false.
func subscriptionSession(t *testing.T, p *fabrictest.Pair, id uint32) (uint16, bool) {
	t.Helper()
	for sid, subs := range deviceSessions(t, p) {
		if slices.Contains(subs, id) {
			return sid, true
		}
	}
	return 0, false
}

// TestServerSubscriptionSessionSupersessionOverTheWire ports by meaning, over
// the site's wire, two cases of matter.js at b25ed960 where a subscription
// meets a newer CASE session of its subscriber: it moves onto it
// (SessionManager.ts:750-769, ServerSubscription.ts:819-834) — and so
// survives the eviction of the session it was set up on.
func TestServerSubscriptionSessionSupersessionOverTheWire(t *testing.T) {
	t.Parallel()

	// packages/node/test/node/ServerSubscriptionSessionTest.ts:156.
	t.Run("reports on its own session when a session the controller dropped was active at the same time", func(t *testing.T) {
		t.Parallel()
		site, p, pl, ep := pairWithPlug(t, fabrictest.Options{})
		ctx := testContext(t)
		first, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOffPath(ep)}, MinIntervalFloor: interaction.Interval(time.Second), MaxIntervalCeiling: interaction.Interval(time.Minute),
			Updated: func(*interaction.Report) error { return nil },
		})
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		before := deviceSessions(t, p)
		if len(before) != 1 {
			t.Fatalf("device sessions %v; want one", before)
		}

		dropSessionsOnController(p)
		first.Close()
		reports := make(chan *interaction.Report, 64)
		second, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOffPath(ep)}, MinIntervalFloor: interaction.Interval(time.Second), MaxIntervalCeiling: interaction.Interval(time.Minute),
			Updated: func(r *interaction.Report) error { reports <- r; return nil },
		})
		if err != nil {
			t.Fatalf("Subscribe on a new session: %v", err)
		}
		t.Cleanup(second.Close)

		after := deviceSessions(t, p)
		if len(after) != 2 {
			t.Fatalf("device sessions %v; want the former session still open beside the new one", after)
		}
		newSession, ok := subscriptionSession(t, p, second.ID())
		if !ok {
			t.Fatalf("the new subscription %d is on no device session (%v)", second.ID(), after)
		}
		if _, former := before[newSession]; former {
			t.Fatalf("the new subscription is on the former session %d", newSession)
		}
		waitOnReported(ctx, t, site, p, pl, ep, reports)
	})

	// packages/node/test/node/ServerSubscriptionSessionTest.ts:176, by
	// meaning: the controller reconnects twice, and each time the device
	// moves the subscription onto the session established, leaving none on
	// the former ones; their close does not end it, and the subscriber
	// still gets its reports. Whether the new session is resumed or a full
	// CASE session is the controller's choice here; the move does not
	// depend on it. "Announced once" — SessionsBehavior's subscriptionAdded
	// not raised again by a move — has no counterpart: the node announces
	// a move as a change of both sessions' subscriptions.
	t.Run("moves to each resumed session once the controller confirms it, announced once", func(t *testing.T) {
		t.Parallel()
		site, p, pl, ep := pairWithPlug(t, fabrictest.Options{})
		ctx := testContext(t)
		reports := make(chan *interaction.Report, 64)
		sub, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOffPath(ep)}, MinIntervalFloor: interaction.Interval(time.Second), MaxIntervalCeiling: interaction.Interval(time.Hour),
			Updated: func(r *interaction.Report) error { reports <- r; return nil },
		})
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		t.Cleanup(sub.Close)
		former, ok := subscriptionSession(t, p, sub.ID())
		if !ok {
			t.Fatal("the subscription is on no device session")
		}
		holders := make([]uint16, 0, 3)
		holders = append(holders, former)
		for range 2 {
			dropSessionsOnController(p)
			if _, err := fabrictest.Read(ctx, p, onOffPath(ep)); err != nil {
				t.Fatalf("read on a new session: %v", err)
			}
			now := fabrictestseam.Peer(p).NewestSession().PeerSessionID()
			on, ok := subscriptionSession(t, p, sub.ID())
			if !ok || on != now {
				t.Fatalf("the subscription is on session %d (%v); want the newest, %d", on, ok, now)
			}
			for sid, subs := range deviceSessions(t, p) {
				if sid != now && len(subs) != 0 {
					t.Fatalf("session %d still holds %v after the move", sid, subs)
				}
			}
			holders = append(holders, now)
		}
		for _, sid := range holders[:2] {
			if err := p.Device.Act(func(n *node.Node) error { return n.Sessions().Close(sid) }); err != nil {
				t.Fatalf("close session %d: %v", sid, err)
			}
		}
		if _, ok := subscriptionSession(t, p, sub.ID()); !ok {
			t.Fatal("the subscription ended with its former sessions")
		}
		waitOnReported(ctx, t, site, p, pl, ep, reports)
	})

	// packages/node/test/node/ServerSubscriptionTest.ts:1882 — there
	// with sessions of other CATs in between, which do not supersede; here
	// the controller's sessions all carry its CATs, so the subscription
	// follows each newer session, and the eviction of the session it was
	// set up on, the least recently used, leaves it alive.
	t.Run("keeps a subscription alive when eviction closes its least recently used session for a newer one (characterization)", func(t *testing.T) {
		t.Parallel()
		site, p, _, ep := pairWithPlug(t, fabrictest.Options{})
		ctx := testContext(t)
		sub, err := fabrictest.Subscribe(ctx, p, interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOffPath(ep)}, MinIntervalFloor: interaction.Interval(time.Second), MaxIntervalCeiling: interaction.Interval(time.Hour),
			Updated: func(*interaction.Report) error { return nil },
		})
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		t.Cleanup(sub.Close)
		setUpOn, ok := subscriptionSession(t, p, sub.ID())
		if !ok {
			t.Fatal("the subscription is on no device session")
		}
		// Five newer sessions: the device holds at most five of the peer's
		// and evicts the least recently used when the sixth is established.
		var newest uint16
		for i := range operational.MaxSessionsPerPeer {
			if err := site.Step(ctx, time.Second); err != nil {
				t.Fatal(err)
			}
			dropSessionsOnController(p)
			if _, err := fabrictest.Read(ctx, p, onOffPath(ep)); err != nil {
				t.Fatalf("read %d on a new session: %v", i, err)
			}
			newest = fabrictestseam.Peer(p).NewestSession().PeerSessionID()
		}
		sessions := deviceSessions(t, p)
		if _, still := sessions[setUpOn]; still {
			t.Fatalf("the session the subscription was set up on (%d) was not evicted: %v", setUpOn, sessions)
		}
		on, ok := subscriptionSession(t, p, sub.ID())
		if !ok {
			t.Fatalf("the subscription closed with the evicted session: %v", sessions)
		}
		if on != newest {
			t.Fatalf("the subscription is on session %d; want the newest, %d", on, newest)
		}
	})
}

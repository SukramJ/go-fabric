// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/im"
)

// The subscriptions an interaction established (#established,
// packages/protocol/src/action/client/ClientInteraction.ts:248, :1121,
// :1129, :1169, :1190 at b25ed960) and its Close (:285-315): Close ends a
// plain and a sustained subscription it established, the set forgets a
// subscription that closed earlier and a sustained one's replaced inner,
// and a second interaction to the same peer keeps its own.
func TestInteractionCloseEstablished(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newScripted(t)
		reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
		cfg := interaction.Config{Subscriptions: reg, Clock: s.clock, Rand: func() float64 { return 0.5 }}
		c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), cfg)
		other := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), cfg)
		req := interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}}

		plain := subscribed(t, s, c, req, 1)
		active := make(chan bool, 4)
		sustained, err := c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Attributes: []im.ConcreteAttributePath{onOff}, Sustain: true,
			ActiveChanged: func(a bool) { active <- a },
		})
		if err != nil {
			t.Fatal(err)
		}
		establish(t, s, 2)
		if !<-active {
			t.Fatal("sustained not active")
		}
		kept := subscribed(t, s, other, req, 3)
		early := subscribed(t, s, c, req, 4)
		// plain, the sustained outer and inner, early.
		if n := interaction.EstablishedCount(c); n != 4 {
			t.Fatalf("%d established, want 4", n)
		}
		early.Close()
		if n := interaction.EstablishedCount(c); n != 3 {
			t.Fatalf("%d established after a close, want 3", n)
		}

		// The sustained one's inner is lost and replaced: the old inner
		// leaves the set, the new one joins it.
		reg.GetPeer(sustained.Peer(), 2).Close()
		if <-active {
			t.Fatal("active after the loss")
		}
		s.clock.waitTimer(t, 16875*time.Millisecond)
		s.clock.Advance(16875 * time.Millisecond)
		establish(t, s, 5)
		if !<-active {
			t.Fatal("replacement not active")
		}
		if n := interaction.EstablishedCount(c); n != 3 {
			t.Fatalf("%d established after the replacement, want 3", n)
		}

		c.Close(nil)
		<-plain.Done()
		<-sustained.Done()
		if n := interaction.EstablishedCount(c); n != 0 {
			t.Fatalf("%d established after Close", n)
		}
		if reg.GetPeer(plain.Peer(), 1) != nil || reg.GetPeer(plain.Peer(), 5) != nil {
			t.Fatal("a subscription of the closed interaction is still registered")
		}
		if as := reg.Active(); len(as) != 1 || as[0] != kept || reg.GetPeer(kept.Peer(), 3) == nil {
			t.Fatalf("active %v, want only the other interaction's", as)
		}
		other.Close(nil)
	})
}

// TestInteractionClosedEstablishesNothing: on a closed interaction a
// subscribe — plain or sustained — is refused with the close reason up
// front, and a subscription whose response arrived while Close ran is
// closed once registered instead of outliving the interaction.
func TestInteractionClosedEstablishesNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newScripted(t)
		reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
		c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg, Clock: s.clock})
		c.Close(nil)
		for _, sustain := range []bool{false, true} {
			sub, err := c.Subscribe(context.Background(), interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}, Sustain: sustain})
			if !errors.Is(err, interaction.ErrClosed) || sub != nil {
				t.Fatalf("sustain %v: %v, want ErrClosed", sustain, err)
			}
		}
		sub, err := interaction.RegisterPeerSubscription(c, 9)
		if !errors.Is(err, interaction.ErrClosed) {
			t.Fatalf("registered after Close: %v, want ErrClosed", err)
		}
		<-sub.Done()
		if reg.GetPeer(sub.Peer(), 9) != nil || len(reg.Active()) != 0 || interaction.EstablishedCount(c) != 0 {
			t.Fatal("a subscription outlives the closed interaction")
		}
	})
}

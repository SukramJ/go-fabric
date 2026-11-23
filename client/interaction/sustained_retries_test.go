// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/im"
)

// TestSustainedRetriesPerSubscription: a sustained subscription whose
// request sets Retries retries on that schedule — the refused attempts of
// an establishment (2 s, then 6 s with a backoff of 3) and the pause
// before replacing a short-lived subscription (2 s) — not on
// DefaultSustainRetries (review II, R3-api-3580, owner decision 562).
func TestSustainedRetriesPerSubscription(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newScripted(t)
		reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
		c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg, Clock: s.clock, Rand: func() float64 { return 0 }})
		active := make(chan bool, 8)
		sub, err := c.Subscribe(context.Background(), interaction.SubscribeRequest{
			Attributes:    []im.ConcreteAttributePath{onOff},
			Sustain:       true,
			Retries:       interaction.RetrySchedule{InitialInterval: 2 * time.Second, BackoffFactor: 3},
			ActiveChanged: func(a bool) { active <- a },
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, wait := range []time.Duration{2 * time.Second, 6 * time.Second} {
			r := s.next(t)
			s.reply(t, r, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusBusy))
			s.clock.waitTimer(t, wait)
			s.clock.Advance(wait)
		}
		establish(t, s, 31)
		if a := <-active; !a {
			t.Fatal("not active")
		}
		reg.CloseForPeer(sub.Peer())
		if a := <-active; a {
			t.Fatal("active after the loss")
		}
		s.clock.waitTimer(t, 2*time.Second)
		s.clock.Advance(2 * time.Second)
		establish(t, s, 32)
		if a := <-active; !a || sub.ID() != 32 {
			t.Fatalf("replacement active %v id %d", a, sub.ID())
		}
		sub.Close()
		<-sub.Done()
	})
}

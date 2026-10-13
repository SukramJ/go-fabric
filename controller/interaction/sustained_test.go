// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
)

// establish answers the next SubscribeRequest with a priming report and
// subscription id.
func establish(t *testing.T, s *scripted, id uint32) {
	t.Helper()
	r := s.next(t)
	if r.proto.Opcode != im.OpcodeSubscribeRequest {
		t.Fatalf("opcode 0x%02X, want a SubscribeRequest", r.proto.Opcode)
	}
	s.reply(t, r, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, true))
	s.reply(t, r, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, id, 30))
}

// TestSustainedSubscription: a sustained subscription is returned at once
// (ClientInteraction.ts:1092-1118); a failed bootstrap read and a refused
// attempt are retried on the schedule of SustainedSubscription.ts:483-499
// — 15 s, then 30 s, a quarter of jitter (with a draw of 0.5: 16.875 s,
// 33.75 s); once established it is active; a lost underlying subscription
// makes it inactive and is replaced after the reachability check (:163-177,
// :268-299), the id following; Close ends the loop, the underlying
// subscription and calls Closed once (:301-309, ClientSubscription.ts:60-92).
func TestSustainedSubscription(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg, Clock: s.clock, Rand: func() float64 { return 0.5 }})
	var mu sync.Mutex
	var states []bool
	active := make(chan bool, 8)
	closed := make(chan struct{}, 2)
	bootstraps := 0
	sub, err := c.Subscribe(context.Background(), interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{onOff},
		Sustain:    true,
		BootstrapRead: func(context.Context) error {
			mu.Lock()
			defer mu.Unlock()
			bootstraps++
			if bootstraps == 1 {
				return errors.New("bootstrap failed")
			}
			return nil
		},
		ActiveChanged: func(a bool) {
			mu.Lock()
			states = append(states, a)
			mu.Unlock()
			active <- a
		},
		Closed: func() { closed <- struct{}{} },
	})
	if err != nil || sub.Active() || sub.ID() != interaction.NoSubscription || sub.MaxInterval() != time.Hour {
		t.Fatalf("sustained subscription %v active %v id %X", err, sub.Active(), sub.ID())
	}
	s.clock.waitTimer(t, 16875*time.Millisecond)
	s.clock.Advance(16875 * time.Millisecond)
	refused := s.next(t)
	s.reply(t, refused, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusResourceExhausted))
	s.clock.waitTimer(t, 33750*time.Millisecond)
	s.clock.Advance(33750 * time.Millisecond)
	establish(t, s, 11)
	if a := <-active; !a {
		t.Fatal("not active once established")
	}
	if sub.ID() != 11 || sub.MaxInterval() != 30*time.Second || !sub.Active() {
		t.Fatalf("id %d, max interval %v", sub.ID(), sub.MaxInterval())
	}
	if len(reg.Active()) != 1 || reg.GetPeer(sub.Peer(), 11) == nil {
		t.Fatal("registration")
	}

	// The underlying subscription is lost: replaced at once (the session
	// provider reports reachable).
	reg.CloseForPeer(sub.Peer())
	if a := <-active; a {
		t.Fatal("active after the loss")
	}
	establish(t, s, 12)
	if a := <-active; !a || sub.ID() != 12 {
		t.Fatalf("replacement active %v id %d", a, sub.ID())
	}

	sub.Close()
	select {
	case <-sub.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("not done after Close")
	}
	<-closed
	if len(closed) != 0 || reg.GetPeer(sub.Peer(), 12) != nil || len(reg.Active()) != 0 || sub.Active() {
		t.Fatal("state after Close")
	}
	mu.Lock()
	defer mu.Unlock()
	if bootstraps != 2 || len(states) != 4 || states[3] {
		t.Fatalf("bootstraps %d, active states %v", bootstraps, states)
	}
}

// TestSustainedCloseWhileRetrying: Close during the retry wait ends the
// loop (SustainedSubscription.ts:244-249).
func TestSustainedCloseWhileRetrying(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg, Clock: s.clock, Rand: func() float64 { return 0 }})
	sub, err := c.Subscribe(context.Background(), interaction.SubscribeRequest{Attributes: []im.ConcreteAttributePath{onOff}, Sustain: true})
	if err != nil {
		t.Fatal(err)
	}
	s.reply(t, s.next(t), im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusBusy))
	s.clock.waitTimer(t, 15*time.Second)
	sub.Close()
	select {
	case <-sub.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("not done after Close")
	}
}

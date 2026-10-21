// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
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

// TestSustainedRefreshRequest: the request is refreshed before every
// attempt but the very first and right after a successful bootstrap read
// (SustainedSubscription.ts:182-186, :200-203); the refreshed paths and
// filters go out, the subscription keeps its own callbacks.
func TestSustainedRefreshRequest(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg, Clock: s.clock, Rand: func() float64 { return 0 }})
	var mu sync.Mutex
	refreshes := 0
	active := make(chan bool, 8)
	sub, err := c.Subscribe(context.Background(), interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{onOff}, Sustain: true,
		ActiveChanged: func(a bool) { active <- a },
		RefreshRequest: func(r interaction.SubscribeRequest) interaction.SubscribeRequest {
			mu.Lock()
			refreshes++
			v := uint32(refreshes)
			mu.Unlock()
			r.DataVersionFilters = []im.DataVersionFilter{{Endpoint: 1, Cluster: 0x0006, DataVersion: v}}
			r.EventFilters = []im.EventMinimumNumber{{EventMin: uint64(v) + 100}}
			r.Closed, r.ActiveChanged = nil, nil
			return r
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	filters := func(m msg) ([]im.DataVersionFilter, []im.EventMinimumNumber) {
		t.Helper()
		req, err := im.UnmarshalSubscribeRequestTLV(tlv.NewDecoder(m.payload))
		if err != nil {
			t.Fatal(err)
		}
		return req.DataVersionFilters, req.EventFilters
	}
	first := s.next(t)
	if dv, ev := filters(first); len(dv) != 0 || len(ev) != 0 {
		t.Fatalf("the first attempt was refreshed: %+v %+v", dv, ev)
	}
	s.reply(t, first, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusBusy))
	s.clock.waitTimer(t, 15*time.Second)
	s.clock.Advance(15 * time.Second)
	second := s.next(t)
	if dv, ev := filters(second); len(dv) != 1 || dv[0].DataVersion != 1 || len(ev) != 1 || ev[0].EventMin != 101 {
		t.Fatalf("second attempt %+v %+v", dv, ev)
	}
	s.reply(t, second, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, true))
	s.reply(t, second, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, 21, 30))
	if a := <-active; !a {
		t.Fatal("not active")
	}
	// A replacement is refreshed from the original request.
	reg.CloseForPeer(sub.Peer())
	if a := <-active; a {
		t.Fatal("active after the loss")
	}
	third := s.next(t)
	if dv, _ := filters(third); len(dv) != 1 || dv[0].DataVersion != 2 {
		t.Fatalf("replacement %+v", dv)
	}
	s.reply(t, third, im.InteractionModelProtocolID, im.OpcodeReportData, reportChunk(t, false, true))
	s.reply(t, third, im.InteractionModelProtocolID, im.OpcodeSubscribeResponse, subscribeResponse(t, 22, 30))
	if a := <-active; !a || sub.ID() != 22 {
		t.Fatalf("replacement active %v id %d", a, sub.ID())
	}
	sub.Close()
	<-sub.Done()
}

// TestSustainedRefreshAfterBootstrap: a successful bootstrap read refreshes
// the request before the first subscription (SustainedSubscription.ts:199-203).
func TestSustainedRefreshAfterBootstrap(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	reg := interaction.NewSubscriptions(interaction.SubscriptionsConfig{Clock: s.clock})
	c := interaction.Bind(interaction.SessionProvider(s.mgr, s.session), interaction.Config{Subscriptions: reg, Clock: s.clock, Rand: func() float64 { return 0 }})
	sub, err := c.Subscribe(context.Background(), interaction.SubscribeRequest{
		Attributes: []im.ConcreteAttributePath{onOff}, Sustain: true,
		BootstrapRead: func(context.Context) error { return nil },
		RefreshRequest: func(r interaction.SubscribeRequest) interaction.SubscribeRequest {
			r.DataVersionFilters = []im.DataVersionFilter{{Endpoint: 1, Cluster: 0x0006, DataVersion: 9}}
			return r
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := s.next(t)
	req, err := im.UnmarshalSubscribeRequestTLV(tlv.NewDecoder(m.payload))
	if err != nil || len(req.DataVersionFilters) != 1 || req.DataVersionFilters[0].DataVersion != 9 {
		t.Fatalf("the request after the bootstrap read %+v (%v)", req.DataVersionFilters, err)
	}
	sub.Close()
	<-sub.Done()
}

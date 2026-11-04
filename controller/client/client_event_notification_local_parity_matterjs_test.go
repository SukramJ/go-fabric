// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client_test

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/internal/fabrictestseam"
)

// A port of matter.js packages/node/test/node/ClientEventNotificationTest.ts
// at 862ad75f, the subscriptions a host opens itself (:737-790): their
// events reach the host's own Updated only — never the node's Event hook
// nor its stored event number (ClientNodeInteraction.ts:115-118, a host's
// subscription leaves emitEvents unset) — and a sustained one resubscribes
// above the last event it delivered (SustainedSubscription.ts:379-384).

// eventNumbers records the event numbers of a subscription's reports.
type eventNumbers struct {
	mu sync.Mutex
	n  []uint64
}

func (e *eventNumbers) updated(r *interaction.Report) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range r.Events {
		e.n = append(e.n, r.Events[i].Number)
	}
	return nil
}

func (e *eventNumbers) get() []uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]uint64(nil), e.n...)
}

// quietSwitchPair is switchPair with autoSubscribe false.
func quietSwitchPair(t *testing.T) *switchPair {
	t.Helper()
	sp := newSwitchPair(t, false)
	if err := sp.site.Run(parityCtx(t), func(ctx context.Context) error { return sp.node.SetAutoSubscribe(ctx, false) }); err != nil {
		t.Fatal(err)
	}
	sp.mu.Lock()
	sp.events = nil
	sp.mu.Unlock()
	return sp
}

func (sp *switchPair) urgentSwitchEvents() []client.EventPath {
	return []client.EventPath{{Endpoint: sp.ep, Cluster: swdef.ClusterID, HasEndpoint: true, HasCluster: true, IsUrgent: true}}
}

func TestClientEventNotificationLocalParity(t *testing.T) {
	t.Parallel()

	t.Run("returns a narrow subscription's events to the caller only", func(t *testing.T) {
		t.Parallel()
		sp := quietSwitchPair(t)
		before := client.MaxEventNumber(sp.node)
		numbers := &eventNumbers{}
		var s *interaction.Subscription
		if err := sp.site.Run(parityCtx(t), func(ctx context.Context) (err error) {
			s, err = sp.node.Subscribe(ctx, client.SubscribeRequest{SubscribeRequest: client.SubscribePaths{
				Events: sp.urgentSwitchEvents(), EventFilters: []im.EventMinimumNumber{{EventMin: before + 1}}, Updated: numbers.updated,
			}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		sp.press(t)
		if err := sp.site.Eventually(parityCtx(t), "the event", func() bool { return len(numbers.get()) > 0 }); err != nil {
			t.Fatal(err)
		}
		if err := sp.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		s.Close()
		sp.mu.Lock()
		observed := len(sp.events)
		sp.mu.Unlock()
		if len(numbers.get()) != 1 || observed != 0 || client.MaxEventNumber(sp.node) != before {
			t.Fatalf("numbers %v, observed %d, stored %d (before %d)", numbers.get(), observed, client.MaxEventNumber(sp.node), before)
		}
	})

	t.Run("resubscribes a sustained subscription above the last event it delivered", func(t *testing.T) {
		t.Parallel()
		sp := quietSwitchPair(t)
		before := client.MaxEventNumber(sp.node)
		numbers := &eventNumbers{}
		var s *interaction.Subscription
		if err := sp.site.Run(parityCtx(t), func(ctx context.Context) (err error) {
			s, err = sp.node.Subscribe(ctx, client.SubscribeRequest{SubscribeRequest: client.SubscribePaths{
				Events: sp.urgentSwitchEvents(), EventFilters: []im.EventMinimumNumber{{EventMin: before + 1}}, Sustain: true, Updated: numbers.updated,
			}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if err := sp.site.Eventually(parityCtx(t), "the subscription", s.Active); err != nil {
			t.Fatal(err)
		}
		sp.press(t)
		if err := sp.site.Eventually(parityCtx(t), "the first event", func() bool { return len(numbers.get()) == 1 }); err != nil {
			t.Fatal(err)
		}
		old := s.ID()
		subs := fabrictestseam.Subscriptions(sp.pair.Controller)
		subs.CloseForPeer(subs.Active()[0].Peer())
		if err := sp.site.Eventually(parityCtx(t), "the resubscription", func() bool { return s.Active() && s.ID() != old }); err != nil {
			t.Fatal(err)
		}
		sp.press(t)
		if err := sp.site.Eventually(parityCtx(t), "the second event", func() bool { return len(numbers.get()) > 1 }); err != nil {
			t.Fatal(err)
		}
		if err := sp.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		got := numbers.get()
		maxEvent, ok := s.MaxEventNumber()
		if len(got) != 2 || got[1] <= got[0] || !ok || maxEvent != got[1] || client.MaxEventNumber(sp.node) != before {
			t.Fatalf("numbers %v, subscription max %d %v, node %d (before %d)", got, maxEvent, ok, client.MaxEventNumber(sp.node), before)
		}
	})
}

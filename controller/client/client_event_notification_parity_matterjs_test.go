// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientEventNotificationTest.ts
// at 862ad75f: the delivery of a peer's events to the controller's
// observers and the event number the client node stores. matter.js's
// observers are peer.eventsOf(...) and the ChangeNotificationService; the
// Go client node has one observer, Hooks.Event, which receives every event
// with its decoded payload, and stores the number after it returns
// (client.MaxEventNumber is matter.js's NetworkClient maxEventNumber).
// matter.js mounts the switch on the root; the Go root serves no Switch,
// so the port's switch is a bridged GenericSwitch.
package client_test

import (
	"context"
	"sync"
	"testing"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	swdef "github.com/SukramJ/go-fabric/cluster/spec/switchcluster"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// seenEvent is an event the hook received, with the number the node had
// stored while the hook ran.
type seenEvent struct {
	event  uint32
	number uint64
	stored uint64
	value  any
}

// switchPair is ClientEventNotificationTest.ts:264-287 switchPair: a
// commissioned pair whose device has a momentary switch (with release and
// multi-press when multi), the client node subscribed, the hook recording
// the switch's events.
type switchPair struct {
	site *fabrictest.Site
	pair *fabrictest.Pair
	node *client.Node
	sw   *device.GenericSwitch
	ep   uint16

	mu     sync.Mutex
	events []seenEvent
	nodeOf func() *client.Node
}

func newSwitchPair(t *testing.T, multi bool) *switchPair {
	t.Helper()
	site := quietSite(t)
	features := uint32(swdef.FeatureMomentarySwitch | swdef.FeatureMomentarySwitchRelease)
	if multi {
		features |= uint32(swdef.FeatureMomentarySwitchMultiPress)
	}
	sw, err := device.NewGenericSwitch(device.Options{StableKey: endpoint.StringKey("switch:1"), Name: "switch:1", Clock: site.Clock()},
		device.WithFeatures(swdef.ClusterID, features))
	if err != nil {
		t.Fatal(err)
	}
	sp := &switchPair{site: site, sw: sw}
	sp.nodeOf = func() *client.Node {
		sp.mu.Lock()
		defer sp.mu.Unlock()
		return sp.node
	}
	sp.pair = site.UncommissionedPair(fabrictest.PairOptions{
		Device: fabrictest.Topology{Bridged: []node.Endpoint{sw}},
		Controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) {
			rc.Node.Hooks.Event = func(_ uint16, cluster, event uint32, e interaction.EventData, v any) {
				if cluster != swdef.ClusterID {
					return
				}
				var stored uint64
				if n := sp.nodeOf(); n != nil {
					stored = client.MaxEventNumber(n)
				}
				sp.mu.Lock()
				sp.events = append(sp.events, seenEvent{event, e.Number, stored, v})
				sp.mu.Unlock()
			}
		}},
	})
	n, err := sp.pair.Controller.CommissionNode(parityCtx(t), sp.pair.Device, client.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sp.mu.Lock()
	sp.node = n
	sp.mu.Unlock()
	if err := site.Eventually(parityCtx(t), "the default subscription", n.SubscriptionActive); err != nil {
		t.Fatal(err)
	}
	sp.ep, _ = sp.pair.Device.EndpointOf("switch:1")
	return sp
}

// emit emits a switch event on the device.
func (sp *switchPair) emit(t *testing.T, event uint32, payload any) {
	t.Helper()
	if err := sp.sw.Switch().Emit(sp.ep, event, payload); err != nil {
		t.Fatal(err)
	}
}

// press is switchPair's press: an InitialPress at position 1.
func (sp *switchPair) press(t *testing.T) {
	t.Helper()
	sp.emit(t, swdef.EventInitialPress, swdef.InitialPressEvent{NewPosition: 1})
}

// seen waits for at least n switch events and returns them.
func (sp *switchPair) seen(t *testing.T, n int) []seenEvent {
	t.Helper()
	if err := sp.site.Eventually(parityCtx(t), "the switch events", func() bool {
		sp.mu.Lock()
		defer sp.mu.Unlock()
		return len(sp.events) >= n
	}); err != nil {
		t.Fatal(err)
	}
	if err := sp.site.Settle(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return append([]seenEvent(nil), sp.events...)
}

func TestClientEventNotificationEventsOfParity(t *testing.T) {
	t.Parallel()

	t.Run("delivers MS initialPress to client", func(t *testing.T) {
		t.Parallel()
		sp := newSwitchPair(t, false)
		sp.press(t)
		if ev, ok := sp.seen(t, 1)[0].value.(swdef.InitialPressEvent); !ok || ev.NewPosition != 1 {
			t.Fatalf("InitialPress %#v", sp.seen(t, 1)[0].value)
		}
	})

	t.Run("delivers MS, MSR and MSM events to client", func(t *testing.T) {
		t.Parallel()
		sp := newSwitchPair(t, true)
		sp.emit(t, swdef.EventInitialPress, swdef.InitialPressEvent{NewPosition: 1})
		sp.emit(t, swdef.EventShortRelease, swdef.ShortReleaseEvent{PreviousPosition: 1})
		sp.emit(t, swdef.EventMultiPressComplete, swdef.MultiPressCompleteEvent{PreviousPosition: 1, TotalNumberOfPressesCounted: 2})
		got := sp.seen(t, 3)
		want := []any{
			swdef.InitialPressEvent{NewPosition: 1},
			swdef.ShortReleaseEvent{PreviousPosition: 1},
			swdef.MultiPressCompleteEvent{PreviousPosition: 1, TotalNumberOfPressesCounted: 2},
		}
		if len(got) != 3 {
			t.Fatalf("%d events, want 3", len(got))
		}
		for i := range want {
			if got[i].value != want[i] {
				t.Fatalf("event %d %#v, want %#v", i, got[i].value, want[i])
			}
		}
	})
}

func TestClientEventNotificationEventNumbersParity(t *testing.T) {
	t.Parallel()

	t.Run("stores the number of a default subscription event only after observers received it", func(t *testing.T) {
		t.Parallel()
		sp := newSwitchPair(t, false)
		sp.press(t)
		got := sp.seen(t, 1)
		if len(got) != 1 || got[0].stored >= got[0].number {
			t.Fatalf("events %+v: the number was stored before the observer received it", got)
		}
		if n := client.MaxEventNumber(sp.node); n != got[0].number {
			t.Fatalf("stored %d, want %d", n, got[0].number)
		}
	})

	t.Run("emits a default subscription event to observers once", func(t *testing.T) {
		t.Parallel()
		sp := newSwitchPair(t, false)
		sp.press(t)
		got := sp.seen(t, 1)
		if len(got) != 1 || client.MaxEventNumber(sp.node) != got[0].number {
			t.Fatalf("events %+v, stored %d", got, client.MaxEventNumber(sp.node))
		}
	})

	t.Run("does not deliver the commissioning read's events again with the subscription", func(t *testing.T) {
		t.Parallel()
		site := quietSite(t)
		var mu sync.Mutex
		var startUps []uint64
		p := site.UncommissionedPair(fabrictest.PairOptions{Controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) {
			rc.Node.Hooks.Event = func(_ uint16, cluster, event uint32, e interaction.EventData, _ any) {
				if cluster == basicinfo.ClusterID && event == basicinfo.EventStartUp {
					mu.Lock()
					startUps = append(startUps, e.Number)
					mu.Unlock()
				}
			}
		}}})
		n, err := p.Controller.CommissionNode(parityCtx(t), p.Device, client.CommissionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := site.Eventually(parityCtx(t), "the default subscription", n.SubscriptionActive); err != nil {
			t.Fatal(err)
		}
		if err := site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(startUps) != 1 || client.MaxEventNumber(n) < startUps[0] {
			t.Fatalf("StartUp events %v, stored %d", startUps, client.MaxEventNumber(n))
		}
	})

	t.Run("resubscribes the default subscription above the number it stored", func(t *testing.T) {
		t.Parallel()
		sp := newSwitchPair(t, false)
		sp.press(t)
		first := sp.seen(t, 1)[0].number
		var mu sync.Mutex
		var mins []uint64
		client.ObserveRequests(sp.node, func(r interaction.SubscribeRequest) {
			if len(r.EventFilters) == 1 {
				mu.Lock()
				mins = append(mins, r.EventFilters[0].EventMin)
				mu.Unlock()
			}
		})
		// matter.js closes the device's sessions; the port loses the
		// underlying subscription on the controller (as TestLiveClientNode).
		subs := sp.pair.Controller.Subscriptions()
		subs.CloseForPeer(subs.Active()[0].Peer())
		if err := sp.site.Eventually(parityCtx(t), "the replacement subscription", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(mins) > 0 && sp.node.SubscriptionActive()
		}); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		if mins[0] != first+1 {
			t.Fatalf("the replacement asks for events from %d, want %d", mins[0], first+1)
		}
		mu.Unlock()
		sp.press(t)
		got := sp.seen(t, 2)
		if len(got) != 2 || got[1].number <= got[0].number || client.MaxEventNumber(sp.node) != got[1].number {
			t.Fatalf("events %+v, stored %d", got, client.MaxEventNumber(sp.node))
		}
	})

	t.Run("never lowers the stored number", func(t *testing.T) {
		t.Parallel()
		sp := newSwitchPair(t, false)
		sp.press(t)
		number := sp.seen(t, 1)[0].number
		client.DeliverEvent(sp.node, interaction.EventData{Path: im.ConcreteEventPath{Endpoint: sp.ep, Cluster: swdef.ClusterID, Event: swdef.EventInitialPress}, Number: 1})
		if got := client.MaxEventNumber(sp.node); got != number {
			t.Fatalf("stored %d after a lower event, want %d", got, number)
		}
	})

	t.Run("resubscribes above the stored number after a restart", func(t *testing.T) {
		t.Parallel()
		sp := newSwitchPair(t, false)
		sp.press(t)
		number := sp.seen(t, 1)[0].number
		id := sp.node.ID()
		c := sp.pair.Controller
		if err := c.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		restored := c.Registry().Get(id)
		if restored == nil || client.MaxEventNumber(restored) != number {
			t.Fatalf("restored node %v, stored %d", restored, client.MaxEventNumber(restored))
		}
		var mu sync.Mutex
		var mins []uint64
		client.ObserveRequests(restored, func(r interaction.SubscribeRequest) {
			if len(r.EventFilters) == 1 {
				mu.Lock()
				mins = append(mins, r.EventFilters[0].EventMin)
				mu.Unlock()
			}
		})
		sp.mu.Lock()
		before := len(sp.events)
		sp.node = restored
		sp.mu.Unlock()
		if err := sp.site.Run(parityCtx(t), func(ctx context.Context) error { return restored.Start(ctx) }); err != nil {
			t.Fatal(err)
		}
		if err := sp.site.Eventually(parityCtx(t), "the restored subscription", restored.SubscriptionActive); err != nil {
			t.Fatal(err)
		}
		if err := sp.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		sp.mu.Lock()
		replayed := len(sp.events) - before
		sp.mu.Unlock()
		if replayed != 0 || len(mins) == 0 || mins[0] != number+1 || client.MaxEventNumber(restored) != number {
			t.Fatalf("replayed %d, asked from %v, stored %d; want none, %d, %d", replayed, mins, client.MaxEventNumber(restored), number+1, number)
		}
	})
}

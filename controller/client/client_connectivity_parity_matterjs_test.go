// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientConnectivityTest.ts at
// 862ad75f: the client node over a device that goes away and comes back —
// commands to an unreachable peer, the default subscription lost and
// re-established (after a timeout and a long absence), closing and
// disabling during reconnection, the controller restarted with an
// uncommissioned node or an unavailable device. The cases on several
// device addresses, moved addresses and blocked DNS-SD stay with the peer
// set's tests (controller/peer) and in the register's missing list:
// fabrictest gives a device one address and answers DNS-SD in-process.
//
// matter.js's lifecycle.isOnline is the node's connection state, a
// subscription id the controller's underlying subscription of the peer
// (interaction.Subscriptions), ClientSubscription.NO_SUBSCRIPTION its
// absence.
package client_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/controller/peer"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
)

// toggle invokes OnOff Toggle on the pair's light through the node.
func (cp *clientPair) toggle(t *testing.T, n *client.Node) error {
	t.Helper()
	var ierr error
	if err := cp.site.Run(parityCtx(t), func(ctx context.Context) error {
		_, ierr = n.Invoke(ctx, im.ConcreteCommandPath{Endpoint: cp.lightEP, Cluster: onoff.ClusterID, Command: onoff.CmdToggle}, nil, interaction.InvokeOptions{})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return ierr
}

// subscriptionID is the id of the controller's underlying subscription of
// the pair's peer, 0 (NoSubscription) without one.
func (cp *clientPair) subscriptionID() uint32 {
	for _, s := range cp.pair.Controller.Subscriptions().Active() {
		return s.ID()
	}
	return interaction.NoSubscription
}

// onOffIs waits for the light's OnOff in the image.
func (cp *clientPair) onOffIs(t *testing.T, n *client.Node, want bool) {
	t.Helper()
	cp.eventually(t, "OnOff in the image", func() bool {
		v, ok := imageValue(n, cp.lightPath(onoff.ClusterID, onoff.AttrOnOff))
		return ok && v.El.Bool == want
	})
}

// resubscribes is the shared body of "resubscribes on timeout" and
// "resubscribes after extended offline period": the device stops, the
// subscription is lost, offline passes, the device starts again and a new
// subscription carries the light's changes.
func resubscribes(t *testing.T, offline time.Duration) {
	t.Helper()
	cp := newClientPair(t, clientPairOptions{})
	initial := cp.subscriptionID()
	if !cp.node.SubscriptionActive() || initial == interaction.NoSubscription {
		t.Fatal("no active subscription")
	}
	if err := cp.pair.Device.Stop(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	cp.eventually(t, "the subscription lost", func() bool { return !cp.node.SubscriptionActive() })
	if offline > 0 {
		cp.advance(t, offline)
	}
	if err := cp.pair.Device.Start(parityCtx(t)); err != nil {
		t.Fatal(err)
	}
	cp.eventually(t, "the subscription back", cp.node.SubscriptionActive)
	if id := cp.subscriptionID(); id == interaction.NoSubscription || id == initial {
		t.Fatalf("subscription id %d, want a new one (was %d)", id, initial)
	}
	cp.onOffIs(t, cp.node, false)
	if err := cp.toggle(t, cp.node); err != nil {
		t.Fatal(err)
	}
	cp.onOffIs(t, cp.node, true)
}

func TestClientConnectivityParity(t *testing.T) {
	t.Parallel()

	t.Run("throws error if node cannot be reached", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if err := cp.pair.Device.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if err := cp.toggle(t, cp.node); err == nil {
			t.Fatal("a command to a stopped device succeeded")
		}
	})

	t.Run("reconnects and updates connection status", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		if err := cp.pair.Device.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		if err := cp.toggle(t, cp.node); err == nil {
			t.Fatal("a command to a stopped device succeeded")
		}
		cp.advance(t, 5*time.Second)
		if err := cp.pair.Device.Start(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		// toggle(undefined, { connectionTimeout: Minutes(5) })
		cp.pair.Controller.Peers().SetTiming(peer.Timing{DefaultConnectionTimeout: 5 * time.Minute})
		if err := cp.toggle(t, cp.node); err != nil {
			t.Fatalf("the command after the device came back: %v", err)
		}
	})

	t.Run("resubscribes on timeout", func(t *testing.T) {
		t.Parallel()
		resubscribes(t, 0)
	})

	t.Run("signals subscription alive when a sustained subscription re-establishes", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		alive := 0
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) {
			rc.Node.Hooks.SubscriptionAlive = func() {
				mu.Lock()
				alive++
				mu.Unlock()
			}
		}}})
		if err := cp.pair.Device.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		cp.eventually(t, "the subscription lost", func() bool { return !cp.node.SubscriptionActive() })
		mu.Lock()
		alive = 0
		mu.Unlock()
		if err := cp.pair.Device.Start(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		cp.eventually(t, "the subscription back", cp.node.SubscriptionActive)
		mu.Lock()
		defer mu.Unlock()
		if alive == 0 {
			t.Fatal("no subscription-alive signal on the re-established subscription")
		}
	})

	t.Run("resubscribes after extended offline period", func(t *testing.T) {
		t.Parallel()
		resubscribes(t, 5*time.Minute)
	})

	t.Run("closes cleanly during sustained subscription reconnection", func(t *testing.T) {
		t.Parallel()
		errs := &errorLog{}
		log := slog.New(errs)
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Logger: log, Registry: func(rc *client.RegistryConfig) {
			rc.Logger, rc.Node.Logger = log, log
		}}})
		if err := cp.pair.Device.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		cp.eventually(t, "the subscription lost", func() bool { return !cp.node.SubscriptionActive() })
		cp.run(t, cp.pair.Controller.Registry().Stop)
		cp.pair.Controller.Close()
		if e := errs.take(); len(e) != 0 {
			t.Fatalf("errors logged: %v", e)
		}
	})

	t.Run("terminates the sustained subscription when the node is disabled", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		cp.run(t, cp.node.Disable)
		if cp.node.SubscriptionActive() || cp.subscriptionID() != interaction.NoSubscription {
			t.Fatal("the subscription survived the disable")
		}
	})

	t.Run("establishes a fresh sustained subscription after disable/enable", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		initial := cp.subscriptionID()
		cp.run(t, cp.node.Disable)
		if cp.node.SubscriptionActive() {
			t.Fatal("subscribed while disabled")
		}
		cp.run(t, cp.node.Enable)
		cp.eventually(t, "the fresh subscription", cp.node.SubscriptionActive)
		if id := cp.subscriptionID(); id == interaction.NoSubscription || id == initial {
			t.Fatalf("subscription id %d, want a new one (was %d)", id, initial)
		}
	})

	t.Run("does not crash on restart when uncommissioned peer exists", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) { rc.AutoStart = true }}})
		id := cp.node.ID()
		client.UpdateRecord(cp.node, func(r *client.Record) { r.PeerAddress = nil })
		c := cp.pair.Controller
		if err := c.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		cp.run(t, c.Registry().Start)
		n := c.Registry().Get(id)
		if n == nil || n.IsCommissioned() {
			t.Fatalf("restored node %v: want the uncommissioned node kept", n)
		}
	})

	t.Run("shuts down without errors whilst establishing exchange", func(t *testing.T) {
		t.Parallel()
		errs := &errorLog{}
		log := slog.New(errs)
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Logger: log, Registry: func(rc *client.RegistryConfig) {
			rc.AutoStart, rc.Logger, rc.Node.Logger = true, log, log
		}}})
		if err := cp.pair.Device.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		c := cp.pair.Controller
		if err := c.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		cp.run(t, c.Registry().Start)
		cp.advance(t, 30*time.Minute)
		c.Close()
		if e := errs.take(); len(e) != 0 {
			t.Fatalf("errors logged: %v", e)
		}
	})

	t.Run("subscribes to a peer that is not initially available (start/stop)", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) { rc.AutoStart = true }}})
		id := cp.node.ID()
		if err := cp.pair.Device.Stop(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		c := cp.pair.Controller
		if err := c.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		cp.run(t, c.Registry().Start)
		cp.advance(t, 5*time.Minute)
		n := c.Registry().Get(id)
		if n == nil || n.SubscriptionActive() {
			t.Fatal("subscribed to a stopped device")
		}
		if err := cp.pair.Device.Start(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		cp.eventually(t, "the subscription once the device is back", n.SubscriptionActive)
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/system/network/ConnectionStateTest.ts
// at 862ad75f.
//
// matter.js commissions a pair on a MockSite and reads the controller's
// peer (subscribedPeer, node-helpers.ts:548-565): lifecycle.connectionState
// and connectionStateChanged. The Go peer is the registry's client.Node of
// the commissioned device (clientPair): Node.ConnectionState and the
// ConnectionStateChanged hook, which every node of the registry gets
// (RegistryConfig.Node.Hooks). loseSubscription (:20-27) is Device.Stop
// and the default subscription found inactive on the site's clock.
//
// matter.js emits the protocol peer's establishmentUnresponsive by hand;
// the Go peer's signal is not emitted from outside (client/peer
// unresponsiveDetector), so the port lets it fire as it does in the field:
// the stopped device leaves the sustained subscription's CASE
// re-establishment unanswered until the detector reports it. A repeated
// signal is the detector of the next attempt. The ICD case needs the
// controller-side IcdClient (the register names why).
package client_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// stateLog records every connection state transition of the registry's
// nodes.
type stateLog struct {
	mu     sync.Mutex
	states []client.ConnectionState
}

func (l *stateLog) record(s client.ConnectionState) {
	l.mu.Lock()
	l.states = append(l.states, s)
	l.mu.Unlock()
}

// since is the transitions after mark.
func (l *stateLog) since(mark int) []client.ConnectionState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.states[mark:])
}

func (l *stateLog) mark() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.states)
}

// connectedPair is addCommissionedPair with subscribedPeer and the
// transitions recorded.
func connectedPair(t *testing.T) (*clientPair, *stateLog) {
	t.Helper()
	log := &stateLog{}
	cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{
		Registry: func(rc *client.RegistryConfig) { rc.Node.Hooks.ConnectionStateChanged = log.record },
	}})
	return cp, log
}

// loseSubscription is loseSubscription (:20-27).
func (cp *clientPair) loseSubscription(t *testing.T) {
	t.Helper()
	if err := cp.site.Run(parityCtx(t), cp.pair.Device.Stop); err != nil {
		t.Fatal(err)
	}
	cp.eventually(t, "the subscription inactive", func() bool { return !cp.node.SubscriptionActive() })
	cp.eventually(t, "the node out of Connected", func() bool { return cp.node.ConnectionState() != client.Connected })
}

func (cp *clientPair) state() client.ConnectionState { return cp.node.ConnectionState() }

// TestConnectionStateParityMatterJS ports the describe "ConnectionState"
// (ConnectionStateTest.ts:29-207).
func TestConnectionStateParityMatterJS(t *testing.T) {
	// :34-41.
	t.Run("reports Connected with an active subscription", func(t *testing.T) {
		cp, _ := connectedPair(t)
		if cp.state() != client.Connected {
			t.Errorf("state %v, want Connected", cp.state())
		}
	})

	// :43-52.
	t.Run("transitions to Reconnecting when the subscription is lost", func(t *testing.T) {
		cp, _ := connectedPair(t)
		cp.loseSubscription(t)
		if cp.state() != client.Reconnecting {
			t.Errorf("state %v, want Reconnecting", cp.state())
		}
	})

	// :54-67.
	t.Run("transitions to WaitingForDeviceDiscovery on an establishment-unresponsive signal", func(t *testing.T) {
		cp, _ := connectedPair(t)
		cp.loseSubscription(t)
		if cp.state() != client.Reconnecting {
			t.Fatalf("state %v after the loss, want Reconnecting", cp.state())
		}
		cp.eventually(t, "WaitingForDeviceDiscovery", func() bool { return cp.state() == client.WaitingForDeviceDiscovery })
	})

	// :69-90.
	t.Run("emits connectionStateChanged once per transition and is idempotent under repeated signals", func(t *testing.T) {
		cp, log := connectedPair(t)
		mark := log.mark()
		cp.loseSubscription(t)
		if got := log.since(mark); !slices.Equal(got, []client.ConnectionState{client.Reconnecting}) {
			t.Fatalf("transitions %v after the loss, want [Reconnecting]", got)
		}
		cp.eventually(t, "WaitingForDeviceDiscovery", func() bool { return cp.state() == client.WaitingForDeviceDiscovery })
		want := []client.ConnectionState{client.Reconnecting, client.WaitingForDeviceDiscovery}
		if got := log.since(mark); !slices.Equal(got, want) {
			t.Fatalf("transitions %v, want %v", got, want)
		}
		// The next unanswered establishments signal again; the latch
		// emits nothing more.
		cp.advance(t, 2*advanceStep*30)
		if got := log.since(mark); !slices.Equal(got, want) {
			t.Errorf("transitions %v after repeated signals, want %v", got, want)
		}
	})

	// :92-115.
	t.Run("recovers WaitingForDeviceDiscovery -> Reconnecting -> Connected", func(t *testing.T) {
		cp, log := connectedPair(t)
		cp.loseSubscription(t)
		cp.eventually(t, "WaitingForDeviceDiscovery", func() bool { return cp.state() == client.WaitingForDeviceDiscovery })
		mark := log.mark()
		if err := cp.site.Run(parityCtx(t), cp.pair.Device.Start); err != nil {
			t.Fatal(err)
		}
		cp.eventually(t, "the subscription active again", cp.node.SubscriptionActive)
		cp.eventually(t, "Connected", func() bool { return cp.state() == client.Connected })
		got := log.since(mark)
		if !slices.Contains(got, client.Reconnecting) || got[len(got)-1] != client.Connected {
			t.Errorf("transitions %v, want Reconnecting on the way and Connected last", got)
		}
	})

	// :117-132.
	t.Run("transitions to Disconnected when disabled", func(t *testing.T) {
		cp, log := connectedPair(t)
		mark := log.mark()
		cp.run(t, func(ctx context.Context) error { return cp.node.Disable(ctx) })
		if cp.state() != client.Disconnected {
			t.Errorf("state %v, want Disconnected", cp.state())
		}
		if got := log.since(mark); !slices.Equal(got, []client.ConnectionState{client.Disconnected}) {
			t.Errorf("transitions %v, want [Disconnected] and no Reconnecting flash", got)
		}
	})

	// :134-144.
	t.Run("transitions to Disconnected when the node is stopped", func(t *testing.T) {
		cp, _ := connectedPair(t)
		cp.run(t, func(ctx context.Context) error { return cp.node.Stop(ctx) })
		if cp.state() != client.Disconnected {
			t.Errorf("state %v, want Disconnected", cp.state())
		}
	})

	// :146-154.
	t.Run("transitions to Disconnected when its controller goes offline", func(t *testing.T) {
		cp, _ := connectedPair(t)
		cp.run(t, func(context.Context) error {
			cp.pair.Controller.Close()
			return nil
		})
		if cp.state() != client.Disconnected {
			t.Errorf("state %v, want Disconnected", cp.state())
		}
	})

	// :156-171. The controller restarts on its stores; the registry
	// restores the node without starting it (no AutoStart).
	t.Run("reports Disconnected for a known peer that has not been started", func(t *testing.T) {
		cp, _ := connectedPair(t)
		id := cp.node.ID()
		if err := cp.pair.Controller.Restart(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		n := cp.pair.Controller.Registry().Get(id)
		if n == nil {
			t.Fatal("the restarted registry does not know the node")
		}
		if n.ConnectionState() != client.Disconnected {
			t.Errorf("state %v, want Disconnected", n.ConnectionState())
		}
	})

	// :173-189. matter.js deletes the protocol peer's sessions without a
	// word on the wire; the Go peer's are given up with a cause
	// (Peer.Disconnect, matter.js Peer.disconnect's force close). The
	// state falls on the session loss alone; the sustained subscription
	// re-establishes afterwards, so the assertion is the first transition.
	t.Run("drops out of Connected when the last operational session is lost", func(t *testing.T) {
		cp, log := connectedPair(t)
		mark := log.mark()
		cp.run(t, func(ctx context.Context) error {
			addr, _ := cp.node.PeerAddress()
			cp.pair.Controller.Peers().Get(addr).Disconnect(ctx, context.Canceled)
			return nil
		})
		cp.eventually(t, "a transition", func() bool { return len(log.since(mark)) > 0 })
		if got := log.since(mark); got[0] != client.Reconnecting {
			t.Errorf("transitions %v, want Reconnecting first", got)
		}
	})
}

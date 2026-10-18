// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientStartUpTest.ts at
// 862ad75f: the controller's handling of a peer's StartUp event against its
// sessions (Peers.ts #onStartUp; client.Registry.onStartUp). The case with
// a live session and an active subscription runs on fabrictest; the other
// two are the registry's scripted tests (TestReactionsNeedSubscription,
// TestReactionStartUp), named in the register.
package client_test

import (
	"sync"
	"testing"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

func TestClientStartUpParity(t *testing.T) {
	t.Parallel()
	t.Run("preserves the current session when startUp event fires with active subscription", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		startUps := 0
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) {
			rc.Node.Hooks.Event = func(_ uint16, cluster, event uint32, _ interaction.EventData, _ any) {
				if cluster == basicinfo.ClusterID && event == basicinfo.EventStartUp {
					mu.Lock()
					startUps++
					mu.Unlock()
				}
			}
		}}})
		addr, _ := cp.node.PeerAddress()
		live := cp.pair.Controller.Peers().Get(addr).NewestSession()
		if live == nil {
			t.Fatal("no live session")
		}
		mu.Lock()
		before := startUps
		mu.Unlock()
		if err := cp.pair.Device.Act(func(n *node.Node) error {
			n.Root().BasicInformation.EmitStartUp()
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		cp.eventually(t, "the StartUp event", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return startUps > before
		})
		if err := cp.site.Settle(parityCtx(t)); err != nil {
			t.Fatal(err)
		}
		kept := false
		for _, s := range cp.pair.Controller.Exchanges().SessionsFor(addr) {
			kept = kept || s == live
		}
		if !kept || live.IsPeerLost() {
			t.Fatal("the live session did not survive the StartUp")
		}
	})
}

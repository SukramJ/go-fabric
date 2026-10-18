// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/client/PeersAutoStartTest.ts
// at 862ad75f: the commissioned peers started when the controller goes
// online, and not started with the opt-out, the expiry of uncommissioned
// nodes running either way (Peers #nodeOnline; client.Registry.Start).
// matter.js auto-starts by default (autoStartCommissionedPeers); the Go
// registry's AutoStart is opt-in, so the default case sets it. "Online" is
// the node's connection state.
package client_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/fabrictest"
)

func TestPeersAutoStartParity(t *testing.T) {
	t.Parallel()

	t.Run("auto-starts commissioned peers when the owner goes online (default)", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{controller: fabrictest.ControllerOptions{Registry: func(rc *client.RegistryConfig) { rc.AutoStart = true }}})
		reg := cp.pair.Controller.Registry()
		if cp.node.ConnectionState() != client.Connected {
			t.Fatalf("state %v, want online", cp.node.ConnectionState())
		}
		cp.run(t, reg.Stop)
		if cp.node.ConnectionState() != client.Disconnected {
			t.Fatalf("state %v after the stop", cp.node.ConnectionState())
		}
		cp.run(t, reg.Start)
		cp.eventually(t, "the node online again", func() bool { return cp.node.ConnectionState() == client.Connected })
	})

	t.Run("does not auto-start commissioned peers when autoStartCommissionedPeers is false", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		reg := cp.pair.Controller.Registry()
		if cp.node.ConnectionState() != client.Connected {
			t.Fatalf("state %v, want online", cp.node.ConnectionState())
		}
		second := otherDevice(cp.site)
		found, err := cp.pair.Controller.Discover(parityCtx(t), discovery.ByLongDiscriminator(second.Discriminator()), 90*time.Second)
		if err != nil || len(found) != 1 {
			t.Fatalf("discovered %+v, %v", found, err)
		}
		n2, err := reg.ForDevice(parityCtx(t), found[0])
		if err != nil || n2.IsCommissioned() {
			t.Fatalf("discovered node %v, %v", n2, err)
		}
		client.UpdateRecord(n2, func(r *client.Record) { r.DiscoveredAt = cp.site.Clock().Now().Add(-20 * time.Minute) })
		cp.run(t, reg.Stop)
		if cp.node.ConnectionState() != client.Disconnected {
			t.Fatalf("state %v after the stop", cp.node.ConnectionState())
		}
		cp.run(t, reg.Start)
		cp.advance(t, 3*time.Minute)
		if cp.node.ConnectionState() != client.Disconnected {
			t.Fatalf("state %v: the node came online without AutoStart", cp.node.ConnectionState())
		}
		if reg.Get(n2.ID()) != nil {
			t.Fatal("the expired uncommissioned node was not culled")
		}
	})
}

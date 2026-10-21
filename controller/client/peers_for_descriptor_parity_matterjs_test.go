// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/client/PeersForDescriptorTest.ts
// at 862ad75f: finding or creating the controller's node for a discovered
// device descriptor (Peers.forDescriptor; client.Registry.ForDevice).
// matter.js runs it on a MockServerNode's peers; the port on a fabrictest
// controller's registry, the device descriptor matter.js's DEVICE.
package client_test

import (
	"net/netip"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// deviceA is PeersForDescriptorTest.ts:13-18 DEVICE.
func deviceA(id string) discovery.Device {
	return discovery.Device{
		InstanceID: id, D: 1000, CM: 1,
		Addresses: []discovery.Address{{IP: netip.MustParseAddr("fe80::1"), Port: 5540}},
	}
}

// peersFor is PeersForDescriptorTest.ts:20-26: the registry's nodes of
// the device identifier.
func peersFor(reg *client.Registry, id string) []*client.Node {
	var out []*client.Node
	for _, n := range reg.Nodes() {
		if n.Record().DeviceIdentifier == id {
			out = append(out, n)
		}
	}
	return out
}

func TestPeersForDescriptorParity(t *testing.T) {
	t.Parallel()

	t.Run("returns the node under construction for a second call with the same descriptor", func(t *testing.T) {
		t.Parallel()
		reg := quietSite(t).AddController(fabrictest.ControllerOptions{}).Registry()
		var wg sync.WaitGroup
		nodes := make([]*client.Node, 2)
		for i := range nodes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				nodes[i], _ = reg.ForDevice(parityCtx(t), deviceA("device-a"))
			}()
		}
		wg.Wait()
		if nodes[0] == nil || nodes[0] != nodes[1] || len(peersFor(reg, "device-a")) != 1 {
			t.Fatalf("nodes %p %p, %d for the device", nodes[0], nodes[1], len(peersFor(reg, "device-a")))
		}
	})

	t.Run("matches a constructed node by its current state, not the descriptor it was created with", func(t *testing.T) {
		t.Parallel()
		reg := quietSite(t).AddController(fabrictest.ControllerOptions{}).Registry()
		n, err := reg.ForDevice(parityCtx(t), deviceA("device-a"))
		if err != nil {
			t.Fatal(err)
		}
		client.UpdateRecord(n, func(r *client.Record) { r.DeviceIdentifier = "device-b" })
		found, err := reg.ForDevice(parityCtx(t), deviceA("device-b"))
		if err != nil || found != n {
			t.Fatalf("found %p (%v), want the renamed node %p", found, err, n)
		}
	})
}

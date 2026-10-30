// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js's endpoint-number reservation tests at 862ad75f:
// packages/node/test/endpoints/BridgeTest.ts:231-269 ("closing and
// re-adding dynamic endpoints during runtime"), :271-341 ("with multiple
// dynamic endpoints close and re-add during runtime"), :343-425 ("with
// multiple dynamic endpoints delete and re-add during runtime"), and
// packages/node/test/node/ServerNodeTest.ts:1114-1142 ("frees the numbers
// of a deleted subtree").
//
// matter.js's close() is node.Aggregator.Remove (the number stays
// reserved for the stable key), its delete() node.Aggregator.Forget (the
// stored number is erased). matter.js's storage re-initialization
// (`store.initialize()`) is a fabrictest.Device.Restart on the same
// database. matter.js numbers from 1 and the Aggregator here is 1, so the
// numbers are compared relative to each other, not as constants.
package node_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// reservationDevice is a started device with an empty Aggregator.
func reservationDevice(t *testing.T) *fabrictest.Device {
	t.Helper()
	return quietSite(t).AddDevice(fabrictest.Topology{})
}

// addLights adds a kit light per key and returns their numbers.
func addLights(t *testing.T, d *fabrictest.Device, keys ...string) map[string]uint16 {
	t.Helper()
	out := map[string]uint16{}
	for _, k := range keys {
		if err := d.Act(func(n *node.Node) error { return n.Aggregator().Add(context.Background(), kitLight(t, k)) }); err != nil {
			t.Fatal(err)
		}
		number, ok := d.EndpointOf(k)
		if !ok {
			t.Fatalf("%s has no endpoint", k)
		}
		out[k] = number
	}
	return out
}

func aggregatorDo(t *testing.T, d *fabrictest.Device, fn func(a *node.Aggregator) error) {
	t.Helper()
	if err := d.Act(func(n *node.Node) error { return fn(n.Aggregator()) }); err != nil {
		t.Fatal(err)
	}
}

func remove(key string) func(a *node.Aggregator) error {
	return func(a *node.Aggregator) error { return a.Remove(context.Background(), endpoint.StringKey(key)) }
}

func forget(key string) func(a *node.Aggregator) error {
	return func(a *node.Aggregator) error { return a.Forget(context.Background(), endpoint.StringKey(key)) }
}

func TestEndpointNumberReservationParityMatterJS(t *testing.T) {
	t.Parallel()

	t.Run("closing and re-adding dynamic endpoints during runtime", func(t *testing.T) {
		t.Parallel()
		d := reservationDevice(t)
		numbers := addLights(t, d, "light1", "light2", "light3")
		aggregatorDo(t, d, remove("light2"))
		// "Numbers are reused"
		if got := addLights(t, d, "light2")["light2"]; got != numbers["light2"] {
			t.Errorf("re-added light2 at %d, want %d", got, numbers["light2"])
		}
	})

	t.Run("with multiple dynamic endpoints close and re-add during runtime", func(t *testing.T) {
		t.Parallel()
		d := reservationDevice(t)
		numbers := addLights(t, d, "light1", "light2", "light3")
		aggregatorDo(t, d, remove("light2"))
		aggregatorDo(t, d, remove("light1"))
		// light4 is new: it takes the next number, not a reserved one.
		readded := addLights(t, d, "light1", "light4")
		if readded["light1"] != numbers["light1"] {
			t.Errorf("re-added light1 at %d, want %d", readded["light1"], numbers["light1"])
		}
		if light3, _ := d.EndpointOf("light3"); light3 != numbers["light3"] {
			t.Errorf("light3 at %d, want %d", light3, numbers["light3"])
		}
		if readded["light4"] != numbers["light3"]+1 {
			t.Errorf("light4 at %d, want %d", readded["light4"], numbers["light3"]+1)
		}
		// Across a restart the reservations hold: light2, still removed,
		// gets its number back when it returns.
		if err := d.Restart(context.Background()); err != nil {
			t.Fatal(err)
		}
		after := addLights(t, d, "light1", "light3", "light4", "light2")
		for _, k := range []string{"light1", "light2", "light3"} {
			if after[k] != numbers[k] {
				t.Errorf("after the restart %s at %d, want %d", k, after[k], numbers[k])
			}
		}
		if after["light4"] != numbers["light3"]+1 {
			t.Errorf("after the restart light4 at %d, want %d", after["light4"], numbers["light3"]+1)
		}
	})

	t.Run("with multiple dynamic endpoints delete and re-add during runtime", func(t *testing.T) {
		t.Parallel()
		d := reservationDevice(t)
		numbers := addLights(t, d, "light1", "light2", "light3")
		aggregatorDo(t, d, forget("light2"))
		aggregatorDo(t, d, forget("light1"))
		readded := addLights(t, d, "light1", "light4")
		if readded["light1"] != numbers["light3"]+1 {
			t.Errorf("re-added light1 at %d, want the fresh %d", readded["light1"], numbers["light3"]+1)
		}
		if readded["light4"] != numbers["light3"]+2 {
			t.Errorf("light4 at %d, want %d", readded["light4"], numbers["light3"]+2)
		}
		if err := d.Restart(context.Background()); err != nil {
			t.Fatal(err)
		}
		after := addLights(t, d, "light1", "light3", "light4")
		if after["light1"] != numbers["light3"]+1 || after["light3"] != numbers["light3"] || after["light4"] != numbers["light3"]+2 {
			t.Errorf("after the restart %v, want light1 %d, light3 %d, light4 %d", after, numbers["light3"]+1, numbers["light3"], numbers["light3"]+2)
		}
	})

	t.Run("frees the numbers of a deleted subtree", func(t *testing.T) {
		t.Parallel()
		d := reservationDevice(t)
		subtree := func() *device.BridgedNode {
			owner, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey("subtree-owner"), Name: "subtree-owner"})
			if err != nil {
				t.Fatal(err)
			}
			if err := owner.AddPart(kitLight(t, "child").Device); err != nil {
				t.Fatal(err)
			}
			return owner
		}
		add := func() (owner, child uint16) {
			t.Helper()
			aggregatorDo(t, d, func(a *node.Aggregator) error { return a.Add(context.Background(), subtree()) })
			for _, bd := range d.Node().Topology().Devices {
				if bd.Key.String() == "subtree-owner" && len(bd.Parts) == 1 {
					return bd.Endpoint, bd.Parts[0].Endpoint
				}
			}
			t.Fatal("the subtree is not served")
			return 0, 0
		}
		owner, child := add()
		// Closed (Remove), the subtree keeps both numbers.
		aggregatorDo(t, d, remove("subtree-owner"))
		if o, c := add(); o != owner || c != child {
			t.Errorf("re-added after Remove at %d/%d, want %d/%d", o, c, owner, child)
		}
		// Deleted (Forget), it frees them: nothing is handed back.
		aggregatorDo(t, d, forget("subtree-owner"))
		if len(d.Node().Topology().Devices) != 0 {
			t.Error("the forgotten subtree is still served")
		}
		if o, c := add(); o == owner || c == child || c == owner || o == child {
			t.Errorf("re-added after Forget at %d/%d, want fresh numbers (were %d/%d)", o, c, owner, child)
		}
	})

	t.Run("RetirePartition frees the partition's reservations", func(t *testing.T) {
		// Go only: a retired partition is the erase of a whole scope.
		t.Parallel()
		d := reservationDevice(t)
		ctx := context.Background()
		var number uint16
		aggregatorDo(t, d, func(a *node.Aggregator) error {
			p := a.Partition("hub")
			if err := p.Add(ctx, kitLight(t, "hub:light")); err != nil {
				return err
			}
			number, _ = d.EndpointOf("hub:light")
			if err := p.Remove(ctx, endpoint.StringKey("hub:light")); err != nil {
				return err
			}
			return a.RetirePartition(ctx, "hub")
		})
		if got := addLights(t, d, "hub:light")["hub:light"]; got == number {
			t.Errorf("hub:light got its number %d back after its partition retired", got)
		}
	})
}

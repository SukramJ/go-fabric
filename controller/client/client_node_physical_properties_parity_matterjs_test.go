// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/ClientNodePhysicalPropertiesTest.ts
// at 862ad75f: the controller retains a peer's physical properties across
// teardown and evaluates them again once the node is back. matter.js holds
// a live accessor object; client.Node.Properties returns a value, frozen
// from Stop until Start, so the port compares values where matter.js also
// compares array identity.
package client_test

import (
	"context"
	"slices"
	"testing"
)

func TestClientNodePhysicalPropertiesParity(t *testing.T) {
	t.Parallel()

	t.Run("retains last-known properties after the node is torn down", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		before := cp.node.Properties()
		if len(before.RootEndpointServerList) == 0 {
			t.Fatal("no root server list")
		}
		cp.run(t, cp.node.Close)
		after := cp.node.Properties()
		if !slices.Equal(after.RootEndpointServerList, before.RootEndpointServerList) || after.SupportsThread != before.SupportsThread || after.BatteryPowered != before.BatteryPowered {
			t.Fatalf("properties after the close %+v, want %+v", after, before)
		}
	})

	t.Run("re-evaluates after the node comes back online", func(t *testing.T) {
		t.Parallel()
		cp := newClientPair(t, clientPairOptions{})
		initial := cp.node.Properties().RootEndpointServerList
		if len(initial) == 0 {
			t.Fatal("no root server list")
		}
		cp.run(t, cp.node.Stop)
		if got := cp.node.Properties().RootEndpointServerList; !slices.Equal(got, initial) {
			t.Fatalf("frozen server list %v, want %v", got, initial)
		}
		cp.run(t, func(ctx context.Context) error { return cp.node.Start(ctx) })
		if got := cp.node.Properties().RootEndpointServerList; !slices.Equal(got, initial) {
			t.Fatalf("re-evaluated server list %v, want %v", got, initial)
		}
	})
}

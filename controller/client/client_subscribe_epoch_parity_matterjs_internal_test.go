// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

// A port of matter.js packages/node/test/node/ClientSubscribeTest.ts at
// 862ad75f, the "cache fabric filter" cases that feed
// clientStructureOf(peer).mutate a report across a switch of the cache's
// fabric filter (:553-629, :681-723). matter.js's mutate takes the
// interaction's epoch when it starts; here Node.applyIn takes the epoch
// the interaction captured when it started (ClientNodeInteraction.ts:128-132).
// Go applies a report whole under the node's lock, so data that passed the
// cache's fabric filter check is applied before a switch that follows it.

// onOffReport is a report of the light's OnOff true with version v.
func onOffReport(v uint32) *report {
	return (&report{}).attr(1, onOffCluster, 0, boolNode(true), v)
}

const onOffCluster = 0x0006

// onOffFilters are the version filters a read of the light's OnOff with
// the fabric filter gets.
func onOffFilters(n *Node, filtered bool) []DataVersionFilter {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.s.withKnownVersions(newScope([]im.ConcreteAttributePath{{Endpoint: 1, Cluster: onOffCluster, HasEndpoint: true, HasCluster: true}}, !filtered), nil, false)
}

func onOffValue(n *Node) any {
	v, _ := n.Endpoint(1).Cluster(onOffCluster).Decoded(0)
	return v
}

func TestClientSubscribeEpochParity(t *testing.T) {
	t.Parallel()

	t.Run("applies data of an interaction that started before a switch to unfiltered without its version", func(t *testing.T) {
		t.Parallel()
		n, _, _ := peerImage(t, &MemoryStore{})
		if _, err := n.adoptFabricFilter(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		in := n.s.epoch
		// The report's data passes the cache check before the switch.
		n.applyIn(wildcard, &onOffReport(77).r, false, in)
		if _, err := n.adoptFabricFilter(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		if f := onOffFilters(n, false); len(f) != 0 {
			t.Fatalf("filters %v, want none", f)
		}
		if v := onOffValue(n); v != true {
			t.Fatalf("OnOff %v, want true", v)
		}
	})

	t.Run("applies data of an interaction that started before a switch to filtered without its version", func(t *testing.T) {
		t.Parallel()
		n, _, _ := peerImage(t, &MemoryStore{})
		if _, err := n.adoptFabricFilter(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		in := n.s.epoch
		if _, err := n.adoptFabricFilter(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		// The interaction started before the switch; its data arrives
		// after it, under the filter it switched to.
		n.applyIn(wildcard, &onOffReport(78).r, false, in)
		if f := onOffFilters(n, true); len(f) != 0 {
			t.Fatalf("filters %v, want none", f)
		}
		if v := onOffValue(n); v != true {
			t.Fatalf("OnOff %v, want true", v)
		}
	})

	t.Run("records no version for data of an interaction that started before the first record of another filter", func(t *testing.T) {
		t.Parallel()
		n, _, _ := peerImage(t, &MemoryStore{})
		if n.s.recordedFabricFiltered != nil {
			t.Fatal("a fresh node has a record")
		}
		unfiltered := newScope([]im.ConcreteAttributePath{{}}, true)
		in := n.s.epoch
		if _, err := n.adoptFabricFilter(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		n.applyIn(unfiltered, &onOffReport(79).r, false, in)
		if f := onOffFilters(n, false); len(f) != 0 {
			t.Fatalf("filters %v, want none", f)
		}
		if v := onOffValue(n); v != true {
			t.Fatalf("OnOff %v, want true", v)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec/descriptor"
	"thingfabric.dev/go-fabric/cluster/spec/onoff"
)

// The package's tests check the running total against the structure at
// every report admit takes (checkImageTotal).
func init() { checkImageTotal = true }

// TestImageRunningTotalMatchesTheStructure (review II, N3-finimg-3): the
// image's running total, which admit reads on every report, equals the
// structure's sum after every kind of change — endpoints and clusters
// added, values changed, a cluster the ServerList drops, endpoints the
// PartsList drops, a report the quota refuses, and a reload from the
// store.
func TestImageRunningTotalMatchesTheStructure(t *testing.T) {
	t.Parallel()
	st := &MemoryStore{}
	n, _ := quotaNode(t, st, 0)
	check := func(n *Node, after string) {
		t.Helper()
		n.mu.RLock()
		defer n.mu.RUnlock()
		if got, want := n.s.imageBytes(), n.s.recountImageBytes(); got != want {
			t.Fatalf("%s: running total %d, structure %d", after, got, want)
		}
	}
	check(n, "a new node")
	n.apply(wildcard, &bridge(3).r, true)
	check(n, "a bridge of three plugs")
	b := &report{}
	b.attr(2, onoff.ClusterID, 0, boolNode(true), 9)
	b.attr(3, onoff.ClusterID, 0, listNode(1, 2, 3, 4, 5), 9)
	n.apply(wildcard, &b.r, true)
	check(n, "values changed")
	b = &report{}
	b.attr(2, descriptor.ClusterID, descriptor.AttrServerList, listNode(uint64(descriptor.ClusterID), uint64(clBridgedBI)), 10)
	n.apply(wildcard, &b.r, true)
	check(n, "a cluster the ServerList drops")
	n.apply(wildcard, &bridge(1).r, true)
	check(n, "endpoints the PartsList drops")
	n.s.maxImageBytes = n.s.imageBytes()
	b = &report{}
	b.attr(2, onoff.ClusterID, 0, blobList(16, 1000), 11)
	n.apply(wildcard, &b.r, true)
	check(n, "a report the quota refuses")
	reloaded, _ := quotaNode(t, st, 0)
	check(reloaded, "a reload")
}

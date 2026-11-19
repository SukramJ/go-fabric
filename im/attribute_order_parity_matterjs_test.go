// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/parity"
)

// The order of a cluster's attributes in every report of this module —
// a Read's wildcard expansion, a priming report, a change report — is
// matter.js's (review II, R2-P4-imread-12): a matter.js server walks a
// cluster's attributes in the order Scope.membersOf yields its model's
// members (packages/node/src/node/integration/ProtocolService.ts:449-522
// at db021da0, clusterTypeProtocolOf; the read response walks that list
// as built, packages/protocol/src/action/server/AttributeReadResponse.ts:378-380).
// The snapshot carries that order per cluster (attributeOrder in
// parity/schema.json, script/extract-from-matter-js.ts "Order layer").
// matter.js has no test that pins an order; the cases below hold the
// snapshot's, and the OnOff case the order a matter.js node at db021da0
// served for a wildcard read of OnOff on a Dimmable Light endpoint.

// sortedByCluster returns ids sorted with [CompareAttributeIDs] for cluster.
func sortedByCluster(cluster uint32, ids []uint32) []uint32 {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uint32) int { return CompareAttributeIDs(cluster, a, b) })
	return out
}

// TestCompareAttributeIDsOnOffIsMatterJSsOrder pins one cluster's order:
// ClusterRevision, FeatureMap, the cluster's own attributes as the model
// defines them, then AttributeList, AcceptedCommandList and
// GeneratedCommandList — the ten paths a matter.js node at db021da0
// answered a wildcard read of OnOff with (EventList, conformance "D",
// is not served there and is not here).
func TestCompareAttributeIDsOnOffIsMatterJSsOrder(t *testing.T) {
	t.Parallel()
	want := []uint32{0xFFFD, 0xFFFC, 0x0000, 0x4000, 0x4001, 0x4002, 0x4003, 0xFFFB, 0xFFF9, 0xFFF8}
	ascending := slices.Sorted(slices.Values(want))
	if got := sortedByCluster(0x0006, ascending); !slices.Equal(got, want) {
		t.Fatalf("OnOff attributes in order %#04x, want matter.js's %#04x", got, want)
	}
}

// TestCompareAttributeIDsFollowsTheSnapshotOrder holds every cluster of
// the snapshot to its attributeOrder: sorting the ids from any start
// order gives that order.
func TestCompareAttributeIDsFollowsTheSnapshotOrder(t *testing.T) {
	t.Parallel()
	var snap struct {
		Clusters []struct {
			ID             uint32   `json:"id"`
			Name           string   `json:"name"`
			AttributeOrder []uint32 `json:"attributeOrder"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(parity.SchemaJSON(), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Clusters) == 0 {
		t.Fatal("snapshot has no clusters")
	}
	for _, c := range snap.Clusters {
		if len(c.AttributeOrder) == 0 {
			t.Errorf("%s: snapshot carries no attributeOrder", c.Name)
			continue
		}
		reversed := slices.Clone(c.AttributeOrder)
		slices.Reverse(reversed)
		for _, start := range [][]uint32{reversed, slices.Sorted(slices.Values(c.AttributeOrder))} {
			if got := sortedByCluster(c.ID, start); !slices.Equal(got, c.AttributeOrder) {
				t.Errorf("%s: order %#04x, want the snapshot's %#04x", c.Name, got, c.AttributeOrder)
			}
		}
	}
}

// TestCompareAttributeIDsUnlistedMembersFirst pins where an attribute goes
// that the cluster's model does not list. matter.js lists the members a
// server's schema adds to a cluster's model before the model's own
// (findAllMembers visits the extending schema before its base,
// packages/model/src/logic/Scope.ts:287-333 at b25ed960), in the order
// the extension defines them; that order is not known here, so such
// attributes come first in ascending id order — this module's rule. A
// cluster the snapshot does not define has the global attributes as its
// model, in the order matter.js appends them to a cluster model that
// defines none (Scope.ts:18, :339-358).
func TestCompareAttributeIDsUnlistedMembersFirst(t *testing.T) {
	t.Parallel()
	t.Run("vendor attributes of a known cluster", func(t *testing.T) {
		t.Parallel()
		ids := []uint32{0xFFFD, 0x1234_0001, 0x0000, 0xFFFC, 0x1234_0000, 0xFFFB}
		want := []uint32{0x1234_0000, 0x1234_0001, 0xFFFD, 0xFFFC, 0x0000, 0xFFFB}
		if got := sortedByCluster(0x0006, ids); !slices.Equal(got, want) {
			t.Fatalf("order %#04x, want %#04x", got, want)
		}
	})
	t.Run("a cluster the snapshot does not define", func(t *testing.T) {
		t.Parallel()
		ids := []uint32{0xFFF8, 0x0002, 0xFFFB, 0xFFFD, 0x0001, 0xFFF9, 0xFFFC}
		want := []uint32{0x0001, 0x0002, 0xFFFD, 0xFFFC, 0xFFFB, 0xFFF9, 0xFFF8}
		if got := sortedByCluster(0xFFF1_FC01, ids); !slices.Equal(got, want) {
			t.Fatalf("order %#04x, want %#04x", got, want)
		}
	})
}

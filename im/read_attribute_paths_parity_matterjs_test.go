// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// A port of matter.js packages/protocol/test/action/request/ReadAttributePathsTest.ts at
// 862ad75f: the wildcard-path dedupe cases of Read.AttributePaths.

import "testing"

// TestReadAttributePathsWildcardDedupe ports the wildcard cases of
// Read.AttributePaths (Read.ts:338-352, keyed by endpoint/cluster/attribute
// with "*" for an absent id). The Go counterpart is [DedupAttributePaths],
// keyed by the whole path value, so an absent id is its own key just as
// matter.js's "*" is.
func TestReadAttributePathsWildcardDedupe(t *testing.T) {
	t.Parallel()
	cluster := ConcreteAttributePath{Endpoint: 1, HasEndpoint: true, Cluster: 0x28, HasCluster: true}
	endpoint := ConcreteAttributePath{Endpoint: 1, HasEndpoint: true}
	concrete := ConcreteAttributePath{Endpoint: 1, HasEndpoint: true, Cluster: 0x28, HasCluster: true, Attribute: 0x01, HasAttribute: true}

	t.Run("dedupes cluster-level wildcard paths (no attributeId)", func(t *testing.T) {
		t.Parallel()
		if got := DedupAttributePaths([]ConcreteAttributePath{cluster, cluster}); len(got) != 1 {
			t.Fatalf("paths %d, want 1", len(got)) // ReadAttributePathsTest.ts:46
		}
	})
	t.Run("dedupes endpoint-level wildcard paths (no clusterId or attributeId)", func(t *testing.T) {
		t.Parallel()
		if got := DedupAttributePaths([]ConcreteAttributePath{endpoint, endpoint}); len(got) != 1 {
			t.Fatalf("paths %d, want 1", len(got)) // ReadAttributePathsTest.ts:53
		}
	})
	t.Run("treats cluster wildcard and specific attribute as distinct", func(t *testing.T) {
		t.Parallel()
		if got := DedupAttributePaths([]ConcreteAttributePath{cluster, concrete}); len(got) != 2 {
			t.Fatalf("paths %d, want 2", len(got)) // ReadAttributePathsTest.ts:60
		}
	})
}

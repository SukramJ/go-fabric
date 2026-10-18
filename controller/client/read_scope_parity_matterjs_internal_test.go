// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/protocol/test/action/client/ReadScopeTest.ts
// at 862ad75f: whether an endpoint and cluster are in scope of a read
// (ReadScope; the client's newScope). The Go read's paths are
// im.ConcreteAttributePath with the wildcard flags; a Read's fabricFilter
// false is NoFabricFilter.
package client

import (
	"testing"

	"github.com/SukramJ/go-fabric/im"
)

func TestReadScopeParity(t *testing.T) {
	t.Parallel()

	t.Run("treats an empty read as out of scope", func(t *testing.T) {
		t.Parallel()
		s := newScope(nil, false)
		if s.isRelevant(1, 6) || s.isWildcard(1, 6) {
			t.Fatal("an empty read is in scope")
		}
	})

	t.Run("treats a full wildcard read as always in scope", func(t *testing.T) {
		t.Parallel()
		s := newScope([]im.ConcreteAttributePath{{}}, false)
		if !s.isRelevant(2, 8) || !s.isWildcard(2, 8) {
			t.Fatal("a full wildcard is out of scope")
		}
	})

	t.Run("scopes a wildcard cluster across all endpoints", func(t *testing.T) {
		t.Parallel()
		s := newScope([]im.ConcreteAttributePath{{Cluster: 6, HasCluster: true}}, false)
		if !s.isRelevant(1, 6) || !s.isRelevant(9, 6) || s.isRelevant(1, 8) {
			t.Fatalf("relevant 1/6 %v, 9/6 %v, 1/8 %v", s.isRelevant(1, 6), s.isRelevant(9, 6), s.isRelevant(1, 8))
		}
	})

	t.Run("scopes a specific endpoint and cluster", func(t *testing.T) {
		t.Parallel()
		s := newScope([]im.ConcreteAttributePath{{Endpoint: 1, Cluster: 6, HasEndpoint: true, HasCluster: true}}, false)
		if !s.isRelevant(1, 6) || s.isRelevant(2, 6) || !s.isWildcard(1, 6) {
			t.Fatalf("relevant 1/6 %v, 2/6 %v, wildcard 1/6 %v", s.isRelevant(1, 6), s.isRelevant(2, 6), s.isWildcard(1, 6))
		}
	})

	t.Run("does not scope a read that targets only a specific attribute", func(t *testing.T) {
		t.Parallel()
		s := newScope([]im.ConcreteAttributePath{{Endpoint: 1, Cluster: 6, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}}, false)
		if s.isRelevant(1, 6) || s.isWildcard(1, 6) {
			t.Fatal("a concrete attribute read scopes its cluster")
		}
	})

	t.Run("exposes the fabric filter flag", func(t *testing.T) {
		t.Parallel()
		if !newScope([]im.ConcreteAttributePath{{}}, false).fabricFiltered || newScope([]im.ConcreteAttributePath{{}}, true).fabricFiltered {
			t.Fatal("the fabric filter flag")
		}
	})
}

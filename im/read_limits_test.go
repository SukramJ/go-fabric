// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"testing"
)

// wildcardPaths returns n copies of the all-wildcard attribute path.
func wildcardPaths(n int) []ConcreteAttributePath {
	return make([]ConcreteAttributePath, n)
}

// TestValidateReadPaths_PathsExhaustedAboveCeiling pins the hard
// acceptance ceiling on paths in one read / subscribe interaction:
// matter.js InteractionServer.ts:79-80 MAX_READ_PATHS /
// MAX_SUBSCRIBE_PATHS = 10 000, rejected with PathsExhausted at
// :366-369 and :600-604. Attribute and event paths count together.
func TestValidateReadPaths_PathsExhaustedAboveCeiling(t *testing.T) {
	t.Parallel()
	if got := ValidateReadPaths(wildcardPaths(MaxReadPaths), nil); got != StatusSuccess {
		t.Errorf("exactly %d attribute paths: status %v, want Success", MaxReadPaths, got)
	}
	if got := ValidateReadPaths(wildcardPaths(MaxReadPaths+1), nil); got != StatusPathsExhausted {
		t.Errorf("%d attribute paths: status %v, want PathsExhausted", MaxReadPaths+1, got)
	}
	events := make([]ConcreteEventPath, 1)
	if got := ValidateReadPaths(wildcardPaths(MaxReadPaths), events); got != StatusPathsExhausted {
		t.Errorf("%d attribute + 1 event paths: status %v, want PathsExhausted (the ceiling counts both kinds)", MaxReadPaths, got)
	}
}

// TestValidateReadPaths_IllegalPathWinsOverCeiling pins the check order
// matter.js uses: validateReadPaths runs before the path count
// (InteractionServer.ts:362-369), so an illegal path is still
// InvalidAction even inside an oversized request.
func TestValidateReadPaths_IllegalPathWinsOverCeiling(t *testing.T) {
	t.Parallel()
	paths := wildcardPaths(MaxReadPaths + 1)
	paths[0] = ConcreteAttributePath{HasAttribute: true, Attribute: 0x0000} // wildcard cluster + concrete non-global attribute
	if got := ValidateReadPaths(paths, nil); got != StatusInvalidAction {
		t.Errorf("status %v, want InvalidAction", got)
	}
}

// countingDispatcher answers every path with one result and counts the
// Read calls it served.
type countingDispatcher struct {
	fakeDispatcher
	reads int
}

func (d *countingDispatcher) Read(ctx context.Context, p ConcreteAttributePath) []ReadResult {
	d.reads++
	return d.fakeDispatcher.Read(ctx, p)
}

// TestHandleReadRequest_IdenticalPathsAreReadOnce pins that a request
// repeating the same AttributePathIB does not expand it again: the
// repeated path yields the very same results, so reading it once bounds
// the materialised report by the node's own attribute count instead of
// by how many times a peer can fit the path into one datagram.
func TestHandleReadRequest_IdenticalPathsAreReadOnce(t *testing.T) {
	t.Parallel()
	d := &countingDispatcher{fakeDispatcher: fakeDispatcher{readVal: AttributeValue{Value: true}}}
	req := ReadRequest{AttributeRequests: wildcardPaths(50)}
	rd := HandleReadRequest(context.Background(), d, req)
	if d.reads != 1 {
		t.Errorf("dispatcher.Read called %d times for 50 identical paths, want 1", d.reads)
	}
	if len(rd.Reports) != 1 {
		t.Errorf("report carries %d entries for 50 identical paths, want 1", len(rd.Reports))
	}
}

// TestDedupAttributePaths_KeepsOrderAndDistinctPaths pins the helper's
// contract: first occurrence wins, order is preserved, and paths that
// differ in any field (including which fields are wildcards) stay.
func TestDedupAttributePaths_KeepsOrderAndDistinctPaths(t *testing.T) {
	t.Parallel()
	a := ConcreteAttributePath{HasEndpoint: true, Endpoint: 1, HasCluster: true, Cluster: 6}
	b := ConcreteAttributePath{HasEndpoint: true, Endpoint: 1, HasCluster: true, Cluster: 6, HasAttribute: true} // attribute 0 named, not wildcard
	c := ConcreteAttributePath{}
	got := DedupAttributePaths([]ConcreteAttributePath{a, c, a, b, c, b})
	want := []ConcreteAttributePath{a, c, b}
	if len(got) != len(want) {
		t.Fatalf("got %d paths, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

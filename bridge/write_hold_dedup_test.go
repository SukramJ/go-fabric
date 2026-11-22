// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	endpointpkg "thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
)

// TestHeldWriteKeepsOnePathPerNotification (R2-P6-bound-0409): N host
// notifications of one path while a write is applied — a wildcard write
// holds every endpoint — leave one held entry per (path, kind), not N.
func TestHeldWriteKeepsOnePathPerNotification(t *testing.T) {
	t.Parallel()
	var tbl writeHoldTable
	tbl.begin(writeScope{wildcard: true})
	ep := &endpointpkg.Endpoint{ID: 3}
	p := im.ConcreteAttributePath{Endpoint: 3, Cluster: 6, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	for range 100 {
		if left := tbl.holdPaths(ep, nil, []im.ConcreteAttributePath{p}); len(left) != 0 {
			t.Fatal("the path was not held")
		}
		tbl.holdAdvance(ep, 6, []uint32{0})
		tbl.holdMarks(ep, nil, 6, []uint32{0})
	}
	tbl.mu.Lock()
	n := len(tbl.held[3].paths)
	tbl.mu.Unlock()
	if n != 3 {
		t.Errorf("held %d entries for one path in three kinds, want 3", n)
	}
}

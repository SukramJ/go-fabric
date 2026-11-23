// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"errors"
	"testing"
)

// TestRefreshEndpointListIsBounded (R2-C2-bound-1102): a node that lists
// more endpoints than maxEndpointsPerNode ends the refresh with
// ErrNoMemory before anything changes, as the refresh's group, binding
// and ACL merges answer a list past their bound (R2-P6-bound-1113).
// Before, every listed endpoint became a persisted NodeEndpointList entry,
// fetched and committed one at a time.
func TestRefreshEndpointListIsBounded(t *testing.T) {
	ctx := t.Context()
	s := newTrackingSync()
	for i := range maxEndpointsPerNode + 1 {
		s.endpointsToFetch = append(s.endpointsToFetch, EndpointEntry{EndpointId: uint16(i + 1), NodeId: 5})
	}
	d := newStore(t, s)
	mustOK(t, d.AddPendingNode(ctx, 5, "n"))
	if err := d.RefreshNode(ctx, 5); !errors.Is(err, ErrNoMemory) {
		t.Fatalf("refresh of a node listing %d endpoints: %v, want ErrNoMemory", maxEndpointsPerNode+1, err)
	}
	if n := len(d.NodeEndpointList()); n != 0 {
		t.Errorf("NodeEndpointList holds %d entries after the refused refresh", n)
	}
	// At the bound the refresh takes the list.
	s.endpointsToFetch = s.endpointsToFetch[:maxEndpointsPerNode]
	mustOK(t, d.RefreshNode(ctx, 5))
	if n := len(d.NodeEndpointList()); n != maxEndpointsPerNode {
		t.Errorf("NodeEndpointList holds %d entries, want %d", n, maxEndpointsPerNode)
	}
}

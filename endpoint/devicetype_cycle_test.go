// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"testing"
	"time"
)

// TestValidateDeviceTypesReturnsOnAParentCycle (review II,
// R2-P1-req2-07): a topology whose endpoints name each other as parent
// is validated, not walked forever — a part whose claim would make its
// claimer its own descendant is not taken.
func TestValidateDeviceTypesReturnsOnAParentCycle(t *testing.T) {
	t.Parallel()
	topo := &Topology{Endpoints: []*Endpoint{
		{ID: 0},
		{ID: 5, ParentEndpointID: 6, HasParentEndpointID: true},
		{ID: 6, ParentEndpointID: 5, HasParentEndpointID: true},
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = ValidateDeviceTypes(topo)
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("ValidateDeviceTypes did not return on a parent cycle")
	}
	facts := newTopologyFacts(topo)
	for _, ep := range []uint16{5, 6} {
		seen := map[uint16]bool{}
		for cur, ok := ep, true; ok; cur, ok = facts.parentOf(cur) {
			if seen[cur] {
				t.Fatalf("endpoint %d's parent chain loops through %d", ep, cur)
			}
			seen[cur] = true
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"math"
	"testing"
)

// TestSustainedEventMinAtTheTopOfTheRange: after an event numbered
// 2^64-1 a sustained re-subscription asks for EventMin 2^64-1 — the top
// held — not for 0, which would ask the peer for every event it still
// holds again. This module's own choice: matter.js computes the bound as
// a bigint (review II, R2-C2-wire-2351, the sustained site).
func TestSustainedEventMinAtTheTopOfTheRange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		max, want uint64
	}{
		{7, 8},
		{math.MaxUint64 - 1, math.MaxUint64},
		{math.MaxUint64, math.MaxUint64},
	} {
		st := &sustainedState{maxEvent: tc.max, hasEvent: true}
		req := st.requestNewEvents(SubscribeRequest{})
		if len(req.EventFilters) != 1 || req.EventFilters[0].EventMin != tc.want {
			t.Fatalf("after event %d: filters %+v, want EventMin %d", tc.max, req.EventFilters, tc.want)
		}
	}
}

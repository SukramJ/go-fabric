// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// One EventFilterIB applies: the first without a NodeID or with this
// node's id; a filter addressed to another node does not lower the floor.
// matter.js packages/protocol/src/action/server/EventReadResponse.ts:68-77,
// packages/protocol/src/events/OccurrenceManager.ts:250-254. Review 2026-11
// W2-wire-05.

import "testing"

func TestEventFiltersForParity(t *testing.T) {
	t.Parallel()
	const self, other uint64 = 0x1111, 0x2222
	cases := []struct {
		name    string
		filters []EventMinimumNumber
		want    uint64
	}{
		{"none", nil, 0},
		{
			"another node's filter first, then one without NodeID (the finding's input)",
			[]EventMinimumNumber{{NodeID: other, HasNodeID: true, EventMin: 1}, {EventMin: 500}},
			500,
		},
		{"this node's filter wins over a later one", []EventMinimumNumber{{NodeID: self, HasNodeID: true, EventMin: 40}, {EventMin: 3}}, 40},
		{"the first applicable, not the minimum", []EventMinimumNumber{{EventMin: 10}, {EventMin: 1}}, 10},
		{"only other nodes' filters: no floor", []EventMinimumNumber{{NodeID: other, HasNodeID: true, EventMin: 99}}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := minEventNumberFromFilters(EventFiltersFor(tc.filters, self)); got != tc.want {
				t.Errorf("floor = %d, want %d", got, tc.want)
			}
		})
	}
}

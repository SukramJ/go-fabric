// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import "testing"

// TestTopologyBridgedWithoutAnAggregator (review II, R2-P1-req2-08): the
// Aggregator is a role, not endpoint 1, so a topology of the root and one
// device has that device as its bridged endpoint, as a topology of the
// root and two has both; a nil entry is skipped.
func TestTopologyBridgedWithoutAnAggregator(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		topo *Topology
		want []uint16
	}{
		{"root and one device", &Topology{Endpoints: []*Endpoint{{ID: 0}, {ID: 1}}}, []uint16{1}},
		{"root and two devices", &Topology{Endpoints: []*Endpoint{{ID: 0}, {ID: 1}, {ID: 2}}}, []uint16{1, 2}},
		{"root, aggregator, device, part", &Topology{Endpoints: []*Endpoint{{ID: 0}, {ID: 1, Aggregator: true}, {ID: 2}, {ID: 3, Part: true}}}, []uint16{2}},
		{"root only", &Topology{Endpoints: []*Endpoint{{ID: 0}}}, nil},
		{"a nil entry", &Topology{Endpoints: []*Endpoint{{ID: 0}, nil, {ID: 4}}}, []uint16{4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []uint16
			for _, ep := range tc.topo.Bridged() {
				got = append(got, ep.ID)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("Bridged() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("Bridged() = %v, want %v", got, tc.want)
				}
			}
			_ = tc.topo.FindByID(9)
			_ = tc.topo.Aggregator()
		})
	}
}

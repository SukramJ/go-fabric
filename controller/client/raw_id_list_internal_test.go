// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"slices"
	"testing"
)

// TestRawIDListKeepsThePeersOrderWithoutDuplicates (review II,
// R2-P6-bound-2358): the set that replaced the quadratic search keeps the
// first occurrence of each id in the peer's order and skips an entry that
// is no 32-bit id.
func TestRawIDListKeepsThePeersOrderWithoutDuplicates(t *testing.T) {
	t.Parallel()
	n := listNode(6, 0x1d, 6, 3, 0x1d, 1<<33, 3, 0)
	n.Children = append(n.Children, boolNode(true))
	if got, want := rawIDList(n), []uint32{6, 0x1d, 3, 0}; !slices.Equal(got, want) {
		t.Fatalf("rawIDList = %v, want %v", got, want)
	}
	if got := rawIDList(uintNode(6)); got != nil {
		t.Fatalf("rawIDList of a scalar = %v, want nil", got)
	}
}

// BenchmarkRawIDList is a ServerList of 100 000 distinct ids, the order
// of what the image quota admits (R2-P6-bound-2358): linear, where the
// former search was quadratic.
func BenchmarkRawIDList(b *testing.B) {
	ids := make([]uint64, 100_000)
	for i := range ids {
		ids[i] = uint64(i)
	}
	n := listNode(ids...)
	b.ReportAllocs()
	for b.Loop() {
		_ = rawIDList(n)
	}
}

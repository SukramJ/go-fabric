// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"slices"
	"testing"
	"time"
)

// TestOmap_Order: the map iterates in insertion order; setting an existing
// key keeps its place, deleting and setting again moves it to the end.
func TestOmap_Order(t *testing.T) {
	t.Parallel()
	o := newOmap[string, int]()
	for i, k := range []string{"a", "b", "c", "d"} {
		o.set(k, i)
	}
	o.set("b", 10)
	if !o.del("c") || o.del("c") {
		t.Fatal("del reports the wrong presence")
	}
	o.del("a")
	o.set("a", 20)
	if got, want := o.snapshotKeys(), []string{"b", "d", "a"}; !slices.Equal(got, want) {
		t.Fatalf("keys %v, want %v", got, want)
	}
	if got, want := o.values(), []int{10, 3, 20}; !slices.Equal(got, want) {
		t.Fatalf("values %v, want %v", got, want)
	}
	if k, ok := o.first(); !ok || k != "b" || o.len() != 3 {
		t.Fatalf("first %q %v, len %d", k, ok, o.len())
	}
	o.clear()
	if _, ok := o.first(); ok || o.len() != 0 || len(o.values()) != 0 {
		t.Fatal("clear left entries")
	}
}

// TestOmap_N3Disc2 (N3-disc-2): deleting every key costs about as much a
// key whatever the map's size. The time of the larger map is compared with
// the smaller one's, the best of several runs, so a busy machine moves
// both; a cost a key that grows with the size multiplies the ratio by the
// size factor.
func TestOmap_N3Disc2(t *testing.T) {
	t.Parallel()
	run := func(n int) time.Duration {
		best := time.Duration(1 << 62)
		for range 5 {
			o := newOmap[int, int]()
			for i := range n {
				o.set(i, i)
			}
			start := time.Now()
			for i := n - 1; i >= 0; i -= 2 {
				o.del(i)
			}
			for i := 0; i < n; i += 2 {
				o.del(i)
			}
			best = min(best, time.Since(start))
		}
		return best
	}
	const small, factor = 1 << 10, 16
	ds, dl := run(small), run(small*factor)
	if ds <= 0 {
		ds = time.Nanosecond
	}
	if ratio := float64(dl) / float64(ds); ratio > 5*factor {
		t.Fatalf("deleting %d keys took %v, %d keys %v: %.0f× for %d× the keys", small*factor, dl, small, ds, ratio, factor)
	}
}

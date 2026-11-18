// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"runtime"
	"testing"
	"time"
)

// TestHeapReadOncePerSample (review II, R2-P6-bound-2453): the heap
// attributes read within heapSampleAge of each other share one
// runtime.ReadMemStats, which stops the world; a read after it reads
// again.
func TestHeapReadOncePerSample(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	reads := 0
	h := &runtimeHeap{
		readMemStats: func(m *runtime.MemStats) { reads++; m.HeapAlloc = uint64(reads) },
		now:          func() time.Time { return now },
	}
	h.CurrentHeapFree()
	used, _ := h.CurrentHeapUsed()
	if reads != 1 || used != 1 {
		t.Fatalf("two reads within the sample: %d heap readings (used %d), want 1", reads, used)
	}
	now = now.Add(heapSampleAge)
	if used, _ := h.CurrentHeapUsed(); reads != 2 || used != 2 {
		t.Errorf("a read after the sample: %d heap readings (used %d), want 2", reads, used)
	}
}

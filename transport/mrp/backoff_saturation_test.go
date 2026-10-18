// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mrp_test

import (
	"math"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestBackoffDurationSaturates holds the overflow guard: from transmission
// 52 on the float product exceeds a Duration, which wrapped to a negative
// wait before. The backoff never shrinks with the transmission number, as
// matter.js MRP.retransmissionIntervalOf (MRP.ts:183-190) in JavaScript
// numbers never does.
func TestBackoffDurationSaturates(t *testing.T) {
	t.Parallel()
	one := func() float64 { return 1 }
	prev := mrp.BackoffDuration(mrp.SessionActiveIntervalDefault, 0, one)
	for n := 1; n <= 200; n++ {
		d := mrp.BackoffDuration(mrp.SessionActiveIntervalDefault, n, one)
		if d < prev {
			t.Fatalf("transmission %d: backoff %v below transmission %d's %v", n, d, n-1, prev)
		}
		prev = d
	}
	if prev != time.Duration(math.MaxInt64) {
		t.Fatalf("transmission 200: backoff %v, want the saturated maximum", prev)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"testing"
	"time"
)

// TestRetries: RetrySchedule.ts:28-60 over the sustain schedule — the base
// doubles, the jitter (floor of 0.25 × base × draw, in ms) is added, the
// sum is clamped to the hour; the zero configuration is 1 s, ×2.
func TestRetries(t *testing.T) {
	t.Parallel()
	r := newRetries(DefaultSustainRetries, func() float64 { return 0 })
	want := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	for i, w := range want {
		if got := r.next(); got != w {
			t.Fatalf("interval %d = %v, want %v", i, got, w)
		}
	}
	j := newRetries(DefaultSustainRetries, func() float64 { return 0.9999 })
	if got := j.next(); got != 15*time.Second+3749*time.Millisecond {
		t.Fatalf("jittered first interval %v", got)
	}
	z := newRetries(RetrySchedule{}, func() float64 { return 0.5 })
	if a, b := z.next(), z.next(); a != time.Second || b != 2*time.Second {
		t.Fatalf("defaults %v, %v", a, b)
	}
}

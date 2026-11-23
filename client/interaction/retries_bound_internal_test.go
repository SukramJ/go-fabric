// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"math"
	"testing"
	"time"
)

// TestSustainRetries_NeverShrinkBelowTheFirstInterval: a sustained
// subscription's schedule built from out-of-range fields never yields an
// interval of 0 s or less nor one below its first (review II,
// R3-api-3580; a hardening beyond matter.js).
func TestSustainRetries_NeverShrinkBelowTheFirstInterval(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]RetrySchedule{
		"backoff 0.5": {InitialInterval: 15 * time.Second, BackoffFactor: 0.5},
		"backoff NaN": {InitialInterval: 15 * time.Second, BackoffFactor: math.NaN()},
		"initial -1s": {InitialInterval: -time.Second, BackoffFactor: 2},
		"maximum -1s": {InitialInterval: 15 * time.Second, BackoffFactor: 2, MaximumInterval: -time.Second},
		"jitter -3":   {InitialInterval: 15 * time.Second, BackoffFactor: 2, JitterFactor: -3},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newRetries(cfg, func() float64 { return 0.5 })
			first := r.next()
			if first <= 0 {
				t.Fatalf("first interval %v, want a positive one", first)
			}
			for i := range 1000 {
				if iv := r.next(); iv < first || iv <= 0 {
					t.Fatalf("step %d: interval %v, want at least %v", i, iv, first)
				}
			}
		})
	}
}

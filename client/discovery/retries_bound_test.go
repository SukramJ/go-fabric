// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"math"
	"testing"
	"time"
)

// TestRetrySchedule_NeverShrinksBelowItsFirstInterval: a schedule built
// from out-of-range fields — a backoff below 1, NaN or negative, a negative
// initial or maximum interval, a negative jitter — never yields an
// interval below the first one nor one of 0 s or less, so a discovery
// without a timeout or count cannot turn into a query on every loop turn
// (review II, R3-api-3580; a hardening beyond matter.js).
func TestRetrySchedule_NeverShrinksBelowItsFirstInterval(t *testing.T) {
	t.Parallel()
	for name, o := range map[string]Retries{
		"backoff 0.5":              {BackoffFactor: 0.5},
		"backoff -2":               {BackoffFactor: -2},
		"backoff NaN":              {BackoffFactor: math.NaN()},
		"initial -1s":              {InitialInterval: -time.Second},
		"maximum -1s":              {MaximumInterval: -time.Second},
		"jitter -3":                {JitterFactor: -3},
		"jitter NaN":               {JitterFactor: math.NaN()},
		"initial -1s, backoff 0.1": {InitialInterval: -time.Second, BackoffFactor: 0.1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newRetrySchedule(DefaultRetries.merge(&o), func() uint32 { return 1 << 31 })
			first, ok := s.next()
			if !ok || first <= 0 {
				t.Fatalf("first interval %v, %v; want a positive one", first, ok)
			}
			for i := range 1000 {
				iv, ok := s.next()
				if !ok {
					t.Fatalf("step %d: the schedule ended without a timeout or count", i)
				}
				if iv < min(first, DefaultRetries.InitialInterval) || iv <= 0 {
					t.Fatalf("step %d: interval %v, want at least %v", i, iv, min(first, DefaultRetries.InitialInterval))
				}
			}
		})
	}
}

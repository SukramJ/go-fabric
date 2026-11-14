// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"math"
	"testing"
)

// TestFloorIntOfNaN (review II, R2-P1-cam-a-03): floorInt maps NaN to 0 on
// every platform, so a 0x0 resolution under an offer's max-mbps of 0
// gives a frame-rate ceiling of 0 and not an implementation-defined int
// (math.MinInt64 on amd64).
func TestFloorIntOfNaN(t *testing.T) {
	t.Parallel()
	if got := floorInt(math.NaN()); got != 0 {
		t.Fatalf("floorInt(NaN) = %d, want 0", got)
	}
	limits := videoCodecLimits{}
	limits.maxPixelsPerSecond.set, limits.maxPixelsPerSecond.value = true, 0
	if got, ok := offerFrameRateCeiling(limits, resolution{}); !ok || got != 0 {
		t.Fatalf("offerFrameRateCeiling(max-mbps 0, 0x0) = %d, %v; want 0, true", got, ok)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sensing_test

import (
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/sensing"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
)

// TestOccupancyHoldTimeWriteAndHostSetAgree: a controller's HoldTime write
// and the host's Set of HoldTime run at once. Whichever goes last, the
// HoldTime attribute and the hold time the timer and the deprecated delay
// attributes use are the same value: the write's change of the hold time
// and its store are one step (review II, R2-P1-clu-04).
func TestOccupancyHoldTimeWriteAndHostSetAgree(t *testing.T) {
	t.Parallel()
	const runs = 2000
	for i := range runs {
		o, _ := newOcc(t, sensing.OccupancyConfig{
			Features:       occdef.FeaturePassiveInfrared,
			Attributes:     []uint32{occdef.AttrHoldTime, occdef.AttrPirOccupiedToUnoccupiedDelay},
			HoldTimeLimits: occLimits(1, 100, 10), HoldTime: ptr[uint16](10), Clock: newFake(),
		})
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			if err := writeU16(o, occdef.AttrHoldTime, 30); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			<-start
			if err := setHoldTime(o, 60); err != nil {
				t.Error(err)
			}
		})
		close(start)
		wg.Wait()
		attr, delay := readU16(t, o, occdef.AttrHoldTime), readU16(t, o, occdef.AttrPirOccupiedToUnoccupiedDelay)
		if attr != delay || attr != o.HoldTime() {
			t.Fatalf("run %d: HoldTime attribute %d, PIROccupiedToUnoccupiedDelay %d, the hold time %d: they disagree", i, attr, delay, o.HoldTime())
		}
	}
}

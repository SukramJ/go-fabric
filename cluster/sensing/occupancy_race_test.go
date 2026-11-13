// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sensing_test

import (
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/sensing"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
)

// TestOccupancyHostReportBeatsAFiringHoldTimer: the hold timer firing and
// the host's occupied report run at once. Whichever goes first, the sensor
// ends occupied — the host's report is the latest word. The timer's check
// that it is still current and its store of unoccupied are one step, so an
// occupied report cannot land between them and be overwritten (review II,
// R2-P1-clu-03). The race is a narrow window, so the case runs many times;
// before the fix about one run in a hundred ended unoccupied.
func TestOccupancyHostReportBeatsAFiringHoldTimer(t *testing.T) {
	t.Parallel()
	const runs = 2000
	for i := range runs {
		clk := newFake()
		o, _ := newOcc(t, sensing.OccupancyConfig{
			Features:   occdef.FeaturePassiveInfrared,
			Attributes: holdTimeAttrs, HoldTimeLimits: occLimits(1, 10, 1), HoldTime: ptr[uint16](5), Clock: clk,
		})
		setOccupied(t, o, true)
		setOccupied(t, o, false) // arms the 5-s hold timer
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() { <-start; clk.Advance(5 * time.Second) })
		wg.Go(func() { <-start; setOccupied(t, o, true) })
		close(start)
		wg.Wait()
		if !occupied(t, o) {
			t.Fatalf("run %d: unoccupied after the host's occupied report: the firing timer overwrote it", i)
		}
	}
}

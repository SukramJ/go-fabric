// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import "testing"

// TestSweepAfterTheWatchStopped (review II, R2-P1-cam-b-05): a sweep asked
// for after the watch stopped — a report hook already running when the
// Service closed — starts no EndSession and adds no work behind the
// stop's wait. Before, it added to the wait group after Close waited and
// ended the camera's leftovers after Close returned.
func TestSweepAfterTheWatchStopped(t *testing.T) {
	t.Parallel()
	f, w, _, _ := newWatchFixture()
	w.stop()
	w.sweep(wCamera, wEndpoint, map[uint16]bool{5: true})
	w.work.Wait() // whatever the sweep started has run
	if _, _, attempts := f.snapshot(); attempts != 0 {
		t.Fatalf("%d EndSession after the watch stopped, want none", attempts)
	}
}

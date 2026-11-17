// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"
)

// TestQuieterChangeDuringACommandWaitsForItsThrottle (review II, N4-x3-1):
// a projected quieter attribute that changes while a command runs against
// its cluster advances the version once with the command's commit, and its
// report still waits for the throttle — the commit's end does not mark it
// at once. matter.js advances the version at the commit
// (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457 at
// db021da0) and reports a quieter property only when its QuietEvent emits
// (packages/node/src/behavior/internal/ServerBehaviorBacking.ts:224-263).
func TestQuieterChangeDuringACommandWaitsForItsThrottle(t *testing.T) {
	t.Parallel()
	r := newQuietMeterRig(t, epmActivePower)
	v0 := r.version()
	r.src.setPower(10) // null → value: at once
	r.step(t, "a change from null", v0, 1, true)
	r.clk.Advance(100 * time.Millisecond)
	key := invokedCluster{endpoint: r.path.Endpoint, cluster: r.path.Cluster}
	r.b.invokeCommits.begin(key)
	r.src.setPower(20) // inside the command, within the second
	held, advance := r.b.invokeCommits.end(key)
	reportCommandChange(r.b.Topology(), r.mgr, key, held, advance)
	r.step(t, "a change inside a command within a second of the last report", v0, 2, false)
	r.clk.Advance(900 * time.Millisecond)
	r.step(t, "after the second", v0, 2, true)
}

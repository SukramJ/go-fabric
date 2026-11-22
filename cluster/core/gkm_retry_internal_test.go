// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
)

// A retry callback that lost the race against a Stop — the retry was
// disarmed and armed again before the callback ran — does nothing: it
// neither clears the newer timer nor starts a chain of its own (review II,
// R2-P6-err-1701 R3-a).
func TestGKMSupersededRetryCallbackLeavesTheArmedOne(t *testing.T) {
	t.Parallel()
	g := &GroupKeyManagement{clk: clocktest.New(time.Unix(1_700_000_000, 0))}
	g.mu.Lock()
	g.armRetryLocked()
	old := g.retrySeq
	g.stopRetryLocked()
	g.armRetryLocked()
	armed, seq := g.retryTimer, g.retrySeq
	g.mu.Unlock()
	g.retryStale(old)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.retryTimer != armed || g.retrySeq != seq {
		t.Errorf("the superseded callback changed the armed retry: timer kept %v, seq %d → %d", g.retryTimer == armed, seq, g.retrySeq)
	}
}

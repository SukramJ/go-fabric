// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"testing"
	"time"
)

// TestDefaultSubscriptionKeepsItsOwnIntervals (review II, R2-P6-api-2356):
// the node keeps a copy of the interval bounds it is given and hands out a
// copy, so neither the caller's pointer nor the returned options change the
// node's setting. Before, clone shared the pointees.
func TestDefaultSubscriptionKeepsItsOwnIntervals(t *testing.T) {
	t.Parallel()
	n, _, _ := testNode(t, &MemoryStore{})
	floor, ceiling := 2*time.Second, time.Minute
	if err := n.SetDefaultSubscription(t.Context(), &SubscribeOptions{MinIntervalFloor: &floor, MaxIntervalCeiling: &ceiling}); err != nil {
		t.Fatal(err)
	}
	floor, ceiling = 7*time.Second, time.Hour
	got := n.DefaultSubscription()
	if *got.MinIntervalFloor != 2*time.Second || *got.MaxIntervalCeiling != time.Minute {
		t.Fatalf("after the caller's write: floor %v, ceiling %v; want 2s, 1m", *got.MinIntervalFloor, *got.MaxIntervalCeiling)
	}
	*got.MinIntervalFloor, *got.MaxIntervalCeiling = 9*time.Second, 2*time.Hour
	again := n.DefaultSubscription()
	if *again.MinIntervalFloor != 2*time.Second || *again.MaxIntervalCeiling != time.Minute {
		t.Fatalf("after a write to the returned options: floor %v, ceiling %v; want 2s, 1m", *again.MinIntervalFloor, *again.MaxIntervalCeiling)
	}
}

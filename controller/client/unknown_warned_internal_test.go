// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import "testing"

// TestUnknownEventsLoggedAreBounded (review II, R2-P6-bound-2357, owner
// decision 223): a peer that reports events under ever new ids grows the
// node's set of unknown events logged to maxUnknownWarned, then one line
// says further ones are not logged. Before, the set grew without a bound.
func TestUnknownEventsLoggedAreBounded(t *testing.T) {
	t.Parallel()
	n, _, logs := testNode(t, &MemoryStore{})
	for id := range uint32(10_000) {
		n.noteUnknown(0xFFF1_FC00, id, true)
	}
	n.mu.RLock()
	size := len(n.warned)
	n.mu.RUnlock()
	if size != maxUnknownWarned {
		t.Fatalf("%d unknown events recorded, want the bound %d", size, maxUnknownWarned)
	}
	if got := logs.count("matter.controller.client.unknown_event"); got != maxUnknownWarned {
		t.Fatalf("%d unknown events logged, want %d", got, maxUnknownWarned)
	}
	if got := logs.count("matter.controller.client.unknown_not_logged"); got != 1 {
		t.Fatalf("the bound was logged %d times, want once", got)
	}
	n.noteUnknown(0xFFF1_FC00, 0, true) // one logged before stays quiet
	if got := logs.count("matter.controller.client.unknown_event"); got != maxUnknownWarned {
		t.Fatalf("an event logged before was logged again: %d", got)
	}
}

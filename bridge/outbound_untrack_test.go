// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"
)

// A message whose send failed leaves nothing to retransmit: Untrack drops
// what Track registered just before the send.
func TestOutboundReliableUntrack(t *testing.T) {
	t.Parallel()
	tr := newOutboundReliableTracker(nil)
	tr.Track(7, 1, 2, []byte{1}, nil, time.Now())
	tr.Track(8, 1, 2, []byte{1}, nil, time.Now())
	tr.Untrack(1, 7)
	if n := tr.Pending(); n != 1 {
		t.Fatalf("pending %d after Untrack, want 1", n)
	}
	if !tr.Ack(1, 8) {
		t.Error("the other message lost its entry")
	}
}

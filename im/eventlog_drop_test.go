// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import "testing"

// TestEventLog_DropBufferedKeepsNumbering: going offline loses the
// buffered events of a volatile log but never restarts its numbering
// (matter.js EventsBehavior, #4594).
func TestEventLog_DropBufferedKeepsNumbering(t *testing.T) {
	t.Parallel()
	l := NewEventLog()
	l.Append(EventRecord{Priority: EventPriorityCritical, Endpoint: 0, Cluster: 0x28, EventID: 1})
	last := l.Append(EventRecord{Priority: EventPriorityInfo, Endpoint: 0, Cluster: 0x28, EventID: 2})
	l.DropBuffered()
	if got := l.Query(0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0); len(got) != 0 {
		t.Fatalf("after DropBuffered the log still answers %d records", len(got))
	}
	if next := l.Append(EventRecord{Priority: EventPriorityInfo, Cluster: 0x28}); next != last+1 {
		t.Fatalf("next event number = %d, want %d — numbering must continue", next, last+1)
	}
}

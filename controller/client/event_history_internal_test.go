// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
)

func recorded(numbers ...uint64) []RecordedEvent {
	out := make([]RecordedEvent, len(numbers))
	for i, n := range numbers {
		out[i] = RecordedEvent{EventData: interaction.EventData{Number: n}}
	}
	return out
}

func numbersOf(events []RecordedEvent) []uint64 {
	out := make([]uint64, len(events))
	for i := range events {
		out[i] = events[i].Number
	}
	return out
}

// TestEventHistoryRing: the history keeps the newest events up to its
// capacity, oldest first; since is inclusive, limit keeps the newest; a
// negative capacity keeps none, 0 the default; clear empties it.
func TestEventHistoryRing(t *testing.T) {
	t.Parallel()
	h := newEventHistory(3)
	for _, e := range recorded(1, 2, 3, 4, 5) {
		h.record(e)
	}
	equal := func(got []RecordedEvent, want ...uint64) {
		t.Helper()
		g := numbersOf(got)
		if len(g) != len(want) {
			t.Fatalf("history %v, want %v", g, want)
		}
		for i := range g {
			if g[i] != want[i] {
				t.Fatalf("history %v, want %v", g, want)
			}
		}
	}
	equal(h.query(0, 0), 3, 4, 5)
	equal(h.query(4, 0), 4, 5)
	equal(h.query(0, 2), 4, 5)
	equal(h.query(6, 0))
	h.clear()
	equal(h.query(0, 0))
	h.record(recorded(9)[0])
	equal(h.query(0, 0), 9)

	none := newEventHistory(-1)
	none.record(recorded(1)[0])
	equal(none.query(0, 0))
	if c := cap(newEventHistory(0).ring); c != DefaultEventHistory {
		t.Fatalf("default capacity %d, want %d", c, DefaultEventHistory)
	}
	var unset *eventHistory
	unset.record(recorded(1)[0])
	unset.clear()
	equal(unset.query(0, 0))
}

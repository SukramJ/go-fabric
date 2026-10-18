// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// A port of matter.js packages/protocol/test/events/NonvolatileEventStoreTest.ts
// at 862ad75f: an event store that persists its events. The Go EventLog
// persists only its numbering (the ceiling), so the cases about persisted
// events stay missing; a case whose assertion is the numbering runs on
// the log as VolatileEventStoreTest's port does (esStart).

import "testing"

// TestNonvolatileEventStoreClear ports the numbering case of the "clear"
// group (NonvolatileEventStoreTest.ts:133-143).
func TestNonvolatileEventStoreClear(t *testing.T) {
	t.Parallel()
	t.Run("continues numbering across a restart after events were persisted again", func(t *testing.T) {
		t.Parallel()
		s := &esStorage{}
		l := esStart(s, 0)
		l.Append(esOccurrence())
		l.DropBuffered()
		last := l.Append(esOccurrence())
		if n := esStart(s, 0).Append(esOccurrence()); n <= last {
			t.Fatalf("after a restart %d, want > %d", n, last)
		}
	})
}

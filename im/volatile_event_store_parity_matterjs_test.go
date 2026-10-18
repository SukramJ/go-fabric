// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// A port of matter.js packages/protocol/test/events/VolatileEventStoreTest.ts
// at 862ad75f: an in-memory event store whose numbering survives a
// restart through a stored reservation. The Go counterpart is EventLog
// with its counter persistence: the reservation is the persisted ceiling,
// a restart a new log seeded from it as node/node.go wires it, clear with
// keepNumbering DropBuffered. The log has no delete, no plain clear and no
// non-volatile predecessor to convert; those cases stay missing.

import (
	"sync"
	"testing"
)

// esStorage is the reservation's storage: the last persisted ceiling.
type esStorage struct {
	mu      sync.Mutex
	ceiling uint64
	writes  int
}

func (s *esStorage) persist(ceiling uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ceiling = ceiling
	s.writes++
}

// esStart boots a log on storage with the given epoch: seeded from the
// stored ceiling, then wired to persist (node/node.go:291-299).
func esStart(s *esStorage, epoch uint64) *EventLog {
	l := NewEventLog()
	s.mu.Lock()
	l.SeedNumber(s.ceiling)
	s.mu.Unlock()
	l.SetCounterPersistence(s.persist, epoch)
	return l
}

// esOccurrence is occurrence() of the test: epoch 1000, Info, a payload.
func esOccurrence() EventRecord {
	return EventRecord{EpochMS: 1_000, Priority: EventPriorityInfo, Endpoint: 1, Cluster: 0x28, EventID: 0, Payload: 1}
}

// esGet is store.get(number) on the log: the buffered record of that number.
func esGet(l *EventLog, number uint64) (EventRecord, bool) {
	for _, r := range l.Query(0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF, number) {
		if r.Number == number {
			return r, true
		}
	}
	return EventRecord{}, false
}

// TestVolatileEventStore ports the top-level cases
// (VolatileEventStoreTest.ts:81-114, :233-237).
func TestVolatileEventStore(t *testing.T) {
	t.Parallel()

	t.Run("loads empty on a fresh store", func(t *testing.T) {
		t.Parallel()
		l := esStart(&esStorage{}, 0)
		if got := l.Query(0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0); len(got) != 0 {
			t.Fatalf("fresh log holds %d events", len(got))
		}
	})

	t.Run("retrieves a stored occurrence by number", func(t *testing.T) {
		t.Parallel()
		l := esStart(&esStorage{}, 0)
		occ := esOccurrence()
		n := l.Append(occ)
		got, ok := esGet(l, n)
		occ.Number = n
		if !ok || got != occ {
			t.Fatalf("get(%d) = %+v (%v), want %+v", n, got, ok, occ)
		}
	})

	t.Run("throws for an unknown event number", func(t *testing.T) {
		t.Parallel()
		// Go returns no record where matter.js throws "Invalid event occurrence".
		if _, ok := esGet(esStart(&esStorage{}, 0), 999); ok {
			t.Fatal("a record for an unknown number")
		}
	})
}

// TestVolatileEventStoreNumbering ports the "numbering" group
// (VolatileEventStoreTest.ts:116-231). A Go reservation write is
// synchronous, made before the number is handed out.
func TestVolatileEventStoreNumbering(t *testing.T) {
	t.Parallel()

	t.Run("hands out numbers synchronously again once the reservation is stored", func(t *testing.T) {
		t.Parallel()
		// The next number inside the stored reservation needs no write.
		s := &esStorage{}
		l := esStart(s, 1_000)
		l.Append(esOccurrence())
		writes := s.writes
		l.Append(esOccurrence())
		if s.writes != writes {
			t.Fatalf("%d reservation writes for a number already covered", s.writes-writes)
		}
	})

	t.Run("stores queued reservations in order", func(t *testing.T) {
		t.Parallel()
		// Block size 1, two adds at once (:162-185).
		s := &esStorage{}
		l := esStart(s, 1)
		var wg sync.WaitGroup
		numbers := make([]uint64, 2)
		for i := range numbers {
			wg.Go(func() { numbers[i] = l.Append(esOccurrence()) })
		}
		wg.Wait()
		last := max(numbers[0], numbers[1])
		if next := esStart(s, 0).Append(esOccurrence()); next <= last {
			t.Fatalf("after a restart %d, want > %d", next, last)
		}
	})

	t.Run("ignores a stored reservation below 1", func(t *testing.T) {
		t.Parallel()
		if n := esStart(&esStorage{ceiling: 0}, 0).Append(esOccurrence()); n != 1 {
			t.Fatalf("first number %d, want 1", n)
		}
	})
}

// TestVolatileEventStoreClear ports the "clear" group
// (VolatileEventStoreTest.ts:239-308) for keepNumbering, which is
// DropBuffered.
func TestVolatileEventStoreClear(t *testing.T) {
	t.Parallel()

	t.Run("continues numbering with keepNumbering", func(t *testing.T) {
		t.Parallel()
		l := esStart(&esStorage{}, 0)
		l.Append(esOccurrence())
		last := l.Append(esOccurrence())
		l.DropBuffered()
		if n := l.Append(esOccurrence()); n != last+1 {
			t.Fatalf("after the clear %d, want %d", n, last+1)
		}
	})

	t.Run("continues numbering across a restart with keepNumbering", func(t *testing.T) {
		t.Parallel()
		s := &esStorage{}
		l := esStart(s, 0)
		last := l.Append(esOccurrence())
		l.DropBuffered()
		if n := esStart(s, 0).Append(esOccurrence()); n != last+1 {
			t.Fatalf("after a restart %d, want %d", n, last+1)
		}
	})

	t.Run("reserves numbers from the kept numbering on, not from 1", func(t *testing.T) {
		t.Parallel()
		s := &esStorage{}
		l := esStart(s, 2)
		for range 5 {
			l.Append(esOccurrence())
		}
		l.DropBuffered()
		last := l.Append(esOccurrence())
		if n := esStart(s, 0).Append(esOccurrence()); n <= last {
			t.Fatalf("after a restart %d, want > %d", n, last)
		}
	})
}

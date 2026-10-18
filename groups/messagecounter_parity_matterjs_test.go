// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/protocol/MessageCounterTest.ts at
// 862ad75f: the persisted counter. The Go PersistedMessageCounter is the group
// data counter (dataCounter), which SessionManager.ts configures with a
// reserve of 1000, a rollover callback (a warning) and the default rollover
// info difference of 100 000 — so the cases run with those options; the
// in-memory counter cases live in transport/mrp.

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
)

// mcWarnings counts the near-rollover warnings (the aboutToRolloverCallback).
type mcWarnings struct{ n atomic.Int32 }

func (w *mcWarnings) Enabled(context.Context, slog.Level) bool { return true }

func (w *mcWarnings) Handle(_ context.Context, r slog.Record) error {
	if r.Message == "matter.groups.counter_near_rollover" {
		w.n.Add(1)
	}
	return nil
}
func (w *mcWarnings) WithAttrs([]slog.Attr) slog.Handler { return w }
func (w *mcWarnings) WithGroup(string) slog.Handler      { return w }

// mcRandom is the random start of MessageCounterTest.ts:22 — randomBytes
// 12345678, so the counter starts at (0x12345678 >>> 4) + 1.
func mcRandom() (uint32, error) { return (0x12345678 >> 4) + 1, nil }

func mcNew(t *testing.T, st *memStore, w *mcWarnings) *dataCounter {
	t.Helper()
	if w == nil {
		w = &mcWarnings{}
	}
	c, err := newDataCounter(context.Background(), st, slog.New(w), mcRandom)
	if err != nil {
		t.Fatalf("newDataCounter: %v", err)
	}
	return c
}

func mcNext(t *testing.T, c *dataCounter) uint32 {
	t.Helper()
	v, err := c.next(context.Background())
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	return v
}

func mcStored(t *testing.T, st *memStore) uint64 {
	t.Helper()
	v, ok, _ := st.GetMetadataCounter(context.Background(), GroupDataCounterKey)
	if !ok {
		t.Fatal("no persisted counter")
	}
	return v
}

func mcSet(st *memStore, v uint64) {
	_ = st.SetMetadataCounter(context.Background(), GroupDataCounterKey, v)
}

func TestMessageCounterPersisted(t *testing.T) {
	t.Parallel()
	t.Run("persistent initialization of counter with no persisted value", func(t *testing.T) {
		t.Parallel()
		c := mcNew(t, newMemStore(), nil)
		if got := mcNext(t, c); got != (0x12345679>>4)+2 { // MessageCounterTest.ts:47
			t.Errorf("counter = %d", got)
		}
	})

	t.Run("persistent initialization of counter with persisted value", func(t *testing.T) {
		t.Parallel()
		st := newMemStore()
		mcSet(st, 0x12345678)
		if got := mcNext(t, mcNew(t, st, nil)); got != 0x12345679 {
			t.Errorf("counter = %#x", got)
		}
	})

	t.Run("persisted counter increases by 1", func(t *testing.T) {
		t.Parallel()
		st := newMemStore()
		mcSet(st, 0x12345678)
		c := mcNew(t, st, nil)
		first := mcNext(t, c)
		if first != 0x12345679 {
			t.Errorf("counter = %#x", first)
		}
		if got := mcNext(t, c); got != first+1 {
			t.Errorf("second = %#x, want %#x", got, first+1)
		}
	})

	t.Run("persisted counter constructor throws in too large value in storage", func(t *testing.T) {
		t.Parallel()
		st := newMemStore()
		mcSet(st, maxCounterValue+1)
		if _, err := newDataCounter(context.Background(), st, slog.New(&mcWarnings{}), mcRandom); err == nil {
			t.Error("a counter beyond 32 bits was accepted")
		}
	})
}

func TestMessageCounterRollover(t *testing.T) {
	t.Parallel()
	t.Run("emits 0xFFFFFFFF before rolling over to 0", func(t *testing.T) {
		t.Parallel()
		st := newMemStore()
		mcSet(st, 0xfffffffe)
		c := mcNew(t, st, nil)
		if got := mcNext(t, c); got != 0xffffffff {
			t.Errorf("first = %#x", got)
		}
		if got := mcNext(t, c); got != 0 {
			t.Errorf("second = %#x", got)
		}
	})

	// MessageCounterTest.ts:114-146 use rolloverInfoDifference 100; the Go
	// counter has the matter.js default 100 000, so the vectors are taken at
	// the same distance from that mark.
	t.Run("Message counter rolls over if allowed and calls callback on increase once", func(t *testing.T) {
		t.Parallel()
		st := newMemStore()
		init := uint64(maxCounterValue - rolloverInfoDifference - 1)
		mcSet(st, init)
		w := &mcWarnings{}
		c := mcNew(t, st, w)
		if w.n.Load() != 0 {
			t.Error("callback on init")
		}
		if got := mcNext(t, c); uint64(got) != init+1 || w.n.Load() != 1 {
			t.Errorf("first = %d, callbacks = %d", got, w.n.Load())
		}
		if got := mcNext(t, c); uint64(got) != init+2 || w.n.Load() != 1 {
			t.Errorf("second = %d, callbacks = %d", got, w.n.Load())
		}
	})

	t.Run("Message counter rolls over if allowed and calls callback already on init once", func(t *testing.T) {
		t.Parallel()
		st := newMemStore()
		init := uint64(maxCounterValue - rolloverInfoDifference)
		mcSet(st, init)
		w := &mcWarnings{}
		c := mcNew(t, st, w)
		if w.n.Load() != 1 {
			t.Errorf("callbacks on init = %d, want 1", w.n.Load())
		}
		if got := mcNext(t, c); uint64(got) != init+1 || w.n.Load() != 1 {
			t.Errorf("first = %d, callbacks = %d", got, w.n.Load())
		}
	})

	t.Run("reserves a block ahead and does not write storage within the block", func(t *testing.T) {
		t.Parallel()
		st := newMemStore()
		c := mcNew(t, st, nil)
		start := uint64((0x12345678 >> 4) + 1) // MessageCounterTest.ts:152
		if got := mcStored(t, st); got != start+1000 {
			t.Errorf("stored = %d, want %d", got, start+1000)
		}
		mcNext(t, c)
		mcNext(t, c)
		if got := mcStored(t, st); got != start+1000 {
			t.Errorf("stored after two = %d, want %d", got, start+1000)
		}
	})

	t.Run("never rolls back across a restart when reserving", func(t *testing.T) {
		t.Parallel()
		st := newMemStore()
		used := mcNext(t, mcNew(t, st, nil))
		if got := mcNext(t, mcNew(t, st, nil)); got <= used {
			t.Errorf("after restart = %d, not above %d", got, used)
		}
	})

	t.Run("re-reserves after a rollover so wrapped values are not re-issued", func(t *testing.T) {
		t.Parallel()
		st := newMemStore()
		mcSet(st, maxCounterValue)
		c := mcNew(t, st, nil)
		if got := mcNext(t, c); got != 0 {
			t.Errorf("rollover = %d", got)
		}
		if got := mcStored(t, st); got != 1000 {
			t.Errorf("stored after rollover = %d, want 1000", got)
		}
		used := mcNext(t, c)
		if got := mcNext(t, mcNew(t, st, nil)); got <= used {
			t.Errorf("after restart = %d, not above %d", got, used)
		}
	})
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// warnCounter counts the near-rollover warnings a manager logs.
type warnCounter struct {
	mu sync.Mutex
	n  int
}

func (w *warnCounter) Enabled(context.Context, slog.Level) bool { return true }
func (w *warnCounter) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn && r.Message == "matter.groups.counter_near_rollover" {
		w.mu.Lock()
		w.n++
		w.mu.Unlock()
	}
	return nil
}
func (w *warnCounter) WithAttrs([]slog.Attr) slog.Handler { return w }
func (w *warnCounter) WithGroup(string) slog.Handler      { return w }

func (w *warnCounter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.n
}

func fixedStart(v uint32) func() (uint32, error) { return func() (uint32, error) { return v, nil } }

// TestGroupDataCounterFirstRun: without a persisted value the counter
// starts at its random 28-bit start (MessageCounter.ts:57-60 — matter.js's
// seed, the legacy per-key send counters of MessagingState.ts:41-60, does
// not exist here) and the reserve is persisted before the first value is
// handed out (MessageCounter.ts:150-152, :161-164).
func TestGroupDataCounterFirstRun(t *testing.T) {
	t.Parallel()
	st := newMemStore()
	c, err := newDataCounter(context.Background(), st, slog.New(slog.DiscardHandler), fixedStart(0x0123_4567))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := st.metaValue(GroupDataCounterKey); !ok || v != 0x0123_4567+1000 {
		t.Fatalf("persisted %d (%v) before the first value, want start+1000", v, ok)
	}
	for want := uint32(0x0123_4568); want < 0x0123_4567+5; want++ {
		if got, err := c.next(context.Background()); err != nil || got != want {
			t.Fatalf("next = %d (%v), want %d", got, err, want)
		}
	}
	if st.metaWrites != 1 {
		t.Errorf("%d writes inside the reserve, want 1", st.metaWrites)
	}
	// The random start is a 28-bit value plus one.
	for range 64 {
		s, err := randomCounterStart()
		if err != nil || s == 0 || s > 1<<28 {
			t.Fatalf("random start %#x (%v)", s, err)
		}
	}
}

// TestGroupDataCounterRestartResumesPastTheMark: a restart — clean or
// not — resumes at the persisted mark, so its first value lies above every
// value handed out before; crossing the mark persists the next one first
// (MessageCounter.ts:131-136, :170-174).
func TestGroupDataCounterRestartResumesPastTheMark(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newMemStore()
	c, err := newDataCounter(ctx, st, slog.New(slog.DiscardHandler), fixedStart(100))
	if err != nil {
		t.Fatal(err)
	}
	var last uint32
	for range 1500 {
		if last, err = c.next(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if last != 1600 {
		t.Fatalf("last %d, want 1600", last)
	}
	mark, _ := st.metaValue(GroupDataCounterKey)
	if mark != 2100 {
		t.Fatalf("mark %d after crossing 1100, want 2100", mark)
	}
	// "Crash": a new counter on the same store, the random start ignored.
	again, err := newDataCounter(ctx, st, slog.New(slog.DiscardHandler), fixedStart(7))
	if err != nil {
		t.Fatal(err)
	}
	if first, _ := again.next(ctx); first != 2101 || first <= last {
		t.Fatalf("after restart %d, want 2101 (> %d)", first, last)
	}
	if v, _ := st.metaValue(GroupDataCounterKey); v != 3100 {
		t.Errorf("restart reserved %d, want 3100", v)
	}
}

// TestGroupDataCounterRollsOverAndWarnsOnce: the counter warns once
// 100 000 values before the wrap (MessageCounter.ts:71-76) — or at start
// when it is already that close (:137-143) — and wraps to 0 instead of
// failing, re-reserving the low values (:64-68, :170-174).
func TestGroupDataCounterRollsOverAndWarnsOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	st := newMemStore()
	_ = st.SetMetadataCounter(ctx, GroupDataCounterKey, maxCounterValue-rolloverInfoDifference-2)
	w := &warnCounter{}
	c, err := newDataCounter(ctx, st, slog.New(w), fixedStart(1))
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if _, err := c.next(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if w.count() != 1 {
		t.Fatalf("%d warnings crossing the threshold, want 1", w.count())
	}

	st = newMemStore()
	_ = st.SetMetadataCounter(ctx, GroupDataCounterKey, maxCounterValue-1)
	w = &warnCounter{}
	c, err = newDataCounter(ctx, st, slog.New(w), fixedStart(1))
	if err != nil {
		t.Fatal(err)
	}
	if w.count() != 1 {
		t.Fatalf("%d warnings at a start near the wrap, want 1", w.count())
	}
	if v, _ := st.metaValue(GroupDataCounterKey); v != maxCounterValue {
		t.Fatalf("reserve capped at %d, want 0xFFFFFFFF", v)
	}
	if v, _ := c.next(ctx); v != maxCounterValue {
		t.Fatalf("next %#x, want 0xFFFFFFFF", v)
	}
	if v, err := c.next(ctx); err != nil || v != 0 {
		t.Fatalf("after the wrap %d (%v), want 0", v, err)
	}
	if v, _ := st.metaValue(GroupDataCounterKey); v != groupDataCounterReserve {
		t.Fatalf("after the wrap reserved %d, want 1000", v)
	}
	if v, _ := c.next(ctx); v != 1 {
		t.Fatalf("then %d, want 1", v)
	}
	if w.count() != 1 {
		t.Errorf("%d warnings, want 1 — the wrap itself does not warn", w.count())
	}
}

func TestGroupDataCounterFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)

	st := newMemStore()
	_ = st.SetMetadataCounter(ctx, GroupDataCounterKey, maxCounterValue+1)
	if _, err := newDataCounter(ctx, st, log, fixedStart(1)); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("a value past 32 bits: %v", err)
	}
	st = newMemStore()
	st.failMeta = errors.New("read")
	if _, err := newDataCounter(ctx, st, log, fixedStart(1)); err == nil {
		t.Error("a failed read went unnoticed")
	}
	st = newMemStore()
	if _, err := newDataCounter(ctx, st, log, func() (uint32, error) { return 0, errors.New("entropy") }); err == nil {
		t.Error("a failed random start went unnoticed")
	}
	st = newMemStore()
	st.failMetaSet = errors.New("write")
	if _, err := newDataCounter(ctx, st, log, fixedStart(1)); err == nil {
		t.Error("a failed first reserve went unnoticed")
	}
	st = newMemStore()
	c, err := newDataCounter(ctx, st, log, fixedStart(1))
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.failMetaSet = errors.New("write")
	st.mu.Unlock()
	for range 999 {
		if _, err := c.next(ctx); err != nil {
			t.Fatalf("inside the reserve: %v", err)
		}
	}
	if _, err := c.next(ctx); err == nil {
		t.Error("crossing the mark without persisting it handed out a value")
	}

	// A store without the counter cannot send.
	m, err := NewManager(struct{ Store }{newMemStore()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.groupDataCounter(ctx); !errors.Is(err, ErrNoCounterStore) {
		t.Errorf("store without counter: %v", err)
	}
	m2, _ := NewManager(newMemStore(), nil)
	c1, err1 := m2.groupDataCounter(ctx)
	c2, err2 := m2.groupDataCounter(ctx)
	if err1 != nil || err2 != nil || c1 != c2 {
		t.Errorf("the counter is not node-global: %p %p (%v %v)", c1, c2, err1, err2)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package logring

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countHandler is an inner handler that counts what reaches it.
type countHandler struct {
	level slog.Level
	n     atomic.Int64
	err   error
}

func (c *countHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= c.level }

func (c *countHandler) Handle(context.Context, slog.Record) error {
	c.n.Add(1)
	return c.err
}
func (c *countHandler) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *countHandler) WithGroup(string) slog.Handler      { return c }

func seqs(rs []Record) []uint64 {
	out := make([]uint64, len(rs))
	for i, r := range rs {
		out[i] = r.Seq
	}
	return out
}

func equalSeqs(t *testing.T, got []Record, want ...uint64) {
	t.Helper()
	g := seqs(got)
	if fmt.Sprint(g) != fmt.Sprint(want) {
		t.Fatalf("seqs = %v, want %v", g, want)
	}
}

func TestRingBoundAndOrder(t *testing.T) {
	h := New(slog.DiscardHandler, Options{Size: 5})
	log := slog.New(h)
	for i := 1; i <= 8; i++ {
		log.Info(fmt.Sprintf("m%d", i))
	}
	got := h.Records(0, 0)
	equalSeqs(t, got, 4, 5, 6, 7, 8)
	for _, r := range got {
		if want := fmt.Sprintf("m%d", r.Seq); r.Message != want {
			t.Fatalf("record %d message %q, want %q", r.Seq, r.Message, want)
		}
		if r.Level != slog.LevelInfo || r.Time.IsZero() {
			t.Fatalf("record %d: level %v time %v", r.Seq, r.Level, r.Time)
		}
	}
}

func TestRecordsSinceLimit(t *testing.T) {
	h := New(slog.DiscardHandler, Options{Size: 5})
	if got := h.Records(0, 0); len(got) != 0 {
		t.Fatalf("empty ring: %v", seqs(got))
	}
	log := slog.New(h)
	for range 3 {
		log.Info("x")
	}
	equalSeqs(t, h.Records(0, 0), 1, 2, 3)
	equalSeqs(t, h.Records(2, 0), 2, 3)
	equalSeqs(t, h.Records(1, 2), 2, 3)
	equalSeqs(t, h.Records(0, 10), 1, 2, 3)
	equalSeqs(t, h.Records(4, 0))
	for range 4 {
		log.Info("x")
	}
	// Kept: 3..7. A since older than the oldest kept starts at the oldest.
	equalSeqs(t, h.Records(1, 0), 3, 4, 5, 6, 7)
	equalSeqs(t, h.Records(5, 0), 5, 6, 7)
	equalSeqs(t, h.Records(5, 1), 7)
	equalSeqs(t, h.Records(3, 2), 6, 7)
	equalSeqs(t, h.Records(8, 1))
}

func TestRecordsReturnsCopies(t *testing.T) {
	h := New(slog.DiscardHandler, Options{})
	slog.New(h).Info("x", "k", 1)
	h.Records(0, 0)[0].Attrs[0].Key = "changed"
	if k := h.Records(0, 0)[0].Attrs[0].Key; k != "k" {
		t.Fatalf("ring record mutated through Records: key %q", k)
	}
}

func TestLevelGateAtRunTime(t *testing.T) {
	inner := &countHandler{level: slog.LevelDebug}
	h := New(inner, Options{})
	log := slog.New(h)
	if h.Level() != slog.LevelInfo {
		t.Fatalf("default level %v", h.Level())
	}
	log.Debug("below")
	log.Info("at")
	h.SetLevel(slog.LevelWarn)
	log.Info("below now")
	log.Warn("warn")
	h.SetLevel(slog.LevelDebug)
	log.Debug("debug")

	got := h.Records(0, 0)
	equalSeqs(t, got, 1, 2, 3)
	for i, want := range []string{"at", "warn", "debug"} {
		if got[i].Message != want {
			t.Fatalf("record %d = %q, want %q", i, got[i].Message, want)
		}
	}
	if n := inner.n.Load(); n != 3 {
		t.Fatalf("inner handled %d records, want 3", n)
	}
	// Handle called directly (bypassing Logger's Enabled) honours the gate too.
	h.SetLevel(slog.LevelError)
	if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "direct", 0)); err != nil {
		t.Fatal(err)
	}
	if n := inner.n.Load(); n != 3 || len(h.Records(0, 0)) != 3 {
		t.Fatalf("a record below the gate reached inner (%d) or ring (%d)", n, len(h.Records(0, 0)))
	}
}

func TestSharedLevelVar(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelWarn)
	h := New(slog.DiscardHandler, Options{Level: lv})
	if h.Level() != slog.LevelWarn {
		t.Fatalf("level %v, want the passed LevelVar's", h.Level())
	}
	h.SetLevel(slog.LevelDebug)
	if lv.Level() != slog.LevelDebug {
		t.Fatal("SetLevel did not move the passed LevelVar")
	}
}

func TestInnerLevelGovernsForwarding(t *testing.T) {
	inner := &countHandler{level: slog.LevelWarn}
	h := New(inner, Options{})
	h.SetLevel(slog.LevelDebug)
	log := slog.New(h)
	if !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("Enabled must answer from the gate, not the inner level")
	}
	log.Debug("d")
	log.Warn("w")
	if n := len(h.Records(0, 0)); n != 2 {
		t.Fatalf("ring kept %d, want 2", n)
	}
	if n := inner.n.Load(); n != 1 {
		t.Fatalf("inner handled %d, want 1 (its own level is Warn)", n)
	}
}

func TestInnerErrorReturned(t *testing.T) {
	boom := errors.New("boom")
	h := New(&countHandler{err: boom}, Options{})
	err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "x", 0))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the inner handler's", err)
	}
	if len(h.Records(0, 0)) != 1 {
		t.Fatal("record not kept when the inner handler fails")
	}
}

type valuer struct{}

func (valuer) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", "v1"), slog.Int("n", 2))
}

func attrMap(r Record) map[string]string {
	m := map[string]string{}
	for _, a := range r.Attrs {
		m[a.Key] = a.Value.String()
	}
	return m
}

func TestWithAttrsWithGroupShareRingAndFlatten(t *testing.T) {
	var buf bytes.Buffer
	h := New(slog.NewTextHandler(&buf, nil), Options{})
	root := slog.New(h)
	a := root.With("node", 7)
	b := a.WithGroup("sess").With("id", 3).WithGroup("peer")

	root.Info("r0")
	a.Info("a0", "k", "v")
	b.Info("b0", "addr", "::1", slog.Group("g", slog.Int("x", 1)), slog.Group("", slog.Int("inl", 9)),
		slog.Group("empty"), "obj", valuer{})

	got := h.Records(0, 0)
	equalSeqs(t, got, 1, 2, 3)
	if m := attrMap(got[0]); len(m) != 0 {
		t.Fatalf("r0 attrs %v", m)
	}
	if m := attrMap(got[1]); fmt.Sprint(m) != "map[k:v node:7]" {
		t.Fatalf("a0 attrs %v", m)
	}
	want := map[string]string{
		"node":             "7",
		"sess.id":          "3",
		"sess.peer.addr":   "::1",
		"sess.peer.g.x":    "1",
		"sess.peer.inl":    "9",
		"sess.peer.obj.id": "v1",
		"sess.peer.obj.n":  "2",
	}
	if m := attrMap(got[2]); fmt.Sprint(m) != fmt.Sprint(want) {
		t.Fatalf("b0 attrs\n got %v\nwant %v", m, want)
	}
	if k := got[2].Attrs[0].Key; k != "node" {
		t.Fatalf("handler attrs first: got %q", k)
	}
	// The derived handlers also forward their attrs and groups to the inner.
	if out := buf.String(); !strings.Contains(out, "sess.id=3") || !strings.Contains(out, "sess.peer.addr=::1") ||
		!strings.Contains(out, "node=7") {
		t.Fatalf("inner output lacks the derived attrs:\n%s", out)
	}
	// Derived handlers share the gate.
	h.SetLevel(slog.LevelError)
	b.Warn("gated")
	if len(h.Records(0, 0)) != 3 {
		t.Fatal("derived handler bypassed the shared gate")
	}
	if h.WithGroup("") != slog.Handler(h) || h.WithAttrs(nil) != slog.Handler(h) {
		t.Fatal("empty WithGroup/WithAttrs must return the handler itself")
	}
}

func TestObserverDeliversInOrder(t *testing.T) {
	h := New(slog.DiscardHandler, Options{})
	var mu sync.Mutex
	var got []Record
	all := make(chan struct{})
	unsub := h.Observe(func(r Record) {
		mu.Lock()
		got = append(got, r)
		n := len(got)
		mu.Unlock()
		if n == 5 {
			close(all)
		}
	})
	defer unsub()
	log := slog.New(h).WithGroup("g")
	for i := range 5 {
		log.Info("x", "i", i)
	}
	select {
	case <-all:
	case <-time.After(5 * time.Second):
		t.Fatal("observer did not receive five records")
	}
	mu.Lock()
	defer mu.Unlock()
	equalSeqs(t, got, 1, 2, 3, 4, 5)
	if got[4].Attrs[0].Key != "g.i" {
		t.Fatalf("observer record attrs %v", got[4].Attrs)
	}
	if h.Dropped() != 0 {
		t.Fatalf("dropped %d", h.Dropped())
	}
}

func TestObserverNeverBlocksTheLogger(t *testing.T) {
	h := New(slog.DiscardHandler, Options{ObserverBuffer: 2})
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	var got []uint64
	unsub := h.Observe(func(r Record) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		mu.Lock()
		got = append(got, r.Seq)
		mu.Unlock()
	})
	log := slog.New(h)

	log.Info("first")
	select {
	case <-entered: // the observer holds record 1 and blocks
	case <-time.After(5 * time.Second):
		t.Fatal("observer never called")
	}
	done := make(chan struct{})
	go func() {
		for range 9 {
			log.Info("more")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a blocked observer blocked the logger")
	}
	// Records 2..10 offered to a queue of 2: 2..8 discarded, 9 and 10 queued.
	if d := h.Dropped(); d != 7 {
		t.Fatalf("Dropped = %d, want 7", d)
	}
	if n := len(h.Records(0, 0)); n != 10 {
		t.Fatalf("the ring lost records to a slow observer: %d", n)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n == 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	unsub()
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(got) != "[1 9 10]" {
		t.Fatalf("delivered %v, want [1 9 10]", got)
	}
}

func TestUnsubscribeIdempotentAndStops(t *testing.T) {
	h := New(slog.DiscardHandler, Options{})
	var n atomic.Int64
	unsub := h.Observe(func(Record) { n.Add(1) })
	unsub()
	unsub()
	slog.New(h).Info("after") // the observer is deregistered: nothing is queued to it
	if n.Load() != 0 {
		t.Fatal("observer called after unsubscribe")
	}
	h.c.mu.Lock()
	left := len(h.c.observers)
	h.c.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d observers still registered", left)
	}
	h.Observe(nil)() // a nil fn registers nothing; its unsubscribe is a no-op
}

func TestDefaults(t *testing.T) {
	h := New(slog.DiscardHandler, Options{Size: -1, ObserverBuffer: -1})
	if cap(h.c.ring) != DefaultSize || h.c.obsDepth != DefaultObserverBuffer {
		t.Fatalf("size %d depth %d", cap(h.c.ring), h.c.obsDepth)
	}
}

func TestConcurrent(t *testing.T) {
	h := New(slog.DiscardHandler, Options{Size: 64, ObserverBuffer: 4})
	h.SetLevel(slog.LevelDebug)
	var seen atomic.Int64
	unsub := h.Observe(func(Record) { seen.Add(1) })
	stop := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
				rs := h.Records(0, 10)
				for i := 1; i < len(rs); i++ {
					if rs[i].Seq != rs[i-1].Seq+1 {
						t.Errorf("non-contiguous page %v", seqs(rs))
						return
					}
				}
				_ = h.Dropped()
			}
		}
	}()
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
				u := h.Observe(func(Record) {})
				h.SetLevel(slog.LevelDebug)
				u()
			}
		}
	}()
	var writers sync.WaitGroup
	const goroutines, each = 8, 200
	for g := range goroutines {
		writers.Add(1)
		go func() {
			defer writers.Done()
			log := slog.New(h).With("g", g).WithGroup("w")
			for i := range each {
				log.Debug("x", "i", i)
			}
		}()
	}
	writers.Wait()
	close(stop)
	readers.Wait()
	unsub()
	rs := h.Records(0, 0)
	if len(rs) != 64 || rs[len(rs)-1].Seq != goroutines*each {
		t.Fatalf("kept %d, last seq %d", len(rs), rs[len(rs)-1].Seq)
	}
	if seen.Load() == 0 {
		t.Fatal("the observer saw nothing")
	}
}

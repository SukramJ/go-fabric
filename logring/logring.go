// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package logring keeps the newest log records of a process in memory so a
// host can show them, for example in a diagnostics page of its own UI.
//
// matter.js keeps no log records and matterjs-server sets a level only; the
// ring is ahead of both, register row BD-Matter-LogRing in
// notes/parity/by_design.md.
//
// [Handler] is a [slog.Handler] that wraps the host's own handler. A host
// builds its logger as
//
//	ring := logring.New(slog.NewTextHandler(os.Stderr, nil), logring.Options{})
//	logger := slog.New(ring)
//
// and hands logger to the module (node.Config.Logger). Every record that
// passes the ring's level gate is kept in a bounded ring of the newest
// [Options.Size] records and forwarded to the wrapped handler. The host's
// UI reads the ring through its own port: [Handler.Records] for a page of
// the past, [Handler.Observe] for what arrives from now on, and
// [Handler.SetLevel] to change the gate at run time.
//
// The level gate is a [slog.LevelVar]. [Handler.Enabled] answers from it
// alone, so the gate decides what the ring keeps. A record below the gate
// reaches neither the ring nor the wrapped handler. A record at or above it
// is forwarded only when the wrapped handler's own Enabled accepts it as
// well, so the wrapped handler's level still governs its own output: a
// host can keep its console at Info while the ring records Debug.
//
// Handlers derived through WithAttrs and WithGroup share the ring, the
// level gate, the observers and the sequence counter with the handler
// they come from. Their attributes and groups apply both to the records
// they keep (flattened, see [Record]) and to the wrapped handler (through
// its own WithAttrs and WithGroup).
//
// Redaction: the ring keeps what the module logs and nothing else, and
// applies no redaction of its own. The module does not log secrets — no
// passcodes, no keys — so the ring holds none unless the host's own code
// logs them through the same logger.
//
// All methods are safe for concurrent use.
package logring

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// DefaultSize is the ring's capacity when [Options.Size] is not positive.
const DefaultSize = 1000

// DefaultObserverBuffer is the depth of each observer's queue when
// [Options.ObserverBuffer] is not positive.
const DefaultObserverBuffer = 256

// Options configures a [Handler]. The zero value is usable.
type Options struct {
	// Size is the number of records the ring keeps; the oldest is
	// discarded when a new one arrives at capacity. DefaultSize when not
	// positive.
	Size int
	// Level is the gate. A nil Level is a fresh [slog.LevelVar], whose
	// zero value is [slog.LevelInfo]. A host may pass a LevelVar it
	// shares with other handlers.
	Level *slog.LevelVar
	// ObserverBuffer is the depth of each observer's queue (see
	// [Handler.Observe]). DefaultObserverBuffer when not positive.
	ObserverBuffer int
}

// Record is one kept log record.
//
// Attrs holds the record's attributes after the handler's own (from
// WithAttrs), each value resolved ([slog.Value.Resolve]) and every group
// flattened into its members: an attribute key inside group "a" is
// "a.key", inside "a" then "b" it is "a.b.key". A group with an empty key
// is inlined, and an empty attribute or an empty group is left out, as
// [slog.Handler] prescribes. Attrs is shared and must not be modified by
// an observer; [Handler.Records] returns copies.
type Record struct {
	// Seq is the record's sequence number: 1 for the first record of a
	// Handler, never repeated.
	Seq     uint64
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   []slog.Attr
}

// core is the state every handler derived from one New shares.
type core struct {
	level    *slog.LevelVar
	obsDepth int

	mu        sync.Mutex
	ring      []Record // capacity Size, used as a circular buffer
	head      int      // index of the oldest record when full
	next      uint64   // sequence number of the next record
	observers map[*observer]struct{}
	dropped   uint64
}

// Handler is the ring-keeping [slog.Handler]. Build it with [New].
type Handler struct {
	c      *core
	inner  slog.Handler
	attrs  []slog.Attr // already flattened, with the prefix they were added under
	prefix string      // the open groups, "a.b." or ""
}

// New wraps inner in a ring-keeping handler.
func New(inner slog.Handler, opts Options) *Handler {
	size := opts.Size
	if size <= 0 {
		size = DefaultSize
	}
	depth := opts.ObserverBuffer
	if depth <= 0 {
		depth = DefaultObserverBuffer
	}
	lv := opts.Level
	if lv == nil {
		lv = new(slog.LevelVar)
	}
	return &Handler{
		c: &core{
			level:     lv,
			obsDepth:  depth,
			ring:      make([]Record, 0, size),
			next:      1,
			observers: map[*observer]struct{}{},
		},
		inner: inner,
	}
}

// Level reports the gate's current level.
func (h *Handler) Level() slog.Level { return h.c.level.Level() }

// SetLevel moves the gate. It takes effect for the next record, for this
// handler and every handler derived from the same New.
func (h *Handler) SetLevel(l slog.Level) { h.c.level.Set(l) }

// Enabled reports whether level passes the gate, independent of the
// wrapped handler's own level.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.c.level.Level()
}

// Handle keeps r in the ring, queues it to every observer and forwards it
// to the wrapped handler when that handler is enabled for r's level. A
// record below the gate is ignored. The error is the wrapped handler's.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if !h.Enabled(ctx, r.Level) {
		return nil
	}
	attrs := make([]slog.Attr, len(h.attrs), len(h.attrs)+r.NumAttrs())
	copy(attrs, h.attrs)
	r.Attrs(func(a slog.Attr) bool {
		attrs = flatten(attrs, h.prefix, a)
		return true
	})
	h.c.append(Record{Time: r.Time, Level: r.Level, Message: r.Message, Attrs: attrs})
	if h.inner.Enabled(ctx, r.Level) {
		return h.inner.Handle(ctx, r)
	}
	return nil
}

// WithAttrs returns a handler sharing this one's ring whose records carry
// attrs as well.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	own := slices.Clone(h.attrs)
	for _, a := range attrs {
		own = flatten(own, h.prefix, a)
	}
	return &Handler{c: h.c, inner: h.inner.WithAttrs(attrs), attrs: own, prefix: h.prefix}
}

// WithGroup returns a handler sharing this one's ring whose later
// attributes are qualified by name. An empty name returns h.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &Handler{c: h.c, inner: h.inner.WithGroup(name), attrs: h.attrs, prefix: h.prefix + name + "."}
}

// flatten appends a to dst: resolved, with prefix, groups expanded.
func flatten(dst []slog.Attr, prefix string, a slog.Attr) []slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		p := prefix
		if a.Key != "" {
			p = prefix + a.Key + "."
		}
		for _, m := range a.Value.Group() {
			dst = flatten(dst, p, m)
		}
		return dst
	}
	if a.Equal(slog.Attr{}) {
		return dst
	}
	a.Key = prefix + a.Key
	return append(dst, a)
}

// append stamps rec with the next sequence number, keeps it and queues it
// to the observers, all under the lock so every observer sees sequence
// order.
func (c *core) append(rec Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rec.Seq = c.next
	c.next++
	if len(c.ring) < cap(c.ring) {
		c.ring = append(c.ring, rec)
	} else {
		c.ring[c.head] = rec
		c.head = (c.head + 1) % len(c.ring)
	}
	for o := range c.observers {
		c.dropped += o.offer(rec)
	}
}

// Records returns the kept records with a sequence number of at least
// since, oldest first; when limit is positive, only the newest limit of
// them. since 0 or 1 means every kept record. A caller that pages
// forward passes the last Seq it saw plus one.
func (h *Handler) Records(since uint64, limit int) []Record {
	c := h.c
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.ring)
	oldest := c.next - uint64(n)
	skip := 0
	if since > oldest {
		if since >= c.next {
			return nil
		}
		skip = int(since - oldest) //nolint:gosec // G115: oldest < since < next, so the difference is below len(c.ring)
	}
	count := n - skip
	if limit > 0 && limit < count {
		skip += count - limit
		count = limit
	}
	out := make([]Record, count)
	for i := range count {
		r := c.ring[(c.head+skip+i)%n]
		r.Attrs = slices.Clone(r.Attrs)
		out[i] = r
	}
	return out
}

// Dropped reports how many records observers have lost, summed over every
// observer this handler (and every handler derived from the same New)
// has had: a record discarded from an observer's full queue counts once
// for that observer.
func (h *Handler) Dropped() uint64 {
	h.c.mu.Lock()
	defer h.c.mu.Unlock()
	return h.c.dropped
}

// observer is one Observe registration: a bounded queue and the goroutine
// that serves it.
type observer struct {
	queue chan Record
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
}

// offer queues rec without blocking. When the queue is full it discards
// the oldest queued record first; it returns the number discarded. Only
// core.append calls it, under core.mu, so it is the queue's only sender.
func (o *observer) offer(rec Record) uint64 {
	var lost uint64
	for {
		select {
		case o.queue <- rec:
			return lost
		default:
		}
		select {
		case <-o.queue:
			lost++
		default: // the observer took one meanwhile; there is room now
		}
	}
}

func (o *observer) run(fn func(Record)) {
	defer close(o.done)
	for {
		select {
		case <-o.stop:
			return
		case rec := <-o.queue:
			select {
			case <-o.stop:
				return
			default:
			}
			fn(rec)
		}
	}
}

// Observe registers fn for every record kept from now on. The logging
// goroutine only queues the record — Handle never waits for fn. fn runs
// on a goroutine of its own, one per observer, called in sequence order.
// An observer that cannot keep up loses records: when its queue
// ([Options.ObserverBuffer] deep) is full, the oldest queued record is
// discarded and counted in [Handler.Dropped]; Record.Seq shows the gap.
//
// unsubscribe stops the observer: no queued record is delivered after it
// returns, and it waits until a call of fn in progress has returned, so fn
// must not call it. It is idempotent. A nil fn registers nothing.
func (h *Handler) Observe(fn func(Record)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	o := &observer{
		queue: make(chan Record, h.c.obsDepth),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	h.c.mu.Lock()
	h.c.observers[o] = struct{}{}
	h.c.mu.Unlock()
	go o.run(fn)
	return func() {
		o.once.Do(func() {
			h.c.mu.Lock()
			delete(h.c.observers, o)
			h.c.mu.Unlock()
			close(o.stop)
		})
		<-o.done
	}
}

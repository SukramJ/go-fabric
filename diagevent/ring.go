// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package diagevent keeps a bounded, in-memory trace of the Matter
// events that explain a failed pairing.
//
// The existing diagnostics report state: which sessions are open, what
// mDNS advertises, which endpoints exist. State answers "what is wrong
// now" and cannot answer "what happened thirty seconds ago", which is
// the question an operator has after a controller refused to pair and
// then went quiet. The daemon logged those moments and nothing else —
// reaching them meant having log access, knowing what to grep for, and
// still having the log.
//
// The trace is deliberately small and lossy. It is a diagnostic, not an
// audit trail: the alarm journal and the audit log are the durable
// records, and neither should be competed with by something that sits on
// the receive path.
package diagevent

import (
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// Kind buckets an event so a reader can filter without parsing prose.
type Kind string

// Kind values.
const (
	// KindPairing covers commissioning: window open and close, PASE
	// refusals, fabric added and removed.
	KindPairing Kind = "pairing"
	// KindSession covers secure sessions opening and closing: a
	// completed CASE handshake, and a peer's CloseSession.
	KindSession Kind = "session"
	// KindDiscovery covers mDNS advertisement changes, such as the
	// re-announce that follows a peer losing its last session.
	KindDiscovery Kind = "discovery"
)

// Severity separates the entries that explain a failure from the ones
// that merely record progress.
type Severity string

// Severity values.
const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Event is one recorded moment.
type Event struct {
	At       time.Time
	Kind     Kind
	Severity Severity
	// Message is one sentence an operator can act on. It is not a log
	// line: it says what happened, not which function noticed.
	Message string
	// Detail carries the identifiers that make the message specific —
	// fabric index, peer node id, the refusal reason.
	Detail map[string]string
}

// Ring is a fixed-capacity trace. The oldest entry is dropped to make
// room, so a burst of noise cannot push out the entries that follow it.
//
// The zero value is not usable; a nil *Ring is, and does nothing. That
// asymmetry is deliberate: the recording points sit on the Matter
// receive path, taken for every packet, and a diagnostic that can panic
// there is worse than no diagnostic.
type Ring struct {
	// mu guards clock, events, full and next.
	mu     sync.RWMutex
	events []Event
	next   int
	full   bool
	clock  clock.Clock // stamps an event recorded without At; nil is clock.System

	// obsMu guards observers and nextObserver; it is never held while an
	// observer runs.
	obsMu        sync.Mutex
	observers    map[uint64]func(Event)
	nextObserver uint64
}

// Observe registers fn to receive every event [Ring.Record] records from
// now on, stamped and defaulted as the ring stores it. fn runs inline on
// the recording goroutine — the receive path — after the ring's lock is
// released, so it may call [Ring.Snapshot] but must not block. It returns
// the function that removes fn. Safe on a nil receiver, which never calls
// fn.
func (r *Ring) Observe(fn func(Event)) (unsubscribe func()) {
	if r == nil || fn == nil {
		return func() {}
	}
	r.obsMu.Lock()
	defer r.obsMu.Unlock()
	if r.observers == nil {
		r.observers = map[uint64]func(Event){}
	}
	id := r.nextObserver
	r.nextObserver++
	r.observers[id] = fn
	return func() {
		r.obsMu.Lock()
		delete(r.observers, id)
		r.obsMu.Unlock()
	}
}

// SetClock sets the clock that stamps an event recorded without At; nil
// is [clock.System]. Safe on a nil receiver.
func (r *Ring) SetClock(c clock.Clock) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clock = c
}

// NewRing returns a ring holding at most capacity events.
//
// A capacity below one is raised to a small usable default rather than
// producing a ring that silently discards everything: the value reaches
// here from configuration, and an unset field must not switch the
// diagnostic off without saying so.
func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = defaultCapacity
	}
	return &Ring{events: make([]Event, capacity)}
}

// defaultCapacity is what an unset or nonsensical capacity becomes —
// enough to cover a pairing attempt and the minute around it.
const defaultCapacity = 200

// Record appends an event, dropping the oldest when full.
//
// Safe on a nil receiver and safe to call concurrently, because both are
// properties the call sites need: they are optional wiring on a
// concurrent receive path.
func (r *Ring) Record(e Event) {
	if r == nil {
		return
	}
	if e.At.IsZero() {
		r.mu.RLock()
		c := r.clock
		r.mu.RUnlock()
		e.At = clock.Or(c).Now()
	}
	if e.Severity == "" {
		e.Severity = SeverityInfo
	}
	r.mu.Lock()
	r.events[r.next] = e
	r.next = (r.next + 1) % len(r.events)
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
	// The observers run outside both locks, in registration order: one
	// that records or reads the ring does not deadlock it.
	r.obsMu.Lock()
	if len(r.observers) == 0 {
		r.obsMu.Unlock()
		return
	}
	ids := slices.Sorted(maps.Keys(r.observers))
	fns := make([]func(Event), 0, len(ids))
	for _, id := range ids {
		fns = append(fns, r.observers[id])
	}
	r.obsMu.Unlock()
	for _, fn := range fns {
		fn(cloneEvent(e))
	}
}

// cloneEvent copies e's Detail, so an observer that keeps or changes the
// map does not change the ring's entry.
func cloneEvent(e Event) Event {
	e.Detail = maps.Clone(e.Detail)
	return e
}

// Snapshot returns the recorded events, newest first.
//
// The result is a copy: the caller serialises it while the receive path
// keeps recording.
func (r *Ring) Snapshot() []Event {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := r.next
	if r.full {
		count = len(r.events)
	}
	out := make([]Event, 0, count)
	for i := range count {
		idx := (r.next - 1 - i + len(r.events)*2) % len(r.events)
		out = append(out, r.events[idx])
	}
	return out
}

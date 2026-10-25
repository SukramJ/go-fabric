// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package icd

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// AwaitResult is what ended an [PeerWakefulness.AwaitAwake].
type AwaitResult uint8

// The three outcomes (IcdPeerWakefulness.ts:56).
const (
	// AwaitAwakeResult: the peer is awake.
	AwaitAwakeResult AwaitResult = iota
	// AwaitTimeout: the timeout passed first.
	AwaitTimeout
	// AwaitAborted: the context ended first.
	AwaitAborted
)

// PeerWakefulness is the controller's view of one LIT ICD peer for the
// peer's lifetime on a fabric: whether it is awake — in its estimated
// active mode, so a message reaches it now — and whether it is available —
// not offline. Any message from the peer wakes it as a Check-In does, for
// its ActiveModeThreshold. While the controller holds a subscription to
// the peer, the subscription's liveness decides and the peer counts as
// available; otherwise the peer's next Check-In is due after its active
// mode plus IdleModeDuration, and once that passes the Check-In is missed.
// Awake implies available. A LIT peer without observed activity starts
// asleep and unavailable; a peer that needs no awaiting (RequiresAwait
// false: not LIT) is always awake and available, with no timers.
//
// The two deadlines come from a [PeerSchedule]; the timers run on the
// injected clock. Every callback runs outside the wakefulness's lock, on
// the goroutine of the change (a timer's for an expiry). Safe for
// concurrent use.
//
// Mirrors matter.js packages/protocol/src/icd/IcdPeerWakefulness.ts:35-272;
// its observables are the On… registrations and the signal channels.
type PeerWakefulness struct {
	clock clock.Clock

	// mu guards every field below; deliver runs the callbacks after it is
	// released, in the order the changes happened.
	mu            sync.Mutex
	awake         bool
	available     bool
	awakeEdge     chan struct{} // closed at the next awake emit
	modeEdge      chan struct{} // closed at the next operating-mode emit
	schedule      *PeerSchedule
	subscriptions map[uint64]struct{}
	nextHold      uint64
	requiresAwait bool
	suspended     bool
	closed        bool
	awakeTimer    clock.Timer
	availTimer    clock.Timer
	gen           uint64
	observers     map[uint64]wakeObserver
	nextObserver  uint64
}

type wakeObserver struct {
	awake, available, mode func(bool)
	missed                 func()
}

// event is one emission, delivered after the lock is released.
type event struct {
	kind  uint8 // 0 awake, 1 available, 2 mode, 3 missed
	value bool
}

// NewPeerWakefulness returns the wakefulness of a peer that needs no
// awaiting: awake and available (IcdPeerWakefulness.ts:36-48). nil clock
// is the system clock.
func NewPeerWakefulness(c clock.Clock) *PeerWakefulness {
	return &PeerWakefulness{
		clock: clock.Or(c), awake: true, available: true,
		awakeEdge: make(chan struct{}), modeEdge: make(chan struct{}),
		schedule: NewPeerSchedule(), subscriptions: map[uint64]struct{}{},
		observers: map[uint64]wakeObserver{},
	}
}

// Awake reports send-now: the peer is in its estimated active mode
// (IcdPeerWakefulness.ts:51-53).
func (w *PeerWakefulness) Awake() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.awake
}

// AwakeSignal returns the awake state and a channel closed at its next
// emission — a change, or the forced emission of a peer that stops being
// awaited.
func (w *PeerWakefulness) AwakeSignal() (awake bool, next <-chan struct{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.awake, w.awakeEdge
}

// Available reports not-offline (IcdPeerWakefulness.ts:69-71).
func (w *PeerWakefulness) Available() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.available
}

// ModeSignal returns a channel closed at the next flip of RequiresAwait —
// a SIT⇄LIT change of the peer's operating mode, on which a sustained
// subscription recreates itself. Suspend and Resume do not close it
// (IcdPeerWakefulness.ts:73-80).
func (w *PeerWakefulness) ModeSignal() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.modeEdge
}

// AwaitAwake waits until the peer is awake, at most timeout on the
// wakefulness's clock and until ctx ends, and reports what ended the wait
// (IcdPeerWakefulness.ts:56-66).
func (w *PeerWakefulness) AwaitAwake(ctx context.Context, timeout time.Duration) AwaitResult {
	awake, next := w.AwakeSignal()
	if awake {
		return AwaitAwakeResult
	}
	t := w.clock.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-next:
		return AwaitAwakeResult
	case <-t.C():
		return AwaitTimeout
	case <-ctx.Done():
		return AwaitAborted
	}
}

// Observer registrations. Each returns its unsubscribe function.

// OnAwakeChanged registers fn for every awake emission.
func (w *PeerWakefulness) OnAwakeChanged(fn func(awake bool)) (unsubscribe func()) {
	return w.observe(wakeObserver{awake: fn})
}

// OnAvailableChanged registers fn for every available emission.
func (w *PeerWakefulness) OnAvailableChanged(fn func(available bool)) (unsubscribe func()) {
	return w.observe(wakeObserver{available: fn})
}

// OnOperatingModeChanged registers fn for every RequiresAwait flip.
func (w *PeerWakefulness) OnOperatingModeChanged(fn func(requiresAwait bool)) (unsubscribe func()) {
	return w.observe(wakeObserver{mode: fn})
}

// OnCheckInMissed registers fn for every missed Check-In of a peer the
// controller awaits — never while subscribed, on a mode flip, on resume or
// on teardown (IcdPeerWakefulness.ts:82-88).
func (w *PeerWakefulness) OnCheckInMissed(fn func()) (unsubscribe func()) {
	return w.observe(wakeObserver{missed: fn})
}

func (w *PeerWakefulness) observe(o wakeObserver) func() {
	w.mu.Lock()
	id := w.nextObserver
	w.nextObserver++
	w.observers[id] = o
	w.mu.Unlock()
	return func() {
		w.mu.Lock()
		delete(w.observers, id)
		w.mu.Unlock()
	}
}

// NextCheckInDue is the deadline of the peer's next Check-In; ok is false
// when the peer needs no awaiting, while a subscription is held, before
// any activity and once the deadline passed (IcdPeerWakefulness.ts:95-104).
func (w *PeerWakefulness) NextCheckInDue() (time.Time, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.nextCheckInDueLocked()
}

func (w *PeerWakefulness) nextCheckInDueLocked() (time.Time, bool) {
	if !w.awaitingLocked() || len(w.subscriptions) > 0 {
		return time.Time{}, false
	}
	due, ok := w.schedule.NextCheckInDue()
	if !ok || !due.After(w.clock.Now()) {
		return time.Time{}, false
	}
	return due, true
}

// NextCheckInWithin is how long a controller waits by default for the peer
// to wake: until NextCheckInDue, but at least the longest the peer may
// idle (IcdPeerWakefulness.ts:110-117).
func (w *PeerWakefulness) NextCheckInWithin() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	idle := w.schedule.IdleWindow()
	due, ok := w.nextCheckInDueLocked()
	if !ok {
		return idle
	}
	return max(due.Sub(w.clock.Now()), idle)
}

// RequiresAwait reports a peer the controller awaits: a LIT peer
// (IcdPeerWakefulness.ts:119-121).
func (w *PeerWakefulness) RequiresAwait() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.requiresAwait
}

// SetRequiresAwait flips the peer between awaited (LIT) and not. Awaited,
// the timers re-arm from the observed activity without counting a passed
// deadline as missed; not awaited, the timers stop and awake and available
// are emitted true, releasing a consumer parked on either edge. Every flip
// emits the operating-mode change. Mirrors IcdPeerWakefulness.ts:123-138.
func (w *PeerWakefulness) SetRequiresAwait(v bool) {
	w.mu.Lock()
	if v == w.requiresAwait {
		w.mu.Unlock()
		return
	}
	w.requiresAwait = v
	var evs []event
	if v {
		evs = w.refreshLocked(false)
	} else {
		w.cancelTimersLocked()
		evs = append(evs, w.emitAwakeLocked(true), w.emitAvailableLocked(true))
	}
	close(w.modeEdge)
	w.modeEdge = make(chan struct{})
	evs = append(evs, event{kind: 2, value: v})
	w.deliver(evs)
}

// SetTimings replaces the peer's timings (IcdPeerWakefulness.ts:141-144).
func (w *PeerWakefulness) SetTimings(t Timings) {
	w.mu.Lock()
	w.schedule.SetTimings(t)
	w.deliver(w.refreshLocked(true))
}

// NoteCheckIn records a Check-In with the threshold it carried, nil for
// the configured one (IcdPeerWakefulness.ts:147-150).
func (w *PeerWakefulness) NoteCheckIn(threshold *time.Duration) {
	w.mu.Lock()
	w.schedule.NoteCheckIn(w.clock.Now(), threshold)
	w.deliver(w.refreshLocked(true))
}

// NoteActive records a message from the peer or its answer
// (IcdPeerWakefulness.ts:153-156).
func (w *PeerWakefulness) NoteActive() {
	w.mu.Lock()
	w.schedule.NoteActive(w.clock.Now())
	w.deliver(w.refreshLocked(true))
}

// NoteStayActive records a StayActive promise (IcdPeerWakefulness.ts:159-162).
func (w *PeerWakefulness) NoteStayActive(promised time.Duration) {
	w.mu.Lock()
	w.schedule.NoteStayActive(w.clock.Now(), promised)
	w.deliver(w.refreshLocked(true))
}

// HoldSubscription registers a subscription the controller holds to the
// peer: while any is held the peer reports instead of checking in, so the
// subscription's liveness decides availability. The release is idempotent
// (IcdPeerWakefulness.ts:168-179).
func (w *PeerWakefulness) HoldSubscription() (release func()) {
	w.mu.Lock()
	h := w.nextHold
	w.nextHold++
	w.subscriptions[h] = struct{}{}
	w.deliver(w.refreshLocked(true))
	return func() {
		w.mu.Lock()
		if _, ok := w.subscriptions[h]; !ok {
			w.mu.Unlock()
			return
		}
		delete(w.subscriptions, h)
		w.deliver(w.refreshLocked(true))
	}
}

// Suspend stops awaiting the peer, while it is not registered: the timers
// stop and awake and available are emitted true, releasing parked
// consumers; the observed activity is kept for Resume
// (IcdPeerWakefulness.ts:185-191).
func (w *PeerWakefulness) Suspend() {
	w.mu.Lock()
	w.suspendLocked()
	evs := []event{w.emitAwakeLocked(true), w.emitAvailableLocked(true)}
	w.deliver(evs)
}

func (w *PeerWakefulness) suspendLocked() {
	w.suspended = true
	w.cancelTimersLocked()
}

// Resume awaits the peer again after Suspend; a deadline that passed
// meanwhile makes it unavailable without a missed Check-In
// (IcdPeerWakefulness.ts:194-200).
func (w *PeerWakefulness) Resume() {
	w.mu.Lock()
	if !w.suspended || w.closed {
		w.mu.Unlock()
		return
	}
	w.suspended = false
	w.deliver(w.refreshLocked(false))
}

// Close suspends for good; nothing arms a timer again
// (IcdPeerWakefulness.ts:203-207).
func (w *PeerWakefulness) Close() {
	w.mu.Lock()
	w.closed = true
	clear(w.subscriptions)
	w.suspendLocked()
	evs := []event{w.emitAwakeLocked(true), w.emitAvailableLocked(true)}
	w.deliver(evs)
}

func (w *PeerWakefulness) awaitingLocked() bool { return w.requiresAwait && !w.suspended }

// refreshLocked re-arms the timers from the schedule
// (IcdPeerWakefulness.ts:221-244). reportMissed is false when awaiting
// starts (a mode flip, a resume): a deadline that passed meanwhile makes
// the peer unavailable without a missed Check-In.
func (w *PeerWakefulness) refreshLocked(reportMissed bool) []event {
	if !w.awaitingLocked() {
		return nil
	}
	now := w.clock.Now()
	activeUntil, active := w.schedule.ActiveUntil()
	due, hasDue := w.schedule.NextCheckInDue()
	if len(w.subscriptions) > 0 {
		hasDue = false
	}
	w.gen++
	gen := w.gen
	w.awakeTimer = w.rearmLocked(w.awakeTimer, activeUntil, active, now, func() { w.expired(gen, false) })
	w.availTimer = w.rearmLocked(w.availTimer, due, hasDue, now, func() { w.expired(gen, true) })
	var evs []event
	evs = appendEvent(evs, w.setAwakeLocked(active && activeUntil.After(now)))
	available := len(w.subscriptions) > 0 || (hasDue && due.After(now))
	missed := reportMissed && !available && hasDue && w.available
	evs = appendEvent(evs, w.setAvailableLocked(available))
	if missed {
		evs = append(evs, event{kind: 3})
	}
	return evs
}

func appendEvent(evs []event, e *event) []event {
	if e != nil {
		evs = append(evs, *e)
	}
	return evs
}

// expired is a timer's fire: awake ends, or the Check-In is missed.
func (w *PeerWakefulness) expired(gen uint64, availability bool) {
	w.mu.Lock()
	if gen != w.gen || !w.awaitingLocked() {
		w.mu.Unlock()
		return
	}
	var evs []event
	if availability {
		w.availTimer = nil
		evs = appendEvent(evs, w.setAvailableLocked(false))
		evs = append(evs, event{kind: 3})
	} else {
		w.awakeTimer = nil
		evs = appendEvent(evs, w.setAwakeLocked(false))
	}
	w.deliver(evs)
}

func (w *PeerWakefulness) rearmLocked(t clock.Timer, at time.Time, ok bool, now time.Time, fire func()) clock.Timer {
	if t != nil {
		t.Stop()
	}
	if !ok || !at.After(now) {
		return nil
	}
	return w.clock.AfterFunc(at.Sub(now), fire)
}

func (w *PeerWakefulness) cancelTimersLocked() {
	w.gen++
	if w.awakeTimer != nil {
		w.awakeTimer.Stop()
		w.awakeTimer = nil
	}
	if w.availTimer != nil {
		w.availTimer.Stop()
		w.availTimer = nil
	}
}

// setAwakeLocked emits only a change (IcdPeerWakefulness.ts:261-265).
func (w *PeerWakefulness) setAwakeLocked(v bool) *event {
	if w.awake == v {
		return nil
	}
	e := w.emitAwakeLocked(v)
	return &e
}

func (w *PeerWakefulness) setAvailableLocked(v bool) *event {
	if w.available == v {
		return nil
	}
	e := w.emitAvailableLocked(v)
	return &e
}

// emitAwakeLocked emits even an unchanged value — the force-emit that
// releases a parked consumer.
func (w *PeerWakefulness) emitAwakeLocked(v bool) event {
	w.awake = v
	close(w.awakeEdge)
	w.awakeEdge = make(chan struct{})
	return event{kind: 0, value: v}
}

func (w *PeerWakefulness) emitAvailableLocked(v bool) event {
	w.available = v
	return event{kind: 1, value: v}
}

// deliver releases the lock the caller holds and runs the callbacks of evs.
func (w *PeerWakefulness) deliver(evs []event) {
	if len(evs) == 0 {
		w.mu.Unlock()
		return
	}
	ids := slices.Sorted(maps.Keys(w.observers))
	obs := make([]wakeObserver, len(ids))
	for i, id := range ids {
		obs[i] = w.observers[id]
	}
	w.mu.Unlock()
	for _, e := range evs {
		for _, o := range obs {
			switch {
			case e.kind == 0 && o.awake != nil:
				o.awake(e.value)
			case e.kind == 1 && o.available != nil:
				o.available(e.value)
			case e.kind == 2 && o.mode != nil:
				o.mode(e.value)
			case e.kind == 3 && o.missed != nil:
				o.missed()
			}
		}
	}
}

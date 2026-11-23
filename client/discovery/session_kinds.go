// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	fabricclock "thingfabric.dev/go-fabric/clock"
)

// FindInstance finds one device and returns it — matter.js InstanceDiscovery
// (packages/node/src/behavior/system/controller/discovery/InstanceDiscovery.ts:18-46
// at db021da0): the first device found is the result and stops the
// search (:25-32); once found, a later interruption is ignored (:37-39);
// none found is [ErrDeviceNotDiscovered] (:41-45).
func FindInstance(ctx context.Context, transports []Transport, opts SessionOptions) *Session[Candidate] {
	return start[Candidate](ctx, &instanceKind{}, transports, opts, describe(opts.Identifier))
}

type instanceKind struct {
	mu     sync.Mutex
	result *Candidate
}

func (k *instanceKind) onDiscovered(s *Session[Candidate], c Candidate) {
	k.mu.Lock()
	if k.result != nil {
		k.mu.Unlock()
		return
	}
	k.result = &c
	k.mu.Unlock()
	s.Stop()
}

func (k *instanceKind) onInterrupt(*Session[Candidate], error) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.result == nil
}

func (k *instanceKind) onComplete(s *Session[Candidate]) (Candidate, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.result == nil {
		return Candidate{}, fmt.Errorf("%w: %s failed: node not found", ErrDeviceNotDiscovered, s)
	}
	return *k.result, nil
}

func (k *instanceKind) onStop(*Session[Candidate]) {}

// FindAll finds every device within opts.Timeout, or until stopped or
// cancelled — matter.js ContinuousDiscovery (ContinuousDiscovery.ts:18-44):
// found, when set, is called as each device is found (the discovered
// observable, :30-32); the result lists them when a timeout bounds the
// search and is empty otherwise (:34-40).
func FindAll(ctx context.Context, transports []Transport, opts SessionOptions, found func(Candidate)) *Session[[]Candidate] {
	return start[[]Candidate](ctx, &continuousKind{bounded: opts.Timeout > 0, found: found}, transports, opts, describe(opts.Identifier))
}

type continuousKind struct {
	bounded bool
	found   func(Candidate)
	mu      sync.Mutex
	result  []Candidate
}

func (k *continuousKind) onDiscovered(_ *Session[[]Candidate], c Candidate) {
	if k.bounded {
		k.mu.Lock()
		k.result = append(k.result, c)
		k.mu.Unlock()
	}
	if k.found != nil {
		k.found(c)
	}
}

func (k *continuousKind) onInterrupt(*Session[[]Candidate], error) bool { return true }

func (k *continuousKind) onComplete(*Session[[]Candidate]) ([]Candidate, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.result, nil
}

func (k *continuousKind) onStop(*Session[[]Candidate]) {}

// CrossDeviceStagger is the delay between the race's attempts on
// different devices — matter.js CROSS_DEVICE_STAGGER_DELAY
// (ParallelPaseDiscovery.ts:30 at b25ed960): the first starts at once,
// each next one a slot after the one before while that one still runs.
// matter.js counts the slot from every attempt found before, ended or not
// (ParallelPaseDiscovery.ts:102-103 at b25ed960); here an attempt that has
// ended holds no slot, and with none running the next starts at once
// (review II, R2-P3-ble-01, R2-P3-ctl-lan-06). A start waits at most
// raceStaggerCeiling, and at most maxRaceAttempts run at once.
const CrossDeviceStagger = time.Second

// maxRaceAttempts is how many of a race's attempts run at the same time.
// A candidate whose ceiling has come while that many run takes the place
// of the oldest attempt that has had no answer from its device yet
// ([AttemptAnswered]), which ends; an answered attempt is never ended for
// a candidate found later, and while every place holds one the candidate
// waits for an attempt to end. The count is the module's bound on the
// handshakes of honest peers awaiting an acknowledgement at one moment:
// the device side's unsecuredEntryLimit (bridge/outbound_reliable.go:301)
// and the controller's inboundUnsecuredAckWaitLimit
// (client/exchange/manager.go:958). matter.js starts every candidate's
// attempt after its stagger and bounds neither the attempts running nor
// the stagger (ParallelPaseDiscovery.ts:102-103, :110-116 at db021da0;
// review II, owner decision 55, R2-P3-ctl-lan-06, R2-P3-ble-01).
const maxRaceAttempts = 16

// maxRaceWaiting is how many of a race's candidates wait, found and not
// yet released, beside those an attempt is already ending for. Past it the
// candidate found longest ago is treated as at its ceiling at once: it
// starts in a free place, or ends the oldest unanswered attempt and takes
// its place, so a device found behind any number of candidates still
// starts within raceStaggerCeiling while a place holds an unanswered
// attempt not already ending for an earlier candidate; a sender announcing
// faster than ended attempts return can still have it dropped. Only when
// no place can be given — every place holds an answered attempt or one
// already ending for an earlier candidate — does it leave the queue
// unattempted. Each waiting candidate holds an entry
// and a goroutine until the race is decided, so a sender announcing
// ever-new matching instances would otherwise grow both for the race's
// length; the queue holds at most maxRaceWaiting + maxRaceAttempts. This
// module's own bound (review II, R2-C2-bound-2153, R3-bound-3501, owner
// decision 552), at the number of places.
const maxRaceWaiting = maxRaceAttempts

// raceStaggerCeiling is the longest a candidate's start waits after it
// was found, however many candidates stand before it: the time the
// stagger takes to fill every place.
const raceStaggerCeiling = maxRaceAttempts * CrossDeviceStagger

// The default timeouts of the two races: CommissioningDiscovery's 3
// minutes, the spec's minimum commissioning window
// (CommissioningDiscovery.ts:40-45), and PaseDiscovery's 60 seconds
// (PaseDiscovery.ts:32-34).
const (
	DefaultCommissioningTimeout = 3 * time.Minute
	DefaultPASETimeout          = 60 * time.Second
)

// Attempt is one attempt of a race on a found device: it establishes PASE
// and calls win once PASE is up. win reports whether the attempt won —
// false when another won first or the discovery ended or was interrupted,
// and the attempt then gives its session up. The winner's attempt runs to
// its end (a commissioning, say) and its result is the race's; ctx ends
// for every attempt but the winner's once the race is decided, and for an
// attempt the race ends to give its place to a later candidate — which it
// does only while the attempt has not called [AttemptAnswered] with ctx.
type Attempt[W any] func(ctx context.Context, c Candidate, win func() bool) (W, error)

// attemptKey is the context key of a race attempt's [attemptMark].
type attemptKey struct{}

// attemptMark is what [AttemptAnswered] finds in an attempt's context.
type attemptMark struct{ answered func() }

// AttemptAnswered tells the race whose attempt runs with ctx (or a
// context derived from it) that the attempt's device has answered — for a
// PASE attempt, that its PBKDFParamResponse has arrived. From then the race
// never ends the attempt to give its place to a candidate found later.
// With any other context it does nothing.
func AttemptAnswered(ctx context.Context) {
	if m, ok := ctx.Value(attemptKey{}).(*attemptMark); ok {
		m.answered()
	}
}

// Race runs attempt on every device found, in parallel and in the order
// found, each next one a [CrossDeviceStagger] after the one before while
// an attempt runs and at once when none does, and the first to win PASE
// wins the race. A start waits at most 16 s after its device was found
// however many stand before it; at most 16 attempts run at once, and a
// candidate whose wait is up while they do takes the place of the oldest
// attempt that has not called [AttemptAnswered] — matter.js has neither
// bound (docs/guides/limits.md). Otherwise it is
// matter.js ParallelPaseDiscovery (ParallelPaseDiscovery.ts:45-217 at
// b25ed960):
//
//   - the race is decided on a win, an interruption before one, or the
//     end of the search without one; from then no attempt starts and win
//     refuses every attempt (:81-116, :157-174);
//   - the winner stops the search, the others' contexts end (:88-100);
//   - the outcome is the winner's result or the winner's own error; with
//     no winner it is [ErrDeviceNotDiscovered] with no attempt,
//     an [*AggregateError] of the attempts' own errors (a loser's
//     cancellation is none), else [ErrDiscoveryIncomplete] (:176-215).
//
// It reports only after every attempt has finished (:187-198).
func Race[W any](ctx context.Context, transports []Transport, opts SessionOptions, attempt Attempt[W]) *Session[W] {
	return startRace(ctx, transports, opts, attempt, nil, "No attempt succeeded")
}

// CommissioningRace is the race of a commissioning — matter.js
// CommissioningDiscovery (CommissioningDiscovery.ts:23-91): a device
// whose advertisement contradicts identity (the payload's vendor and
// product) is passed over before an attempt starts (:61-90), and the
// timeout is [DefaultCommissioningTimeout] unless opts sets one.
func CommissioningRace[W any](ctx context.Context, transports []Transport, opts SessionOptions, identity Identity, attempt Attempt[W]) *Session[W] {
	if opts.Timeout == 0 {
		opts.Timeout = DefaultCommissioningTimeout
	}
	accept := func(s *Session[W], c Candidate) bool {
		m := MismatchOf(identity, c.Device.AdvertisedIdentity())
		if m == nil {
			return true
		}
		s.log.Info("matter.controller.discovery.passing_over", slog.String("device", c.Device.InstanceID), slog.String("reason", m.Facet),
			slog.Int("offered", int(m.Advertised)), slog.Int("expected", int(m.Payload)))
		return false
	}
	return startRace(ctx, transports, opts, attempt, accept, "No device could be commissioned")
}

// PASERace is the race of a PASE session alone — matter.js PaseDiscovery
// (PaseDiscovery.ts:28-71): the timeout is [DefaultPASETimeout] unless
// opts sets one.
func PASERace[W any](ctx context.Context, transports []Transport, opts SessionOptions, attempt Attempt[W]) *Session[W] {
	if opts.Timeout == 0 {
		opts.Timeout = DefaultPASETimeout
	}
	return startRace(ctx, transports, opts, attempt, nil, "No PASE session could be established")
}

func startRace[W any](ctx context.Context, transports []Transport, opts SessionOptions, attempt Attempt[W], accept func(*Session[W], Candidate) bool, failure string) *Session[W] {
	k := &raceKind[W]{attempt: attempt, accept: accept, failure: failure, decided: make(chan struct{})}
	return start[W](ctx, k, transports, opts, describe(opts.Identifier))
}

type raceKind[W any] struct {
	attempt Attempt[W]
	accept  func(*Session[W], Candidate) bool
	failure string

	mu           sync.Mutex
	isDecided    bool
	decided      chan struct{}
	paseWon      bool
	winnerIndex  int
	winnerDone   chan struct{}
	winner       W
	winnerErr    error
	attemptCount int
	startedCount int
	attemptErrs  []error
	pending      sync.WaitGroup

	// The stagger and the places: waiting holds the attempts found and not
	// yet released, in the order found; active those released and not yet
	// ended, in the order released (at most maxRaceAttempts); lastRelease
	// is when the latest was released, and staggerTimer the wait for the
	// next release.
	waiting      []*raceAttempt
	forced       int // the first forced of waiting are treated as at their ceiling
	active       []*raceAttempt
	lastRelease  time.Time
	staggerTimer fabricclock.Timer
}

// raceAttempt is one candidate's attempt as the race schedules it; the
// fields after release are set under the race's mutex.
type raceAttempt struct {
	index   int
	device  string
	foundAt time.Time
	release chan struct{}
	dropped chan struct{} // closed when it left the queue unreleased (maxRaceWaiting)

	ctx      context.Context
	cancel   context.CancelFunc
	answered bool // AttemptAnswered was called
	replaced bool // ended for a later candidate; it holds its place until it returns
}

// decideLocked closes the race: no attempt starts from now, win refuses,
// and every attempt's context but the winner's ends. The caller holds
// k.mu.
func (k *raceKind[W]) decideLocked() {
	if k.isDecided {
		return
	}
	k.isDecided = true
	close(k.decided)
	k.waiting = nil
	k.forced = 0
	if k.staggerTimer != nil {
		k.staggerTimer.Stop()
		k.staggerTimer = nil
	}
	for _, a := range k.active {
		if !k.paseWon || a.index != k.winnerIndex {
			a.cancel()
		}
	}
}

func (k *raceKind[W]) onDiscovered(s *Session[W], c Candidate) {
	k.mu.Lock()
	if k.paseWon {
		k.mu.Unlock()
		return
	}
	k.mu.Unlock()
	if k.accept != nil && !k.accept(s, c) {
		return
	}
	k.mu.Lock()
	a := &raceAttempt{index: k.attemptCount, device: c.Device.InstanceID, foundAt: s.clock.Now(), release: make(chan struct{}), dropped: make(chan struct{})}
	k.attemptCount++
	if !k.isDecided {
		k.waiting = append(k.waiting, a)
		k.releaseLocked(s)
		k.overflowLocked(s, a)
	}
	k.mu.Unlock()
	k.pending.Go(func() { k.run(s, c, a) })
}

// overflowLocked keeps the waiting candidates bounded after a has been
// found: past maxRaceWaiting beside those an attempt is already ending
// for, the one found longest ago of the rest is treated as at its ceiling
// at once when a place can be given to it — a free one, or that of an
// unanswered attempt not yet ending — and leaves the queue unattempted
// only when none can. The caller holds k.mu.
func (k *raceKind[W]) overflowLocked(s *Session[W], a *raceAttempt) {
	covered := min(k.replacingLocked(), k.dueLocked(s.clock.Now()))
	if k.isDecided || len(k.waiting)-covered <= maxRaceWaiting {
		return
	}
	if len(k.active) < maxRaceAttempts || k.spareLocked() != nil {
		k.forced = max(k.forced, covered+1)
		k.releaseLocked(s)
		return
	}
	oldest := k.waiting[covered]
	k.waiting = slices.Delete(k.waiting, covered, covered+1)
	if covered < k.forced {
		k.forced--
	}
	close(oldest.dropped)
	s.log.Info("matter.controller.discovery.candidate_dropped", slog.String("discovery", s.description),
		slog.String("device", oldest.device), slog.String("candidate", a.device), slog.Int("max", maxRaceWaiting))
}

// dueLocked is how many of the first waiting candidates are due a place:
// the forced ones and those whose ceiling has come. The ceilings follow
// the order found, so they are a prefix. The caller holds k.mu.
func (k *raceKind[W]) dueLocked(now time.Time) int {
	n := k.forced
	for n < len(k.waiting) && !now.Before(k.waiting[n].foundAt.Add(raceStaggerCeiling)) {
		n++
	}
	return min(n, len(k.waiting))
}

// replacingLocked is how many running attempts are ending for a later
// candidate and have not returned. The caller holds k.mu.
func (k *raceKind[W]) replacingLocked() int {
	n := 0
	for _, a := range k.active {
		if a.replaced {
			n++
		}
	}
	return n
}

// spareLocked is the oldest running attempt that has had no answer and is
// not already ending for a later candidate, or nil. The caller holds k.mu.
func (k *raceKind[W]) spareLocked() *raceAttempt {
	for _, a := range k.active {
		if !a.answered && !a.replaced {
			return a
		}
	}
	return nil
}

// releaseLocked releases the waiting attempts whose start has come. The
// first waiting one starts at once when no attempt runs, else one
// [CrossDeviceStagger] after the latest release, and at the latest
// raceStaggerCeiling after it was found; a forced one (overflowLocked)
// as at its ceiling. A start needs a place: with maxRaceAttempts running
// each waiting candidate due a place ends the oldest running attempt that
// has had no answer, whose place goes to the first waiting once it has
// returned; while every running attempt has had an answer or is already
// ending they wait for an attempt to end. The caller holds k.mu.
func (k *raceKind[W]) releaseLocked(s *Session[W]) {
	if k.staggerTimer != nil {
		k.staggerTimer.Stop()
		k.staggerTimer = nil
	}
	for !k.isDecided && len(k.waiting) > 0 {
		now := s.clock.Now()
		next := k.waiting[0]
		ceiling := next.foundAt.Add(raceStaggerCeiling)
		if k.forced > 0 {
			ceiling = now
		}
		due := now
		if len(k.active) > 0 {
			due = k.lastRelease.Add(CrossDeviceStagger)
		}
		if ceiling.Before(due) {
			due = ceiling
		}
		if len(k.active) >= maxRaceAttempts {
			k.replaceLocked(s, now)
			return
		}
		if now.Before(due) {
			k.armLocked(s, due.Sub(now))
			return
		}
		k.waiting = k.waiting[1:]
		if k.forced > 0 {
			k.forced--
		}
		mark := &attemptMark{answered: func() {
			k.mu.Lock()
			next.answered = true
			k.mu.Unlock()
		}}
		next.ctx, next.cancel = context.WithCancel(context.WithValue(s.ctx, attemptKey{}, mark)) //nolint:gosec // G118: run and decideLocked call cancel
		k.active = append(k.active, next)
		k.lastRelease = now
		close(next.release)
	}
}

// armLocked arms the stagger timer to release again after wait. The caller
// holds k.mu.
func (k *raceKind[W]) armLocked(s *Session[W], wait time.Duration) {
	var t fabricclock.Timer
	t = s.clock.AfterFunc(wait, func() {
		k.mu.Lock()
		defer k.mu.Unlock()
		if k.staggerTimer != t {
			return
		}
		k.staggerTimer = nil
		k.releaseLocked(s)
	})
	k.staggerTimer = t
}

// replaceLocked, with every place taken, ends one running attempt for
// each waiting candidate due a place beyond those already ending — the
// oldest that has had no answer each time — for the first waiting to take
// its place when it has returned ([raceKind.ended]); an attempt ends for
// one candidate only. It then arms the stagger timer for the ceiling of
// the first candidate not yet due. The caller holds k.mu.
func (k *raceKind[W]) replaceLocked(s *Session[W], now time.Time) {
	due := k.dueLocked(now)
	for ending := k.replacingLocked(); ending < due; ending++ {
		oldest := k.spareLocked()
		if oldest == nil {
			break
		}
		oldest.replaced = true
		oldest.cancel()
		s.log.Info("matter.controller.discovery.attempt_replaced", slog.String("discovery", s.description),
			slog.String("device", oldest.device), slog.String("candidate", k.waiting[ending].device))
	}
	if due < len(k.waiting) {
		k.armLocked(s, k.waiting[due].foundAt.Add(raceStaggerCeiling).Sub(now))
	}
}

// ended is an attempt's end: its place is free, so the next waiting one
// may start.
func (k *raceKind[W]) ended(s *Session[W], a *raceAttempt) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if i := slices.Index(k.active, a); i >= 0 {
		k.active = slices.Delete(k.active, i, i+1)
	}
	k.releaseLocked(s)
}

// run is one registered attempt (registerAttempt,
// ParallelPaseDiscovery.ts:81-151 at b25ed960).
func (k *raceKind[W]) run(s *Session[W], c Candidate, a *raceAttempt) {
	select {
	case <-a.release:
		defer k.ended(s, a)
	case <-a.dropped:
		return
	case <-k.decided:
		return
	}
	defer a.cancel()
	s.checkCaller()
	k.mu.Lock()
	if k.isDecided || a.replaced {
		k.mu.Unlock()
		return
	}
	k.startedCount++
	k.mu.Unlock()
	isWinner := false
	win := func() bool {
		s.checkCaller()
		k.mu.Lock()
		if k.isDecided || a.replaced {
			k.mu.Unlock()
			return false
		}
		k.paseWon, isWinner, k.winnerIndex = true, true, a.index
		k.winnerDone = make(chan struct{})
		k.decideLocked()
		k.mu.Unlock()
		s.Stop()
		return true
	}
	w, err := k.attempt(a.ctx, c, win)
	k.mu.Lock()
	defer k.mu.Unlock()
	if isWinner {
		k.winner, k.winnerErr = w, err
		close(k.winnerDone)
		return
	}
	if err == nil {
		return
	}
	if (k.isDecided || a.replaced) && errors.Is(err, context.Canceled) {
		s.log.Debug("matter.controller.discovery.attempt_canceled", slog.String("discovery", s.description), slog.Any("err", err))
		return
	}
	k.attemptErrs = append(k.attemptErrs, err)
	s.log.Debug("matter.controller.discovery.attempt_failed", slog.String("discovery", s.description), slog.Any("err", err))
}

func (k *raceKind[W]) onStop(*Session[W]) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.paseWon {
		k.decideLocked()
	}
}

func (k *raceKind[W]) onInterrupt(*Session[W], error) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.paseWon {
		return false
	}
	k.decideLocked()
	return true
}

func (k *raceKind[W]) onComplete(s *Session[W]) (W, error) {
	k.mu.Lock()
	k.decideLocked()
	done := k.winnerDone
	k.mu.Unlock()
	if done != nil {
		<-done
	}
	k.pending.Wait()
	k.mu.Lock()
	defer k.mu.Unlock()
	var zero W
	if k.paseWon {
		if k.winnerErr != nil {
			return zero, k.winnerErr
		}
		return k.winner, nil
	}
	if k.attemptCount == 0 {
		return zero, fmt.Errorf("%w: %s failed: no commissionable device was discovered", ErrDeviceNotDiscovered, s)
	}
	if len(k.attemptErrs) > 0 {
		return zero, &AggregateError{
			Message: fmt.Sprintf("%s failed: %s (%d of %d started attempt(s) failed, %d discovered)", s, k.failure, len(k.attemptErrs), k.startedCount, k.attemptCount),
			Errs:    append([]error(nil), k.attemptErrs...),
		}
	}
	return zero, fmt.Errorf("%w: %s failed: %s (%d attempt(s) started of %d discovered, all canceled or timed out)", ErrDiscoveryIncomplete, s, k.failure, k.startedCount, k.attemptCount)
}

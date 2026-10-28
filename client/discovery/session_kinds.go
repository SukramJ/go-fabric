// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// FindInstance finds one device and returns it — matter.js InstanceDiscovery
// (packages/node/src/behavior/system/controller/discovery/InstanceDiscovery.ts:18-46
// at 862ad75f): the first device found is the result and stops the
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
// (ParallelPaseDiscovery.ts:30 at 862ad75f): the first starts at once,
// each next one a slot later.
const CrossDeviceStagger = time.Second

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
// for every attempt but the winner's once the race is decided.
type Attempt[W any] func(ctx context.Context, c Candidate, win func() bool) (W, error)

// Race runs attempt on every device found, in parallel and one
// [CrossDeviceStagger] apart, and the first to win PASE wins the race —
// matter.js ParallelPaseDiscovery (ParallelPaseDiscovery.ts:45-216):
//
//   - the race is decided on a win, an interruption before one, or the
//     end of the search without one; from then no attempt starts and win
//     refuses every attempt (:81-109, :157-174);
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
	k := &raceKind[W]{attempt: attempt, accept: accept, failure: failure, decided: make(chan struct{}), cancels: map[int]context.CancelFunc{}}
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
	cancels      map[int]context.CancelFunc
	pending      sync.WaitGroup
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
	for i, cancel := range k.cancels {
		if !k.paseWon || i != k.winnerIndex {
			cancel()
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
	index := k.attemptCount
	k.attemptCount++
	k.mu.Unlock()
	k.pending.Go(func() { k.run(s, c, index) })
}

// run is one registered attempt (registerAttempt, :81-147).
func (k *raceKind[W]) run(s *Session[W], c Candidate, index int) {
	if stagger := time.Duration(index) * CrossDeviceStagger; stagger > 0 {
		t := s.clock.NewTimer(stagger)
		select {
		case <-t.C():
		case <-k.decided:
			t.Stop()
			return
		}
	}
	s.checkCaller()
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	k.mu.Lock()
	if k.isDecided {
		k.mu.Unlock()
		return
	}
	k.cancels[index] = cancel
	k.startedCount++
	k.mu.Unlock()
	isWinner := false
	win := func() bool {
		s.checkCaller()
		k.mu.Lock()
		if k.isDecided {
			k.mu.Unlock()
			return false
		}
		k.paseWon, isWinner, k.winnerIndex = true, true, index
		k.winnerDone = make(chan struct{})
		k.decideLocked()
		k.mu.Unlock()
		s.Stop()
		return true
	}
	w, err := k.attempt(ctx, c, win)
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.cancels, index)
	if isWinner {
		k.winner, k.winnerErr = w, err
		close(k.winnerDone)
		return
	}
	if err == nil {
		return
	}
	if k.isDecided && errors.Is(err, context.Canceled) {
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

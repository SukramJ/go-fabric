// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
)

// DelayBeforeNextAddress is how long the latest PASE attempt runs before
// the next candidate address gets one, unless it settles first. Mirrors
// matter.js packages/protocol/src/peer/CommissioningConnection.ts:35
// (DELAY_BEFORE_NEXT_ADDRESS = 10 s) at 0ceb202d.
const DelayBeforeNextAddress = 10 * time.Second

// errBudget is the cause the connection's own timeout aborts with —
// matter.js's TimeoutError of the Abort's timeout.
var errBudget = errors.New("commissioner: PASE budget exceeded")

// errLostRace is the cause a late PASE session closes with
// (CommissioningConnection.ts:146-151).
var errLostRace = errors.New("commissioner: PASE session lost the race to another commissioning candidate")

// candidate is one device to connect to — matter.js CommissionableDevice
// as ControllerCommissioner.#addressesToCandidates builds it: its
// identifier, its discovery data and its addresses in order.
type candidate struct {
	identifier string
	device     discovery.Device
	addrs      []*net.UDPAddr
}

// establishFunc runs one PASE attempt — CommissioningConnection.Options
// .establishSession; onPBKDF marks the attempt as holding the responder.
type establishFunc func(ctx context.Context, addr *net.UDPAddr, c *candidate, onPBKDF func()) (*exchange.Session, error)

// connectionConfig is CommissioningConnection.Options.
type connectionConfig struct {
	devices []*candidate
	timeout time.Duration
	// delay is delayBeforeNextAddress: nil is DelayBeforeNextAddress, and
	// zero launches every address at once. Mirrors matter.js
	// packages/protocol/src/peer/CommissioningConnection.ts:64 at 862ad75f
	// (options.delayBeforeNextAddress ?? DELAY_BEFORE_NEXT_ADDRESS).
	delay     *time.Duration
	establish establishFunc
	// giveUp ends a session established after the run was decided, where
	// matter.js force-closes it (CommissioningConnection.ts:140-157 at
	// db021da0); the commissioner disarms it first. nil force-closes it
	// with the cause.
	giveUp func(s *exchange.Session, cause error)
	clock  exchange.Clock
	log    *slog.Logger
}

// connectionResult is CommissioningConnection.Result.
type connectionResult struct {
	session *exchange.Session
	device  *candidate
}

// queued is one (device, address) attempt.
type queued struct {
	device *candidate
	addr   *net.UDPAddr
}

// attemptState is one attempt's start and whether it settled.
type attemptState struct {
	startedAt time.Time
	settled   bool
}

// connection is one run of CommissioningConnection.
type connection struct {
	cfg      connectionConfig
	delay    time.Duration
	abortCtx context.Context
	abort    context.CancelCauseFunc
	changed  chan struct{}

	// mu guards holding, lastErr, lastNonRetryable, pending and winner.
	mu      sync.Mutex
	queue   []queued
	winner  *connectionResult
	holding int
	// holdSince is when the latest attempt began to hold its responder:
	// a hold stops the scheduler for at most the delay from then
	// (R2-P6-fsm-1806).
	holdSince        time.Time
	latest           *attemptState
	timer            exchange.Timer
	pending          int
	lastErr          error
	lastNonRetryable error
}

// connectPASE attempts PASE with the candidates and returns the first
// session established. Mirrors matter.js
// packages/protocol/src/peer/CommissioningConnection.ts:60-250 at
// 0ceb202d:
//
//   - one Abort holds the budget (cfg.timeout) and the caller's ctx (:63);
//   - the candidates are the devices' addresses in order (:86); one
//     scheduler (:93-117) launches the next while there is no winner and
//     the abort has not fired, waiting until the latest unsettled attempt
//     is delay old (:106-113);
//   - an attempt (:119-201) that receives the PBKDFParamResponse holds the
//     responder (:127-133); its session wins unless a winner exists or the
//     abort fired — then the session is force-closed (:137-157) — and the
//     winner aborts every other attempt (:158-161);
//   - errors of an attempt that was not cancelled (:163-190): a wrong
//     passcode or other unexpected data is the non-retryable error, a
//     transient or network error the retryable last error, anything else
//     aborts the run;
//   - the result (:205-249): the winner without waiting for the losers;
//     else, every attempt settled, the non-retryable error, else on an
//     abort without a retryable error the external cause as is or, for
//     the budget, a PairRetransmissionLimitReachedError, else that error
//     around the last retryable one.
//
// Two rules are this module's, ahead of matter.js (review II,
// R2-P6-fsm-1806, owner decision 207). A wrong passcode — the key
// confirmation failing — ends that address only, not the device: the
// device's next address is tried, and the mismatch is the result only
// when no address is left, as connectedhomeip's SetUpCodePairer tries the
// next address after any PASE failure (src/controller/SetUpCodePairer.cpp
// :959-967 at 6170af84). matter.js drops every address of the device
// (CommissioningConnection.ts:167-170 at db021da0), so a responder at any
// address of the device — one a party on the link added to its host
// record — decided for all of them. And an attempt that holds its
// responder stops the scheduler for at most the delay; after it the next
// address starts while the holding attempt runs on (matter.js holds
// without a bound, :97; the bound is the module's own rule).
func connectPASE(ctx context.Context, cfg connectionConfig) (connectionResult, error) {
	delay := DelayBeforeNextAddress
	if cfg.delay != nil {
		delay = *cfg.delay
	}
	abortCtx, abort := context.WithCancelCause(ctx)
	defer abort(nil)
	budget := cfg.clock.AfterFunc(cfg.timeout, func() { abort(errBudget) })
	defer budget.Stop()
	c := &connection{
		cfg: cfg, delay: delay, abortCtx: abortCtx, abort: abort,
		changed: make(chan struct{}, 1),
	}
	for _, d := range cfg.devices {
		for _, a := range d.addrs {
			c.queue = append(c.queue, queued{device: d, addr: a})
		}
	}
	defer func() {
		c.mu.Lock()
		if c.timer != nil {
			c.timer.Stop()
		}
		c.mu.Unlock()
	}()

	c.mu.Lock()
	c.schedule() //nolint:contextcheck // every attempt derives its context from abortCtx
	c.mu.Unlock()

	// Drain until a winner is picked or the abort fires (:208-210). The
	// winner and the abort are read in one look under c.mu: the winner's
	// attempt records itself and aborts the others with errWon under it,
	// so a look at the winner before and at the abort after that instant
	// read the winner's own abort as the run's failure (the errWon flake,
	// brief CG §3) — matter.js reads both in one turn of its event loop
	// (CommissioningConnection.ts:208-212 at 862ad75f).
	for {
		c.mu.Lock()
		winner, pending, aborted := c.winner, c.pending, abortCtx.Err() != nil
		c.mu.Unlock()
		if winner != nil {
			return *winner, nil
		}
		if pending == 0 || aborted {
			break
		}
		select {
		case <-c.changed:
		case <-abortCtx.Done():
		}
	}
	// No winner: every attempt settles first (:222).
	for {
		c.mu.Lock()
		pending := c.pending
		c.mu.Unlock()
		if pending == 0 {
			break
		}
		<-c.changed
	}
	err := c.failure()
	abort(err)
	return connectionResult{}, err
}

// failure is the error of a run without a winner (:223-239).
func (c *connection) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastNonRetryable != nil {
		return c.lastNonRetryable
	}
	if c.abortCtx.Err() != nil && c.lastErr == nil {
		if reason := context.Cause(c.abortCtx); reason != nil && !errors.Is(reason, errBudget) {
			return reason
		}
		return &PairRetransmissionLimitReachedError{Message: "Failed to connect on any discovered server before timeout"}
	}
	if c.lastErr != nil {
		return &PairRetransmissionLimitReachedError{Message: "Failed to connect on any discovered server", Cause: c.lastErr}
	}
	return &PairRetransmissionLimitReachedError{Message: "Failed to connect on any discovered server"}
}

// schedule is the scheduler (:93-117); c.mu is held.
func (c *connection) schedule() {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	for c.winner == nil && c.abortCtx.Err() == nil {
		if len(c.queue) == 0 {
			return
		}
		// A holding attempt stops the scheduler for at most the delay
		// from its hold, then the latest unsettled attempt for the delay
		// from its start.
		var remaining time.Duration
		if c.holding > 0 {
			remaining = c.delay - c.cfg.clock.Now().Sub(c.holdSince)
		}
		if c.latest != nil && !c.latest.settled {
			remaining = max(remaining, c.delay-c.cfg.clock.Now().Sub(c.latest.startedAt))
		}
		if remaining > 0 {
			c.timer = c.cfg.clock.AfterFunc(remaining, func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				c.schedule()
			})
			return
		}
		next := c.queue[0]
		c.queue = c.queue[1:]
		c.launch(next)
	}
}

// launch is launchAttempt (:119-201); c.mu is held.
func (c *connection) launch(q queued) {
	st := &attemptState{startedAt: c.cfg.clock.Now()}
	c.latest = st
	actx, acancel := context.WithCancelCause(c.abortCtx)
	holds := false
	onPBKDF := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if !holds && !st.settled {
			holds = true
			c.holding++
			c.holdSince = c.cfg.clock.Now()
		}
	}
	c.pending++
	go func() {
		defer acancel(nil)
		s, err := c.cfg.establish(actx, q.addr, q.device, onPBKDF)
		var loser *exchange.Session
		var loserCause error
		c.mu.Lock()
		switch {
		case err == nil && (c.winner != nil || c.abortCtx.Err() != nil):
			loser, loserCause = s, context.Cause(actx)
			if loserCause == nil {
				loserCause = errLostRace
			}
		case err == nil:
			c.winner = &connectionResult{session: s, device: q.device}
			c.abort(errWon)
		case c.abortCtx.Err() == nil:
			c.classify(q, err)
		}
		// The attempt is decided: it holds the responder no longer, and the
		// next address may start.
		st.settled = true
		if holds {
			c.holding--
		}
		c.schedule() //nolint:contextcheck // every attempt derives its context from abortCtx
		if loser != nil {
			// The losing session is given up before the attempt stops
			// counting as pending, so a run without a winner, which waits
			// for every attempt, returns only once its devices are free
			// (review II).
			c.mu.Unlock()
			if c.cfg.giveUp != nil {
				c.cfg.giveUp(loser, loserCause)
			} else {
				loser.HandlePeerLoss(loserCause)
			}
			c.mu.Lock()
		}
		c.pending--
		c.mu.Unlock()
		select {
		case c.changed <- struct{}{}:
		default:
		}
	}()
}

// errWon is the cause the winner aborts the other attempts with.
var errWon = errors.New("commissioner: another PASE attempt won")

// classify records an attempt's error (:163-190); c.mu is held. A wrong
// passcode ends the address, not the device (R2-P6-fsm-1806).
func (c *connection) classify(q queued, err error) {
	switch {
	case errors.Is(err, handshake.ErrPasscodeMismatch):
		c.cfg.log.Info("matter.controller.commissioning.address_passcode_mismatch", slog.String("peer", q.addr.String()), slog.String("candidate", q.device.identifier), slog.Any("err", err))
		c.lastNonRetryable = err
	case errors.Is(err, handshake.ErrUnexpectedData):
		c.cfg.log.Info("matter.controller.commissioning.address_failed", slog.String("peer", q.addr.String()), slog.String("candidate", q.device.identifier), slog.Any("err", err))
		c.lastNonRetryable = err
	case isRetryable(err):
		c.lastErr = err
		c.cfg.log.Warn("matter.controller.commissioning.address_unreachable", slog.String("peer", q.addr.String()), slog.String("candidate", q.device.identifier))
	default:
		c.abort(err)
		c.lastNonRetryable = err
	}
}

// isRetryable is causedBy(error, NoResponseTimeoutError,
// TransientPeerCommunicationError, NetworkError) (:178): the exchange
// layer's transient class (an unanswered message, a StatusReport, which
// ControllerCommissioner.ts:481-485 turns into NoResponseTimeoutError) and
// its network class.
func isRetryable(err error) bool {
	return errors.Is(err, exchange.ErrTransient) || errors.Is(err, exchange.ErrNetwork) || errors.Is(err, exchange.ErrRetransmissionLimit)
}

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

	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
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
	clock     exchange.Clock
	log       *slog.Logger
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

// deviceAbort cancels every attempt of one device.
type deviceAbort struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
}

// connection is one run of CommissioningConnection.
type connection struct {
	cfg      connectionConfig
	delay    time.Duration
	abortCtx context.Context
	abort    context.CancelCauseFunc
	changed  chan struct{}

	mu               sync.Mutex
	queue            []queued
	devices          map[string]*deviceAbort
	winner           *connectionResult
	holding          int
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
//     scheduler (:93-117) launches the next while there is no winner, the
//     abort has not fired and no attempt holds its responder, skipping the
//     addresses of a dropped device (:98-100) and waiting until the latest
//     unsettled attempt is delay old (:106-113);
//   - an attempt (:119-201) that receives the PBKDFParamResponse holds the
//     responder until it settles (:127-133); its session wins unless a
//     winner exists, the abort fired or its device was dropped — then the
//     session is force-closed (:137-157) — and the winner aborts every
//     other attempt (:158-161);
//   - errors of an attempt that was not cancelled (:163-190): a wrong
//     passcode drops the device and is the non-retryable error, other
//     unexpected data is the non-retryable error, a transient or network
//     error the retryable last error, anything else aborts the run;
//   - the result (:205-249): the winner without waiting for the losers;
//     else, every attempt settled, the non-retryable error, else on an
//     abort without a retryable error the external cause as is or, for
//     the budget, a PairRetransmissionLimitReachedError, else that error
//     around the last retryable one.
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
		devices: make(map[string]*deviceAbort),
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

	// Drain until a winner is picked or the abort fires (:208-210).
	for {
		c.mu.Lock()
		winner, pending := c.winner, c.pending
		c.mu.Unlock()
		if winner != nil {
			return *winner, nil
		}
		if pending == 0 || abortCtx.Err() != nil {
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

// deviceAbortOf is getDeviceAbort (:75-82); c.mu is held.
func (c *connection) deviceAbortOf(id string) *deviceAbort {
	d, ok := c.devices[id]
	if !ok {
		ctx, cancel := context.WithCancelCause(context.Background())
		d = &deviceAbort{ctx: ctx, cancel: cancel}
		c.devices[id] = d
	}
	return d
}

// schedule is the scheduler (:93-117); c.mu is held.
func (c *connection) schedule() {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	for c.winner == nil && c.abortCtx.Err() == nil && c.holding == 0 {
		for len(c.queue) > 0 && c.deviceAbortOf(c.queue[0].device.identifier).ctx.Err() != nil {
			c.queue = c.queue[1:]
		}
		if len(c.queue) == 0 {
			return
		}
		next := c.queue[0]
		c.queue = c.queue[1:]
		if c.latest != nil && !c.latest.settled {
			if remaining := c.delay - c.cfg.clock.Now().Sub(c.latest.startedAt); remaining > 0 {
				c.queue = append([]queued{next}, c.queue...)
				c.timer = c.cfg.clock.AfterFunc(remaining, func() {
					c.mu.Lock()
					defer c.mu.Unlock()
					c.schedule()
				})
				return
			}
		}
		c.launch(next)
	}
}

// launch is launchAttempt (:119-201); c.mu is held.
func (c *connection) launch(q queued) {
	st := &attemptState{startedAt: c.cfg.clock.Now()}
	c.latest = st
	dev := c.deviceAbortOf(q.device.identifier)
	actx, acancel := context.WithCancelCause(c.abortCtx)
	stop := context.AfterFunc(dev.ctx, func() { acancel(context.Cause(dev.ctx)) })
	holds := false
	onPBKDF := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if !holds {
			holds = true
			c.holding++
		}
	}
	c.pending++
	go func() {
		defer acancel(nil)
		s, err := c.cfg.establish(actx, q.addr, q.device, onPBKDF)
		stop()
		var loser *exchange.Session
		var loserCause error
		c.mu.Lock()
		switch {
		case err == nil && (c.winner != nil || c.abortCtx.Err() != nil || dev.ctx.Err() != nil):
			loser, loserCause = s, context.Cause(actx)
			if loserCause == nil {
				loserCause = errLostRace
			}
		case err == nil:
			c.winner = &connectionResult{session: s, device: q.device}
			c.abort(errWon)
		case c.abortCtx.Err() == nil && dev.ctx.Err() == nil:
			c.classify(q, dev, err)
		}
		c.pending--
		st.settled = true
		if holds {
			c.holding--
		}
		c.schedule() //nolint:contextcheck // every attempt derives its context from abortCtx
		c.mu.Unlock()
		if loser != nil {
			loser.HandlePeerLoss(loserCause)
		}
		select {
		case c.changed <- struct{}{}:
		default:
		}
	}()
}

// errWon is the cause the winner aborts the other attempts with.
var errWon = errors.New("commissioner: another PASE attempt won")

// classify records an attempt's error (:163-190); c.mu is held.
func (c *connection) classify(q queued, dev *deviceAbort, err error) {
	switch {
	case errors.Is(err, handshake.ErrPasscodeMismatch):
		c.cfg.log.Info("matter.controller.commissioning.device_dropped", slog.String("device", q.device.identifier), slog.String("err", err.Error()))
		c.lastNonRetryable = err
		dev.cancel(err)
	case errors.Is(err, handshake.ErrUnexpectedData):
		c.cfg.log.Info("matter.controller.commissioning.address_failed", slog.String("address", q.addr.String()), slog.String("device", q.device.identifier), slog.String("err", err.Error()))
		c.lastNonRetryable = err
	case isRetryable(err):
		c.lastErr = err
		c.cfg.log.Warn("matter.controller.commissioning.address_unreachable", slog.String("address", q.addr.String()), slog.String("device", q.device.identifier))
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

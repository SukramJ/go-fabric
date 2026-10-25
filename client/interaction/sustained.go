// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	clientpeer "github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/im"
)

// RetrySchedule configures the retries of a sustained subscription —
// matter.js RetrySchedule.Configuration
// (packages/general/src/net/RetrySchedule.ts:67-109). A zero field takes
// the schedule's default (an initial interval of 1 s, a backoff of 2); a
// zero MaximumInterval and JitterFactor mean none.
type RetrySchedule struct {
	InitialInterval time.Duration
	BackoffFactor   float64
	MaximumInterval time.Duration
	JitterFactor    float64
}

// DefaultSustainRetries is the schedule a sustained subscription retries
// on: 15 s, doubling, at most an hour, a quarter of jitter, no timeout.
// Mirrors matter.js
// packages/protocol/src/action/client/subscription/SustainedSubscription.ts:483-499
// (SustainedSubscription.DefaultRetrySchedule) at 0ceb202d.
var DefaultSustainRetries = RetrySchedule{
	InitialInterval: 15 * time.Second,
	BackoffFactor:   2,
	MaximumInterval: time.Hour,
	JitterFactor:    0.25,
}

// retries yields the intervals of a schedule without timeout or count
// limit — RetrySchedule's iterator (RetrySchedule.ts:28-60): the jitter
// (the floor, in milliseconds, of jitterFactor × interval × a draw in
// [0, 1)) is added, the sum clamped to the maximum, and the base
// multiplied by the backoff.
type retries struct {
	cfg  RetrySchedule
	rand func() float64
	base time.Duration
}

func newRetries(cfg RetrySchedule, rand func() float64) *retries {
	if cfg.InitialInterval == 0 {
		cfg.InitialInterval = time.Second
	}
	if cfg.BackoffFactor == 0 {
		cfg.BackoffFactor = 2
	}
	return &retries{cfg: cfg, rand: rand, base: cfg.InitialInterval}
}

func (r *retries) next() time.Duration {
	maxJitterMs := r.cfg.JitterFactor * float64(r.base) / float64(time.Millisecond)
	jitter := time.Duration(math.Floor(maxJitterMs*r.rand())) * time.Millisecond
	interval := r.base + jitter
	if r.cfg.MaximumInterval != 0 && interval > r.cfg.MaximumInterval {
		interval = r.cfg.MaximumInterval
	}
	if next := float64(r.base) * r.cfg.BackoffFactor; next < math.MaxInt64 {
		r.base = time.Duration(next)
	}
	return interval
}

// sustainedState is what a sustained subscription keeps beside its
// Subscription.
type sustainedState struct {
	finished chan struct{}

	// mu guards failureLogged, hasEvent, maxEvent, inner, active, trusted
	// and hold.
	mu      sync.Mutex
	inner   *Subscription
	active  bool
	trusted bool
	// maxEvent is the highest event number delivered, valid once hasEvent.
	maxEvent uint64
	hasEvent bool
	// changed is the request's MaxEventNumberChanged; log and failureLogged
	// carry #logLocalFailure's warn-once rule.
	changed       func(uint64) error
	log           *slog.Logger
	failureLogged bool
	// hold releases the wakefulness the established subscription holds
	// (SustainedSubscription.ts:228, #subscriptionHold); guarded by mu.
	hold func()
}

func (st *sustainedState) setHold(release func()) {
	st.mu.Lock()
	st.hold = release
	st.mu.Unlock()
}

// releaseHold is #releaseSubscriptionHold (SustainedSubscription.ts:449-452).
func (st *sustainedState) releaseHold() {
	st.mu.Lock()
	release := st.hold
	st.hold = nil
	st.mu.Unlock()
	if release != nil {
		release()
	}
}

// MaxEventNumber is the highest event number of the reports a sustained
// subscription delivered to its Updated callback, false before the first
// event; it never decreases. A plain subscription reports none. Mirrors
// matter.js SustainedSubscription.maxEventNumber
// (packages/protocol/src/action/client/subscription/SustainedSubscription.ts:105-114)
// at 862ad75f.
func (s *Subscription) MaxEventNumber() (uint64, bool) {
	if s.sustained == nil {
		return 0, false
	}
	s.sustained.mu.Lock()
	defer s.sustained.mu.Unlock()
	return s.sustained.maxEvent, s.sustained.hasEvent
}

// deliver hands r to the consumer and counts its events once the consumer
// returned. Mirrors matter.js SustainedSubscription.#deliver and
// #raiseMaxEventNumber
// (packages/protocol/src/action/client/subscription/SustainedSubscription.ts:339-364)
// at 862ad75f, the maxEventNumberChanged hook included; the failure of a
// consumer has no Go counterpart (Updated returns nothing).
func (st *sustainedState) deliver(r *Report, consumer func(*Report)) {
	consumer(r)
	st.mu.Lock()
	raised := false
	for i := range r.Events {
		if n := r.Events[i].Number; !st.hasEvent || n > st.maxEvent {
			st.maxEvent, st.hasEvent, raised = n, true, true
		}
	}
	number := st.maxEvent
	st.mu.Unlock()
	if !raised || st.changed == nil {
		return
	}
	if err := st.changed(number); err != nil {
		st.logLocalFailure("handling the event number of a report", err)
	}
}

// logLocalFailure logs a failure of the subscription's own processing: the
// first as a warning, later ones at debug, since a peer can trigger one with
// every report. Mirrors matter.js SustainedSubscription.#logLocalFailure
// (packages/protocol/src/action/client/subscription/SustainedSubscription.ts:366-377)
// at 862ad75f.
func (st *sustainedState) logLocalFailure(what string, err error) {
	if st.log == nil {
		return
	}
	st.mu.Lock()
	first := !st.failureLogged
	st.failureLogged = true
	st.mu.Unlock()
	logf := st.log.Debug
	if first {
		logf = st.log.Warn
	}
	logf("matter.controller.interaction.sustain_local_failure", slog.String("what", what), slog.Any("err", err))
}

// delivering is the consumer reached through deliver, nil without one
// (SustainedSubscription.ts:120-121 at 862ad75f).
func (st *sustainedState) delivering(consumer func(*Report)) func(*Report) {
	if consumer == nil {
		return nil
	}
	return func(r *Report) { st.deliver(r, consumer) }
}

// requestNewEvents asks for the events above the highest one delivered,
// the request's filters kept until then. Mirrors matter.js
// SustainedSubscription.#requestNewEvents
// (packages/protocol/src/action/client/subscription/SustainedSubscription.ts:379-384)
// at 862ad75f.
func (st *sustainedState) requestNewEvents(req SubscribeRequest) SubscribeRequest {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.hasEvent {
		req.EventFilters = []im.EventMinimumNumber{{EventMin: st.maxEvent + 1}}
	}
	return req
}

func (st *sustainedState) current() *Subscription {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.inner
}

// Active reports whether a sustained subscription holds an established
// underlying subscription — SustainedSubscription.active
// (SustainedSubscription.ts:83-95); a plain subscription is active until
// closed.
func (s *Subscription) Active() bool {
	if s.sustained == nil {
		select {
		case <-s.done:
			return false
		default:
			return true
		}
	}
	s.sustained.mu.Lock()
	defer s.sustained.mu.Unlock()
	return s.sustained.active
}

// sustain starts a sustained subscription — ClientInteraction.ts:1092-1115
// with SustainedSubscription (SustainedSubscription.ts:52-310): the
// subscription is returned at once and its loop establishes the
// underlying one, with a connection that does not time out (:1093-1095).
func (c *Interaction) sustain(peer exchange.PeerAddress, req SubscribeRequest, floor, ceiling time.Duration) *Subscription {
	outer := c.subs.newSubscription(peer, req, c.log)
	outer.sustained = &sustainedState{finished: make(chan struct{}), trusted: true, changed: req.MaxEventNumberChanged, log: c.log}
	c.subs.addActive(outer)
	go c.runSustained(outer, floor, ceiling)
	return outer
}

// setActive is the active/inactive observable pair (:262-263, :312-316).
func (s *Subscription) setActive(active bool) {
	st := s.sustained
	st.mu.Lock()
	changed := st.active != active
	st.active = active
	st.mu.Unlock()
	if changed && s.request.ActiveChanged != nil {
		s.request.ActiveChanged(active)
	}
}

// SITRetryInterval is the delay before a sustained subscription retries a
// peer whose subscribe failed while it required awaiting and that stopped
// requiring it since — a runtime LIT→SIT flip leaves no wake edge to park
// for (SustainedSubscription.ts:60, SIT_RETRY_INTERVAL; :539-551).
const SITRetryInterval = 15 * time.Second

// runSustained is SustainedSubscription.#run (SustainedSubscription.ts:116-337
// at 862ad75f): the session is trusted at first; after a loss the
// provider's reachability is verified, a failed probe meaning a new
// session anyway (:185-199); the attempts run on the retry schedule — the
// bootstrap read hook first while it has not succeeded (:209-223), then
// the subscription (:224-230), a failure that was not our abort trusting
// the session again (:231-235) and waiting the next interval (:258-268);
// once established the subscription is active (:282) until the underlying
// one closes, then inactive and replaced (:287-318); an abort ends the
// loop and the underlying subscription is closed (:320-336).
//
// With a wakefulness provider ([SubscribeRequest].Wakefulness) the ICD
// branches apply, the wakefulness asked live on every iteration: a peer
// that requires awaiting is not probed but parked until it wakes
// (:162-184), after a failed attempt until a fresh wake signal — no timed
// retry (:246-256, :166-171, :539-551); every connection to it is the
// priority one (:142-149); the subscription holds the wakefulness while it
// is established (:224-230); and an operating-mode flip — or, without a
// wakefulness, the peer's registration ([SubscribeRequest].PeerFed) —
// tears the subscription down deliberately and recreates it at once on the
// priority connection, without an inactive dip (:287-316, :459-508).
func (c *Interaction) runSustained(outer *Subscription, floor, ceiling time.Duration) {
	st := outer.sustained
	ctx := outer.ctx
	wakefulness := outer.wakefulness
	defer func() {
		st.mu.Lock()
		inner := st.inner
		st.inner = nil
		st.mu.Unlock()
		st.releaseHold()
		if inner != nil {
			inner.Close()
		}
		outer.setActive(false)
		close(st.finished)
	}()
	bootstrap := outer.request.BootstrapRead
	updated := st.delivering(outer.request.Updated)
	// needRefresh is false only for the very first attempt
	// (SustainedSubscription.ts:127, :205-208).
	needRefresh := false
	// awaitFreshSignal: a failed attempt on a peer that requires awaiting
	// waits for the next wake, not a retry in the current window (:129-131).
	awaitFreshSignal := false
	// recreatePriority: a mode-flip recreate goes out on the priority
	// connection (:133-135).
	recreatePriority := false
	for {
		req, closed, deliberate := innerRequest(outer, updated)
		// The attempt's network: a mode-flip recreate's goes out on the
		// priority network, and so does every attempt to a peer that
		// requires awaiting when the caller pinned none (:139-149).
		if recreatePriority {
			req.Network = priorityNetwork
		} else if req.Network == "" && requiresAwait(wakefulness()) {
			req.Network = priorityNetwork
		}
		recreatePriority = false

		if !c.sustainGate(ctx, outer, wakefulness(), &awaitFreshSignal) {
			return
		}

		schedule := newRetries(DefaultSustainRetries, c.rand)
		for {
			inner, err := c.sustainAttempt(ctx, st, outer.peer, &req, floor, ceiling, &bootstrap, &needRefresh)
			if err == nil {
				established(outer, inner, wakefulness())
				break
			}
			if ctx.Err() != nil {
				return
			}
			st.mu.Lock()
			st.trusted = true
			st.mu.Unlock()
			outer.setActive(false)
			// A peer that requires awaiting has no timed retry: park for its
			// next wake (:246-256).
			if w := wakefulness(); w != nil && w.RequiresAwait() {
				c.log.Info("matter.controller.interaction.sustain_park",
					slog.Uint64("node_id", outer.peer.NodeID), slog.Any("err", err))
				awaitFreshSignal = true
				break
			}
			retry := schedule.next()
			c.log.Info("matter.controller.interaction.sustain_retry",
				slog.Uint64("node_id", outer.peer.NodeID), slog.Duration("retry", retry), slog.Any("err", err))
			if !c.sleep(ctx, retry) {
				return
			}
		}
		if awaitFreshSignal {
			continue
		}
		outer.setActive(true)
		flipped := c.awaitClosedOrModeFlip(ctx, outer, wakefulness(), closed)
		if ctx.Err() != nil {
			return
		}
		if !flipped {
			// A genuine loss: report not live so consumers re-establish. A
			// deliberate recreate keeps active until it re-established or has
			// to park (:289-293).
			outer.setActive(false)
			c.log.Info("matter.controller.interaction.sustain_replacing", slog.Uint64("node_id", outer.peer.NodeID))
			continue
		}
		deliberate.Store(true)
		tearDownForRecreate(outer)
		c.log.Info("matter.controller.interaction.sustain_recreating", slog.Uint64("node_id", outer.peer.NodeID),
			slog.String("reason", "ICD operating mode change"))
		recreatePriority = true
	}
}

// wakefulness is the peer's ICD wakefulness, read live on every use so a
// peer registered or flipped SIT⇄LIT later is honoured
// (SustainedSubscription.ts:160-162); nil for a peer that is no ICD.
func (s *Subscription) wakefulness() *icd.PeerWakefulness {
	if s.request.Wakefulness == nil {
		return nil
	}
	return s.request.Wakefulness()
}

// innerRequest is one attempt's request (SustainedSubscription.ts:141-158):
// the outer request without the sustained fields, its closed callback
// clearing the inner subscription and closing the returned channel, unless
// the returned flag marks a teardown of our own (:305-308).
func innerRequest(outer *Subscription, updated func(*Report)) (SubscribeRequest, <-chan struct{}, *atomic.Bool) {
	st := outer.sustained
	closed := make(chan struct{})
	var closeOnce sync.Once
	deliberate := new(atomic.Bool)
	req := outer.request
	req.Updated = updated
	req.Sustain, req.BootstrapRead, req.ActiveChanged = false, nil, nil
	req.Wakefulness, req.PeerFed = nil, nil
	req.Closed = func() {
		if deliberate.Load() {
			return
		}
		closeOnce.Do(func() {
			st.mu.Lock()
			st.inner = nil
			st.trusted = false
			st.mu.Unlock()
			st.releaseHold()
			outer.mu.Lock()
			outer.id = NoSubscription
			outer.mu.Unlock()
			close(closed)
		})
	}
	return req, closed, deliberate
}

// priorityNetwork is the network profile of a time-critical attempt
// (SustainedSubscription.ts:136).
const priorityNetwork = clientpeer.NetworkICDLit

// requiresAwait reports a peer whose interactions wait for its wake; every
// connection to it is the priority one (SustainedSubscription.ts:145-149).
func requiresAwait(w *icd.PeerWakefulness) bool { return w != nil && w.RequiresAwait() }

// sustainGate runs before an attempt (SustainedSubscription.ts:129-131,
// :160-200): a peer that requires awaiting is waited for — its next wake
// after a failed attempt, its wake while it sleeps — and any other peer
// whose session is not trusted is probed. It reports false when ctx ended.
func (c *Interaction) sustainGate(ctx context.Context, outer *Subscription, w *icd.PeerWakefulness, awaitFreshSignal *bool) bool {
	st := outer.sustained
	if w != nil && w.RequiresAwait() {
		switch {
		case *awaitFreshSignal:
			outer.setActive(false)
			if !c.nextWake(ctx, w) {
				return false
			}
			*awaitFreshSignal = false
		case !w.Awake():
			outer.setActive(false)
			if !w.Awake() && !c.awaitAwake(ctx, w) {
				return false
			}
		}
		return ctx.Err() == nil
	}
	st.mu.Lock()
	trusted := st.trusted
	st.mu.Unlock()
	if s := c.provider.Session(); s != nil && s.Transport() == exchange.TransportTCP {
		// TCP evicts the session when its connection drops: no liveness
		// probe (ClientInteraction.ts:1155-1159 at 862ad75f).
		trusted = true
	}
	if !trusted {
		if !c.provider.VerifyReachability(ctx) && ctx.Err() == nil {
			// Probing failed, so we get a new session anyway.
			c.log.Info("matter.controller.interaction.sustain_probe_failed", slog.Uint64("node_id", outer.peer.NodeID))
		}
		if ctx.Err() != nil {
			return false
		}
	}
	return true
}

// established records the inner subscription an attempt established and,
// for an ICD peer, holds the peer's wakefulness for it
// (SustainedSubscription.ts:224-229).
func established(outer, inner *Subscription, w *icd.PeerWakefulness) {
	st := outer.sustained
	st.mu.Lock()
	st.inner = inner
	st.trusted = true
	st.mu.Unlock()
	outer.mu.Lock()
	outer.id = inner.ID()
	outer.mu.Unlock()
	st.releaseHold()
	if w != nil {
		st.setHold(w.HoldSubscription())
	}
}

// tearDownForRecreate closes the inner subscription for a mode-flip
// recreate; the CASE session is untouched, so the next attempt does not
// probe (SustainedSubscription.ts:300-312).
func tearDownForRecreate(outer *Subscription) {
	st := outer.sustained
	st.mu.Lock()
	inner := st.inner
	st.inner = nil
	st.trusted = true
	st.mu.Unlock()
	outer.mu.Lock()
	outer.id = NoSubscription
	outer.mu.Unlock()
	st.releaseHold()
	if inner != nil {
		inner.Close()
	}
}

// awaitClosedOrModeFlip waits until the subscription closes, the peer's
// operating mode flips — or, without a wakefulness, the peer's ICD
// registration starts — or ctx ends; true when a flip or the registration
// came first. Mirrors SustainedSubscription.ts:459-508.
func (c *Interaction) awaitClosedOrModeFlip(ctx context.Context, outer *Subscription, w *icd.PeerWakefulness, closed <-chan struct{}) bool {
	if w == nil {
		fed := outer.request.PeerFed
		if fed == nil {
			select {
			case <-closed:
			case <-ctx.Done():
			}
			return false
		}
		signal := make(chan struct{})
		var once sync.Once
		unsubscribe := fed(func(id uint64) {
			if id == outer.peer.NodeID {
				once.Do(func() { close(signal) })
			}
		})
		defer unsubscribe()
		select {
		case <-closed:
			return false
		case <-signal:
			return ctx.Err() == nil
		case <-ctx.Done():
			return false
		}
	}
	select {
	case <-closed:
		return false
	case <-w.ModeSignal():
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

// awaitAwake waits for the peer's next awake emission of true — a fresh
// wake signal — or ctx; false when ctx ended. Mirrors
// SustainedSubscription.ts:515-532.
func (c *Interaction) awaitAwake(ctx context.Context, w *icd.PeerWakefulness) bool {
	for {
		_, next := w.AwakeSignal()
		select {
		case <-next:
			if w.Awake() {
				return true
			}
		case <-ctx.Done():
			return false
		}
	}
}

// nextWake resumes after a failed attempt on a peer that required awaiting:
// its next wake, or, for a peer that no longer requires it, a bounded delay
// (SustainedSubscription.ts:539-551).
func (c *Interaction) nextWake(ctx context.Context, w *icd.PeerWakefulness) bool {
	if !w.RequiresAwait() {
		return c.sleep(ctx, SITRetryInterval)
	}
	return c.awaitAwake(ctx, w)
}

// sustainAttempt is one attempt (SustainedSubscription.ts:180-211): the
// request refreshed unless this is the very first attempt (:182-186), the
// bootstrap read while it has not succeeded and the request refreshed
// after it (:187-204), then the subscription. Each refresh asks for the
// events above the highest one delivered (SustainedSubscription.ts:125-126
// at 862ad75f).
func (c *Interaction) sustainAttempt(ctx context.Context, st *sustainedState, peer exchange.PeerAddress, req *SubscribeRequest, floor, ceiling time.Duration, bootstrap *func(ctx context.Context) error, needRefresh *bool) (*Subscription, error) {
	if *needRefresh {
		*req = st.requestNewEvents(refreshed(*req))
	}
	*needRefresh = true
	if *bootstrap != nil {
		if err := (*bootstrap)(ctx); err != nil {
			return nil, err
		}
		*bootstrap = nil
		*req = st.requestNewEvents(refreshed(*req))
	}
	opts := req.exchangeOptions()
	opts.ConnectionTimeout = -1
	if c.attemptHook != nil {
		return c.attemptHook(ctx, *req, opts)
	}
	return c.subscribeOnce(ctx, peer, *req, floor, ceiling, opts)
}

// refreshed is the request its RefreshRequest hook rewrites, with the
// attempt's own callbacks kept; without a hook the request as it is.
func refreshed(req SubscribeRequest) SubscribeRequest {
	if req.RefreshRequest == nil {
		return req
	}
	out := req.RefreshRequest(req)
	out.Sustain, out.BootstrapRead, out.ActiveChanged = false, nil, nil
	out.Updated, out.KeepaliveReceived, out.Closed, out.RefreshRequest = req.Updated, req.KeepaliveReceived, req.Closed, req.RefreshRequest
	out.MinIntervalFloor, out.MaxIntervalCeiling = req.MinIntervalFloor, req.MaxIntervalCeiling
	return out
}

// sleep waits d on the interaction's clock; false when ctx ended first.
func (c *Interaction) sleep(ctx context.Context, d time.Duration) bool {
	wake := make(chan struct{})
	t := c.clock.AfterFunc(d, func() { close(wake) })
	defer t.Stop()
	select {
	case <-wake:
		return true
	case <-ctx.Done():
		return false
	}
}

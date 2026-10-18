// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
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
	logf("matter.controller.interaction.sustain_local_failure", slog.String("what", what), slog.String("err", err.Error()))
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

// runSustained is SustainedSubscription.#run (SustainedSubscription.ts:97-310)
// without the ICD branches: the session is trusted at first; after a loss
// the provider's reachability is verified, a failed probe meaning a new
// session anyway (:163-177); the attempts run on the retry schedule —
// the bootstrap read hook first while it has not succeeded (:187-204),
// then the subscription (:205-211), a failure that was not our abort
// trusting the session again (:213-216) and waiting the next interval
// (:239-249); once established the subscription is active (:262-263)
// until the underlying one closes, then inactive and replaced (:268-299);
// an abort ends the loop (:264-266, :277-279) and the underlying
// subscription is closed (:301-309).
func (c *Interaction) runSustained(outer *Subscription, floor, ceiling time.Duration) {
	st := outer.sustained
	ctx := outer.ctx
	defer func() {
		st.mu.Lock()
		inner := st.inner
		st.inner = nil
		st.mu.Unlock()
		if inner != nil {
			inner.Close()
		}
		outer.setActive(false)
		close(st.finished)
	}()
	bootstrap := outer.request.BootstrapRead
	updated := st.delivering(outer.request.Updated)
	// needRefresh is false only for the very first attempt
	// (SustainedSubscription.ts:104, :182-186).
	needRefresh := false
	for {
		closed := make(chan struct{})
		var closeOnce sync.Once
		req := outer.request
		req.Updated = updated
		req.Sustain, req.BootstrapRead, req.ActiveChanged = false, nil, nil
		req.Closed = func() {
			closeOnce.Do(func() {
				st.mu.Lock()
				st.inner = nil
				st.trusted = false
				st.mu.Unlock()
				outer.mu.Lock()
				outer.id = NoSubscription
				outer.mu.Unlock()
				close(closed)
			})
		}
		st.mu.Lock()
		trusted := st.trusted
		st.mu.Unlock()
		if !trusted {
			if !c.provider.VerifyReachability(ctx) && ctx.Err() == nil {
				// Probing failed, so we get a new session anyway.
				c.log.Info("matter.controller.interaction.sustain_probe_failed", slog.Uint64("peer_node", outer.peer.NodeID))
			}
			if ctx.Err() != nil {
				return
			}
		}
		schedule := newRetries(DefaultSustainRetries, c.rand)
		for {
			inner, err := c.sustainAttempt(ctx, st, outer.peer, &req, floor, ceiling, &bootstrap, &needRefresh)
			if err == nil {
				st.mu.Lock()
				st.inner = inner
				st.trusted = true
				st.mu.Unlock()
				outer.mu.Lock()
				outer.id = inner.ID()
				outer.mu.Unlock()
				break
			}
			if ctx.Err() != nil {
				return
			}
			st.mu.Lock()
			st.trusted = true
			st.mu.Unlock()
			outer.setActive(false)
			retry := schedule.next()
			c.log.Info("matter.controller.interaction.sustain_retry",
				slog.Uint64("peer_node", outer.peer.NodeID), slog.Duration("retry", retry), slog.String("err", err.Error()))
			if !c.sleep(ctx, retry) {
				return
			}
		}
		outer.setActive(true)
		select {
		case <-closed:
		case <-ctx.Done():
			return
		}
		outer.setActive(false)
		if ctx.Err() != nil {
			return
		}
		c.log.Info("matter.controller.interaction.sustain_replacing", slog.Uint64("peer_node", outer.peer.NodeID))
	}
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
	return c.subscribeOnce(ctx, peer, *req, floor, ceiling, ExchangeOptions{ConnectionTimeout: -1})
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

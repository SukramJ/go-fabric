// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
)

// SubscriptionsConfig configures [NewSubscriptions].
type SubscriptionsConfig struct {
	// Clock paces the liveness timer; nil is the system clock.
	Clock exchange.Clock
	// Logger receives the diagnostics; nil discards them.
	Logger *slog.Logger
	// ReportStarted is called when a report the peer pushes starts
	// arriving on one of its subscriptions, with the session it arrived
	// over; a priming report does not call it, and a panic in it is
	// logged and contained — matter.js ClientSubscriptions.reportStarted
	// (ClientSubscriptions.ts:152-158).
	ReportStarted func(s *exchange.Session)
}

// Subscriptions is the controller's subscription registry: the
// user-facing subscriptions, the peer subscriptions by (peer,
// subscription id) the inbound reports are routed by, and the liveness
// timer. One serves a controller (one exchange manager). It is matter.js
// ClientSubscriptions (packages/protocol/src/action/client/subscription/ClientSubscriptions.ts:34-278
// at 0ceb202d). It is safe for concurrent use.
type Subscriptions struct {
	clock         exchange.Clock
	log           *slog.Logger
	reportStarted func(s *exchange.Session)

	// mu guards active, blocked, drained, peers and timer.
	mu       sync.Mutex
	active   map[*Subscription]struct{}
	peers    map[exchange.PeerAddress]map[uint32]*Subscription
	timer    exchange.Timer
	blocked  bool
	inFlight int
	drained  chan struct{}

	// readingCtx ends with BlockNewActivity: the in-flight report reads
	// stop (readingAbortSignal, :65-71).
	readingCtx    context.Context
	readingCancel context.CancelFunc
}

// NewSubscriptions returns an empty registry.
func NewSubscriptions(cfg SubscriptionsConfig) *Subscriptions {
	r := &Subscriptions{
		clock:         cfg.Clock,
		log:           cfg.Logger,
		reportStarted: cfg.ReportStarted,
		active:        make(map[*Subscription]struct{}),
		peers:         make(map[exchange.PeerAddress]map[uint32]*Subscription),
	}
	if r.clock == nil {
		r.clock = exchange.SystemClock{}
	}
	if r.log == nil {
		r.log = slog.New(slog.DiscardHandler)
	}
	r.readingCtx, r.readingCancel = context.WithCancel(context.Background())
	return r
}

// IsBlocked reports whether new report processing is blocked (:58-63).
func (r *Subscriptions) IsBlocked() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.blocked
}

// BlockNewActivity blocks new report processing, ends the reads in flight
// and waits for them — ClientSubscriptions.blockNewActivity (:73-95). A
// second call waits as well. It returns ctx's error when ctx ends first.
func (r *Subscriptions) BlockNewActivity(ctx context.Context) error {
	r.mu.Lock()
	if !r.blocked {
		r.blocked = true
		r.readingCancel()
		if r.timer != nil {
			r.timer.Stop()
		}
	}
	var wait chan struct{}
	if r.inFlight > 0 {
		if r.drained == nil {
			r.drained = make(chan struct{})
		}
		wait = r.drained
	}
	r.mu.Unlock()
	if wait == nil {
		return nil
	}
	select {
	case <-wait:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// addActive registers a user-facing subscription (:125-130).
func (r *Subscriptions) addActive(s *Subscription) {
	r.mu.Lock()
	r.active[s] = struct{}{}
	r.mu.Unlock()
}

// AddPeer registers a peer subscription by (peer, id) and restarts the
// liveness timer (:132-143).
func (r *Subscriptions) AddPeer(s *Subscription) {
	r.mu.Lock()
	forPeer := r.peers[s.peer]
	if forPeer == nil {
		forPeer = make(map[uint32]*Subscription)
		r.peers[s.peer] = forPeer
	}
	forPeer[s.ID()] = s
	r.mu.Unlock()
	r.ResetTimer()
}

// GetPeer returns the peer subscription with id, nil for none (:145-150).
func (r *Subscriptions) GetPeer(addr exchange.PeerAddress, id uint32) *Subscription {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peers[addr][id]
}

// CloseForPeer closes the peer subscriptions of addr, so they are
// re-established — after a session migrated to a new address
// (PeerAddressMonitor.ts:244-246), as ClientSubscriptions.closeForPeer
// does (:160-171).
func (r *Subscriptions) CloseForPeer(addr exchange.PeerAddress) {
	r.mu.Lock()
	subs := make([]*Subscription, 0, len(r.peers[addr]))
	for _, s := range r.peers[addr] {
		subs = append(subs, s)
	}
	r.mu.Unlock()
	if len(subs) == 0 {
		return
	}
	for _, s := range subs {
		s.Close()
	}
	r.ResetTimer()
}

// Delete unregisters a subscription (:173-184).
func (r *Subscriptions) Delete(s *Subscription) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if forPeer := r.peers[s.peer]; forPeer != nil && forPeer[s.ID()] == s {
		delete(forPeer, s.ID())
		if len(forPeer) == 0 {
			delete(r.peers, s.peer)
		}
	}
	delete(r.active, s)
}

// Active returns the user-facing subscriptions (the iterator of :186-191).
func (r *Subscriptions) Active() []*Subscription {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Subscription, 0, len(r.active))
	for s := range r.active {
		out = append(out, s)
	}
	return out
}

// Close terminates every subscription: new reads are blocked and the
// reads in flight drained, the timer stopped, every user-facing
// subscription closed and waited for — ClientSubscriptions.close
// (:193-219). It returns ctx's error when ctx ends first.
func (r *Subscriptions) Close(ctx context.Context) error {
	if err := r.BlockNewActivity(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	r.mu.Unlock()
	active := r.Active()
	for _, s := range active {
		s.Close()
	}
	for _, s := range active {
		select {
		case <-s.Done():
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// ResetTimer restarts the liveness timer for the peer subscriptions —
// ClientSubscriptions.resetTimer (:221-277): a subscription that is
// reading is skipped; one without a deadline gets now + its timeout; one
// past its deadline times out and closes; the timer is armed for the
// earliest deadline, or stopped when there is none or the registry is
// blocked.
func (r *Subscriptions) ResetTimer() {
	r.mu.Lock()
	if r.blocked {
		if r.timer != nil {
			r.timer.Stop()
		}
		r.mu.Unlock()
		return
	}
	now := r.clock.Now()
	var next time.Time
	var expired []*Subscription
	for _, forPeer := range r.peers {
		for _, s := range forPeer {
			s.mu.Lock()
			if s.isReading {
				s.mu.Unlock()
				continue
			}
			if s.timeoutAt.IsZero() {
				s.timeoutAt = now.Add(s.timeout())
			} else if !s.timeoutAt.After(now) {
				s.mu.Unlock()
				expired = append(expired, s)
				continue
			}
			at := s.timeoutAt
			s.mu.Unlock()
			if next.IsZero() || at.Before(next) {
				next = at
			}
		}
	}
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	if !next.IsZero() {
		r.timer = r.clock.AfterFunc(next.Sub(now), r.ResetTimer)
	}
	r.mu.Unlock()
	for _, s := range expired {
		s.timedOut()
	}
}

// Subscription is a subscription with a peer. A plain one is the Matter
// subscription the peer granted — matter.js PeerSubscription
// (PeerSubscription.ts:15-54) over ClientSubscription
// (ClientSubscription.ts:19-101); a sustained one keeps re-establishing
// its underlying subscription ([SubscribeRequest.Sustain]).
type Subscription struct {
	reg     *Subscriptions
	peer    exchange.PeerAddress
	request SubscribeRequest
	log     *slog.Logger

	// ctx is the subscription's abort (ClientSubscription.ts:41-44): Close
	// cancels it.
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	// closed is the registry's callback (CI:1058, :1101).
	closed func()

	// mu guards isClosed.
	mu                  sync.Mutex
	id                  uint32
	maxInterval         uint16
	maxPeerResponseTime time.Duration
	isReading           bool
	timeoutAt           time.Time
	isClosed            bool

	// sustained is the state of a sustained subscription, nil for a
	// plain one.
	sustained *sustainedState
}

// NoSubscription is the id of a subscription the peer has not granted
// yet — ClientSubscription.NO_SUBSCRIPTION (ClientSubscription.ts:34,
// :104).
const NoSubscription = ^uint32(0)

// ID is the subscription id; [NoSubscription] for a sustained subscription
// between underlying ones.
func (s *Subscription) ID() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// Peer is the peer's address.
func (s *Subscription) Peer() exchange.PeerAddress { return s.peer }

// MaxInterval is the maximum interval the peer granted; for a sustained
// subscription the underlying one's, one hour without one
// (SustainedSubscription.ts:426-428).
func (s *Subscription) MaxInterval() time.Duration {
	if s.sustained != nil {
		if inner := s.sustained.current(); inner != nil {
			return inner.MaxInterval()
		}
		return time.Hour
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Duration(s.maxInterval) * time.Second
}

// Done is closed once the subscription closed and its Closed callback ran.
func (s *Subscription) Done() <-chan struct{} { return s.done }

// timeout is the liveness timeout: the maximum interval plus twice the
// maximum peer response time (PeerSubscription.ts:38-40).
func (s *Subscription) timeout() time.Duration {
	return time.Duration(s.maxInterval)*time.Second + 2*s.maxPeerResponseTime
}

// timedOut closes a subscription past its deadline (PeerSubscription.ts:42-53).
func (s *Subscription) timedOut() {
	s.log.Info("matter.controller.interaction.subscription_timed_out",
		slog.Uint64("subscription", uint64(s.ID())), slog.Int("fabric_index", int(s.peer.FabricIndex)),
		slog.Uint64("peer_node", s.peer.NodeID), slog.Duration("timeout", s.timeout()))
	s.Close()
}

// Close closes the subscription: its work is aborted, it leaves the
// registry and the request's Closed callback runs — after a sustained
// subscription's loop ended. The peer is told nothing
// (ClientRequest.ts:60-61): it learns from the InvalidSubscription its
// next report gets. Mirrors ClientSubscription.close
// (ClientSubscription.ts:60-92). It is idempotent.
func (s *Subscription) Close() {
	s.mu.Lock()
	if s.isClosed {
		s.mu.Unlock()
		return
	}
	s.isClosed = true
	s.mu.Unlock()
	s.cancel()
	if s.closed != nil {
		s.closed()
	}
	finish := func() {
		if s.request.Closed != nil {
			s.request.Closed()
		}
		close(s.done)
	}
	if s.sustained != nil {
		// A sustained subscription's worker ends first (:76-82).
		go func() {
			<-s.sustained.finished
			finish()
		}()
		return
	}
	finish()
}

// newSubscription builds a subscription of the registry.
func (r *Subscriptions) newSubscription(peer exchange.PeerAddress, req SubscribeRequest, log *slog.Logger) *Subscription {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Subscription{reg: r, peer: peer, request: req, log: log, ctx: ctx, cancel: cancel, done: make(chan struct{}), id: NoSubscription}
	s.closed = func() { r.Delete(s) }
	return s
}

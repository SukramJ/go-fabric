// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/clock"
)

// The reboot armer's timings (RebootResubscribeArmer.ts:18-30).
const (
	// RebootResubscribeGrace is the grace after the returning device's new
	// session before its subscriptions are forced to re-subscribe.
	RebootResubscribeGrace = 30 * time.Second
	// rebootExpectedReturn is how long a device may take to come back
	// after it applies (EXPECTED_RETURN_TIMEOUT).
	rebootExpectedReturn = 3 * time.Minute
	// rebootMaxApplyDelay is the longest apply delay a device waits; a
	// requestor may treat a longer one as this (MAX_APPLY_DELAY).
	rebootMaxApplyDelay = 24 * time.Hour
)

// RebootReturnTimeout is how long an armed device may take to return: the
// apply delay, at least 0 and at most a day, plus three minutes. Mirrors
// matter.js RebootResubscribeArmer.ts:102-104 (returnTimeout).
func RebootReturnTimeout(applyDelay time.Duration) time.Duration {
	return min(max(applyDelay, 0), rebootMaxApplyDelay) + rebootExpectedReturn
}

// ArmerSession is a secure session as the reboot armer sees it.
type ArmerSession interface {
	// Peer is the session's peer.
	Peer() exchange.PeerAddress
	// Initiator reports a session we opened.
	Initiator() bool
	// CreatedAt is the session's creation time.
	CreatedAt() time.Time
}

// RebootArmerContext is what the reboot armer acts on — matter.js's
// SessionManager and ClientSubscriptions.
type RebootArmerContext struct {
	// Clock runs the deadlines; nil is the system clock.
	Clock clock.Clock
	// ClosePeerSessionsBefore closes the peer's sessions created before t,
	// the dead pre-reboot ones (SessionManager.handlePeerShutdown).
	ClosePeerSessionsBefore func(peer exchange.PeerAddress, t time.Time)
	// PeerLost gives every session of the peer up as unresponsive
	// (SessionManager.handlePeerLoss).
	PeerLost func(peer exchange.PeerAddress)
	// CloseSubscriptions closes our subscriptions with the peer, so the
	// sustained ones re-subscribe (ClientSubscriptions.closeForPeer).
	CloseSubscriptions func(peer exchange.PeerAddress)
	// Logger receives the lines; nil is slog.Default.
	Logger *slog.Logger
}

type armState struct {
	returned    bool
	sessions    []ArmerSession // opened since the return, the announcing one first
	reported    bool
	graceTimer  clock.Timer
	returnTimer clock.Timer
	gen         uint64
}

// RebootResubscribeArmer re-subscribes a peer that restarts after an OTA
// apply: armed for a peer, it waits for the peer to return — its first
// session it opens towards us — closes the older sessions, and closes the
// peer's subscriptions unless a report arrives over a post-return session
// within the grace window; a peer that does not return in time is given
// up as unresponsive and its subscriptions closed. The host wires
// SessionAdded and ReportStarted to its session manager and subscription
// registry; the OTA provider path arms a peer when it lets it apply.
// Safe for concurrent use.
//
// Mirrors matter.js packages/protocol/src/peer/RebootResubscribeArmer.ts:58-213.
type RebootResubscribeArmer struct {
	ctx   RebootArmerContext
	clock clock.Clock
	log   *slog.Logger

	// mu guards armed and gen; no context function runs under it.
	mu    sync.Mutex
	armed map[exchange.PeerAddress]*armState
	gen   uint64
}

// NewRebootResubscribeArmer returns an armer over ctx.
func NewRebootResubscribeArmer(ctx RebootArmerContext) *RebootResubscribeArmer {
	log := ctx.Logger
	if log == nil {
		log = slog.Default()
	}
	return &RebootResubscribeArmer{ctx: ctx, clock: clock.Or(ctx.Clock), log: log, armed: map[exchange.PeerAddress]*armState{}}
}

// Arm expects peer to restart within applyDelay and the return timeout,
// replacing a previous arming of it (RebootResubscribeArmer.ts:76-96).
func (a *RebootResubscribeArmer) Arm(peer exchange.PeerAddress, applyDelay time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if prev := a.armed[peer]; prev != nil {
		stopTimers(prev)
	}
	a.gen++
	st := &armState{gen: a.gen}
	a.armed[peer] = st
	gen := st.gen
	st.returnTimer = a.clock.AfterFunc(RebootReturnTimeout(applyDelay), func() { a.returnTimedOut(peer, gen) })
}

// Disarm forgets peer; a no-op for a peer not armed
// (RebootResubscribeArmer.ts:106-115).
func (a *RebootResubscribeArmer) Disarm(peer exchange.PeerAddress) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.disarmLocked(peer)
}

func (a *RebootResubscribeArmer) disarmLocked(peer exchange.PeerAddress) {
	if st := a.armed[peer]; st != nil {
		stopTimers(st)
		delete(a.armed, peer)
	}
}

func stopTimers(st *armState) {
	if st.graceTimer != nil {
		st.graceTimer.Stop()
	}
	if st.returnTimer != nil {
		st.returnTimer.Stop()
	}
}

// SessionAdded is a new session with a peer: the first one an armed peer
// opens announces its return, so its older sessions close and the grace
// window starts; a later one joins the return's evidence; one we opened
// announces nothing (RebootResubscribeArmer.ts:117-150).
func (a *RebootResubscribeArmer) SessionAdded(s ArmerSession) {
	peer := s.Peer()
	a.mu.Lock()
	st := a.armed[peer]
	if st == nil {
		a.mu.Unlock()
		return
	}
	if st.returned {
		// The device opens further sessions for its own reasons; the grace
		// window it runs in stands.
		st.sessions = append(st.sessions, s)
		a.mu.Unlock()
		return
	}
	if s.Initiator() {
		a.mu.Unlock()
		return
	}
	if st.returnTimer != nil {
		st.returnTimer.Stop()
		st.returnTimer = nil
	}
	st.returned, st.sessions, st.reported = true, []ArmerSession{s}, false
	gen := st.gen
	st.graceTimer = a.clock.AfterFunc(RebootResubscribeGrace, func() { a.graceExpired(peer, gen) })
	a.mu.Unlock()
	if fn := a.ctx.ClosePeerSessionsBefore; fn != nil {
		fn(peer, s.CreatedAt())
	}
}

// ReportStarted is a report over a session: one over a post-return
// session keeps the subscriptions (RebootResubscribeArmer.ts:165-172).
func (a *RebootResubscribeArmer) ReportStarted(s ArmerSession) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.armed[s.Peer()]
	if st == nil || !st.returned {
		return
	}
	for _, x := range st.sessions {
		if x == s {
			st.reported = true
			return
		}
	}
}

// graceExpired closes the subscriptions of a peer no post-return report
// reached (RebootResubscribeArmer.ts:152-163).
func (a *RebootResubscribeArmer) graceExpired(peer exchange.PeerAddress, gen uint64) {
	a.mu.Lock()
	st := a.armed[peer]
	if st == nil || st.gen != gen {
		a.mu.Unlock()
		return
	}
	reported := st.reported
	a.disarmLocked(peer)
	a.mu.Unlock()
	if !reported {
		if fn := a.ctx.CloseSubscriptions; fn != nil {
			fn(peer)
		}
	}
}

// returnTimedOut recovers a peer that did not return: its sessions are
// given up and its subscriptions closed (RebootResubscribeArmer.ts:174-204).
func (a *RebootResubscribeArmer) returnTimedOut(peer exchange.PeerAddress, gen uint64) {
	a.mu.Lock()
	st := a.armed[peer]
	if st == nil || st.gen != gen || st.returned {
		a.mu.Unlock()
		return
	}
	a.disarmLocked(peer)
	a.mu.Unlock()
	a.log.Info("matter.controller.peer.reboot_return_timeout", slog.String("peer", addressString(peer)),
		slog.String("reason", "no session after the expected reboot; assuming it restarted, reconnecting"))
	if fn := a.ctx.PeerLost; fn != nil {
		fn(peer)
	}
	if fn := a.ctx.CloseSubscriptions; fn != nil {
		fn(peer)
	}
}

// Close disarms every peer (RebootResubscribeArmer.ts:206-213).
func (a *RebootResubscribeArmer) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for p := range a.armed {
		a.disarmLocked(p)
	}
}

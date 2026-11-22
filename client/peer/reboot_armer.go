// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"log/slog"
	"slices"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/clock"
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

// ArmerSession is a secure session as the reboot armer sees it. The armer
// compares sessions with ==, so an implementation is a comparable value
// that is equal for the same session wherever it is handed over — a
// pointer, or a struct of the session's pointer.
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
	// the dead pre-reboot ones (SessionManager.handlePeerShutdown with a
	// Timestamp). Used only while ClosePeerSessionsRegisteredBefore is nil.
	//
	// Deprecated: two sessions may have the same creation time, and the
	// older one then survives the return; set
	// ClosePeerSessionsRegisteredBefore, which cuts by registration order,
	// as matter.js deprecates the Timestamp form (SessionManager.ts:719-721
	// at b25ed960). Removable in v0.26.0.
	ClosePeerSessionsBefore func(peer exchange.PeerAddress, t time.Time)
	// ClosePeerSessionsRegisteredBefore closes the peer's sessions that
	// registered before keep — the dead pre-reboot ones — and keeps keep
	// and every session registered after it, whatever their creation
	// times (SessionManager.handlePeerShutdown with a session,
	// exchange.Manager.HandlePeerLossBefore).
	ClosePeerSessionsRegisteredBefore func(peer exchange.PeerAddress, keep ArmerSession)
	// SessionsRegisteredSince returns the open sessions with the peer of s
	// that registered at or after it, s among them, also those whose
	// handshake still runs; none for a session that never registered
	// (SessionManager.sessionsRegisteredSince,
	// exchange.Manager.SessionsRegisteredSince). Nil: only the sessions
	// handed to SessionAdded count as the return's.
	SessionsRegisteredSince func(s ArmerSession) []ArmerSession
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
	returned bool
	// sessions are the return's: the announcing one first, those
	// registered since it when it was handed over, and those established
	// after it.
	sessions []ArmerSession
	// announcing is the session that announced the return; a session
	// registered after it is the return's whenever it reports.
	announcing ArmerSession
	reported   bool
	// reportedBeforeReturn are the sessions that carried a report before
	// the return: a returning device may send its priming report before
	// its announcing session is established (RebootResubscribeArmer.ts:33-37
	// at b25ed960).
	reportedBeforeReturn []ArmerSession
	graceTimer           clock.Timer
	returnTimer          clock.Timer
	gen                  uint64
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

// SessionAdded is a newly established session with a peer: the first one
// an armed peer opens announces its return, so the sessions registered
// before it close and the grace window starts; it and the sessions
// registered since it are the return's, and the return counts as
// reported when one of them carried a report before it; a later one joins
// the return's evidence; one we opened announces nothing. Mirrors matter.js
// packages/protocol/src/peer/RebootResubscribeArmer.ts:127-131 at b25ed960
// (#onSessionRegistered) and :133-164 (#onSessionAdded).
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
	// The sessions registered since s run a handshake with the returned
	// device, or are ours to it: read outside the lock, as no context
	// function runs under it.
	a.mu.Unlock()
	var since []ArmerSession
	if fn := a.ctx.SessionsRegisteredSince; fn != nil {
		since = fn(s)
	}
	a.mu.Lock()
	if a.armed[peer] != st || st.returned {
		// Re-armed, disarmed or returned meanwhile.
		a.mu.Unlock()
		return
	}
	sessions := []ArmerSession{s}
	for _, x := range since {
		if !slices.Contains(sessions, x) {
			sessions = append(sessions, x)
		}
	}
	reported := slices.ContainsFunc(sessions, func(x ArmerSession) bool { return slices.Contains(st.reportedBeforeReturn, x) })
	st.returned, st.sessions, st.announcing, st.reported, st.reportedBeforeReturn = true, sessions, s, reported, nil
	gen := st.gen
	st.graceTimer = a.clock.AfterFunc(RebootResubscribeGrace, func() { a.graceExpired(peer, gen) })
	a.mu.Unlock()
	switch {
	case a.ctx.ClosePeerSessionsRegisteredBefore != nil:
		a.ctx.ClosePeerSessionsRegisteredBefore(peer, s)
	case a.ctx.ClosePeerSessionsBefore != nil:
		a.ctx.ClosePeerSessionsBefore(peer, s.CreatedAt())
	}
}

// ReportStarted is a report over a session: one over a session of the
// return keeps the subscriptions; one before the return is remembered, and
// counts once its session turns out to be the return's. A session
// registered after the announcing one is the return's as matter.js adds
// it on registration (#onSessionRegistered), read here from
// SessionsRegisteredSince when it reports. Mirrors matter.js
// packages/protocol/src/peer/RebootResubscribeArmer.ts:179-193 at b25ed960
// (#onReportStarted).
func (a *RebootResubscribeArmer) ReportStarted(s ArmerSession) {
	peer := s.Peer()
	a.mu.Lock()
	st := a.armed[peer]
	if st == nil {
		a.mu.Unlock()
		return
	}
	if !st.returned {
		if !slices.Contains(st.reportedBeforeReturn, s) {
			st.reportedBeforeReturn = append(st.reportedBeforeReturn, s)
		}
		a.mu.Unlock()
		return
	}
	if slices.Contains(st.sessions, s) {
		st.reported = true
		a.mu.Unlock()
		return
	}
	announcing := st.announcing
	a.mu.Unlock()
	fn := a.ctx.SessionsRegisteredSince
	if fn == nil || !slices.Contains(fn(announcing), s) {
		return
	}
	a.mu.Lock()
	if a.armed[peer] == st {
		st.sessions = append(st.sessions, s)
		st.reported = true
	}
	a.mu.Unlock()
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

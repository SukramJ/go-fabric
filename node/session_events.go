// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import "sync"

// sessionEvents turns the session table's and the subscription engine's
// changes into the Observers' session hooks, as matter.js's
// SessionsBehavior turns its SessionManager's (packages/node/src/behavior/
// system/sessions/SessionsBehavior.ts:49-93 at 862ad75f): a CASE session
// is announced when it opens and remembered, so its close and its
// subscription changes are reported only for a session that was
// announced; a PASE session is never.
type sessionEvents struct {
	// n is the node, set once New built the security layer.
	n *Node

	// mu guards known and announcing.
	mu sync.Mutex
	// known are the announced sessions, by id — matter.js's
	// state.sessions.
	known map[uint16]SessionInfo
	// announcing are the sessions opened is announcing, by id; true once
	// a close arrived meanwhile, which opened then reports after the open.
	announcing map[uint16]bool
}

// info is the session id as the facade reports it now; ok is false for a
// session the table does not hold.
func (e *sessionEvents) info(id uint16) (SessionInfo, bool) {
	n := e.n
	if n == nil || n.sec == nil {
		return SessionInfo{}, false
	}
	subs := n.subscriptionIDs()
	for _, s := range n.sec.sessions.Sessions() {
		if s.SessionID == id {
			return sessionInfoOf(s, subs[id], n.Clock().Now()), true
		}
	}
	return SessionInfo{}, false
}

// opened announces a session a handshake established (#sessionOpened,
// SessionsBehavior.ts:57-64): a PASE session is skipped. The session can
// close on another goroutine at any point: one that closed before opened
// recorded it — the table no longer holds it then, and closed found
// nothing to report — is dropped unannounced, and one that closes while
// its open is announced is reported closed after the open, so every
// observer sees an open before its close and no session stays recorded
// as open (TestSessionClosedWhileOpenedRecordsItLeavesNoOpenSession,
// TestSessionClosedDuringItsOpenHookReportsOpenBeforeClose).
func (e *sessionEvents) opened(id uint16) {
	info, ok := e.info(id)
	if !ok || info.PASE {
		return
	}
	e.mu.Lock()
	if e.known == nil {
		e.known = map[uint16]SessionInfo{}
	}
	if e.announcing == nil {
		e.announcing = map[uint16]bool{}
	}
	e.known[id] = info
	e.announcing[id] = false
	e.mu.Unlock()
	// The table drops a session before its close hook runs: one gone now
	// closed before or while it was recorded.
	if !e.inTable(id) {
		e.mu.Lock()
		delete(e.known, id)
		delete(e.announcing, id)
		e.mu.Unlock()
		return
	}
	if n := e.n; n != nil {
		n.operationalConnected(info.FabricIndex)
	}
	e.emit("session_opened", info, func(o Observers) func(SessionInfo) { return o.OnSessionOpened })
	e.mu.Lock()
	closedMeanwhile := e.announcing[id]
	delete(e.announcing, id)
	last := e.known[id]
	if closedMeanwhile {
		delete(e.known, id)
	}
	e.mu.Unlock()
	if closedMeanwhile {
		e.emit("session_closed", last, func(o Observers) func(SessionInfo) { return o.OnSessionClosed })
	}
}

// inTable reports whether the session table still holds id.
func (e *sessionEvents) inTable(id uint16) bool {
	n := e.n
	if n == nil || n.sec == nil {
		return false
	}
	_, err := n.sec.sessions.Get(id)
	return err == nil
}

// closed reports an announced session's close with its last state
// (#sessionClosed, SessionsBehavior.ts:66-73); a session whose open is
// still being announced has its close reported by opened, after the open.
func (e *sessionEvents) closed(id uint16) {
	e.mu.Lock()
	if _, pending := e.announcing[id]; pending {
		e.announcing[id] = true
		e.mu.Unlock()
		return
	}
	info, ok := e.known[id]
	delete(e.known, id)
	e.mu.Unlock()
	if !ok {
		return
	}
	e.emit("session_closed", info, func(o Observers) func(SessionInfo) { return o.OnSessionClosed })
}

// subscriptionsChanged reports an announced session's new subscription
// count (#subscriptionsChanged, SessionsBehavior.ts:75-93).
func (e *sessionEvents) subscriptionsChanged(id uint16) {
	e.mu.Lock()
	info, ok := e.known[id]
	e.mu.Unlock()
	if !ok {
		return
	}
	if now, live := e.info(id); live {
		info = now
	} else if n := e.n; n != nil && n.sec != nil && n.sec.subs != nil {
		ids := n.subscriptionIDs()[id]
		info.Subscriptions, info.SubscriptionIDs = len(ids), ids
	}
	e.mu.Lock()
	if _, still := e.known[id]; still {
		e.known[id] = info
	}
	e.mu.Unlock()
	e.emit("subscriptions_changed", info, func(o Observers) func(SessionInfo) { return o.OnSubscriptionsChanged })
}

// emit runs every observer's hook for event, after the facade's own
// handling, as the other Observers run.
func (e *sessionEvents) emit(event string, info SessionInfo, hook func(Observers) func(SessionInfo)) {
	n := e.n
	if n == nil {
		return
	}
	for _, o := range n.observerList() {
		if fn := hook(o); fn != nil {
			observe(n.logger, event, func() { fn(info) })
		}
	}
}

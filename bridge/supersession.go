// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"log/slog"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// SessionSuperseded tells the bridge that secure session newerID, just
// established, supersedes olderID: a CASE session of the same subscriber
// with the same CASE Authenticated Tags. The host calls it from the session
// registry's supersession hook (operational.Manager.SetOnSessionSuperseded),
// which fires before the eviction that establishment causes closes
// anything. The subscriptions of olderID move onto newerID in the attached
// subscription manager ([subscription.Manager.MoveSession]) and their
// routes with them: each reports on newerID from its next report on, keeps
// its id, request and intervals, and starts its count of failed updates
// again. olderID stays open; its close no longer reaches the moved
// subscriptions. A report already under way on olderID that fails there
// is not counted and is resent at once on newerID.
//
// Mirrors matter.js packages/node/src/node/server/ServerSubscription.ts
// :819-834 at b25ed960 (#follow: the session changes, the subscription
// joins the newer session, #sendUpdateErrorCounter = 0 — "Failures on the
// former session say nothing about the new one") and :569-577 (a failure on
// the session it left is no error count, the update is resent at once).
// Calling it for sessions that carry no subscription is a no-op.
//
// The hook names both sessions by id outside the registry's lock, so the
// method checks what it can: nothing moves when newerID resolves to
// another fabric or subject node than the subscriptions on olderID, and
// what moved onto a newerID the session lookup no longer holds — closed
// before the move, its close hook already run — is closed here.
func (b *Bridge) SessionSuperseded(olderID, newerID uint16) {
	if olderID == newerID {
		return
	}
	// The manager moves first, then the routes of the subscriptions it
	// moved. A route stored meanwhile — the capture after a Subscribe or a
	// Restore — is re-pointed by whichever of this loop and
	// [Bridge.followMovedSession] runs after it: both take subUpdateMu
	// after the manager's move. The manager's change hook fires outside
	// the bridge's locks.
	m := b.subscriptionManagerLocked()
	if m == nil {
		return
	}
	if !b.supersedingSubjectMatches(m, olderID, newerID) {
		return
	}
	ids := m.MoveSession(olderID, newerID)
	// A newer session closed between its establishment and this call has
	// had its close hook already, which found nothing on it yet: what just
	// moved onto it closes here, as the close would have closed it. Read
	// after the move, so a close after this point finds the moved
	// subscriptions itself.
	if len(ids) > 0 && !b.sessionLive(newerID) {
		m.CloseSession(newerID)
		return
	}
	b.subUpdateMu.Lock()
	var moved int
	for _, subID := range ids {
		if b.followMovedSessionLocked(m, subID) {
			moved++
		}
	}
	b.subUpdateMu.Unlock()
	if moved > 0 && b.logger != nil {
		b.logger.Debug("matter.subscribe.session_superseded",
			slog.Int("from_session_id", int(olderID)),
			slog.Int("to_session_id", int(newerID)),
			slog.Int("subscription_count", moved))
	}
}

// sessionLive reports whether the session lookup still holds sessionID;
// true without a lookup, which cannot tell.
func (b *Bridge) sessionLive(sessionID uint16) bool {
	b.mu.RLock()
	sessions := b.sessions
	b.mu.RUnlock()
	if sessions == nil {
		return true
	}
	_, ok := sessions.Lookup(sessionID)
	return ok
}

// supersedingSubjectMatches reports whether newerID's fabric and subject
// node are those of every subscription on olderID. The session registry
// established newerID as a session of the same subject, but the hook names
// both by id outside its lock: an id freed and allocated again in between
// would name another subject's session, and nothing moves onto it. A
// session the bridge cannot resolve — closed, or no resolver wired — is
// not judged here.
func (b *Bridge) supersedingSubjectMatches(m *subscription.Manager, olderID, newerID uint16) bool {
	fabricIndex := b.resolveSessionFabric(newerID)
	nodeID, _ := b.resolveSessionSubject(newerID)
	if fabricIndex == 0 || fabricIndex == endpoint.FabricIndexUnresolvable || nodeID == 0 {
		return true
	}
	for _, info := range m.Snapshot() {
		if info.SessionID == olderID && (info.FabricIndex != fabricIndex || info.PeerNodeID != nodeID) {
			if b.logger != nil {
				b.logger.Warn("matter.subscribe.session_superseded.subject_mismatch",
					slog.Int("from_session_id", int(olderID)),
					slog.Int("to_session_id", int(newerID)),
					slog.Int("subscription_id", int(info.ID)))
			}
			return false
		}
	}
	return true
}

// supersededTarget is target re-pointed at newerID. The subject — fabric,
// node id, CATs — is the same on newerID by the supersession rule, and the
// address is read per session at every send ([Bridge.currentPeerAddr]);
// the fallback address becomes newerID's. The DestNodeID echo
// (peerSourceNodeID) stays: it names the subscriber's node, which newerID
// shares. Ongoing reports open a fresh exchange each, so the Subscribe
// exchange the route remembers is not used on newerID.
func (b *Bridge) supersededTarget(target subTarget, newerID uint16) subTarget {
	target.src = b.currentPeerAddr(newerID, target.src)
	target.sessionID = newerID
	return target
}

// followMovedSession re-points subID's route at the session the
// subscription reports on when it moved there before the route was stored
// — a supersession between the manager's Subscribe or Restore and the
// route's capture.
func (b *Bridge) followMovedSession(m *subscription.Manager, subID uint32) {
	if m == nil {
		return
	}
	b.subUpdateMu.Lock()
	defer b.subUpdateMu.Unlock()
	b.followMovedSessionLocked(m, subID)
}

// followMovedSessionLocked re-points subID's route at the session the
// subscription reports on in m, when it differs, and starts the count of
// failed updates again; it reports whether it did. The caller holds
// subUpdateMu.
func (b *Bridge) followMovedSessionLocked(m *subscription.Manager, subID uint32) bool {
	sub, err := m.Get(subID)
	if err != nil {
		return false
	}
	raw, ok := b.routing.subTargets.Load(subID)
	if !ok {
		return false
	}
	target, ok := raw.(subTarget)
	current := sub.CurrentSessionID()
	if !ok || current == target.sessionID {
		return false
	}
	b.routing.subTargets.Store(subID, b.supersededTarget(target, current))
	// Failures on the former session say nothing about the new one
	// (matter.js ServerSubscription.ts:830-831 at b25ed960).
	b.subSendErrorCount.Delete(subID)
	return true
}

// requeueLeftSession owes what a report carried again after it failed on
// a session sub has left since it went out: no failed update is counted —
// neither the bridge's count nor the subscription's — and the subscription
// engine resends it at once on the session sub reports on now
// ([subscription.Subscription.RequeueAbortedAttributes]). Mirrors matter.js
// packages/node/src/node/server/ServerSubscription.ts:569-577 at b25ed960
// (#requeueUpdate, #sendNextUpdateImmediately, no #sendUpdateErrorCounter).
func (b *Bridge) requeueLeftSession(sub *subscription.Subscription, owed *owedReport, op string, err error) {
	if b.logger != nil {
		b.logger.Debug("matter.tx.subscribe."+op+".left_session",
			slog.Int("subscription_id", int(sub.ID)),
			slog.Any("err", err))
	}
	owed.requeueAborted(sub)
}

// leftSession reports whether subID's route has moved off sessionID, the
// session one of its reports went out on, since that report was sent: a
// failure there says nothing about the session the subscription reports
// on now. Mirrors matter.js
// packages/node/src/node/server/ServerSubscription.ts:569-577 at b25ed960
// (sentOn !== this.session).
func (b *Bridge) leftSession(subID uint32, sessionID uint16) bool {
	raw, ok := b.routing.subTargets.Load(subID)
	if !ok {
		return false
	}
	target, ok := raw.(subTarget)
	return ok && target.sessionID != sessionID
}

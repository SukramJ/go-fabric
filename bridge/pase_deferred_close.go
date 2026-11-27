// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/transport/message"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// paseCloseDeferralCap bounds how long the graceful close of a revoked PASE
// session waits for the session to become idle ([Bridge.paseSessionActive]):
// past it the close runs anyway, its CloseSession still sent. matter.js has
// no such bound — its deferred close waits for the session's last exchange
// however long that takes (packages/protocol/src/session/Session.ts:304-320,
// packages/protocol/src/session/NodeSession.ts:438-462 at db91886c), each
// exchange bounded only by its own timers. The cap is this module's (owner
// decision W6, 2026-11-22): the active set here is assembled from several
// tables, and a leak in one of them must not pin a revoked PASE session —
// with the commissioning channel's implicit Administer — open.
const paseCloseDeferralCap = 60 * time.Second

// paseDeferredClose is the graceful close of one revoked PASE session that
// waits for the session to become idle. Guarded by the bridge's mu.
type paseDeferredClose struct {
	// cap runs the close at [paseCloseDeferralCap].
	cap clock.Timer
	// timed re-checks the session when its latest timed window elapses.
	timed clock.Timer
}

// closePASESessionsOnRevoke is what a [CommissioningWindow] attached to the
// bridge runs when it is revoked: the PASE sessions open at that moment —
// the ones AddNOC adopted onto a fabric included, those already closing
// left out — are closed gracefully, each when it has become idle
// ([Bridge.paseSessionActive]): a CloseSession on its session while the
// keys are live, then the session closes, by id. A session idle at the
// revoke (the revoke came over CASE and the PASE session is quiet) closes
// at once, before the revoke's response. Only the sessions named here
// close: a PASE session a new commissioning establishes while a close is
// deferred is not this revoke's. The host's [PaseSessionCloser] is the
// window's, at the revoke ([CommissioningWindow.RevokeWindow]), not part of
// this close. Unlike CommissioningComplete and the fail-safe expiry (force
// closes, [Bridge.closePASESessions]), the revoke's close is matter.js's
// graceful one.
//
// Mirrors matter.js
// packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:143-154
// at db91886c (revokeCommissioning, step 1: `getPaseSession()` →
// `paseSession.initiateClose()`), whose close is deferred while the session
// has an active exchange:
// packages/protocol/src/session/Session.ts:304-320 at db91886c (initiateClose),
// packages/protocol/src/session/NodeSession.ts:438-448 at db91886c (deferredClose
// set when hasActiveExchanges, Session.ts:381-383), and runs when the last
// exchange closes:
// packages/protocol/src/session/NodeSession.ts:455-462 at db91886c.
// The graceful close's event is observed by
// packages/protocol/src/protocol/ExchangeManager.ts:757-768 at db91886c (gracefulClose)
// and answered with a CloseSession on the closing session by
// packages/protocol/src/protocol/ExchangeManager.ts:790-800 at db91886c (#sendCloseSession)
// (F-b25ed960-56).
//
// ctx is the revoke's: when it is the context of an interaction on one of
// the PASE sessions (a RevokeCommissioning over PASE), that interaction is
// a handler turn on its session until its response is out
// ([im.DeferAfterResponse]) — the receive path counts it as well
// ([Bridge.enterSessionTurn]); a caller outside the receive path is held
// the same way.
func (b *Bridge) closePASESessionsOnRevoke(ctx context.Context) {
	ids := b.markPASECloses(b.listPASESessions())
	if len(ids) == 0 {
		return
	}
	if im.IsPASEFromContext(ctx) {
		// The interaction's session, or — a context that names none —
		// every one of them: it is one of the PASE sessions.
		invoking := core.InvokeSessionIDFromContext(ctx)
		for _, id := range ids {
			if invoking != 0 && id != invoking {
				continue
			}
			turn := b.enterSessionTurn(id)
			if !im.DeferAfterResponse(ctx, func() { b.exitSessionTurn(id, turn) }) {
				b.exitSessionTurn(id, turn)
			}
		}
	}
	for _, id := range ids {
		capTimer := b.clk().AfterFunc(paseCloseDeferralCap, func() { b.finishPASEClose(id, "deferral_cap") })
		b.mu.Lock()
		if st := b.paseDeferred[id]; st != nil && st.cap == nil {
			st.cap = capTimer
		} else {
			capTimer.Stop()
		}
		b.mu.Unlock()
		b.recheckPASEClose(id)
	}
}

// markPASECloses marks each of ids closing with a deferred close and
// returns those it marked: an id another revoke marked first — two revokes
// can list the same session before either marks it — is that revoke's, and
// is left out.
func (b *Bridge) markPASECloses(ids []uint16) []uint16 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.paseClosing == nil {
		b.paseClosing = make(map[uint16]struct{})
	}
	if b.paseDeferred == nil {
		b.paseDeferred = make(map[uint16]*paseDeferredClose)
	}
	marked := ids[:0:0]
	for _, id := range ids {
		// paseClosing holds every deferred id and every id a force close
		// waits to close (the fail-safe's rollback, CommissioningComplete):
		// a closing session is not closed again (Session.ts:305-307 at db91886c).
		if _, closing := b.paseClosing[id]; closing {
			continue
		}
		b.paseClosing[id] = struct{}{}
		b.paseDeferred[id] = &paseDeferredClose{}
		b.paseDeferredN.Add(1)
		marked = append(marked, id)
	}
	return marked
}

// dropPASECloses forgets every deferred close and stops its timers: the
// bridge stops, and its shutdown closes the sessions itself
// ([Bridge.Stop]).
func (b *Bridge) dropPASECloses() {
	b.mu.RLock()
	ids := make([]uint16, 0, len(b.paseDeferred))
	for id := range b.paseDeferred {
		ids = append(ids, id)
	}
	b.mu.RUnlock()
	for _, id := range ids {
		b.dropPASEClose(id)
	}
}

// paseCloseDeferred reports whether sessionID is a PASE session whose
// graceful close waits for it to become idle.
func (b *Bridge) paseCloseDeferred(sessionID uint16) bool {
	if sessionID == 0 || b.paseDeferredN.Load() == 0 {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.paseDeferred[sessionID]
	return ok
}

// paseSessionActive reports whether sessionID has an exchange in flight, the
// Go union of matter.js's `hasActiveExchanges` (every MessageExchange of the
// session, packages/protocol/src/session/Session.ts:381-383,
// packages/protocol/src/protocol/MessageExchange.ts:369 at db91886c): (a) a
// handler turn running on it ([Bridge.enterSessionTurn]), (b) a message this
// node sent on it still waiting for its acknowledgement — retransmitted
// until acknowledged or given up, as matter.js holds an exchange whose last
// message is unacknowledged
// (packages/protocol/src/protocol/MessageExchange.ts:1187-1216 at db91886c),
// (c) a timed window not yet elapsed or a chunked write still open. timedUntil
// is the latest timed deadline when (c)'s timed windows are all that keep
// it active. Two holds have no trigger of their own and end at
// [paseCloseDeferralCap]: a stalled chunked write, counted until a later
// write sweeps it, and a message dropped from the retransmit table
// outside a turn of the session (an Untrack after a failed send).
func (b *Bridge) paseSessionActive(sessionID uint16) (active bool, timedUntil time.Time) {
	if b.sessionTurnsOf(sessionID) > 0 {
		return true, time.Time{}
	}
	b.mu.RLock()
	outbound := b.outboundReliable
	b.mu.RUnlock()
	if outbound.pendingOnSession(sessionID) {
		return true, time.Time{}
	}
	if b.routing.openWriteTxs(sessionID) > 0 {
		return true, time.Time{}
	}
	if deadline, ok := b.routing.latestTimedDeadline(sessionID, b.clk().Now()); ok {
		return true, deadline
	}
	return false, time.Time{}
}

// recheckPASEClose runs the deferred close of sessionID when the session has
// become idle. Its triggers: the end of a handler turn on the session, the
// retransmit table giving up a message of it, the session's latest timed
// window elapsing — never inside a turn that waits on the session.
func (b *Bridge) recheckPASEClose(sessionID uint16) {
	if !b.paseCloseDeferred(sessionID) {
		return
	}
	active, timedUntil := b.paseSessionActive(sessionID)
	if !active {
		b.finishPASEClose(sessionID, "idle")
		return
	}
	if timedUntil.IsZero() {
		return
	}
	wait := timedUntil.Sub(b.clk().Now()) + time.Millisecond
	t := b.clk().AfterFunc(wait, func() { b.recheckPASEClose(sessionID) })
	b.mu.Lock()
	st := b.paseDeferred[sessionID]
	var prev clock.Timer
	if st != nil {
		prev, st.timed = st.timed, t
	}
	b.mu.Unlock()
	if prev != nil {
		prev.Stop()
	}
	if st == nil {
		t.Stop()
	}
}

// takePASEClose removes sessionID's deferred close and returns it; nil when
// there is none (finished already, or never deferred).
func (b *Bridge) takePASEClose(sessionID uint16) *paseDeferredClose {
	b.mu.Lock()
	st := b.paseDeferred[sessionID]
	if st != nil {
		delete(b.paseDeferred, sessionID)
		b.paseDeferredN.Add(-1)
	}
	b.mu.Unlock()
	if st == nil {
		return nil
	}
	if st.cap != nil {
		st.cap.Stop()
	}
	if st.timed != nil {
		st.timed.Stop()
	}
	return st
}

// finishPASEClose runs sessionID's deferred close once: the CloseSession,
// then the close by id.
func (b *Bridge) finishPASEClose(sessionID uint16, reason string) {
	st := b.takePASEClose(sessionID)
	if st == nil {
		return
	}
	b.logger.Info("matter.session.pase_close",
		slog.String("reason", "revoke_"+reason),
		slog.Int("session_id", int(sessionID)))
	b.notifySessionsClosing([]uint16{sessionID})
	b.mu.RLock()
	reg := b.sessionRegistry
	b.mu.RUnlock()
	if reg != nil {
		_ = reg.Close(sessionID) // gone already: nothing to clear
	}
	b.mu.Lock()
	delete(b.paseClosing, sessionID)
	b.mu.Unlock()
}

// dropPASEClose forgets sessionID's deferred close and stops its timers:
// the session went another way ([Bridge.SessionClosed]), or the bridge
// stops.
func (b *Bridge) dropPASEClose(sessionID uint16) {
	if b.takePASEClose(sessionID) == nil {
		return
	}
	b.mu.Lock()
	delete(b.paseClosing, sessionID)
	b.mu.Unlock()
}

// enterSessionTurn counts a handler turn on the secure session sessionID:
// a message of it in dispatch, or an interaction whose response is not
// out yet. It returns the counter the turn was counted on, which
// [Bridge.exitSessionTurn] ends it on — a session id taken again by a
// later session gets a counter of its own. nil for session 0. One atomic
// add on the common path; the per-session counter is created once.
func (b *Bridge) enterSessionTurn(sessionID uint16) *atomic.Int32 {
	if sessionID == 0 {
		return nil
	}
	v, ok := b.sessionTurns.Load(sessionID)
	if !ok {
		v, _ = b.sessionTurns.LoadOrStore(sessionID, new(atomic.Int32))
	}
	turn, _ := v.(*atomic.Int32)
	turn.Add(1)
	return turn
}

// exitSessionTurn ends a turn [Bridge.enterSessionTurn] counted on turn,
// and runs a deferred PASE close the session's last turn held.
func (b *Bridge) exitSessionTurn(sessionID uint16, turn *atomic.Int32) {
	if turn == nil {
		return
	}
	if turn.Add(-1) <= 0 {
		b.recheckPASEClose(sessionID)
	}
}

// sessionTurnsOf is the number of handler turns running on sessionID.
func (b *Bridge) sessionTurnsOf(sessionID uint16) int32 {
	v, ok := b.sessionTurns.Load(sessionID)
	if !ok {
		return 0
	}
	turn, _ := v.(*atomic.Int32)
	return turn.Load()
}

// forgetSessionTurns drops sessionID's turn counter: the session closed.
// A turn still running on it ends on the counter it holds.
func (b *Bridge) forgetSessionTurns(sessionID uint16) {
	b.sessionTurns.Delete(sessionID)
}

// errNewExchangeOnClosingSession is what the receive path returns for a
// message dropped by [Bridge.refuseOnClosingPASE].
var errNewExchangeOnClosingSession = errors.New("bridge: new exchange on a closing PASE session")

// refuseNewMessage refuses a new (not duplicate) message before its ack is
// owed: one that leaves this node's unacknowledged message on its exchange
// unacknowledged ([Bridge.refuseUnacknowledging]), and one that opens a new
// exchange on a closing PASE session ([Bridge.refuseOnClosingPASE]).
func (b *Bridge) refuseNewMessage(src net.Addr, hdr *message.Header, proto message.ProtocolHeader) error {
	if err := b.refuseUnacknowledging(src, hdr, proto); err != nil {
		return err
	}
	if b.refuseOnClosingPASE(hdr, proto) {
		return errNewExchangeOnClosingSession
	}
	return nil
}

// refuseOnClosingPASE reports whether a message is dropped because it opens
// a new exchange on a PASE session whose graceful close is deferred: no
// ack is owed, nothing is handled. A message of an exchange that exists —
// one this node opened, one holding a message of this node unacknowledged,
// a timed window, a chunked write, a chunk wait or a BDX transfer — and
// every standalone ack pass, as does a duplicate (its ack is still sent)
// and a message the peer sends as responder. matter.js declines those two
// as well when their exchange is gone; here they are acknowledged and not
// served.
// Mirrors matter.js packages/protocol/src/protocol/ExchangeManager.ts:433-438
// at db91886c (a closing session takes no new exchange: logged, no ack).
func (b *Bridge) refuseOnClosingPASE(hdr *message.Header, proto message.ProtocolHeader) bool {
	if !proto.Initiator || !b.paseCloseDeferred(hdr.SessionID) {
		return false
	}
	if proto.ProtocolID == mrp.SecureChannelProtocolID && proto.Opcode == mrp.StandaloneAckOpcode {
		return false
	}
	if b.peerExchangeHeld(hdr.SessionID, proto.ExchangeID) {
		return false
	}
	b.logger.Debug("matter.rx.closing_session",
		slog.Int("session_id", int(hdr.SessionID)),
		slog.Int("exchange_id", int(proto.ExchangeID)),
		slog.Int("protocol_id", int(proto.ProtocolID)),
		slog.Int("opcode", int(proto.Opcode)))
	return true
}

// peerExchangeHeld reports whether the exchange the peer opened on the
// secure session holds state here.
func (b *Bridge) peerExchangeHeld(sessionID, exchangeID uint16) bool {
	key := mrp.ExchangeKey{SessionID: sessionID, ExchangeID: exchangeID, Initiator: false}
	if _, ok := b.routing.statusResponseWaits.Load(key); ok {
		return true
	}
	if _, ok := b.routing.writeTxs.Load(key); ok {
		return true
	}
	if _, ok := b.routing.timedDeadlines.Load(timedKey{sessionID: sessionID, exchangeID: exchangeID}); ok {
		return true
	}
	if _, ok := b.bdx.exchanges.Load(secureExchangeKey{sessionID: sessionID, exchangeID: exchangeID}); ok {
		return true
	}
	b.mu.RLock()
	outbound := b.outboundReliable
	b.mu.RUnlock()
	_, pending := outbound.pendingOnExchange(sessionID, exchangeID, exchangeResponder)
	return pending
}

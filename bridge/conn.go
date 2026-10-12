// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
	"github.com/SukramJ/go-fabric/transport/tcp"
	"github.com/SukramJ/go-fabric/transport/udp"
)

// The receive and reply paths are transport-neutral: a message arrives
// on a [transport.Conn] and every reply goes back on the same one.
//
// Inside the bridge the reply route is a net.Addr, for one reason: the
// UDP path keeps its *net.UDPAddr exactly as before — it is the UDP
// adapter's short form, sent through the bridge's own listener — so the
// wire behaviour over UDP does not move and the existing tests keep
// driving it with a plain address. A connection-oriented transport
// (TCP) is carried as a *connRoute, one per connection, so the route's
// identity is the connection's: a session bound to it can be found again
// when the connection drops.

// ErrGroupOverReliable reports a group message that arrived over TCP.
// Group messages are multicast UDP only.
var ErrGroupOverReliable = errors.New("bridge: group message over a reliable connection")

// errNoRoute is returned when a reply has nowhere to go.
var errNoRoute = errors.New("bridge: no route to the peer")

// connRoute is the reply route over a connection-oriented transport.
type connRoute struct {
	conn transport.Conn
	// sessions counts the secure sessions bound to the connection:
	// bindSessionRoute adds one when a session first binds to it,
	// unbindSessionRoute takes it away again; at zero the connection is
	// closed. Guarded by Bridge.routeBindMu.
	sessions int
	// handshakes holds the PASE / CASE handshakes in flight on the
	// connection, by exchange id: from PBKDFParamRequest / Sigma1 until
	// the secure session it established binds to the connection, it
	// fails, or its exchange ends. A connection with a handshake in
	// flight is not closed when its last secure session goes. Guarded by
	// Bridge.routeBindMu.
	handshakes map[uint16]handshakeHold
}

// handshakeHold is one handshake in flight on a connection.
type handshakeHold struct {
	pase  bool
	since time.Time
}

// Network names the transport of the connection's peer address.
func (r *connRoute) Network() string { return r.conn.Remote().Network() }

// String names the connection for the log.
func (r *connRoute) String() string {
	if s, ok := r.conn.(fmt.Stringer); ok {
		return s.String()
	}
	return r.conn.Remote().Network() + "://" + r.conn.Remote().String()
}

// routeFor returns the reply route for a message that arrived on c:
// the peer's address for the UDP adapter, the connection's one
// *connRoute otherwise. A nil connection or UDP peer address yields nil.
func (b *Bridge) routeFor(c transport.Conn) net.Addr {
	if c == nil {
		return nil
	}
	if u, ok := c.(*udp.Conn); ok {
		if u == nil || u.Addr() == nil {
			return nil
		}
		return u.Addr()
	}
	if raw, ok := b.connRoutes.Load(c); ok {
		r, _ := raw.(*connRoute)
		return r
	}
	raw, _ := b.connRoutes.LoadOrStore(c, &connRoute{conn: c})
	r, _ := raw.(*connRoute)
	return r
}

// isNilRoute reports whether dst names no peer — nil, or a typed nil
// held in the interface (a nil *net.UDPAddr a caller passed through).
func isNilRoute(dst net.Addr) bool {
	switch d := dst.(type) {
	case nil:
		return true
	case *net.UDPAddr:
		return d == nil
	case *connRoute:
		return d == nil || d.conn == nil
	default:
		return false
	}
}

// udpAddrOf is dst as a UDP peer address, nil for a connection. Group
// messages are multicast UDP only; receive drops one that arrived on a
// connection before it reaches the group path.
func udpAddrOf(dst net.Addr) *net.UDPAddr {
	u, _ := dst.(*net.UDPAddr)
	return u
}

// isConnRoute reports whether dst is a connection rather than a UDP
// peer address — a reply on it needs no UDP listener.
func isConnRoute(dst net.Addr) bool {
	r, ok := dst.(*connRoute)
	return ok && r != nil && r.conn != nil
}

// routeUsesMRP reports whether MRP runs for traffic on dst: not over a
// reliable connection (transport/mrp UsesMRP; matter.js
// Session.ts:355-356). Every unicast session supports MRP, so the
// channel alone decides.
func routeUsesMRP(dst net.Addr) bool {
	reliable := false
	if r, ok := dst.(*connRoute); ok && r != nil && r.conn != nil {
		reliable = r.conn.Reliable()
	}
	return mrp.UsesMRP(true, reliable)
}

// sendVia ships datagram to dst: on its connection for a *connRoute,
// through listener for a UDP peer address. The listener is the caller's
// snapshot of b.listener, taken under b.mu like before.
func sendVia(listener *udp.Listener, dst net.Addr, datagram []byte) error {
	switch d := dst.(type) {
	case *connRoute:
		if d == nil || d.conn == nil {
			return errNoRoute
		}
		return d.conn.Send(datagram)
	case *net.UDPAddr:
		if d == nil {
			return errNoRoute
		}
		if listener == nil {
			return fmt.Errorf("%w: listener nil", errNoRoute)
		}
		return listener.Send(d, datagram)
	default:
		return fmt.Errorf("%w: %T", errNoRoute, dst)
	}
}

// bindSessionRoute records dst as the route of secure session sid,
// called for every message that authenticated under it. The UDP path
// keeps its rule — the newest authenticated source address wins
// (matter.js ExchangeManager.ts:425-431). A session is never moved
// between transports or connections: matter.js replaces a session's
// channel only when both the old and the new one are UDP
// (MessageChannel.ts:140-148), so a session first seen on a TCP
// connection stays bound to it, and one first seen over UDP stays on
// UDP. The binding is what the connection's drop evicts
// (ExchangeManager.ts:633-645 #sessionsOnChannel).
//
// "The same session" is the session object, not the id: an id is
// reused once its session is gone, and the new session binds afresh
// even where a teardown path left the old entry behind.
func (b *Bridge) bindSessionRoute(sid uint16, dst net.Addr) {
	b.mu.RLock()
	lookup := b.sessions
	b.mu.RUnlock()
	var sess *channel.Session
	if lookup != nil {
		sess, _ = lookup.Lookup(sid)
	}
	b.routeBindMu.Lock()
	prev, loaded := b.sessionPeerAddrs.Load(sid)
	prevAddr, _ := prev.(net.Addr)
	if loaded {
		moves := isConnRoute(prevAddr) || isConnRoute(dst)
		if moves && prevAddr != dst && sess != nil {
			if owner, ok := b.sessionRouteOwners.Load(sid); ok && owner == sess {
				b.routeBindMu.Unlock()
				return
			}
		}
	}
	b.sessionPeerAddrs.Store(sid, dst)
	if sess != nil {
		b.sessionRouteOwners.Store(sid, sess)
	}
	var closeConn *connRoute
	if !loaded || prevAddr != dst {
		if r, ok := dst.(*connRoute); ok && r != nil {
			r.sessions++
		}
		// The entry overwritten here was left behind by a teardown that
		// never reached unbindSessionRoute: its session is gone, so it
		// leaves the count of its connection.
		if r, ok := prevAddr.(*connRoute); ok && r != nil {
			closeConn = b.releaseConnRouteLocked(r)
		}
	}
	b.routeBindMu.Unlock()
	b.closeConnRoute(closeConn)
}

// unbindSessionRoute forgets the route of secure session sid, which is
// closing or closed. When the route is a connection and sid was the last
// session bound to it, the connection is closed — the Go translation of
// matter.js ExchangeManager.ts:669-676 #closeTcpChannelIfLastSession
// ("Mimics CHIP SDK behavior: close TCP connection when last session is
// removed"), scheduled from :691-708 #deleteSession for every session
// whose transport channel is TCP. matter.js recounts the live sessions on
// the channel (#sessionsOnChannel, :629-643); the bridge keeps the count
// on the route instead. The 10 s first-message timer of the listener is
// not involved: it covers only a connection that never carried a message.
func (b *Bridge) unbindSessionRoute(sid uint16) {
	b.routeBindMu.Lock()
	raw, loaded := b.sessionPeerAddrs.LoadAndDelete(sid)
	b.sessionRouteOwners.Delete(sid)
	var closeConn *connRoute
	if r, ok := raw.(*connRoute); loaded && ok && r != nil {
		closeConn = b.releaseConnRouteLocked(r)
	}
	b.routeBindMu.Unlock()
	b.closeConnRoute(closeConn)
}

// releaseConnRouteLocked takes one session off r's count and returns r
// when that was the last one and no handshake is in flight on it, nil
// otherwise. The caller holds b.routeBindMu.
//
// matter.js counts the unsecured sessions on the channel with the secure
// ones (ExchangeManager.ts:629-643 #sessionsOnChannel, both loops at
// :639-640), so #closeTcpChannelIfLastSession (:669-676) keeps a
// connection that carries a handshake. A handshake's own end closes
// nothing: only a secure session's removal schedules the check
// (#deleteSession :691-708; #addSession :678-689 observes NodeSessions
// only).
func (b *Bridge) releaseConnRouteLocked(r *connRoute) *connRoute {
	r.sessions--
	if r.sessions > 0 {
		return nil
	}
	r.sessions = 0
	if r.liveHandshakesLocked(time.Now()) > 0 {
		return nil
	}
	return r
}

// liveHandshakesLocked counts the handshakes in flight on r, dropping a
// PASE hold older than [pasePairingTimeout], the bound after which the
// single-active-PASE claim expires too (matter.js PaseServer.ts:33
// PASE_PAIRING_TIMEOUT). A CASE hold ends with its handshake or with the
// per-exchange CASE state's eviction ([Bridge.ForgetSigma1Replied]). The
// caller holds b.routeBindMu.
func (r *connRoute) liveHandshakesLocked(now time.Time) int {
	for xid, h := range r.handshakes {
		if h.pase && now.Sub(h.since) > pasePairingTimeout {
			delete(r.handshakes, xid)
		}
	}
	return len(r.handshakes)
}

// holdHandshake records a PASE (pase) or CASE handshake starting on
// exchangeID over src. Only a connection is held; over UDP there is
// nothing to keep open.
func (b *Bridge) holdHandshake(src net.Addr, exchangeID uint16, pase bool) {
	r, ok := src.(*connRoute)
	if !ok || r == nil || r.conn == nil {
		return
	}
	b.routeBindMu.Lock()
	defer b.routeBindMu.Unlock()
	if _, held := r.handshakes[exchangeID]; held {
		return // a retransmitted first message: the handshake already counts
	}
	if r.handshakes == nil {
		r.handshakes = make(map[uint16]handshakeHold)
	}
	r.handshakes[exchangeID] = handshakeHold{pase: pase, since: time.Now()}
}

// releaseHandshake ends the hold of the handshake on exchangeID over src.
// It never closes the connection — see [Bridge.releaseConnRouteLocked].
func (b *Bridge) releaseHandshake(src net.Addr, exchangeID uint16) {
	r, ok := src.(*connRoute)
	if !ok || r == nil {
		return
	}
	b.routeBindMu.Lock()
	delete(r.handshakes, exchangeID)
	b.routeBindMu.Unlock()
}

// releaseCASEHandshakeEverywhere ends the hold of a CASE handshake on
// exchangeID on every connection — its per-exchange state was evicted.
func (b *Bridge) releaseCASEHandshakeEverywhere(exchangeID uint16) {
	b.connRoutes.Range(func(_, v any) bool {
		r, ok := v.(*connRoute)
		if !ok || r == nil {
			return true
		}
		b.routeBindMu.Lock()
		if h, held := r.handshakes[exchangeID]; held && !h.pase {
			delete(r.handshakes, exchangeID)
		}
		b.routeBindMu.Unlock()
		return true
	})
}

// establishedSessionReporter is the optional capability of a PASE or CASE
// handler to name the secure session its handshake established
// ([PaseAdapter], [CaseAdapter]).
type establishedSessionReporter interface {
	establishedSession() (sessionID uint16, ok bool)
}

// settleHandshake follows one handshake step on exchangeID over src — the
// handler h's answer (respOpcode, respPayload) or its error — and ends the
// handshake's hold on a connection when the step ended the handshake. A
// session it established is bound to the connection first, as matter.js
// creates the secure session on the channel the handshake ran on
// (`createSecureSession({ channel, … })`, CaseServer.ts:175-176 resume,
// :316-317 Sigma3; PaseServer.ts:184-185) before the unsecured session
// is closed in the handler's finally (CaseServer.ts:94-98,
// PaseServer.ts:111-118): the connection is then held by the session
// itself, before its first authenticated message. A replayed step (the handler's state rejects it)
// leaves the original handshake running.
func (b *Bridge) settleHandshake(src net.Addr, exchangeID uint16, h any, respOpcode uint8, respPayload []byte, err error) {
	if !isConnRoute(src) {
		return
	}
	switch {
	case err != nil:
		if errors.Is(err, spake2.ErrSessionState) || errors.Is(err, sigma.ErrSessionState) {
			return
		}
	case respOpcode == mrp.SCOpcodeSigma2Resume:
		b.bindEstablishedSession(src, h)
	case respOpcode == mrp.SCOpcodeStatusReport:
		if general, _, _, ok := decodeStatusReport(respPayload); ok && general == mrp.SCStatusGeneralSuccess {
			b.bindEstablishedSession(src, h)
		}
	default:
		return // PBKDFParamResponse, Pake2, Sigma2: the handshake goes on
	}
	b.releaseHandshake(src, exchangeID)
}

// bindEstablishedSession binds the secure session handler h established
// to src, when h names it.
func (b *Bridge) bindEstablishedSession(src net.Addr, h any) {
	rep, ok := h.(establishedSessionReporter)
	if !ok {
		return
	}
	if sid, ok := rep.establishedSession(); ok && sid != 0 {
		b.bindSessionRoute(sid, src)
	}
}

// closeConnRoute closes r's connection, whose last session went; nil is
// a no-op. The connection's own close then runs the drop path
// (handleTCPClose → evictConn), which finds nothing bound any more and
// forgets the route.
func (b *Bridge) closeConnRoute(r *connRoute) {
	if r == nil || r.conn == nil {
		return
	}
	c, ok := r.conn.(interface{ Close() error })
	if !ok {
		return
	}
	b.logger.Info("matter.tcp.close_last_session", slog.String("conn", r.String()))
	_ = c.Close()
}

// SessionClosed tells the bridge that secure session sessionID is gone.
// The host calls it from the session registry's close hook — the hook
// that also cascades into the subscription manager
// (operational.Manager.SetOnSessionClose) — so every close path reaches
// it: CloseSession, fabric removal, NOC rotation, the idle reaper, a
// peer-loss eviction. The bridge forgets the session's route and closes
// a TCP connection whose last session this was (unbindSessionRoute;
// matter.js ExchangeManager.ts:669-676). Calling it for a session the
// bridge never routed, or twice, is a no-op.
func (b *Bridge) SessionClosed(sessionID uint16) {
	b.unbindSessionRoute(sessionID)
}

// receive is the transport-neutral receive entry: buf is one complete
// Matter message, c the connection it arrived on, which every reply
// goes back to.
func (b *Bridge) receive(ctx context.Context, buf []byte, c transport.Conn) error {
	route := b.routeFor(c)
	if isConnRoute(route) {
		if isGroupDatagram(buf) {
			b.logger.Debug("matter.tcp.rx.group_dropped", slog.String("conn", route.String()))
			return ErrGroupOverReliable
		}
		// An unsecured (PASE / CASE handshake) message is served on the
		// connection like any other: matter.js creates the unsecured
		// session for an inbound message from whatever channel carried it
		// (ExchangeManager.ts:241-264 `createUnsecuredSession({ channel,
		// initiatorNodeId })`), and its secure-channel servers never ask
		// which transport they run on. Replies go back on the connection
		// (sendVia) without MRP (routeUsesMRP). The handshake holds the
		// connection open while it runs (holdHandshake, settleHandshake),
		// as matter.js counts the unsecured session on the channel
		// (ExchangeManager.ts:629-643).
	}
	return b.dispatch(ctx, buf, route)
}

// isUnsecuredUnicast reports whether buf is a message of the unsecured
// session (unicast session type, session id 0).
func isUnsecuredUnicast(buf []byte) bool {
	if len(buf) < 4 {
		return false
	}
	if message.SessionType(buf[3]&secFlagSessionTypeBits) != message.SessionUnsecured {
		return false
	}
	return binary.LittleEndian.Uint16(buf[1:3]) == 0
}

// handleTCPMessage is the TCP listener's handler: the dispatch context
// carries the release of the connection's turn, as the ordered UDP
// handler's carries the session's (session_order.go).
func (b *Bridge) handleTCPMessage(c *tcp.Conn, msg []byte, release func()) {
	ctx := context.WithValue(b.handlerContext(), sessionTurnKey{}, release)
	_ = b.receive(ctx, msg, c)
}

// handleTCPClose evicts every session bound to a dropped connection —
// the Go translation of matter.js ExchangeManager.ts:650-665
// #onConnectionDisconnect ("Mimics CHIP SDK behavior: evict all
// sessions when TCP connection drops"), subscribed to the transport's
// disconnect event at :600-609.
func (b *Bridge) handleTCPClose(c *tcp.Conn) {
	b.evictConn(c)
}

// evictConn closes every session whose route is c's and forgets c.
// Returns the evicted session ids.
func (b *Bridge) evictConn(c transport.Conn) []uint16 {
	raw, ok := b.connRoutes.LoadAndDelete(c)
	if !ok {
		// No message ever arrived on the connection: nothing is bound.
		return nil
	}
	route, _ := raw.(*connRoute)
	var sids []uint16
	b.sessionPeerAddrs.Range(func(k, v any) bool {
		sid, okK := k.(uint16)
		r, okV := v.(*connRoute)
		if okK && okV && r == route {
			sids = append(sids, sid)
		}
		return true
	})
	if len(sids) == 0 {
		return nil
	}
	b.logger.Info("matter.tcp.evict",
		slog.String("conn", route.String()),
		slog.Int("sessions", len(sids)))
	for _, sid := range sids {
		b.evictSession(sid, route)
	}
	return sids
}

// evictSession force-closes one session after its connection dropped:
// first its exchanges, then the session itself without a CloseSession
// farewell (the peer is lost), which cascades to its subscriptions
// through the registry's close hook. Mirrors matter.js
// ExchangeManager.ts:654-664 (close every exchange, then
// Session.initiateForceClose, Session.ts:310-331, which closes the
// subscriptions and does not notify the peer — NodeSession.ts:338-341
// marks it lost).
func (b *Bridge) evictSession(sid uint16, route *connRoute) {
	// Exchanges: a chunked report waiting for its StatusResponse on the
	// dead connection is released now rather than at its timeout, and
	// the per-exchange reply routes and timed deadlines go with the
	// session.
	b.routing.statusResponseWaits.Range(func(k, _ any) bool {
		if key, ok := k.(mrp.ExchangeKey); ok && key.SessionID == sid {
			b.disarmStatusResponseWait(key.SessionID, key.ExchangeID, key.Initiator)
		}
		return true
	})
	b.routing.exchangeSrcs.Range(func(k, _ any) bool {
		if key, ok := k.(mrp.ExchangeKey); ok && key.SessionID == sid {
			b.routing.exchangeSrcs.Delete(k)
		}
		return true
	})
	b.routing.dropSessionTimedDeadlines(sid)
	b.sessionPeerAddrs.Delete(sid)
	b.sessionRouteOwners.Delete(sid)

	b.mu.RLock()
	reg := b.sessionRegistry
	b.mu.RUnlock()
	if reg == nil {
		b.logger.Debug("matter.tcp.evict.no_registry", slog.Int("session_id", int(sid)))
		return
	}
	// Close is the peer-lost close: no CloseSession echo, subscription
	// cascade through the manager's close hook (secure/operational
	// Manager.Close, NodeSession.ts:294-303 handlePeerClose →
	// initiateForceClose).
	if err := reg.Close(sid); err != nil {
		b.logger.Debug("matter.tcp.evict.miss",
			slog.Int("session_id", int(sid)),
			slog.String("err", err.Error()))
		return
	}
	b.diagRing().Record(diagevent.Event{
		Kind:     diagevent.KindSession,
		Severity: diagevent.SeverityInfo,
		Message:  "A TCP connection dropped; the secure session bound to it was closed.",
		Detail: map[string]string{
			"peer":       route.String(),
			"session_id": strconv.Itoa(int(sid)),
		},
	})
}

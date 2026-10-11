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

	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/secure/channel"
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
	if prev, loaded := b.sessionPeerAddrs.Load(sid); loaded {
		prevAddr, _ := prev.(net.Addr)
		moves := isConnRoute(prevAddr) || isConnRoute(dst)
		if moves && prevAddr != dst && sess != nil {
			if owner, ok := b.sessionRouteOwners.Load(sid); ok && owner == sess {
				return
			}
		}
	}
	b.sessionPeerAddrs.Store(sid, dst)
	if sess != nil {
		b.sessionRouteOwners.Store(sid, sess)
	}
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
		// (sendVia) without MRP (routeUsesMRP).
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

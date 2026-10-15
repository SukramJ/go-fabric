// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// This file generalises the ADR 0008 CASE initiator (case_initiator.go) by
// exactly what the OTA Software Update Requestor needs (ADR 0019): a
// session to the provider a commissioner named — its node id on one of this
// node's fabrics, resolved as ADR 0008 resolves a former subscriber — and on
// it one Invoke at a time and one BDX exchange. It is still no controller
// role: the only caller is cluster/core's requestor, and the only peer it
// reaches is a provider in DefaultOTAProviders or an AnnounceOTAProvider.

// otaProviderConnectTimeout bounds reaching a provider: matter.js
// PeerTimingParameters.defaultConnectionTimeout (packages/protocol/src/peer/
// PeerTimingParameters.ts:188, Seconds(90)), which Peer.connect applies
// when the caller names none (Peer.ts:467) — the requestor's
// `peers.forAddress` names none.
const otaProviderConnectTimeout = 90 * time.Second

// exchangeMaxPayloadSize is what one message on a UDP session carries:
// matter.js MAX_UDP_MESSAGE_SIZE (packages/general/src/net/udp/
// UdpSocket.ts:12, 1280 - 48) minus MATTER_MESSAGE_OVERHEAD
// (packages/protocol/src/protocol/MessageExchange.ts:156, 26 + 12 + the
// 16-byte MIC), as MessageExchange.maxPayloadSize computes it (:435-439).
const exchangeMaxPayloadSize = (1280 - 48) - matterMessageOverhead

// matterMessageOverhead is matter.js MATTER_MESSAGE_OVERHEAD
// (MessageExchange.ts:156, 26 + 12 + the 16-byte MIC).
const matterMessageOverhead = 26 + 12 + 16

// ErrExchangeClosed is a send or receive on an exchange already closed.
var ErrExchangeClosed = errors.New("bridge: exchange closed")

// ConnectOtaProvider implements [core.OtaProviderConnector]: it returns a
// secure session with the provider, reusing the newest live one or
// establishing one by CASE as this node's initiator (matter.js
// OtaSoftwareUpdateRequestorServer.ts:553-565 #connectOtaProviderFor →
// ServerNode.peers.forAddress → Peer.connect). Needs the collaborators ADR
// 0008 attaches: [Bridge.AttachCaseInitiatorProvider] and
// [Bridge.AttachOperationalResolver].
func (b *Bridge) ConnectOtaProvider(ctx context.Context, fabricIndex uint8, providerNodeID uint64) (core.OtaProviderSession, error) {
	ctx, cancel := context.WithTimeout(ctx, otaProviderConnectTimeout)
	defer cancel()
	link, err := b.connectPeer(ctx, fabricIndex, providerNodeID)
	if err != nil {
		return nil, err
	}
	return &peerSession{b: b, link: link}, nil
}

// peerSession is a secure session with a peer this node reached as the
// initiator.
type peerSession struct {
	b    *Bridge
	link peerLink
}

// invokeStatusError is a command the peer answered with a status other
// than Success.
type invokeStatusError struct {
	status  im.StatusIB
	command uint32
}

func (e *invokeStatusError) Error() string {
	if e.status.HasClusterStatus {
		return fmt.Sprintf("bridge: command 0x%02x answered %s (cluster status %d)", e.command, e.status.Status, e.status.ClusterStatus)
	}
	return fmt.Sprintf("bridge: command 0x%02x answered %s", e.command, e.status.Status)
}

// MatterStatusCode implements [im.StatusCodeError].
func (e *invokeStatusError) MatterStatusCode() im.StatusCode { return e.status.Status }

// Invoke implements [core.OtaProviderSession]: one InvokeRequest on a new
// exchange, the InvokeResponse awaited for as long as matter.js's exchange
// waits for a peer's response (MessageExchange.ts:839-850,
// calculateMaximumPeerResponseTime — [Bridge.chunkStatusResponseTimeout]).
// A command answered with data returns its fields; one answered with
// Success returns nil; any other status is an error carrying it.
func (s *peerSession) Invoke(ctx context.Context, endpoint uint16, clusterID, commandID uint32, fields spec.Encodable) (*spec.Node, error) {
	ex, err := s.b.openInitiatedExchange(s.link)
	if err != nil {
		return nil, err
	}
	defer func() { _ = ex.Close() }()

	enc := tlv.NewEncoder()
	im.MarshalInvokeRequestTLV(enc, im.InvokeRequest{Invokes: []im.CommandInvocation{{
		Path:   im.ConcreteCommandPath{Endpoint: endpoint, Cluster: clusterID, Command: commandID, HasEndpoint: true, HasCluster: true, HasCommand: true},
		Fields: fields,
	}}}, func(enc *tlv.Encoder, tag tlv.Tag, v any) {
		if f, ok := v.(spec.Encodable); ok && f != nil {
			f.EncodeTLV(enc, tag)
			return
		}
		enc.StartStruct(tag)
		_ = enc.EndContainer()
	})
	payload, err := enc.Bytes()
	if err != nil {
		return nil, err
	}
	if err := ex.Send(ctx, im.InteractionModelProtocolID, im.OpcodeInvokeRequest, payload); err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, s.b.chunkStatusResponseTimeout(s.link.sessionID))
	defer cancel()
	for {
		msg, err := ex.Receive(waitCtx)
		if err != nil {
			return nil, err
		}
		if msg.ProtocolID != im.InteractionModelProtocolID {
			continue
		}
		switch msg.Opcode {
		case im.OpcodeStatusResponse:
			sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(msg.Payload))
			if err != nil {
				return nil, err
			}
			return nil, &invokeStatusError{status: im.StatusIB{Status: sr.Status}, command: commandID}
		case im.OpcodeInvokeResponse:
			return decodeInvokeResult(msg.Payload, commandID)
		}
	}
}

// decodeInvokeResult takes the one response of a single-command invoke.
func decodeInvokeResult(payload []byte, commandID uint32) (*spec.Node, error) {
	resp, err := im.UnmarshalInvokeResponseTLV(tlv.NewDecoder(payload), func(_ im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		if !el.IsContainer {
			return nil, errors.New("bridge: command response fields are not a structure")
		}
		n, err := spec.ReadContainer(dec, el)
		if err != nil {
			return nil, err
		}
		return &n, nil
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Responses) == 0 {
		return nil, errors.New("bridge: InvokeResponse without a response")
	}
	r := resp.Responses[0]
	if r.IsStatus {
		if r.Status.Status.IsSuccess() {
			return nil, nil
		}
		return nil, &invokeStatusError{status: r.Status, command: commandID}
	}
	n, ok := r.Response.(*spec.Node)
	if !ok {
		return nil, errors.New("bridge: command response without fields")
	}
	return n, nil
}

// OpenExchange implements [core.OtaProviderSession]: a new exchange this
// node initiates on the session, for BDX (matter.js
// ClientNodeInteraction.initBdx → BdxMessenger over a fresh exchange).
func (s *peerSession) OpenExchange(context.Context) (bdx.Exchange, error) {
	return s.b.openInitiatedExchange(s.link)
}

// secureExchangeKey keys an exchange this node opened on a secure session
// in resumptionState.initiated; the CASE handshake's unsecured exchanges
// are keyed by their bare exchange id there, so the two never collide.
type secureExchangeKey struct {
	sessionID  uint16
	exchangeID uint16
}

// initiatedSecureExchange is an exchange this node opened on a secure
// session; it implements [bdx.Exchange]. The receive path hands it every
// message the peer sends on it ([Bridge.deliverInitiatedSecure]).
type initiatedSecureExchange struct {
	b          *Bridge
	sessionID  uint16
	addr       net.Addr
	exchangeID uint16
	// createdAt bounds the peer-loss sweep this exchange may trigger:
	// a session the peer established after the exchange was opened is
	// not what went unanswered (matter.js ExchangeManager.ts:554 takes
	// the context's creation time, SessionManager.ts:688 compares).
	createdAt time.Time
	// responder is set on an exchange the peer opened (bdx_responder.go):
	// this node answers on it with the I flag clear.
	responder bool
	// turn tracks how far the responder consumed what was delivered to it
	// (bdx_responder.go, responderTurn); nil on an exchange this node
	// opened.
	turn      *responderTurn
	in        chan bdx.Message
	done      chan struct{}
	closeOnce sync.Once
}

var _ bdx.Exchange = (*initiatedSecureExchange)(nil)

func (b *Bridge) openInitiatedExchange(link peerLink) (*initiatedSecureExchange, error) {
	b.mu.RLock()
	sessions := b.sessions
	b.mu.RUnlock()
	if sessions == nil {
		return nil, ErrUnsolicitedSessionMissing
	}
	if _, ok := sessions.Lookup(link.sessionID); !ok {
		return nil, ErrUnsolicitedSessionMissing
	}
	ex := &initiatedSecureExchange{
		b: b, sessionID: link.sessionID, addr: link.addr, exchangeID: b.nextOutboundExchangeID(),
		createdAt: time.Now(), in: make(chan bdx.Message, 16), done: make(chan struct{}),
	}
	b.resumption.initiated.Store(secureExchangeKey{sessionID: ex.sessionID, exchangeID: ex.exchangeID}, ex)
	return ex, nil
}

// peerLossCloser is the optional [SessionRegistry] capability that
// force-closes every session of a peer created before asOf
// (secure/operational.Manager.ClosePeerLost).
type peerLossCloser interface {
	ClosePeerLost(fabricIndex uint8, peerNodeID uint64, asOf time.Time) int
}

// handleInitiatedPeerLoss is the outbound-reliable tracker giving up on a
// message of an exchange this node opened: the peer is lost. Every
// session with that peer is force-closed — the next
// [Bridge.ConnectOtaProvider] then runs a fresh CASE instead of reusing
// a session the peer no longer knows (a provider restarted under the same
// node id answers nothing on the old keys). Mirrors matter.js
// MessageExchange.ts:620 (retransmissions exhausted → context.peerLost) →
// ExchangeManager.ts:561-585 (a commissioned peer: SessionManager.
// handlePeerLoss for the peer address, asOf the exchange context's
// creation; otherwise the session alone) → SessionManager.ts:677-695.
// An exhausted counter of a subscription report takes the same path
// through [Bridge.reportPeerLoss].
func (b *Bridge) handleInitiatedPeerLoss(sessionID, exchangeID uint16) {
	raw, ok := b.resumption.initiated.Load(secureExchangeKey{sessionID: sessionID, exchangeID: exchangeID})
	if !ok {
		return
	}
	ex, ok := raw.(*initiatedSecureExchange)
	if !ok {
		return
	}
	b.closeLostPeer(sessionID, exchangeID, ex.createdAt, "an exchange this node opened")
}

// reportPeerLoss reports whether the exhausted message (sessionID,
// exchangeID, counter) is evidence that the peer is lost, and the exchange's
// creation time to bound the sweep: the message is a live ongoing report's
// (its counter is still owned) and the first one sent on its exchange, so
// nothing was heard from the peer on it. matter.js reports peer loss when a
// send fails "only if we never heard from it on this exchange"
// (MessageExchange.ts:616-621, #reportsPeerLoss :630-633,
// #messageReceivedCounter === 0); a later chunk is sent only after the
// peer's StatusResponse to the previous one, so its loss is not.
func (b *Bridge) reportPeerLoss(sessionID, exchangeID uint16, counter uint32) (time.Time, bool) {
	if _, owned := b.reportCounterOwner.Load(reportCounterKey(sessionID, counter)); !owned {
		return time.Time{}, false
	}
	raw, ok := b.reportExchangeOrigins.Load(mrp.ExchangeKey{SessionID: sessionID, ExchangeID: exchangeID, Initiator: true})
	if !ok {
		return time.Time{}, false
	}
	origin, ok := raw.(reportExchangeOrigin)
	if !ok || origin.firstCounter != counter {
		return time.Time{}, false
	}
	return origin.createdAt, true
}

// closeLostPeer force-closes the sessions of the peer of sessionID that
// were created before asOf — every one with a commissioned peer
// (SessionManager.ts:677-695 #handlePeerLoss), the session alone without a
// fabric or node id (ExchangeManager.ts:561-571). The sessions'
// subscriptions fall with them through the registry's close hook. what
// names the exchange for the log.
func (b *Bridge) closeLostPeer(sessionID, exchangeID uint16, asOf time.Time, what string) {
	b.mu.RLock()
	sessions := b.sessions
	registry := b.sessionRegistry
	b.mu.RUnlock()
	if sessions == nil || registry == nil {
		return
	}
	sess, live := sessions.Lookup(sessionID)
	if !live {
		return
	}
	peerNodeID := sess.PeerNodeID()
	var fabricIndex uint8
	if resolver, ok := sessions.(SessionFabricResolver); ok {
		fabricIndex, _ = resolver.FabricFor(sessionID)
	}
	closed := 0
	closer, perPeer := registry.(peerLossCloser)
	if perPeer && fabricIndex != 0 && peerNodeID != 0 {
		closed = closer.ClosePeerLost(fabricIndex, peerNodeID, asOf)
	} else if err := registry.Close(sessionID); err == nil {
		closed = 1
	}
	if b.logger != nil {
		b.logger.Info("matter.session.peer_lost",
			slog.Int("session_id", int(sessionID)),
			slog.Int("exchange_id", int(exchangeID)),
			slog.Int("fabric", int(fabricIndex)),
			slog.Uint64("peer_node", peerNodeID),
			slog.Int("closed", closed),
			slog.String("hint", "max retransmissions reached on "+what+"; the peer's sessions are force-closed"))
	}
}

// deliverInitiatedSecure hands a message the peer sent as the responder on
// a secure exchange this node opened to that exchange. Returns false when
// no such exchange is open, leaving the message to the ordinary dispatch.
// Acknowledgements are handled before this point and are not handed on.
func (b *Bridge) deliverInitiatedSecure(hdr *message.Header, proto message.ProtocolHeader, payload []byte) bool {
	if hdr.SessionID == 0 || proto.Initiator {
		return false
	}
	if proto.ProtocolID == mrp.SecureChannelProtocolID && proto.Opcode == mrp.StandaloneAckOpcode {
		return false
	}
	raw, ok := b.resumption.initiated.Load(secureExchangeKey{sessionID: hdr.SessionID, exchangeID: proto.ExchangeID})
	if !ok {
		return false
	}
	ex, ok := raw.(*initiatedSecureExchange)
	if !ok {
		return false
	}
	msg := bdx.Message{ProtocolID: proto.ProtocolID, Opcode: proto.Opcode, Payload: append([]byte(nil), payload...)}
	select {
	case ex.in <- msg:
	case <-ex.done:
	default:
		b.logger.Debug("matter.exchange.initiated.overflow",
			slog.Int("session_id", int(hdr.SessionID)), slog.Int("exchange_id", int(proto.ExchangeID)))
	}
	return true
}

// route is where the exchange's messages go: the session's current route
// (a TCP connection it is bound to, or its newest UDP address), the
// link's address otherwise.
func (e *initiatedSecureExchange) route() net.Addr {
	return e.b.currentPeerAddr(e.sessionID, e.addr)
}

// Reliable reports whether the exchange runs over a reliable connection
// (TCP) — the Go form of matter.js's `messenger.channel.type === "tcp"`,
// on which the OTA requestor picks its BDX block size
// (OtaSoftwareUpdateRequestorServer.ts:975-976). A session never moves
// between transports (bindSessionRoute), so the answer holds for the
// exchange's life.
func (e *initiatedSecureExchange) Reliable() bool {
	r, ok := e.route().(*connRoute)
	return ok && r != nil && r.conn != nil && r.conn.Reliable()
}

// Send implements [bdx.Exchange]: one message on the exchange, reliable
// when the MRP tracker is wired and the route is not a reliable
// connection (MRP is off over TCP, [routeUsesMRP]), piggybacking the
// acknowledgement owed on the exchange.
func (e *initiatedSecureExchange) Send(_ context.Context, protocolID uint16, opcode uint8, payload []byte) error {
	select {
	case <-e.done:
		return ErrExchangeClosed
	default:
	}
	b := e.b
	b.mu.RLock()
	listener := b.listener
	sessions := b.sessions
	tracker := b.outboundReliable
	ackTracker := b.ackTracker
	b.mu.RUnlock()
	dest := e.route()
	if !routeUsesMRP(dest) {
		tracker = nil
	}
	if listener == nil && !isConnRoute(dest) {
		return ErrUnsolicitedListenerMissing
	}
	if sessions == nil {
		return ErrUnsolicitedSessionMissing
	}
	sess, ok := sessions.Lookup(e.sessionID)
	if !ok {
		return ErrUnsolicitedSessionMissing
	}
	proto := message.ProtocolHeader{
		Initiator:  !e.responder,
		Opcode:     opcode,
		ExchangeID: e.exchangeID,
		ProtocolID: protocolID,
		NeedsAck:   tracker != nil,
	}
	if ackTracker != nil {
		if counter, owed := ackTracker.LookupAndDischarge(e.sessionID, e.exchangeID, !e.responder); owed {
			proto.HasAck, proto.AckCounter = true, counter
		}
	}
	body := append(proto.Marshal(), payload...) //nolint:gocritic // single-allocation join
	hdr := message.Header{SessionID: e.sessionID}
	if peerID := sess.PeerSessionID(); peerID != 0 {
		hdr.SessionID = peerID
	}
	enc, err := b.encryptSecureOutbound(sess, e.sessionID, &hdr, body)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnsolicitedEncrypt, err)
	}
	datagram := append(hdr.Marshal(), enc.Ciphertext...) //nolint:gocritic // single-allocation join
	if tracker != nil {
		tracker.Track(hdr.MessageCounter, e.sessionID, e.exchangeID, datagram, dest, time.Now())
	}
	if err := sendVia(listener, dest, datagram); err != nil {
		if tracker != nil {
			tracker.Untrack(e.sessionID, hdr.MessageCounter)
		}
		return err
	}
	return nil
}

// Receive implements [bdx.Exchange].
func (e *initiatedSecureExchange) Receive(ctx context.Context) (bdx.Message, error) {
	e.turn.waiting()
	select {
	case msg := <-e.in:
		e.turn.took()
		return msg, nil
	case <-e.done:
		return bdx.Message{}, ErrExchangeClosed
	case <-ctx.Done():
		return bdx.Message{}, ctx.Err()
	}
}

// MaxPayloadSize implements [bdx.Exchange]: the channel's maximum less the
// message overhead, as matter.js MessageExchange.maxPayloadSize computes
// it (MessageExchange.ts:435-439) — over UDP [exchangeMaxPayloadSize],
// over a TCP connection the connection's largest message
// (tcp.Conn.MaxMessageSize, the frame less its length prefix; matter.js
// subtracts from the frame size, TcpChannel.ts:47, :115-117) less the same
// overhead.
func (e *initiatedSecureExchange) MaxPayloadSize() int {
	if r, ok := e.route().(*connRoute); ok && r != nil && r.conn != nil && r.conn.Reliable() {
		if m, ok := r.conn.(interface{ MaxMessageSize() int }); ok {
			if n := m.MaxMessageSize() - matterMessageOverhead; n > exchangeMaxPayloadSize {
				return n
			}
		}
	}
	return exchangeMaxPayloadSize
}

// Close implements [bdx.Exchange]: the exchange stops receiving, and an
// acknowledgement still owed on it goes out now — matter.js
// MessageExchange sends the standalone ack of the last received message
// when an exchange is destroyed, as the CASE initiator does
// (case_initiator.go). A reliable message still unacknowledged keeps its
// retransmissions.
func (e *initiatedSecureExchange) Close() error {
	e.closeOnce.Do(func() {
		close(e.done)
		key := secureExchangeKey{sessionID: e.sessionID, exchangeID: e.exchangeID}
		if e.responder {
			e.b.bdx.exchanges.Delete(key)
		} else {
			e.b.resumption.initiated.Delete(key)
		}
		e.b.expediteDuplicateAck(e.sessionID, e.exchangeID, !e.responder)
		e.b.RunAckPumpOnce(time.Now())
	})
	return nil
}

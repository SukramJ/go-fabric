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
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// This file generalises the ADR 0008 CASE initiator (case_initiator.go) by
// exactly what a device-side server needs to reach a peer a controller
// named: a session to a node id on one of this node's fabrics, resolved as
// ADR 0008 resolves a former subscriber — and on it one Invoke at a time
// and one BDX exchange. It is still no controller role: the callers are
// cluster/core's OTA requestor (ADR 0019: a provider in DefaultOTAProviders
// or an AnnounceOTAProvider) and cluster/camera's WebRTC provider (the
// requestor that opened a session, issue #351), and no peer is reached
// that a controller did not name.
//
// Since ADR 0032 a node composes its peer path itself: a live session of
// this bridge is reused through [Bridge.LivePeerSession] (never a
// handshake), and a new one is established on the node's client stack.
// [Bridge.ConnectPeer] and [Bridge.ConnectOtaProvider], which still
// establish through the ADR 0008 initiator, are deprecated.

// peerConnectTimeout bounds reaching a peer: matter.js
// PeerTimingParameters.defaultConnectionTimeout (packages/protocol/src/peer/
// PeerTimingParameters.ts:188, Seconds(90)), which Peer.connect applies
// when the caller names none (Peer.ts:467) — the OTA requestor's
// `peers.forAddress` names none.
const peerConnectTimeout = 90 * time.Second

// exchangeMaxPayloadSize is what one message on a UDP session carries:
// matter.js MAX_UDP_MESSAGE_SIZE (packages/general/src/net/udp/
// UdpSocket.ts:12, 1280 - 48) minus MATTER_MESSAGE_OVERHEAD
// (packages/protocol/src/protocol/MessageExchange.ts:182, 26 + 12 + the
// 16-byte MIC), as MessageExchange.maxPayloadSize computes it (:465-469),
// at db021da0.
const exchangeMaxPayloadSize = (1280 - 48) - matterMessageOverhead

// matterMessageOverhead is matter.js MATTER_MESSAGE_OVERHEAD
// (MessageExchange.ts:182 at db021da0, 26 + 12 + the 16-byte MIC).
const matterMessageOverhead = 26 + 12 + 16

// ErrExchangeClosed is a send or receive on an exchange already closed.
var ErrExchangeClosed = errors.New("bridge: exchange closed")

var (
	// errExchangeSessionGone is why an exchange ended when its session
	// did: the session closed ([Bridge.endSessionExchanges]), or its id
	// now names another session ([initiatedSecureExchange.boundTo]).
	errExchangeSessionGone = errors.New("bridge: the exchange's session is gone")
	// errExchangeEvicted is why an exchange ended when its session opened
	// more than [maxExchangesPerSession] ([Bridge.boundSessionExchanges]).
	errExchangeEvicted = errors.New("bridge: the exchange lost its place to a newer exchange of its session")
	// errExchangeIDsExhausted is an exchange this node cannot open: every
	// exchange id of the session is held by an open exchange it opened
	// ([Bridge.openInitiatedExchange]).
	errExchangeIDsExhausted = errors.New("bridge: every exchange id of the session is held by an open exchange")
)

// ErrPayloadTooLarge is an Invoke whose request does not fit one message
// of the session it would go over: over UDP [exchangeMaxPayloadSize], over
// a TCP connection the connection's largest message, bounded by the
// MaxTcpMessageSize the session's peer stated
// ([initiatedSecureExchange.MaxPayloadSize]); and a request for a session
// that carries large messages ([PeerSessionRequirement]) on one that does
// not. [Bridge.ConnectPeer] initiates CASE over UDP only and reuses the
// newest live session of any transport, so a large payload reaches the
// peer only when the peer came in over TCP; a node's peer path selects a
// TCP session for a Large Message command (docs/adr/0032). It is
// [camera.ErrPayloadTooLarge], the value a camera host — which signals
// through camera.Provider — recognises with cluster/camera alone.
var ErrPayloadTooLarge = camera.ErrPayloadTooLarge

// ConnectOtaProvider implements [core.OtaProviderConnector]: it returns a
// secure session with the provider (matter.js
// OtaSoftwareUpdateRequestorServer.ts:553-565 #connectOtaProviderFor →
// ServerNode.peers.forAddress → Peer.connect) — [Bridge.ConnectPeer].
//
// Deprecated: a node reaches its OTA provider over its peer path, which
// reuses a session of the server stack ([Bridge.LivePeerSession]) or
// establishes one on the client stack (docs/adr/0032); a host that runs
// the bridge without a node attaches its own [core.OtaProviderConnector].
// ConnectOtaProvider keeps its behaviour and is removable in the next minor
// release.
func (b *Bridge) ConnectOtaProvider(ctx context.Context, fabricIndex uint8, providerNodeID uint64) (core.OtaProviderSession, error) {
	s, err := b.ConnectPeer(ctx, fabricIndex, providerNodeID)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// ConnectPeer returns a secure session with the node peerNodeID on the
// fabric fabricIndex, reusing the newest live one or establishing one by
// CASE as this node's initiator within [peerConnectTimeout] (matter.js
// ServerNode.peers.forAddress → Peer.connect; connectedhomeip
// CASESessionManager::FindOrEstablishSession). Needs the collaborators
// ADR 0008 attaches: [Bridge.AttachCaseInitiatorProvider] and
// [Bridge.AttachOperationalResolver]. The callers are the device-side
// servers that answer a controller on a peer the controller named: the OTA
// requestor ([Bridge.ConnectOtaProvider]) and the camera's
// WebRtcTransportProvider (node wires it as the camera.PeerConnector).
//
// Deprecated: a node's peer path reuses a live session of the bridge
// through [Bridge.LivePeerSession] and establishes a new one on its client
// stack, which carries TCP for a Large Message command (docs/adr/0032).
// ConnectPeer keeps its behaviour — the newest live session of any
// transport, else CASE over UDP through the ADR 0008 initiator — and is
// removable in the next minor release.
func (b *Bridge) ConnectPeer(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (*PeerSession, error) {
	ctx, cancel := clock.WithTimeout(ctx, b.clk(), peerConnectTimeout)
	defer cancel()
	link, err := b.connectPeer(ctx, fabricIndex, peerNodeID)
	if err != nil {
		return nil, err
	}
	return &PeerSession{b: b, link: link}, nil
}

// PeerSessionRequirement is what a session with a peer must offer to carry
// a request: the zero value is any live session.
type PeerSessionRequirement struct {
	// LargeMessage requires a session that carries Large Messages — one
	// bound to a TCP connection — for a command with the Large Message
	// quality (schema.CommandLargeMessage). A required transport is the
	// only one a session is taken from (matter.js
	// packages/protocol/src/peer/Peer.ts:369-376, :440-448 at db021da0).
	LargeMessage bool
}

// LivePeerSession returns the live secure session with the node nodeID on
// the fabric fabricIndex that meets req — of any opener, this node's or the
// peer's — the one the peer sent on last; false when there is none. It
// never starts a handshake: a node establishes a missing session on its
// client stack (docs/adr/0032). Mirrors matter.js Peer.connect's reuse of
// newestSession (packages/protocol/src/peer/Peer.ts:440-460, :582-602 at
// db021da0), whose peer holds every session of the fabric whoever opened it
// (packages/protocol/src/peer/PeerSet.ts:104, :108-110, :116-125).
func (b *Bridge) LivePeerSession(fabricIndex uint8, nodeID uint64, req PeerSessionRequirement) (*PeerSession, bool) {
	link, ok := b.livePeerLink(fabricIndex, nodeID, req)
	if !ok {
		return nil, false
	}
	return &PeerSession{b: b, link: link, req: req}, true
}

// PeerSession is a secure session with a peer of this node's server stack:
// one this node reached as the initiator ([Bridge.ConnectPeer]) or one
// [Bridge.LivePeerSession] found. It implements core.OtaProviderSession
// and camera.PeerSession.
type PeerSession struct {
	b    *Bridge
	link peerLink
	// req is what the session was selected for ([Bridge.LivePeerSession]).
	req PeerSessionRequirement
}

// peerActivityReader is the optional [SessionRegistry] capability that
// reads a session's entry (secure/operational.Manager.Get).
type peerActivityReader interface {
	Get(sessionID uint16) (*operational.Entry, error)
}

// LastActivityMonotonic is when the peer last sent on the session, on the
// clock's monotonic reading (operational.Entry.LastPeerActivityMonotonic):
// the order [Bridge.LivePeerSession] selects by, comparable with another
// stack's reading on the same clock as a wall time is not across a
// wall-clock step. False when the session is gone or the registry does not
// keep it.
func (s *PeerSession) LastActivityMonotonic() (time.Duration, bool) {
	s.b.mu.RLock()
	reg := s.b.sessionRegistry
	s.b.mu.RUnlock()
	r, ok := reg.(peerActivityReader)
	if !ok {
		return 0, false
	}
	e, err := r.Get(s.link.sessionID)
	if err != nil || e == nil {
		return 0, false
	}
	return e.LastPeerActivityMonotonic()
}

// transport is the network of the session's route as [net.Addr.Network]
// names it: "udp" for a session over the bridge's UDP socket, "tcp" for one
// bound to a TCP connection — the one transport that carries Large
// Messages.
func (s *PeerSession) transport() string {
	if s.link.addr == nil {
		return ""
	}
	if r, ok := s.link.addr.(*connRoute); ok {
		if r == nil {
			return ""
		}
		return connNetwork(r.conn)
	}
	return s.link.addr.Network()
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
// waits for a peer's response (MessageExchange.ts:877-886 at db021da0,
// calculateMaximumPeerResponseTime — [Bridge.chunkStatusResponseTimeout]).
// A command answered with data returns its fields; one answered with
// Success returns nil; any other status is an error carrying it
// ([im.StatusCodeError]). A request larger than one message of the
// session is [ErrPayloadTooLarge] and is not sent.
func (s *PeerSession) Invoke(ctx context.Context, endpoint uint16, clusterID, commandID uint32, fields spec.Encodable) (*spec.Node, error) {
	if s.req.LargeMessage && !routeCarriesLargeMessages(s.link.addr) {
		return nil, fmt.Errorf("%w: command 0x%02x needs a session that carries large messages", ErrPayloadTooLarge, commandID)
	}
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
	if limit := ex.MaxPayloadSize(); len(payload) > limit {
		return nil, fmt.Errorf("%w: command 0x%02x of %d bytes, the session carries %d", ErrPayloadTooLarge, commandID, len(payload), limit)
	}
	if err := ex.Send(ctx, im.InteractionModelProtocolID, im.OpcodeInvokeRequest, payload); err != nil {
		return nil, err
	}
	waitCtx, cancel := clock.WithTimeout(ctx, s.b.clk(), s.b.chunkStatusResponseTimeout(ex.route(), s.link.sessionID))
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

// decodeInvokeResult takes the one response of a single-command invoke,
// whatever path it names: the response is attributed to the one request,
// as matter.js attributes the response of a single command by its
// singleCommandRef and only logs its path
// (packages/protocol/src/action/client/ClientInteraction.ts:513, :538-539
// at db021da0). Comparing the path would be ahead of matter.js (review
// II, R2-P6-err-0313).
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
func (s *PeerSession) OpenExchange(context.Context) (bdx.Exchange, error) {
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
	// session is the session the exchange was opened on. A session id is
	// handed out again once its session closed, so the id alone does not
	// name it: a message or a send under the id finds another session
	// object once the session closed, and the exchange ends instead of
	// passing to that session ([initiatedSecureExchange.boundTo]). nil on
	// an exchange built without a session lookup (tests).
	session *channel.Session
	// lastActive orders the exchanges of a session by their latest
	// message, in or out ([Bridge.boundSessionExchanges]): a value of
	// bdxRouting.activity.
	lastActive atomic.Uint64
	// createdAt bounds the peer-loss sweep this exchange may trigger:
	// a session the peer established after the exchange was opened is
	// not what went unanswered (matter.js ExchangeManager.ts:614, :641 takes
	// the context's creation time, SessionManager.ts:740 compares, at
	// db021da0).
	// createdAtMono is the same instant on the clock's monotonic reading,
	// the one the sweep compares (matter.js's asOf is Time.nowUs).
	createdAt     time.Time
	createdAtMono time.Duration
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
	// cause is why the exchange ended when it did not end by Close: the
	// reliable tracker gave up on a message it sent (fail).
	cause atomic.Pointer[error]
	// peerMargin (nanoseconds) is the additional MRP delay of every
	// message this exchange sends ([initiatedSecureExchange.SetPeerAdditionalMrpDelay]).
	peerMargin atomic.Int64
	// notify is the [bdx.ReceiveNotifier] callback the outbound flow
	// installs; the receive path calls it before it queues a message.
	notify atomic.Pointer[func(bdx.Message)]
}

// OnReceive implements [bdx.ReceiveNotifier]: fn is called with every
// message the receive path queues for Receive, just before it is queued
// ([initiatedSecureExchange.enqueue]) — so a transfer finished by its
// final BlockAckEof is finished before the session's next datagram is
// dispatched.
func (e *initiatedSecureExchange) OnReceive(fn func(bdx.Message)) { e.notify.Store(&fn) }

// ended reports whether the exchange ended (Close, or fail).
func (e *initiatedSecureExchange) ended() bool {
	select {
	case <-e.done:
		return true
	default:
		return false
	}
}

// enqueue queues msg for Receive and reports whether it did. The receive
// notifier hears of msg first, so the flow acts on it before Receive can
// return it — but only when it is queued: a full queue, or an ended
// exchange, drops msg without telling the notifier ([bdx.ReceiveNotifier]).
// A session's messages arrive one at a time on its ordered turn
// (ServeOrdered, message.SessionOrderKey), so the room checked before the
// notifier is the room the send finds.
func (e *initiatedSecureExchange) enqueue(msg bdx.Message) bool {
	if e.ended() || len(e.in) == cap(e.in) {
		return false
	}
	e.touch()
	if fn := e.notify.Load(); fn != nil {
		(*fn)(msg)
	}
	select {
	case e.in <- msg:
		return true
	default:
		return false
	}
}

// SetPeerAdditionalMrpDelay sets the margin every later message of the
// exchange adds to the peer's MRP base interval before the backoff
// amplifies it. Mirrors matter.js MessageExchange.peerAdditionalMrpDelay
// (MessageExchange.ts:377-379 at db021da0), the setter for an exchange the
// peer initiated, which the BDX responder calls on an inbound init
// (BdxProtocol.ts:139-142). It implements [bdx.MrpDelayExchange].
func (e *initiatedSecureExchange) SetPeerAdditionalMrpDelay(d time.Duration) {
	e.peerMargin.Store(int64(max(d, 0)))
}

var (
	_ bdx.Exchange         = (*initiatedSecureExchange)(nil)
	_ bdx.MrpDelayExchange = (*initiatedSecureExchange)(nil)
	_ bdx.ReceiveNotifier  = (*initiatedSecureExchange)(nil)
)

// fail ends the exchange because a message it sent was never
// acknowledged: a pending Receive and every later Send return err.
// Mirrors matter.js MessageExchange.send (MessageExchange.ts:604-624),
// whose await of the reliable send throws once the retransmissions are
// exhausted — a BDX transfer whose peer went away fails within the MRP
// backoff instead of waiting out the 5-minute transfer idle timeout
// (BdxMessenger.ts:55).
func (e *initiatedSecureExchange) fail(err error) {
	e.cause.CompareAndSwap(nil, &err)
	_ = e.Close()
}

// closedErr is what an operation on the ended exchange returns.
func (e *initiatedSecureExchange) closedErr() error {
	if c := e.cause.Load(); c != nil {
		return fmt.Errorf("%w: %w", ErrExchangeClosed, *c)
	}
	return ErrExchangeClosed
}

// touch records activity on the exchange ([Bridge.boundSessionExchanges]).
func (e *initiatedSecureExchange) touch() {
	if e.b != nil {
		e.lastActive.Store(e.b.bdx.activity.Add(1))
	}
}

// boundTo reports whether sess — what the session lookup holds under the
// exchange's session id now — is the session the exchange was opened on.
// When it is not, the session closed and its id went to another session:
// the exchange ends ([errExchangeSessionGone]) rather than take that
// session's messages or seal its own under that session's keys (review II,
// R2-P6-sec-0302).
func (e *initiatedSecureExchange) boundTo(sess *channel.Session, live bool) bool {
	if e.session == nil || (live && sess == e.session) {
		return true
	}
	e.fail(errExchangeSessionGone)
	return false
}

// sessionNow is the session the lookup holds under the exchange's session
// id, and whether it holds one.
func (b *Bridge) sessionNow(sessionID uint16) (*channel.Session, bool) {
	b.mu.RLock()
	sessions := b.sessions
	b.mu.RUnlock()
	if sessions == nil {
		return nil, false
	}
	return sessions.Lookup(sessionID)
}

func (b *Bridge) openInitiatedExchange(link peerLink) (*initiatedSecureExchange, error) {
	b.mu.RLock()
	sessions := b.sessions
	b.mu.RUnlock()
	if sessions == nil {
		return nil, ErrUnsolicitedSessionMissing
	}
	sess, ok := sessions.Lookup(link.sessionID)
	if !ok {
		return nil, ErrUnsolicitedSessionMissing
	}
	ex := &initiatedSecureExchange{
		b: b, sessionID: link.sessionID, addr: link.addr, session: sess,
		createdAt: b.clk().Now(), createdAtMono: clock.MonotonicOf(b.clk()), in: make(chan bdx.Message, 16), done: make(chan struct{}),
	}
	ex.touch()
	// The id skips one an open exchange this node opened on the session
	// holds, so the counter coming round never gives two open exchanges
	// one key (review II, R2-P2-ctr-03). Every id held means 65536 open
	// exchanges on one session, past any bound; the open fails.
	for range 1 << 16 {
		ex.exchangeID = b.nextOutboundExchangeID()
		if _, held := b.resumption.initiated.LoadOrStore(secureExchangeKey{sessionID: ex.sessionID, exchangeID: ex.exchangeID}, ex); !held {
			b.boundSessionExchanges(ex.sessionID, ex)
			return ex, nil
		}
	}
	return nil, errExchangeIDsExhausted
}

// peerLossCloser is the optional [SessionRegistry] capability that
// force-closes every session of a peer created before a monotonic
// reading of the registry's clock (secure/operational.Manager.ClosePeerLostSince).
type peerLossCloser interface {
	ClosePeerLostSince(fabricIndex uint8, peerNodeID uint64, sinceMono time.Duration) int
}

// handleInitiatedPeerLoss is the outbound-reliable tracker giving up on a
// message of an exchange this node opened: the peer is lost. Every
// session with that peer is force-closed — the next
// [Bridge.ConnectOtaProvider] then runs a fresh CASE instead of reusing
// a session the peer no longer knows (a provider restarted under the same
// node id answers nothing on the old keys). Mirrors matter.js
// MessageExchange.ts:643-651 at db021da0 (the send failed with a peer
// communication error → context.peerLost) → ExchangeManager.ts:621-644
// (a commissioned peer: SessionManager.handlePeerLoss for the peer
// address, asOf the exchange context's creation; otherwise the session
// alone) → SessionManager.ts:686-688.
// An exhausted counter of a subscription report is no peer loss
// ([Bridge.noteReportUndelivered]).
func (b *Bridge) handleInitiatedPeerLoss(sessionID, exchangeID uint16) {
	raw, ok := b.resumption.initiated.Load(secureExchangeKey{sessionID: sessionID, exchangeID: exchangeID})
	if !ok {
		return
	}
	ex, ok := raw.(*initiatedSecureExchange)
	if !ok {
		return
	}
	b.closeLostPeer(sessionID, exchangeID, ex.createdAtMono, "an exchange this node opened")
	ex.fail(mrp.ErrMaxRetransmissionsReached)
}

// closeLostPeer force-closes the sessions of the peer of sessionID that
// were created before sinceMono, the clock's monotonic reading of the
// exchange's creation ([clock.MonotonicOf]) — every one with a commissioned peer
// (SessionManager.ts:677-695 #handlePeerLoss), the session alone without a
// fabric or node id (ExchangeManager.ts:561-571). The sessions'
// subscriptions fall with them through the registry's close hook. what
// names the exchange for the log.
func (b *Bridge) closeLostPeer(sessionID, exchangeID uint16, sinceMono time.Duration, what string) {
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
		closed = closer.ClosePeerLostSince(fabricIndex, peerNodeID, sinceMono)
	} else if err := registry.Close(sessionID); err == nil {
		closed = 1
	}
	if b.logger != nil {
		b.logger.Info("matter.session.peer_lost",
			slog.Int("session_id", int(sessionID)),
			slog.Int("exchange_id", int(exchangeID)),
			slog.Int("fabric_index", int(fabricIndex)),
			slog.Uint64("node_id", peerNodeID),
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
	if !ex.boundTo(b.sessionNow(hdr.SessionID)) {
		// The exchange's session closed and its id names another
		// session now: the message is that session's, for the ordinary
		// dispatch.
		return false
	}
	msg := bdx.Message{ProtocolID: proto.ProtocolID, Opcode: proto.Opcode, Payload: append([]byte(nil), payload...)}
	if !ex.enqueue(msg) && !ex.ended() {
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
		return e.closedErr()
	default:
	}
	b := e.b
	b.mu.RLock()
	listener := b.listener
	sessions := b.sessions
	tracker := b.outboundReliable
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
	if !e.boundTo(sess, ok) {
		return e.closedErr()
	}
	if !ok {
		return ErrUnsolicitedSessionMissing
	}
	e.touch()
	proto := message.ProtocolHeader{
		Initiator:  !e.responder,
		Opcode:     opcode,
		ExchangeID: e.exchangeID,
		ProtocolID: protocolID,
		NeedsAck:   tracker != nil,
	}
	if counter, owed := b.takeOwedAck(e.sessionID, e.exchangeID, !e.responder); owed {
		proto.HasAck, proto.AckCounter = true, counter
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
		tracker.TrackWithMargin(hdr.MessageCounter, e.sessionID, e.exchangeID, datagram, dest, e.b.clk().Now(), time.Duration(e.peerMargin.Load()))
		// The session may have closed since the check above, its entries
		// dropped by the purge before this one was tracked: the entry is
		// taken back and the exchange ends, so none of a closed session
		// stays under its bare id (R2-P6-sec-0302).
		if now, live := b.sessionNow(e.sessionID); e.session != nil && (!live || now != e.session) {
			tracker.Untrack(e.sessionID, hdr.MessageCounter)
			e.fail(errExchangeSessionGone)
			return e.closedErr()
		}
	}
	if err := e.b.sendRouted(listener, dest, e.sessionID, datagram); err != nil {
		if tracker != nil {
			tracker.Untrack(e.sessionID, hdr.MessageCounter)
		}
		if isConnRoute(dest) {
			// A send on a connection that is gone is the reliable
			// transport's "retransmissions exhausted": the peer is lost
			// on that session, as matter.js's send reports peerLost on a
			// failed send (MessageExchange.ts:643-651 at db021da0) — the
			// next connection to the peer runs a fresh CASE.
			b.handleInitiatedPeerLoss(e.sessionID, e.exchangeID)
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
		return bdx.Message{}, e.closedErr()
	case <-ctx.Done():
		return bdx.Message{}, ctx.Err()
	}
}

// MaxPayloadSize implements [bdx.Exchange]: the channel's maximum less the
// message overhead, as matter.js MessageExchange.maxPayloadSize computes
// it (packages/protocol/src/protocol/MessageExchange.ts:465-469 at
// db021da0) — over UDP [exchangeMaxPayloadSize], over a TCP connection the
// connection's largest message (tcp.Conn.MaxMessageSize,
// MAX_TCP_MESSAGE_SIZE without the length prefix, TcpChannel.ts:37-41)
// capped at the MaxTcpMessageSize the session's peer stated, less the same
// overhead: matter.js's MessageChannel.maxPayloadSize caps a TCP channel at
// Session.peerMaxTcpMessageSize (MessageChannel.ts:86-92), never below the
// UDP size (Session.ts:237-239). [Bridge.invokeMaxPayloadSize] computes
// it, the bound a reply on the session keeps too ([Bridge.chunkLimitsFor]).
func (e *initiatedSecureExchange) MaxPayloadSize() int {
	return e.b.invokeMaxPayloadSize(e.route(), e.sessionID)
}

// invokeMaxPayloadSize is the largest payload one message of a request's
// exchange carries, for the invoke context ([im.WithMaxPayloadSize]) and
// for an exchange of the session this node opens or answers on
// ([initiatedSecureExchange.MaxPayloadSize]): the route's message limit
// ([Bridge.connMessageLimit] — over TCP the connection's maximum bounded
// by the peer's, matter.js MessageChannel.maxPayloadSize) less
// MATTER_MESSAGE_OVERHEAD (packages/protocol/src/protocol/MessageExchange.ts:465-469
// at db021da0), and [exchangeMaxPayloadSize] over UDP; never below that.
func (b *Bridge) invokeMaxPayloadSize(route net.Addr, sessionID uint16) int {
	size, ok := b.connMessageLimit(route, sessionID)
	if !ok {
		return exchangeMaxPayloadSize
	}
	return max(size-matterMessageOverhead, exchangeMaxPayloadSize)
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
		// Its own entry only: the key may name a newer exchange by now.
		key := secureExchangeKey{sessionID: e.sessionID, exchangeID: e.exchangeID}
		if e.responder {
			e.b.bdx.exchanges.CompareAndDelete(key, e)
		} else {
			e.b.resumption.initiated.CompareAndDelete(key, e)
		}
		e.b.expediteDuplicateAck(e.sessionID, e.exchangeID, !e.responder)
		e.b.RunAckPumpOnce(e.b.clk().Now())
	})
	return nil
}

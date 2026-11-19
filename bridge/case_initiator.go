// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// This file is the bridge's CASE *initiator*. It exists for one caller —
// re-establishing former subscriptions after a restart
// ([Bridge.ReestablishFormerSubscriptions], docs/adr/0008) — and reaches
// only a peer that already held a subscription here, on a fabric this node
// is on. It is not a controller role: there is no API that opens a session
// to an arbitrary node. Until their removal the deprecated
// [Bridge.ConnectPeer] and [Bridge.ConnectOtaProvider] reach it too; a node
// establishes its other peer sessions on its client stack (docs/adr/0032).

// Errors of the initiator path.
var (
	// ErrCaseInitiatorMissing: no [CaseInitiatorProvider] is attached.
	ErrCaseInitiatorMissing = errors.New("bridge: no CASE initiator provider attached")
	// ErrOperationalResolverMissing: no [OperationalResolver] is attached.
	ErrOperationalResolverMissing = errors.New("bridge: no operational resolver attached")
	// ErrCaseRejected: the peer answered the handshake with a failure
	// StatusReport.
	ErrCaseRejected = errors.New("bridge: peer rejected the CASE handshake")
)

// CaseInitiation is one CASE handshake this node opens as the initiator,
// prepared by the host's [CaseInitiatorProvider].
type CaseInitiation struct {
	// Initiator is a fresh [sigma.NewPeerInitiator] for the peer, carrying
	// this node's identity on the peer's fabric, the local session id it
	// reserved, and — when the host holds one — the resumption record of
	// the peer (matter.js CaseClient offers resumption whenever
	// SessionManager has a record for the peer address).
	Initiator *sigma.Initiator
	// CompressedFabricID names the peer's operational DNS-SD instance.
	CompressedFabricID [8]byte
	// OnEstablished registers the session under Initiator.SessionID()
	// (secure/operational.Manager.OpenFromSigmaAsInitiatorWithStake) and may
	// persist the new resumption record. ctx is the handshake's. Returning
	// an error abandons the session.
	OnEstablished func(ctx context.Context, result sigma.InitiatorResult) error
	// OnAbandoned, optional, releases what the provider reserved — the
	// local session id — when the handshake does not complete.
	OnAbandoned func()
}

// CaseInitiatorProvider prepares a CASE handshake towards peerNodeID on the
// fabric at fabricIndex. It is consulted to re-establish former
// subscriptions, and by the deprecated [Bridge.ConnectPeer] until its
// removal; ctx bounds the attempt.
type CaseInitiatorProvider func(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (*CaseInitiation, error)

// OperationalResolver finds the addresses of a peer's operational instance
// (`<CompressedFabricID>-<NodeID>._matter._tcp.local`), most desirable
// first. *mdns.OperationalResolver implements it.
type OperationalResolver interface {
	ResolveOperational(ctx context.Context, compressedFabricID [8]byte, nodeID uint64) ([]*net.UDPAddr, error)
}

// AttachCaseInitiatorProvider wires the CASE initiator the re-establishment
// of former subscriptions uses. Unattached (the default), every former
// subscription is dropped at [Bridge.ReestablishFormerSubscriptions] —
// silently: each controller recovers through its own liveness timeout.
func (b *Bridge) AttachCaseInitiatorProvider(p CaseInitiatorProvider) {
	b.resumption.mu.Lock()
	b.resumption.caseInitiator = p
	b.resumption.mu.Unlock()
}

// AttachOperationalResolver wires the operational address resolution the
// re-establishment of former subscriptions uses. Unattached (the
// default), every former subscription is dropped, as with no initiator.
func (b *Bridge) AttachOperationalResolver(r OperationalResolver) {
	b.resumption.mu.Lock()
	b.resumption.resolver = r
	b.resumption.mu.Unlock()
}

// peerLink is a secure session to a peer, ready to carry reports.
type peerLink struct {
	sessionID uint16
	// addr is the session's route: the peer's UDP address, or the
	// *connRoute of the TCP connection the session is bound to.
	addr net.Addr
}

// peerSessionLister is the optional [SessionRegistry] capability that
// names the live sessions of a peer (secure/operational.Manager has it).
type peerSessionLister interface {
	SessionIDsForPeer(fabricIndex uint8, peerNodeID uint64) []uint16
}

// connectPeer obtains a secure session with the peer, establishing one as
// necessary — matter.js Peer.connect: reuse the newest session, otherwise
// discover the peer and run CASE (PeerConnection → CaseClient.pair). ctx
// bounds the whole attempt (matter.js `connectionTimeout`).
func (b *Bridge) connectPeer(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (peerLink, error) {
	if link, ok := b.existingPeerSession(fabricIndex, peerNodeID); ok {
		return link, nil
	}
	b.resumption.mu.Lock()
	provider := b.resumption.caseInitiator
	resolver := b.resumption.resolver
	b.resumption.mu.Unlock()
	if provider == nil {
		return peerLink{}, ErrCaseInitiatorMissing
	}
	if resolver == nil {
		return peerLink{}, ErrOperationalResolverMissing
	}
	ctx, done := b.beginInitiation(ctx, fabricIndex)
	defer done()
	link, err := b.initiateCASE(ctx, provider, resolver, fabricIndex, peerNodeID)
	if err != nil && errors.Is(context.Cause(ctx), errFabricUpdated) {
		return peerLink{}, fmt.Errorf("%w: %w", errFabricUpdated, err)
	}
	return link, err
}

// initiateCASE is [Bridge.connectPeer]'s handshake: prepare, resolve, and
// run CASE to each address until one completes.
func (b *Bridge) initiateCASE(ctx context.Context, provider CaseInitiatorProvider, resolver OperationalResolver, fabricIndex uint8, peerNodeID uint64) (peerLink, error) {
	init, err := provider(ctx, fabricIndex, peerNodeID)
	if err != nil {
		return peerLink{}, fmt.Errorf("bridge: prepare CASE: %w", err)
	}
	if init == nil || init.Initiator == nil {
		return peerLink{}, errors.New("bridge: CASE initiator provider returned no initiator")
	}
	// The resolution has no other trace: a query that waits on it until
	// ctx ends leaves a log that stops at the caller's last line
	// (TC-SU-2.4 in CI, 2026-10-31: Querying reported, no Sigma1 sent in
	// 60 s), so its start and outcome are logged here.
	started := b.clk().Now()
	b.logger.Debug("matter.case.initiator.resolving", slog.Int("fabric_index", int(fabricIndex)), slog.Uint64("node_id", peerNodeID))
	addrs, err := resolver.ResolveOperational(ctx, init.CompressedFabricID, peerNodeID)
	if err != nil {
		b.logger.Debug("matter.case.initiator.resolve_failed", slog.Uint64("node_id", peerNodeID), slog.Duration("took", b.clk().Now().Sub(started)), slog.Any("err", err))
		abandon(init)
		return peerLink{}, err
	}
	b.logger.Debug("matter.case.initiator.resolved", slog.Uint64("node_id", peerNodeID), slog.Int("addresses", len(addrs)), slog.Duration("took", b.clk().Now().Sub(started)))
	var lastErr error
	for i, addr := range addrs {
		if i > 0 {
			// An initiator runs one handshake; each further address gets
			// a fresh one.
			if init, err = provider(ctx, fabricIndex, peerNodeID); err != nil || init == nil || init.Initiator == nil {
				break
			}
		}
		sessionID, err := b.runCASEInitiator(ctx, addr, init)
		if err == nil {
			return peerLink{sessionID: sessionID, addr: addr}, nil
		}
		abandon(init)
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("bridge: no operational address for peer")
	}
	return peerLink{}, lastErr
}

// errFabricUpdated ends a CASE initiation whose fabric an UpdateNOC
// replaced ([Bridge.abortFabricInitiations]).
var errFabricUpdated = errors.New("bridge: the fabric of the CASE initiation was updated")

// beginInitiation registers a CASE initiation on fabricIndex: the returned
// context ends with errFabricUpdated when the fabric is updated before the
// initiation is done; done deregisters it.
func (b *Bridge) beginInitiation(ctx context.Context, fabricIndex uint8) (initCtx context.Context, done func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	in := &caseInitiating{fabricIndex: fabricIndex, cancel: cancel}
	b.resumption.mu.Lock()
	if b.resumption.initiating == nil {
		b.resumption.initiating = make(map[*caseInitiating]struct{})
	}
	b.resumption.initiating[in] = struct{}{}
	b.resumption.mu.Unlock()
	return ctx, func() {
		b.resumption.mu.Lock()
		delete(b.resumption.initiating, in)
		b.resumption.mu.Unlock()
		cancel(nil)
	}
}

// abortFabricInitiations ends every CASE initiation this node runs on
// fabricIndex: its handshake stops at its next wait and is abandoned, its
// caller sees errFabricUpdated. connectedhomeip aborts a pending
// establishment on the updated fabric in either role — the initiator's
// CASESession registers for the fabric's events as the responder's does
// (src/protocols/secure_channel/CASESession.cpp:476, :529, the abort
// :423-433 at 6170af84); matter.js's initiator has no such abort: the
// fabric is read once per attempt and the handshake's only abort is the
// caller's signal (packages/protocol/src/peer/PeerConnection.ts:568,
// packages/protocol/src/session/case/CaseClient.ts:67 at db021da0). The
// session admission's identity compare stays the backstop for a handshake
// that completes before its abort is seen. Returns how many it ended.
// Review II, R2-P6-sec-2412, owner decision 233.
func (b *Bridge) abortFabricInitiations(fabricIndex uint8) int {
	b.resumption.mu.Lock()
	defer b.resumption.mu.Unlock()
	ended := 0
	for in := range b.resumption.initiating {
		if in.fabricIndex == fabricIndex {
			in.cancel(errFabricUpdated)
			ended++
		}
	}
	return ended
}

func abandon(init *CaseInitiation) {
	if init != nil && init.OnAbandoned != nil {
		init.OnAbandoned()
	}
}

// newestPeerSessionFinder is the optional [SessionRegistry] capability
// that names the live session of a peer the peer sent on last
// (secure/operational.Manager.NewestSessionForPeer has it). Its contract,
// which livePeerLink relies on: usable is asked newest first and not
// again after its first true, whose id is the one returned.
type newestPeerSessionFinder interface {
	NewestSessionForPeer(fabricIndex uint8, peerNodeID uint64, usable func(id uint16) bool) (uint16, bool)
}

// existingPeerSession returns the newest live session with the peer whose
// address is known, of any transport — matter.js Peer.connect returns
// `newestSession()` before it considers a new handshake.
func (b *Bridge) existingPeerSession(fabricIndex uint8, peerNodeID uint64) (peerLink, bool) {
	return b.livePeerLink(fabricIndex, peerNodeID, PeerSessionRequirement{})
}

// livePeerLink returns the live session with the peer that meets req and
// whose route is known, the one the peer sent on last — matter.js
// newestSession, the highest activeTimestamp among the sessions neither
// closing nor peer-lost, optionally of one channel type
// (packages/protocol/src/peer/Peer.ts:582-602 at db021da0), the
// requirement being that channel type: a required transport is the only
// one considered (:369-376, :440-448). A registry without the activity
// order falls back to the highest live id.
func (b *Bridge) livePeerLink(fabricIndex uint8, peerNodeID uint64, req PeerSessionRequirement) (peerLink, bool) {
	b.mu.RLock()
	reg := b.sessionRegistry
	lookup := b.sessions
	b.mu.RUnlock()
	if lookup == nil {
		return peerLink{}, false
	}
	var link peerLink
	usable := func(id uint16) bool {
		if _, live := lookup.Lookup(id); !live {
			return false
		}
		raw, known := b.sessionPeerAddrs.Load(id)
		addr, isAddr := raw.(net.Addr)
		if !known || !isAddr || isNilRoute(addr) {
			return false
		}
		// A Large Message request needs a session that carries it: one
		// bound to a TCP connection (connectedhomeip attaches only to a
		// session with the wanted payload capability,
		// src/app/OperationalSessionSetup.cpp:75-94 at 6170af84).
		if req.LargeMessage && !routeCarriesLargeMessages(addr) {
			return false
		}
		link = peerLink{sessionID: id, addr: addr}
		return true
	}
	if f, ok := reg.(newestPeerSessionFinder); ok {
		if _, found := f.NewestSessionForPeer(fabricIndex, peerNodeID, usable); found {
			return link, true
		}
		return peerLink{}, false
	}
	lister, ok := reg.(peerSessionLister)
	if !ok {
		return peerLink{}, false
	}
	ids := lister.SessionIDsForPeer(fabricIndex, peerNodeID)
	for i := len(ids) - 1; i >= 0; i-- {
		if usable(ids[i]) {
			return link, true
		}
	}
	return peerLink{}, false
}

// initiatedMessage is one Secure-Channel message the peer sent on an
// exchange this node opened.
type initiatedMessage struct {
	opcode  uint8
	counter uint32
	payload []byte
}

// initiatedExchange is the rendezvous of one initiated CASE exchange.
type initiatedExchange struct {
	ch chan initiatedMessage
}

// deliverInitiatedSecureChannel hands an unsecured Secure-Channel message
// the peer sent as the *responder* on an exchange this node initiated to
// the waiting handshake. Returns false when no handshake owns the exchange.
//
// The handshake is found by its [HandshakeKey]: the exchange id and the
// ephemeral node id this node chose as the initiator, which a response
// names as its destination. A message on the exchange id that names another
// initiator is not this handshake's — matter.js finds the unsecured session
// of a response by that destination node id ("Responses include our
// ephemeral initiator nodeId as destNodeId",
// packages/protocol/src/protocol/ExchangeManager.ts:306-312 at db021da0) and
// the exchange inside it (:398-401), as the responder side does here.
func (b *Bridge) deliverInitiatedSecureChannel(hdr *message.Header, proto message.ProtocolHeader, payload []byte) bool {
	if proto.Initiator || hdr.SessionID != 0 || proto.Opcode == mrp.StandaloneAckOpcode {
		return false
	}
	raw, ok := b.resumption.initiated.Load(handshakeKeyOf(hdr, proto.ExchangeID))
	if !ok {
		return false
	}
	ex, ok := raw.(*initiatedExchange)
	if !ok {
		return false
	}
	select {
	case ex.ch <- initiatedMessage{opcode: proto.Opcode, counter: hdr.MessageCounter, payload: append([]byte(nil), payload...)}:
	default: // a retransmitted duplicate while the handshake is busy; MRP re-sends it
	}
	return true
}

// runCASEInitiator drives one CASE handshake to addr over the bridge's UDP
// socket and returns the local session id. Mirrors matter.js
// CaseClient.ts:pair on an unsecured exchange: Sigma1 → Sigma2 → Sigma3 →
// StatusReport(Success), or Sigma1 → Sigma2_Resume → StatusReport(Success)
// sent by the initiator.
func (b *Bridge) runCASEInitiator(ctx context.Context, addr *net.UDPAddr, init *CaseInitiation) (uint16, error) {
	exchangeID := b.nextOutboundExchangeID()
	// The unsecured session of the initiator is identified by an
	// ephemeral source node id (Matter §4.13.2.1 / chip
	// UnauthenticatedSession); the responder addresses its replies to it,
	// and the handshake takes only replies addressed so.
	ephemeral, err := randomEphemeralNodeID()
	if err != nil {
		return 0, err
	}
	key := HandshakeKey{InitiatorNodeID: ephemeral, ExchangeID: exchangeID}
	ex := &initiatedExchange{ch: make(chan initiatedMessage, 4)}
	b.resumption.initiated.Store(key, ex)
	defer b.resumption.initiated.Delete(key)
	// A handshake that completes takes what it sent and the peer has not
	// acknowledged with it, before its own last message goes out. One that
	// fails drops nothing: a Sigma1 or Sigma3 still unacknowledged goes on
	// being retransmitted until it is acknowledged, its retries are spent or
	// [unsecuredEntryLifetime] is over, as on the responder side
	// ([Bridge.sendHandshakeFailureReport]; owner decision 173), and so
	// does the StatusReport that reports the failure. The unsecured
	// session's entries of this handshake only: exchange ids are per
	// initiator, and a secure session's peer or another initiator may have
	// picked the same one.
	abandon := func() { b.abandonHandshakeReplies(key) }

	sigma1, err := init.Initiator.GenerateSigma1()
	if err != nil {
		return 0, err
	}
	if err := b.sendInitiatedUnsecured(addr, exchangeID, ephemeral, mrp.SCOpcodeSigma1, sigma1, nil); err != nil {
		return 0, err
	}

	reply, err := awaitInitiated(ctx, ex, mrp.SCOpcodeSigma2, mrp.SCOpcodeSigma2Resume)
	if err != nil {
		return 0, err
	}
	switch reply.opcode {
	case mrp.SCOpcodeSigma2:
		sigma3, perr := init.Initiator.ProcessSigma2Bytes(reply.payload)
		if perr != nil {
			b.sendCASEError(addr, exchangeID, ephemeral, reply.counter, b.handshakeReplyUnacknowledged(key))
			return 0, perr
		}
		if err := b.sendInitiatedUnsecured(addr, exchangeID, ephemeral, mrp.SCOpcodeSigma3, sigma3, &reply.counter); err != nil {
			return 0, err
		}
		status, err := awaitInitiated(ctx, ex)
		if err != nil {
			return 0, err
		}
		if err := statusReportSuccess(status.payload); err != nil {
			return 0, err
		}
		abandon()
		// The responder sent its StatusReport reliably and the exchange
		// ends here, so it is acknowledged now — matter.js
		// MessageExchange sends the standalone ack of the last received
		// message when an exchange is destroyed (MessageExchange.ts destroy).
		// Unacknowledged, the responder retransmitted it until its MRP
		// budget ran out (seen against matter.js's controller).
		b.sendInitiatedAck(addr, exchangeID, ephemeral, status.counter)
	case mrp.SCOpcodeSigma2Resume:
		if perr := init.Initiator.ProcessSigma2Resume(reply.payload); perr != nil {
			b.sendCASEError(addr, exchangeID, ephemeral, reply.counter, b.handshakeReplyUnacknowledged(key))
			return 0, perr
		}
		abandon()
		ok := mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolSessionEstablishmentSuccess, nil)
		// matter.js CaseClient: "Error sending Sigma2Resume-Success,
		// assume session still valid" — a send failure is not fatal.
		if err := b.sendInitiatedUnsecured(addr, exchangeID, ephemeral, mrp.SCOpcodeStatusReport, ok, &reply.counter); err != nil {
			b.logger.Debug("matter.case.initiator.resume_success_send", slog.Any("err", err))
		}
	}

	result, ok := init.Initiator.Result()
	if !ok {
		return 0, errors.New("bridge: CASE initiator finished without a result")
	}
	if init.OnEstablished != nil {
		if err := init.OnEstablished(ctx, result); err != nil {
			return 0, fmt.Errorf("bridge: register initiated session: %w", err)
		}
	}
	sessionID := init.Initiator.SessionID()
	// Bound as every session is bound, its owner recorded, so it never
	// moves between transports (review II, R2-P6-conc-0314).
	b.bindSessionRoute(sessionID, addr)
	b.recordSessionsOpen()
	b.logger.Info("matter.case.initiator.established",
		slog.String("peer", addr.String()),
		slog.Int("session_id", int(sessionID)),
		slog.Bool("resumed", result.Resumed))
	return sessionID, nil
}

// awaitInitiated waits for the next message on the exchange. A
// StatusReport always ends the wait; with opcodes given, any other opcode
// is skipped as a stray retransmission.
func awaitInitiated(ctx context.Context, ex *initiatedExchange, opcodes ...uint8) (initiatedMessage, error) {
	for {
		select {
		case <-ctx.Done():
			return initiatedMessage{}, ctx.Err()
		case msg := <-ex.ch:
			if msg.opcode == mrp.SCOpcodeStatusReport {
				if len(opcodes) == 0 {
					return msg, nil
				}
				return msg, statusReportSuccessOr(msg.payload, ErrCaseRejected)
			}
			for _, op := range opcodes {
				if msg.opcode == op {
					return msg, nil
				}
			}
		}
	}
}

// statusReportSuccess decodes a CASE StatusReport and returns nil only for
// SessionEstablishmentSuccess.
func statusReportSuccess(payload []byte) error {
	return statusReportSuccessOr(payload, ErrCaseRejected)
}

func statusReportSuccessOr(payload []byte, failure error) error {
	general, protocolID, code, ok := decodeStatusReport(payload)
	if ok && general == mrp.SCStatusGeneralSuccess && protocolID == uint32(mrp.SecureChannelProtocolID) && code == mrp.SCStatusProtocolSessionEstablishmentSuccess {
		return nil
	}
	return fmt.Errorf("%w: general=%d protocol=%#x code=%d", failure, general, protocolID, code)
}

// sendCASEError reports a failed handshake to the peer, best effort —
// matter.js CaseClient.pair sends StatusReport(InvalidParam) unless the
// failure was a transport one. The report is tracked like the rest. Nothing is
// sent when sigma1Unacked: the Sigma1 still waits for its acknowledgement,
// and the handshake ends unreported (review II, F-db021da0-7;
// [Bridge.endHandshakeUnreported] on the responder side, which also says
// where the guard is still reached since decision 277: a reply that does
// not acknowledge the Sigma1 is refused on receive).
func (b *Bridge) sendCASEError(addr *net.UDPAddr, exchangeID uint16, ephemeral uint64, ackCounter uint32, sigma1Unacked bool) {
	if sigma1Unacked {
		b.logger.Debug("matter.sc.handshake.failure_report_skipped",
			slog.String("stage", "case_initiator"),
			slog.Int("exchange_id", int(exchangeID)),
			slog.String("reason", "reply_unacknowledged"))
		return
	}
	report := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolInvalidParameter, nil)
	if err := b.sendInitiatedUnsecured(addr, exchangeID, ephemeral, mrp.SCOpcodeStatusReport, report, &ackCounter); err != nil {
		b.logger.Debug("matter.case.initiator.error_send", slog.Any("err", err))
	}
}

// sendInitiatedUnsecured ships one unsecured Secure-Channel message on an
// exchange this node initiated, reliably when the MRP tracker is wired —
// and then always tracked: it shares the unsecured session's table with
// the replies to peers' handshakes, where a full table gives up its oldest
// entry and never leaves a new message untracked ([unsecuredEntryLimit]).
// ackCounter, when non-nil, piggybacks the acknowledgement of the peer's
// last message and discharges the pump's standalone-ack obligation for it.
func (b *Bridge) sendInitiatedUnsecured(addr *net.UDPAddr, exchangeID uint16, ephemeral uint64, opcode uint8, payload []byte, ackCounter *uint32) error {
	b.mu.RLock()
	listener := b.listener
	tracker := b.outboundReliable
	b.mu.RUnlock()
	if listener == nil {
		return ErrUnsolicitedListenerMissing
	}
	proto := message.ProtocolHeader{
		Initiator:  true,
		Opcode:     opcode,
		ExchangeID: exchangeID,
		ProtocolID: mrp.SecureChannelProtocolID,
		NeedsAck:   tracker != nil,
	}
	if ackCounter != nil {
		proto.HasAck = true
		proto.AckCounter = *ackCounter
	}
	hdr := message.Header{
		SessionID:       0,
		MessageCounter:  b.nextUnsecuredCounter(),
		HasSourceNodeID: true,
		SourceNodeID:    ephemeral,
	}
	datagram := append(hdr.Marshal(), proto.Marshal()...) //nolint:gocritic // single-allocation join
	datagram = append(datagram, payload...)
	// Tracked before it is sent, so an ack handled before Send returns is
	// not lost (bridge/reply.go sendReplyReliable).
	if tracker != nil {
		tracker.trackUnsecured(hdr.MessageCounter, HandshakeKey{InitiatorNodeID: ephemeral, ExchangeID: exchangeID}, datagram, addr, b.clk().Now(), nil)
	}
	if err := listener.Send(addr, datagram); err != nil {
		if tracker != nil {
			tracker.Untrack(0, hdr.MessageCounter)
		}
		return err
	}
	if ackCounter != nil {
		b.dischargeOwedHandshakeAck(HandshakeKey{InitiatorNodeID: ephemeral, ExchangeID: exchangeID}, true)
	}
	return nil
}

// sendInitiatedAck sends the standalone acknowledgement of counter on an
// unsecured exchange this node initiated. Best effort: a lost ack makes the
// peer retransmit, which the unsecured handler answers by nothing worse.
func (b *Bridge) sendInitiatedAck(addr *net.UDPAddr, exchangeID uint16, ephemeral uint64, counter uint32) {
	b.mu.RLock()
	listener := b.listener
	b.mu.RUnlock()
	if listener == nil {
		return
	}
	proto := message.ProtocolHeader{
		Initiator:  true,
		Opcode:     mrp.StandaloneAckOpcode,
		ExchangeID: exchangeID,
		ProtocolID: mrp.SecureChannelProtocolID,
		HasAck:     true,
		AckCounter: counter,
	}
	hdr := message.Header{
		SessionID:       0,
		MessageCounter:  b.nextUnsecuredCounter(),
		HasSourceNodeID: true,
		SourceNodeID:    ephemeral,
	}
	datagram := append(hdr.Marshal(), proto.Marshal()...) //nolint:gocritic // single-allocation join
	if err := listener.Send(addr, datagram); err != nil {
		b.logger.Debug("matter.case.initiator.ack_send", slog.Any("err", err))
	}
	b.dischargeOwedHandshakeAck(HandshakeKey{InitiatorNodeID: ephemeral, ExchangeID: exchangeID}, true)
}

// randomEphemeralNodeID draws the initiator's ephemeral node id for the
// unsecured session: random, non-zero.
func randomEphemeralNodeID() (uint64, error) {
	var b [8]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			return 0, fmt.Errorf("bridge: ephemeral node id: %w", err)
		}
		if id := binary.LittleEndian.Uint64(b[:]); id != 0 {
			return id, nil
		}
	}
}

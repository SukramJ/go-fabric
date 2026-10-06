// SPDX-License-Identifier: MIT
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
	"time"

	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// This file is the bridge's CASE *initiator*. It exists for exactly one
// caller — re-establishing former subscriptions after a restart
// ([Bridge.ReestablishFormerSubscriptions], docs/adr/0008) — and reaches
// only a peer that already held a subscription here, on a fabric this node
// is on. It is not a controller role: there is no API that opens a session
// to an arbitrary node.

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
	// (secure/operational.Manager.OpenFromSigmaAsInitiatorWithID) and may
	// persist the new resumption record. ctx is the handshake's. Returning
	// an error abandons the session.
	OnEstablished func(ctx context.Context, result sigma.InitiatorResult) error
	// OnAbandoned, optional, releases what the provider reserved — the
	// local session id — when the handshake does not complete.
	OnAbandoned func()
}

// CaseInitiatorProvider prepares a CASE handshake towards peerNodeID on the
// fabric at fabricIndex. It is consulted only to re-establish former
// subscriptions; ctx bounds the attempt.
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
	addr      *net.UDPAddr
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

	init, err := provider(ctx, fabricIndex, peerNodeID)
	if err != nil {
		return peerLink{}, fmt.Errorf("bridge: prepare CASE: %w", err)
	}
	if init == nil || init.Initiator == nil {
		return peerLink{}, errors.New("bridge: CASE initiator provider returned no initiator")
	}
	addrs, err := resolver.ResolveOperational(ctx, init.CompressedFabricID, peerNodeID)
	if err != nil {
		abandon(init)
		return peerLink{}, err
	}
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

func abandon(init *CaseInitiation) {
	if init != nil && init.OnAbandoned != nil {
		init.OnAbandoned()
	}
}

// existingPeerSession returns the newest live session with the peer whose
// address is known — matter.js Peer.connect returns `newestSession()`
// before it considers a new handshake.
func (b *Bridge) existingPeerSession(fabricIndex uint8, peerNodeID uint64) (peerLink, bool) {
	b.mu.RLock()
	reg := b.sessionRegistry
	lookup := b.sessions
	b.mu.RUnlock()
	lister, ok := reg.(peerSessionLister)
	if !ok || lookup == nil {
		return peerLink{}, false
	}
	ids := lister.SessionIDsForPeer(fabricIndex, peerNodeID)
	for i := len(ids) - 1; i >= 0; i-- {
		id := ids[i]
		if _, live := lookup.Lookup(id); !live {
			continue
		}
		raw, known := b.sessionPeerAddrs.Load(id)
		addr, isAddr := raw.(*net.UDPAddr)
		if known && isAddr && addr != nil {
			return peerLink{sessionID: id, addr: addr}, true
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
func (b *Bridge) deliverInitiatedSecureChannel(hdr *message.Header, proto message.ProtocolHeader, payload []byte) bool {
	if proto.Initiator || hdr.SessionID != 0 || proto.Opcode == mrp.StandaloneAckOpcode {
		return false
	}
	raw, ok := b.resumption.initiated.Load(proto.ExchangeID)
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
	ex := &initiatedExchange{ch: make(chan initiatedMessage, 4)}
	b.resumption.initiated.Store(exchangeID, ex)
	defer b.resumption.initiated.Delete(exchangeID)
	defer func() {
		b.mu.RLock()
		tracker := b.outboundReliable
		b.mu.RUnlock()
		if tracker != nil {
			tracker.AbandonExchange(exchangeID)
		}
	}()

	// The unsecured session of the initiator is identified by an
	// ephemeral source node id (Matter §4.13.2.1 / chip
	// UnauthenticatedSession); the responder addresses its replies to it.
	ephemeral, err := randomEphemeralNodeID()
	if err != nil {
		return 0, err
	}
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
			b.sendCASEError(addr, exchangeID, ephemeral, reply.counter)
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
		// The responder sent its StatusReport reliably and the exchange
		// ends here, so it is acknowledged now — matter.js
		// MessageExchange sends the standalone ack of the last received
		// message when an exchange is destroyed (MessageExchange.ts destroy).
		// Unacknowledged, the responder retransmitted it until its MRP
		// budget ran out (seen against matter.js's controller).
		b.sendInitiatedAck(addr, exchangeID, ephemeral, status.counter)
	case mrp.SCOpcodeSigma2Resume:
		if perr := init.Initiator.ProcessSigma2Resume(reply.payload); perr != nil {
			b.sendCASEError(addr, exchangeID, ephemeral, reply.counter)
			return 0, perr
		}
		ok := mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolSessionEstablishmentSuccess, nil)
		// matter.js CaseClient: "Error sending Sigma2Resume-Success,
		// assume session still valid" — a send failure is not fatal.
		if err := b.sendInitiatedUnsecured(addr, exchangeID, ephemeral, mrp.SCOpcodeStatusReport, ok, &reply.counter); err != nil {
			b.logger.Debug("matter.case.initiator.resume_success_send", slog.String("err", err.Error()))
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
	b.sessionPeerAddrs.Store(sessionID, addr)
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
// failure was a transport one.
func (b *Bridge) sendCASEError(addr *net.UDPAddr, exchangeID uint16, ephemeral uint64, ackCounter uint32) {
	report := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolInvalidParameter, nil)
	if err := b.sendInitiatedUnsecured(addr, exchangeID, ephemeral, mrp.SCOpcodeStatusReport, report, &ackCounter); err != nil {
		b.logger.Debug("matter.case.initiator.error_send", slog.String("err", err.Error()))
	}
}

// sendInitiatedUnsecured ships one unsecured Secure-Channel message on an
// exchange this node initiated, reliably when the MRP tracker is wired.
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
	if err := listener.Send(addr, datagram); err != nil {
		return err
	}
	if ackCounter != nil {
		b.dischargeOwedAck(0, exchangeID, true)
	}
	if tracker != nil {
		tracker.Track(hdr.MessageCounter, 0, exchangeID, datagram, addr, time.Now())
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
		b.logger.Debug("matter.case.initiator.ack_send", slog.String("err", err.Error()))
	}
	b.dischargeOwedAck(0, exchangeID, true)
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

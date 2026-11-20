// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/diagevent"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Errors surfaced by the Secure-Channel routing layer. As with the
// IM routing errors, these are logged inside the bridge and dropped
// — the commissioner retries per MRP.
var (
	// ErrPaseHandlerMissing is returned when a PASE opcode arrives
	// but no [PaseHandler] is wired. The bridge still drops the
	// datagram with a debug log so PASE traffic during pre-commissioner
	// boot does not flood the warn channel.
	ErrPaseHandlerMissing = errors.New("bridge: PASE handler not wired")
	// ErrCaseHandlerMissing is returned when a CASE (Sigma) opcode
	// arrives but no [CaseHandler] is wired.
	ErrCaseHandlerMissing = errors.New("bridge: CASE handler not wired")
)

// PaseHandler is the inbound PASE port — the Spake2+ verifier the
// `secure/spake2.Verifier` typically backs. Each method takes the
// inbound TLV payload (already stripped of message + protocol
// headers) and returns either a response opcode + payload pair (the
// router ships it back via [Bridge.sendReply]) or an error. A nil
// response with nil error indicates "no reply needed" (e.g. Pake3
// completes the exchange and the bridge's session manager picks up
// the new session in a separate hook).
//
// Method naming mirrors the spec opcode names. The bridge's router
// fans out by opcode → method, so adding a new PASE opcode is a
// one-line addition both here and in the router's switch.
type PaseHandler interface {
	// ProcessPBKDFParamRequest handles the commissioner's PBKDF param
	// request and produces a PBKDFParamResponse (opcode 0x21).
	ProcessPBKDFParamRequest(payload []byte) (uint8, []byte, error)
	// ProcessPake1 handles Pake1 and produces Pake2 (opcode 0x23).
	ProcessPake1(payload []byte) (uint8, []byte, error)
	// ProcessPake3 handles Pake3 and produces no reply (the bridge's
	// operational manager picks up the freshly-derived session keys
	// out-of-band). A non-nil response is allowed — the router ships
	// it if returned, useful for status acks.
	ProcessPake3(payload []byte) (uint8, []byte, error)
}

// CaseHandler is the inbound CASE (Sigma) port — typically backed
// by `secure/sigma.Responder`.
type CaseHandler interface {
	// ProcessSigma1 handles the commissioner's Sigma1 and produces
	// Sigma2 (opcode 0x31).
	ProcessSigma1(payload []byte) (uint8, []byte, error)
	// ProcessSigma3 handles Sigma3 and produces no reply on success
	// (the operational session is registered out-of-band). May
	// return a status ack.
	ProcessSigma3(payload []byte) (uint8, []byte, error)
	// ProcessSigma2Resume handles the resumption variant. Returns
	// either a Sigma2_Resume reply or an error rejecting the
	// resumption (commissioner falls back to full Sigma1). The bridge's
	// router does not call it: a Sigma2_Resume the responder receives
	// is acknowledged and not answered ([Bridge.dispatchSigma2Resume]).
	ProcessSigma2Resume(payload []byte) (uint8, []byte, error)
}

// AckHandler is the inbound MRP-ack port: the router calls Discharge for
// any Secure Channel datagram that carries HasAck=true regardless of
// opcode. The bridge wires none by default; an [mrp.AckTracker] does not
// back it — its obligations are this node's own acknowledgements, which a
// peer's acknowledgement does not fulfil ([Bridge.AttachAckTracker]).
type AckHandler interface {
	// Discharge marks the obligation for the (session, exchange, role)
	// triple as fulfilled (the peer ACKed our outbound message).
	// Exchange IDs are only unique per session AND per side, so both
	// extra dimensions are needed to keep concurrent controllers — and a
	// bridge-opened exchange colliding with a peer-opened one — from
	// discharging each other's obligations. initiator is the LOCAL
	// side's role. Returns whether an obligation existed
	// (informational; the router does not branch on the result).
	Discharge(sessionID, exchangeID uint16, initiator bool) bool
}

// noopPaseHandler is the default when no PASE handler is wired.
type noopPaseHandler struct{}

func (noopPaseHandler) ProcessPBKDFParamRequest([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, ErrPaseHandlerMissing
}

func (noopPaseHandler) ProcessPake1([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, ErrPaseHandlerMissing
}

func (noopPaseHandler) ProcessPake3([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, ErrPaseHandlerMissing
}

// noopCaseHandler is the default when no CASE handler is wired.
type noopCaseHandler struct{}

func (noopCaseHandler) ProcessSigma1([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, ErrCaseHandlerMissing
}

func (noopCaseHandler) ProcessSigma3([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, ErrCaseHandlerMissing
}

func (noopCaseHandler) ProcessSigma2Resume([]byte) (opcode uint8, payload []byte, err error) {
	return 0, nil, ErrCaseHandlerMissing
}

// noopAckHandler is the default when no ack handler is wired —
// every Discharge returns false (obligation never existed).
type noopAckHandler struct{}

func (noopAckHandler) Discharge(uint16, uint16, bool) bool { return false }

// AttachPaseHandler wires the PASE port. Pass nil to revert to noop.
// Calling this twice replaces the previous handler.
//
// The handler is shared across every PASE exchange. The bridge admits
// one PASE handshake at a time ([Bridge.claimPase]), so commissioners
// pair one after another; a handler scoped to one handshake — its
// initiator and exchange — comes from [Bridge.AttachPaseHandlerProvider]
// instead, and when a provider is wired the singleton handler is
// ignored.
//
// Deprecated: wire PASE through [Bridge.AttachPaseHandlerProvider]; a
// provider that returns one handler for every handshake is this
// method's shape. Removable in v0.25.0.
func (b *Bridge) AttachPaseHandler(h PaseHandler) {
	// A fresh acceptor marks a commissioning-window boundary; give the new
	// window its own PASE brute-force budget and a free
	// single-active-PASE slot.
	b.resetPaseFailures()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.paseInFlight = HandshakeKey{}
	b.paseInFlightSince = time.Time{}
	if h == nil {
		b.paseHandler = noopPaseHandler{}
		return
	}
	b.paseHandler = h
}

// HandshakeKey names one handshake on the unsecured session: the node id of
// its initiator and the exchange id that initiator chose. Exchange ids are
// the initiator's choice and only unique per initiator, so a handshake's
// state is found by both — a message from another initiator naming the same
// exchange id belongs to another handshake. Mirrors matter.js, which finds
// the unsecured session by the initiator's node id and the exchange inside
// that session (packages/protocol/src/protocol/ExchangeManager.ts:308-312 and
// :401 at db021da0).
type HandshakeKey struct {
	// InitiatorNodeID is the initiator's (ephemeral) node id: the message's
	// destination node id when it carries one — a responder's message names
	// the initiator there — else its source node id
	// (ExchangeManager.ts:308, `packet.header.destNodeId ??
	// packet.header.sourceNodeId`). Zero for a message that carries neither.
	InitiatorNodeID uint64
	// ExchangeID is the exchange the initiator opened the handshake on.
	ExchangeID uint16
}

// handshakeKeyOf is the [HandshakeKey] of an unsecured message on
// exchangeID.
func handshakeKeyOf(hdr *message.Header, exchangeID uint16) HandshakeKey {
	key := HandshakeKey{ExchangeID: exchangeID}
	switch {
	case hdr.DestSize == message.DestNodeID:
		key.InitiatorNodeID = hdr.DestNodeID
	case hdr.HasSourceNodeID:
		key.InitiatorNodeID = hdr.SourceNodeID
	}
	return key
}

// PaseHandlerProvider returns a [PaseHandler] scoped to a single
// PASE handshake — one initiator's exchange ([HandshakeKey]). The
// bridge's SecureChannel router calls the provider once per inbound
// PASE opcode (PBKDFParamRequest, Pake1, Pake3) and dispatches to the
// returned handler. Returning the same handler instance for the same
// key across the three opcode arrivals lets the underlying
// [PaseAdapter] track verifier state across Pake1 → Pake3; a message
// of another initiator on the same exchange id has another key and
// must not reach it.
//
// Returning nil drops the datagram with a debug log — the
// commissioner retries.
type PaseHandlerProvider func(key HandshakeKey) PaseHandler

// AttachPaseHandlerProvider wires a per-exchange PASE provider. When
// set, the provider takes precedence over [Bridge.AttachPaseHandler]
// for every inbound PASE opcode. Pass nil to clear and fall back to
// the singleton handler.
func (b *Bridge) AttachPaseHandlerProvider(p PaseHandlerProvider) {
	// A fresh acceptor marks a commissioning-window boundary; give the new
	// window its own PASE brute-force budget and a free
	// single-active-PASE slot.
	b.resetPaseFailures()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.paseInFlight = HandshakeKey{}
	b.paseInFlightSince = time.Time{}
	b.paseProvider = p
}

// resolvePaseHandler picks the right PaseHandler for an inbound
// handshake: the provider wins when set; otherwise the singleton.
// Returns nil only when both the provider returned nil AND no
// singleton was wired — the caller drops the datagram with a debug
// log.
func (b *Bridge) resolvePaseHandler(key HandshakeKey) PaseHandler {
	b.mu.RLock()
	provider := b.paseProvider
	singleton := b.paseHandler
	b.mu.RUnlock()
	if provider != nil {
		if h := provider(key); h != nil {
			return h
		}
	}
	return singleton
}

// CaseHandlerProvider returns a [CaseHandler] scoped to a single
// CASE handshake — one initiator's exchange ([HandshakeKey]). The
// bridge's SecureChannel router calls the provider once per inbound
// CASE opcode (Sigma1, Sigma3) and dispatches to the
// returned handler. Returning a fresh handler per key is required
// because Apple Home opens parallel CASE sessions from different IPs
// (iPhone over IPv6, HomePod over IPv4) — a singleton CaseAdapter gets
// stuck in `Finished` after the first successful Sigma3 and rejects
// every subsequent Sigma1 with `ProcessSigma1 already called` — and a
// message of another initiator on the same exchange id has another key
// and must not reach it.
//
// Returning nil drops the datagram with a debug log — the
// commissioner retries.
type CaseHandlerProvider func(key HandshakeKey) CaseHandler

// AttachCaseHandlerProvider wires a per-exchange CASE provider. When
// set, the provider takes precedence over [Bridge.AttachCaseHandler]
// for every inbound CASE opcode. Pass nil to clear and fall back to
// the singleton handler.
func (b *Bridge) AttachCaseHandlerProvider(p CaseHandlerProvider) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.caseProvider = p
}

// resolveCaseHandler picks the right CaseHandler for an inbound
// handshake: the provider wins when set; otherwise the singleton.
// Returns the noop handler when both the provider returned nil AND
// no singleton was wired — the caller surfaces a debug log.
func (b *Bridge) resolveCaseHandler(key HandshakeKey) CaseHandler {
	h, _ := b.resolveCaseHandlerOf(key)
	return h
}

// resolveCaseHandlerOf is [Bridge.resolveCaseHandler] that also reports
// whether the provider answered for key — false when the singleton serves
// it.
func (b *Bridge) resolveCaseHandlerOf(key HandshakeKey) (h CaseHandler, provided bool) {
	h, provided, _ = b.resolveCaseHandlerGen(key)
	return h, provided
}

// resolveCaseHandlerGen is [Bridge.resolveCaseHandlerOf] with the
// generation of the singleton handler it read ([Bridge.caseHandlerGen]),
// read with it.
func (b *Bridge) resolveCaseHandlerGen(key HandshakeKey) (h CaseHandler, provided bool, gen uint64) {
	b.mu.RLock()
	provider := b.caseProvider
	singleton := b.caseHandler
	gen = b.caseHandlerGen
	b.mu.RUnlock()
	if provider != nil {
		if h := provider(key); h != nil {
			return h, true, gen
		}
	}
	return singleton, false, gen
}

// caseHandlerSwappedSince reports whether a singleton CASE handler was
// attached since generation gen.
func (b *Bridge) caseHandlerSwappedSince(gen uint64) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.caseHandlerGen != gen
}

// errSigma1OverCompleted stands in for the outcome of a Sigma1 on the key of
// a handshake the router holds — in progress or completed — that began
// nothing ([Bridge.handleSigma1]): the handler's failure — an error or a
// failure report — is not sent. It is a state refusal
// ([sigma.ErrSessionState]): the handshake the record holds goes on, its
// hold on a connection included ([Bridge.settleHandshake]).
var errSigma1OverCompleted = fmt.Errorf("bridge: a Sigma1 on the key of a handshake in progress or completed began nothing: %w", sigma.ErrSessionState)

// resolveCaseContinuation is [Bridge.resolveCaseHandler] for a Sigma3: a
// handshake no Sigma1 began ([Bridge.markSigma1Replied])
// has no handler — begun is false — whatever the wiring: the provider is
// not asked, as it would build an adapter for a handshake that cannot
// start, and the singleton is not handed a message of a handshake it does
// not run (review II, R2-P4-nw-01). provided reports whether the provider
// answered for key ([Bridge.resolveCaseHandlerOf]). matter.js routes only
// a Sigma1 that opens an exchange to its CASE server
// (packages/protocol/src/securechannel/SecureChannelProtocol.ts:146-147
// at db021da0).
func (b *Bridge) resolveCaseContinuation(key HandshakeKey) (h CaseHandler, provided, begun bool) {
	b.mu.RLock()
	_, begun = b.sigma1Replied[key]
	b.mu.RUnlock()
	if !begun {
		return nil, false, false
	}
	h, provided = b.resolveCaseHandlerOf(key)
	return h, provided, true
}

// resolvePaseContinuation is [Bridge.resolvePaseHandler] for a Pake1 or
// Pake3: a handshake that does not hold the single-active-PASE claim — no
// PBKDFParamRequest began it — has no handler — begun is false — whatever
// the wiring: the provider is not asked, and the singleton handler of
// [Bridge.AttachPaseHandler] is not handed a message of a handshake it does
// not run (review II, N5-rw2-1). matter.js routes only a PbkdfParamRequest
// that opens an exchange to its PASE server (SecureChannelProtocol.ts:129
// at db021da0).
func (b *Bridge) resolvePaseContinuation(key HandshakeKey) (h PaseHandler, begun bool) {
	b.mu.RLock()
	begun = !b.paseInFlightSince.IsZero() && b.paseInFlight == key &&
		b.clk().Now().Sub(b.paseInFlightSince) <= pasePairingTimeout
	b.mu.RUnlock()
	if !begun {
		return nil, false
	}
	return b.resolvePaseHandler(key), true
}

// paseWindowClosed reports whether a PBKDFParamRequest meets a closed
// commissioning window: a window is attached ([Bridge.AttachCommissioningWindow])
// and closed, and no PASE acceptor is — neither a provider nor a singleton
// handler, as a node detaches its acceptor when its window closes. A
// bridge without a window, or with an acceptor attached, is not in that
// state: an unattached PASE port stays the fail-closed noop, and an
// attached acceptor runs the handshake.
func (b *Bridge) paseWindowClosed() bool {
	b.mu.RLock()
	provider, singleton, win := b.paseProvider, b.paseHandler, b.commissioningWindow
	b.mu.RUnlock()
	if provider != nil || win == nil {
		return false
	}
	if _, noop := singleton.(noopPaseHandler); !noop {
		return false
	}
	return !win.IsOpen()
}

// refusePaseWindowClosed answers a PBKDFParamRequest that meets a closed
// commissioning window ([Bridge.paseWindowClosed]) with a failure report
// (GeneralFailure, InvalidParameter). matter.js sends InvalidParam when no
// PASE server is set (SecureChannelProtocol.ts:129-137 at db021da0; review
// II, owner decision 97). It begins no handshake: the single-active-PASE claim
// is not taken and no PASE failure is counted.
func (b *Bridge) refusePaseWindowClosed(src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader) error {
	b.logger.Info("matter.rx.sc.pase_window_closed",
		slog.String("src", srcString(src)),
		slog.Int("exchange_id", int(proto.ExchangeID)))
	body := mrp.EncodeStatusReport(
		mrp.SCStatusGeneralFailure,
		uint32(mrp.SecureChannelProtocolID),
		mrp.SCStatusProtocolInvalidParameter,
		nil,
	)
	if err := b.sendHandshakeFailureReport(src, requestHdr, proto, body, handshakePeerTimingOf(nil, b.clk().Now())); err != nil {
		b.logger.Debug("matter.rx.sc.pase_window_closed.status_report_send_failed", slog.Any("err", err))
	}
	return nil
}

// dropHandshakeNotBegun ends the handling of a Pake1, Pake3, Sigma3 or
// Sigma2_Resume whose handshake no PBKDFParamRequest or Sigma1 began: the
// message is acknowledged — the MRP layer owes the acknowledgement already
// — and no more: no StatusReport, no counted PASE failure, nothing of
// another handshake touched (review II, R2-P4-cert-02, owner decision 70).
// A Sigma3 of a completed handshake ends here too ([Bridge.dispatchSigma3];
// review II, phase 7, decision 284), and so does a Sigma1 on a completed
// handshake's key that begins nothing ([Bridge.handleSigma1]).
// Mirrors matter.js
// packages/protocol/src/securechannel/SecureChannelProtocol.ts:152-153 and
// :43-50 at db021da0 (any other opening message goes to the base handler,
// which throws StatusResponseError) with
// packages/protocol/src/protocol/ExchangeManager.ts:494 and :538-549 (the
// error is logged, nothing is sent); connectedhomeip registers unsolicited
// handlers for PBKDFParamRequest and Sigma1 only
// (src/app/server/CommissioningWindowManager.cpp:280-281,
// src/protocols/secure_channel/CASEServer.cpp:54-55 at 6170af84) and for a
// message no handler takes sends the standalone acknowledgement only
// (src/messaging/ExchangeMgr.cpp:437).
func (b *Bridge) dropHandshakeNotBegun(src net.Addr, proto message.ProtocolHeader) error {
	b.logger.Debug("matter.rx.sc.handshake_not_begun",
		slog.String("src", srcString(src)),
		slog.Int("opcode", int(proto.Opcode)),
		slog.Int("exchange_id", int(proto.ExchangeID)))
	return nil
}

// AttachCaseHandler wires the CASE port. Pass nil to revert to noop. The
// records of the handshakes the handler it replaces began end with it
// ([Bridge.sigma3Handled]): a completed one's success report is still
// retransmitted until it is acknowledged; one that did not complete ends as
// an evicted one does ([Bridge.boundSigma1Records]) — its Sigma2 is no
// longer retransmitted, and its Sigma3 is acknowledged and not answered, as
// one no Sigma1 began, instead of reaching a handler that never saw its
// Sigma1 (review II, R2-P4-rr-sec-02).
//
// Deprecated: wire CASE through [Bridge.AttachCaseHandlerProvider]; a
// provider that returns one handler for every handshake is this
// method's shape. Removable in v0.25.0.
func (b *Bridge) AttachCaseHandler(h CaseHandler) {
	var unfinished []HandshakeKey
	b.mu.Lock()
	for key, rec := range b.sigma1Replied {
		switch {
		case rec.completed != nil:
			if rec.completed.bySingleton {
				delete(b.sigma1Replied, key)
			}
		case rec.bySingleton:
			delete(b.sigma1Replied, key)
			unfinished = append(unfinished, key)
		}
	}
	if h == nil {
		b.caseHandler = noopCaseHandler{}
	} else {
		b.caseHandler = h
	}
	b.caseHandlerGen++
	b.mu.Unlock()
	for _, key := range unfinished {
		b.releaseCASEHandshakeEverywhere(key)
		b.abandonHandshakeReplies(key)
	}
}

// AttachAckHandler wires the MRP-ack port. Pass nil to revert to noop.
func (b *Bridge) AttachAckHandler(h AckHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if h == nil {
		b.ackHandler = noopAckHandler{}
		return
	}
	b.ackHandler = h
}

// dispatchSecureChannel is the inbound router for ProtocolID==0x0000
// datagrams, an opcode fan-out:
//
//   - 0x10 StandaloneAck         → AckHandler.Discharge (no reply)
//   - 0x20 PBKDFParamRequest     → PaseHandler.ProcessPBKDFParamRequest → 0x21 PBKDFParamResponse
//   - 0x22 Pake1                 → PaseHandler.ProcessPake1 → 0x23 Pake2
//   - 0x24 Pake3                 → PaseHandler.ProcessPake3 (no reply or status ack)
//   - 0x30 Sigma1                → CaseHandler.ProcessSigma1 → 0x31 Sigma2
//   - 0x32 Sigma3                → CaseHandler.ProcessSigma3 (no reply or status ack)
//   - 0x33 Sigma2Resume          → acknowledged, no handler, no reply
//   - 0x40 StatusReport          → CloseSession closes the session;
//     anything else logs (no reply)
//   - everything else (incl. 0x21/0x23/0x25/0x31 = bridge-initiated
//     responses we should never receive on the listener) → drop with
//     debug log.
//
// The router is logged at debug for happy paths and warn for handler
// errors so operators can tail the bridge during commissioner
// pairing without grepping noise.
func (b *Bridge) dispatchSecureChannel(src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, payload []byte) error {
	// The attached AckHandler learns of the peer's acknowledgement, keyed
	// on the exchange. The acknowledgement this node owes for the
	// message is not its to clear ([Bridge.AttachAckTracker]); the
	// outbound-reliable counter tracker is handled in receive.go.
	if proto.HasAck {
		b.mu.RLock()
		ack := b.ackHandler
		b.mu.RUnlock()
		if ack != nil {
			ack.Discharge(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
		}
	}

	// Remember the authenticated peer address per secure session
	// (Secure-Channel datagrams reach this router only after the
	// decrypt in receive.go succeeds). [Bridge.dispatch] records the
	// same for every decrypted datagram, IM included; this store keeps
	// the router self-contained for callers that enter it directly.
	// matter.js keeps the association on the session itself via its
	// MessageChannel (packages/protocol/src/session/Session.ts,
	// `get channel()`).
	if requestHdr.SessionID != 0 && !isNilRoute(src) {
		b.bindSessionRoute(requestHdr.SessionID, src)
	}

	// The peer answering, as responder, a CASE handshake this node opened
	// to re-establish former subscriptions (case_initiator.go).
	if b.deliverInitiatedSecureChannel(requestHdr, proto, payload) {
		return nil
	}

	if requestHdr.SessionID == 0 && endsAwaitedResume(proto.Opcode) {
		// A message other than the awaited StatusReport on the exchange of
		// a resumption fails it: matter.js's read of the initiator's
		// Success throws on another message type and force-closes the
		// session (SecureChannelMessenger.ts:94-98,
		// packages/protocol/src/session/case/CaseServer.ts:204-210 at
		// b25ed960).
		b.endAwaitedResume(handshakeKeyOf(requestHdr, proto.ExchangeID))
	}

	switch proto.Opcode {
	case mrp.StandaloneAckOpcode:
		// Pure StandaloneAck has no payload after the protocol header.
		// receive.go stopped the retransmission it acknowledges;
		// nothing more to do.
		b.logger.Debug("matter.rx.sc.ack",
			slog.String("src", srcString(src)),
			slog.Int("ack_counter", int(proto.AckCounter)))
		return nil

	case mrp.SCOpcodePBKDFParamRequest, mrp.SCOpcodePake1, mrp.SCOpcodePake3:
		if b.paseLockedOut() {
			// Brute-force cap reached for the currently installed
			// acceptor: stop processing PASE entirely until the lockout
			// cooldown expires or a new commissioning window installs a
			// fresh acceptor. matter.js aborts commissioning at
			// PASE_COMMISSIONING_MAX_ERRORS (PaseServer.ts:95-110) and its
			// PaseServer dies with the window; ours can outlive every
			// window, so the refusal has to live on the receive path — and
			// has to expire, or the refusal itself becomes the denial of
			// service.
			b.logger.Warn("matter.rx.sc.pase_locked_out",
				slog.String("src", srcString(src)),
				slog.Int("opcode", int(proto.Opcode)),
				slog.String("hint", "too many failed pairing attempts; PASE re-enables after the lockout period, or immediately when a new pairing window is opened"))
			return nil
		}
		return b.dispatchPase(src, requestHdr, proto, payload)

	case mrp.SCOpcodeSigma1:
		// Apple iOS multicasts the same Sigma1 onto IPv4 + IPv6-LL +
		// IPv6-Global, so we observe 5 identical inbound Sigma1
		// datagrams on the same exchange in <1 ms. Bug A's responder-
		// mutex + equality cache makes every parallel ProcessSigma1
		// return byte-identical Sigma2 bytes, but `handleCase` still
		// calls `sendReply` 5 times — Apple processes the first, sends
		// Sigma3, and rejects our late copies with `CASESession.cpp:
		// 2507: CHIP Error 0x0000002A: Invalid message type` (state 3
		// no longer accepts Sigma2). Dedupe at the router by hashing
		// the Sigma1 payload and short-circuiting replays on the same
		// (exchangeID, sigma1Hash) tuple. The dedupe is this module's
		// own: matter.js's CASE server has no such step (its entry
		// points are onNewExchange and #handleSigma1,
		// packages/protocol/src/session/case/CaseServer.ts:61-63,
		// :102-146 at db021da0); a retransmission meets its exchange
		// layer, where a duplicate is acknowledged
		// (packages/protocol/src/protocol/MessageExchange.ts:501-511;
		// the retransmission sent in the acknowledgement's place is
		// for BDX alone, :580-583), and a message whose id the pending
		// reply acknowledges gets that reply again (:512-520).
		key := handshakeKeyOf(requestHdr, proto.ExchangeID)
		h := sha256.Sum256(payload)
		replay, overRecord := b.markSigma1Replied(key, h)
		if replay {
			b.logger.Debug("matter.rx.sc.sigma1.replay_dropped",
				slog.String("src", srcString(src)),
				slog.Int("exchange_id", int(proto.ExchangeID)))
			return nil
		}
		// The handshake holds a TCP connection it runs on open until it
		// ends (matter.js ExchangeManager.ts:634-648 counts the unsecured
		// session on the channel). Over the record of another handshake
		// the hold is taken once the Sigma1 began one ([Bridge.handleSigma1]).
		if !overRecord {
			b.holdHandshake(src, key, false)
		}
		return b.handleSigma1(src, requestHdr, proto, payload, key, h, overRecord)
	case mrp.SCOpcodeSigma3:
		return b.dispatchSigma3(src, requestHdr, proto, payload)
	case mrp.SCOpcodeSigma2Resume:
		return b.dispatchSigma2Resume(src, requestHdr, proto, payload)

	case mrp.SCOpcodeStatusReport:
		if requestHdr.SessionID == 0 {
			// The peer ended a handshake on this exchange with a failure:
			// it no longer holds a connection open.
			if general, _, _, ok := decodeStatusReport(payload); ok && general != mrp.SCStatusGeneralSuccess {
				b.releaseHandshake(src, handshakeKeyOf(requestHdr, proto.ExchangeID))
			}
			// The initiator's answer to a Sigma2_Resume ends the wait of
			// the session the resumption installed.
			b.settleAwaitedResume(handshakeKeyOf(requestHdr, proto.ExchangeID), payload)
		}
		if b.abortPaseOnStatusReport(src, requestHdr, proto, payload) {
			return nil
		}
		return b.handleSecureChannelStatusReport(src, requestHdr, payload)

	default:
		b.logger.Debug("matter.rx.sc.unhandled",
			slog.String("src", srcString(src)),
			slog.Int("opcode", int(proto.Opcode)),
			slog.Int("payload_bytes", len(payload)))
		return nil
	}
}

// dispatchSigma3 routes a Sigma3: to nothing but the acknowledgement when
// its handshake completed or no Sigma1 began it
// ([Bridge.dropHandshakeNotBegun]), else to the handler of its handshake.
//
// A Sigma3 of a completed handshake — the bytes and the route that
// completed it included — is acknowledged and not answered, and nothing
// about the established session changes: not its route, not the record of
// its establishment (review II, R2-P3-rr-dev-01, phase 7, decision 284).
// While the success report waits for its acknowledgement the initiator's
// retransmission of that Sigma3, which acknowledges the Sigma2 and not the
// report, is refused on receive, and the report's own retransmission
// serves the initiator whose report was lost (decision 277). In matter.js
// a Sigma3 again on the handshake's exchange is a duplicate, acknowledged
// and not handed to CASE again
// (packages/protocol/src/protocol/MessageExchange.ts:501-511 at db021da0),
// an error of that exchange that is only logged (:522-534,
// ExchangeManager.ts:435-437 and :538-549), a new exchange of a closing
// session that is declined (ExchangeManager.ts:441-444), or an opening
// message SecureChannel does not take
// (packages/protocol/src/securechannel/SecureChannelProtocol.ts:152-153,
// :43-50; ExchangeManager.ts:491-494) — none hands it to CASE or builds a
// report for it; only while the report is still unacknowledged does a
// message that repeats the Sigma3's id without being flagged a duplicate
// get that pending report again (MessageExchange.ts:512-519), the span in
// which this node refuses it on receive and retransmits the report on its
// own timer (decision 277); connectedhomeip takes only Sigma1 unsolicited
// (src/protocols/secure_channel/CASEServer.cpp:54-55 at 6170af84) and
// acknowledges the rest (src/messaging/ExchangeMgr.cpp:437).
func (b *Bridge) dispatchSigma3(src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, payload []byte) error {
	// The initiator has moved past Sigma2: a Sigma2 still waiting for
	// its acknowledgement is done — its retransmissions are wasted
	// bandwidth at best and confuse Apple iOS's MRP layer at worst (it
	// drops a late Sigma2 with `Dropping message without piggyback ack
	// when we are waiting for an ack`, then aborts the handshake).
	// Once the handler established the session with the Sigma3 and
	// before its reply is sent
	// (processSigma3 below), so the reply to Sigma3 itself — the
	// StatusReport that ends the handshake — stays tracked and is
	// retransmitted until the initiator acknowledges it:
	// matter.js sends it as an ordinary reliable message and waits for
	// its acknowledgement (packages/protocol/src/session/case/CaseServer.ts:332,
	// packages/protocol/src/securechannel/SecureChannelMessenger.ts:209-232,
	// packages/protocol/src/protocol/MessageExchange.ts:692, :785-790
	// and :821-828 at db021da0), connectedhomeip likewise
	// (src/protocols/secure_channel/PairingSession.h:157-190,
	// src/messaging/ExchangeMessageDispatch.cpp:54-75 at 6170af84).
	// Dropped after the handler, the report went out once: lost, it
	// left the initiator waiting out its timeout for a session this
	// node had already installed. A Sigma3 the handler did not take — one
	// that reached it after it established its session, a garbage one
	// racing the completing Sigma3 included — abandons nothing: the
	// success report it sent stays tracked (review II, N3-fixrrdev-1,
	// N4-rwsub-3). Nor does a Sigma3 that fails: an unacknowledged Sigma2
	// goes on being retransmitted (F-db021da0-7, owner decision 173).
	key := handshakeKeyOf(requestHdr, proto.ExchangeID)
	if ok := b.handshakeCompleted(key); ok {
		return b.dropHandshakeNotBegun(src, proto)
	}
	ch, provided, begun := b.resolveCaseContinuation(key)
	if !begun {
		return b.dropHandshakeNotBegun(src, proto)
	}
	// The session's route is bound, and its establishment recorded,
	// for the one Sigma3 that established it ([sigma3Step]).
	step := &sigma3Step{handler: ch}
	processSigma3 := func(p []byte) (uint8, []byte, error) {
		op, reply, outcome, err := processSigma3With(ch, p)
		step.outcome = outcome
		// A Sigma3 that fails drops nothing: an unacknowledged Sigma2
		// goes on being retransmitted (owner decision 173).
		if outcome == sigma3Established {
			b.abandonHandshakeReplies(key)
		}
		if outcome == sigma3OfCompleted {
			// The handler had established a session before this Sigma3
			// reached it — another handshake's, which took over the
			// singleton handler by resumption: the route a completed
			// handshake completed on is the router's knowledge, not
			// the handler's. The Sigma3 is acknowledged and not
			// answered, as one of a completed handshake is (review II,
			// R2-P4-nw-01, decision 284); its record ends at the wait
			// for Sigma3 ([Bridge.sigma1RecordOver]).
			return 0, nil, nil
		}
		return op, reply, err
	}
	err := b.handleCase(src, requestHdr, proto, payload, "sigma3", processSigma3, step)
	// A handler's error fails the handshake as a failure report does — its
	// outcome is sigma3Failed — and ends the record with it (review II,
	// R2-P4-fr-sec-02); a Sigma3 that established the session is recorded
	// as completed even when its report could not be sent.
	b.sigma3Handled(key, step.outcome, completedSigma3{bySingleton: !provided})
	if err != nil {
		return err
	}
	if step.outcome != sigma3Established {
		return nil
	}
	// A completed Sigma3 is the moment a secure session exists. An
	// operator reading the trace after a controller went quiet needs
	// the open as much as the close: "it never came back after 14:02"
	// is only readable when both ends of the session are recorded.
	b.diagRing().Record(diagevent.Event{
		Kind:     diagevent.KindSession,
		Severity: diagevent.SeverityInfo,
		Message:  "A controller completed a secure-session handshake with the bridge.",
		Detail:   map[string]string{"peer": srcString(src)},
	})
	return nil
}

// dispatchSigma2Resume ends the handling of a Sigma2_Resume this node
// receives as the responder: it is acknowledged and no more
// ([Bridge.dropHandshakeNotBegun]) — whether no Sigma1 began its handshake,
// the handshake is in progress or it completed. The responder sends
// Sigma2_Resume and never receives one (the bridge's own handshakes as
// initiator take theirs before this router, case_initiator.go), so it is no
// step of any handshake here: no handler is asked, no failure report is
// sent, and no reply that waits for its acknowledgement — a Sigma2 or the
// success report — is taken from the retransmission (review II,
// R2-P4-rr-sec-03, R2-P4-fr-sec-03). In matter.js a message on the
// handshake's live exchange that does not acknowledge the pending reply
// fails before the retransmission timer is touched
// (packages/protocol/src/protocol/MessageExchange.ts:521-534 at db021da0),
// and a Sigma2_Resume opening an exchange is not taken
// (packages/protocol/src/securechannel/SecureChannelProtocol.ts:146-153).
func (b *Bridge) dispatchSigma2Resume(src net.Addr, _ *message.Header, proto message.ProtocolHeader, _ []byte) error {
	return b.dropHandshakeNotBegun(src, proto)
}

// dispatchPase routes one inbound PASE opcode to the handler resolved
// for its exchange. Split out of the Secure-Channel opcode switch so the
// single-active-PASE claim, the handler resolution and the post-Pake3
// release stay together while the switch keeps one entry per family.
func (b *Bridge) dispatchPase(
	src net.Addr,
	requestHdr *message.Header,
	proto message.ProtocolHeader,
	payload []byte,
) error {
	if proto.Opcode == mrp.SCOpcodePBKDFParamRequest && b.paseWindowClosed() {
		// Before the PASE-session gate: the window's timer closes the
		// window without closing a PASE session that is still alive
		// (CommissioningWindow.closeLocked), and with no PASE server set
		// matter.js answers whatever sessions remain
		// (SecureChannelProtocol.ts:129-137 at db021da0).
		return b.refusePaseWindowClosed(src, requestHdr, proto)
	}
	if b.paseSessionExists() {
		// An established PASE session that is not closing: no new PASE
		// exchange is accepted until it ends. matter.js ignores the
		// exchange ("Pairing already in progress (PASE session exists)",
		// PaseServer.ts:80-82 at db021da0); connectedhomeip unregisters the
		// PBKDFParamRequest handler once the PASE session is established
		// (src/app/server/CommissioningWindowManager.cpp:211, :224 and
		// :560-561 at 6170af84). Nothing is resolved or counted; the MRP
		// layer has already acknowledged the datagram.
		b.logger.Info("matter.rx.sc.pase_session_exists",
			slog.String("src", srcString(src)),
			slog.Int("opcode", int(proto.Opcode)),
			slog.Int("exchange_id", int(proto.ExchangeID)))
		return nil
	}
	key := handshakeKeyOf(requestHdr, proto.ExchangeID)
	if proto.Opcode != mrp.SCOpcodePBKDFParamRequest && b.paseHandshakeOfAnother(key) {
		// A pairing is in progress and this Pake1 / Pake3 is not of it:
		// ignored before a handler is resolved and before anything is
		// counted, as matter.js ignores every new exchange while a pairing
		// is in progress (PaseServer.ts:80-86 at db021da0) and counts only
		// the errors of the pairing it accepted (:94-95). The MRP layer has
		// already acknowledged the datagram.
		b.logger.Debug("matter.rx.sc.pase_other_handshake",
			slog.String("src", srcString(src)),
			slog.Int("opcode", int(proto.Opcode)),
			slog.Int("exchange_id", int(proto.ExchangeID)))
		return nil
	}
	switch proto.Opcode {
	case mrp.SCOpcodePBKDFParamRequest:
		claim := b.claimPase(key)
		if claim == paseClaimRepeated {
			// The handshake asks again on its live claim: an error of
			// the pairing the claim accepted, which ends it and counts
			// — not answered, acknowledged by the MRP layer
			// ([Bridge.endRepeatedPairing]).
			b.endRepeatedPairing(src, requestHdr, proto)
			return nil
		}
		if claim == paseClaimBusy {
			// Single-active-PASE (Matter §4.13.1): a second
			// commissioner's PBKDFParamRequest while a handshake is in
			// progress is IGNORED — matter.js PaseServer.ts:80-86
			// onNewExchange logs "Pairing already in progress" and
			// drops the exchange. The MRP layer has already acked the
			// datagram; the rejected commissioner times out and
			// retries after the active handshake finished or expired.
			b.logger.Info("matter.rx.sc.pase_busy",
				slog.String("src", srcString(src)),
				slog.Int("exchange_id", int(proto.ExchangeID)))
			b.diagRing().Record(diagevent.Event{
				Kind:     diagevent.KindPairing,
				Severity: diagevent.SeverityWarning,
				Message: "A commissioner tried to pair while another pairing was already " +
					"in progress; its attempt was dropped and it will retry.",
				Detail: map[string]string{"peer": srcString(src)},
			})
			return nil
		}
		b.holdHandshake(src, key, true)
		h := b.resolvePaseHandler(key)
		return b.handlePase(src, requestHdr, proto, payload, "pbkdf_param_req",
			h.ProcessPBKDFParamRequest, h)
	case mrp.SCOpcodePake1:
		h, begun := b.resolvePaseContinuation(key)
		if !begun {
			return b.dropHandshakeNotBegun(src, proto)
		}
		return b.handlePase(src, requestHdr, proto, payload, "pake1",
			h.ProcessPake1, h)
	case mrp.SCOpcodePake3:
		h, begun := b.resolvePaseContinuation(key)
		if !begun {
			return b.dropHandshakeNotBegun(src, proto)
		}
		// A Pake3 the handler takes moves the commissioner past Pake2:
		// whatever of the handshake still waits for an acknowledgement is
		// done. After the handler and before its reply is sent, so the
		// reply to Pake3 itself stays tracked, and handlePase has read
		// whether the Pake2 was unacknowledged before (F-db021da0-7). A
		// Pake3 that fails drops nothing: an unacknowledged Pake2 goes on
		// being retransmitted (owner decision 173).
		processPake3 := func(p []byte) (uint8, []byte, error) {
			op, reply, err := h.ProcessPake3(p)
			if err == nil && !failureReport(op, reply) {
				b.abandonHandshakeReplies(key)
			}
			return op, reply, err
		}
		err := b.handlePase(src, requestHdr, proto, payload, "pake3",
			processPake3, h)
		// Pake3 terminates the handshake either way (success installs
		// the session, failure aborts the attempt) — release the
		// single-active-PASE claim so the next commissioner can start.
		// matter.js clears #pairingMessenger in the finally block of
		// onNewExchange (PaseServer.ts:112-118).
		b.releasePaseInFlight(key)
		return err
	}
	return nil
}

// paseMaxErrors is the number of PASE pairing failures within a
// commissioning window that aborts the window. Mirrors matter.js
// PaseServer.ts PASE_COMMISSIONING_MAX_ERRORS (= 20). Without this cap an
// attacker on the LAN could hammer passcode guesses for the whole window
// — up to 900 s commissioned, or 48 h for an uncommissioned bridge.
const paseMaxErrors = 20

// maxUnsecuredWindows caps the number of per-SourceNodeID duplicate-detection
// windows held for unsecured traffic at one time. SourceNodeID is
// attacker-controlled and unauthenticated at this stage. A window lives only
// as long as the handshake it serves ([unsecuredWindowTable]), so the windows
// held are those of the handful of live handshakes; only sources whose
// messages name the exchange of a live handshake can reach the cap. Past it a
// new source is treated as fresh — the PASE handshake handler's own
// state-replay guard still rejects a genuine replay.
const maxUnsecuredWindows = 256

// maxCaseHandshakes caps the CASE handshakes the responder holds state
// for at one time: the Sigma1 records of the router
// ([Bridge.markSigma1Replied]) and the adapters of a
// [PerExchangeCaseProvider]. Both are keyed by the initiator's
// unauthenticated node id and exchange id, so the key no longer bounds
// them (review II, R2-P3-dr-dev-03). Each handshake holds one unsecured
// duplicate window as long as it holds its record, so the bound is the
// windows' own, [maxUnsecuredWindows]; past it the oldest record and the
// least recently used adapter lose their place, as the unsecured session's
// oldest reliable message does ([unsecuredEntryLimit]).
const maxCaseHandshakes = maxUnsecuredWindows

// pasePairingTimeout bounds how long a single PASE handshake may hold
// the single-active-PASE claim. Mirrors matter.js PaseServer.ts:33
// PASE_PAIRING_TIMEOUT (60 s) — an abandoned handshake (commissioner
// crashed between PBKDFParamRequest and Pake3) self-expires so pairing
// is not locked out for the rest of the window.
const pasePairingTimeout = 60 * time.Second

// paseLockoutCooldown is how long PASE stays refused after the first time
// [paseMaxErrors] is reached, and [paseLockoutMaxCooldown] is the ceiling
// the doubling backoff runs into.
//
// matter.js and chip both end the refusal with the commissioning window:
// matter.js counts failed attempts per window and, at
// MAX_FAILED_COMMISSIONING_ATTEMPTS = 20, closes the window
// (DeviceCommissioner.ts:44, :181-198), with no timer keeping PASE refused
// afterwards; chip calls
// CommissioningWindowManager::Cleanup once mFailedCommissioningAttempts
// reaches kMaxFailedCommissioningAttempts
// (CommissioningWindowManager.cpp:160-192). Neither can lock pairing out
// beyond the window, because neither keeps a passcode acceptor armed once
// the window closes. This bridge does: an uncommissioned daemon answers
// PASE from its configured passcode with no window open, so the refusal
// needs its own expiry or 20 malformed Pake1s from any LAN host would
// disable pairing until the operator restarts the daemon.
//
// 20 guesses per 15 min (80/h) leaves the ~10^8 valid-passcode space out
// of reach while keeping an operator who mistyped the code waiting
// minutes, not hours — and opening a pairing window clears the lockout
// immediately.
const (
	paseLockoutCooldown    = 15 * time.Minute
	paseLockoutMaxCooldown = 4 * time.Hour
)

// paseClaim is the outcome of [Bridge.claimPase].
type paseClaim int

const (
	// paseClaimTaken: the slot was idle or its claim had run out; key
	// holds it now, for [pasePairingTimeout] from this request.
	paseClaimTaken paseClaim = iota
	// paseClaimBusy: another handshake holds the slot.
	paseClaimBusy
	// paseClaimRepeated: key holds the slot already — a further
	// PBKDFParamRequest of the handshake, not its retransmission (the
	// unsecured reception window drops that before dispatch).
	paseClaimRepeated
)

// claimPase attempts to claim the single-active-PASE slot for the
// handshake key, for a PBKDFParamRequest. The claim runs
// [pasePairingTimeout] from the request that took it and is not renewed: a
// further PBKDFParamRequest of the same handshake is reported as
// [paseClaimRepeated], which ends the pairing ([Bridge.endRepeatedPairing]),
// so whoever keeps asking cannot hold the slot past its time. A claim that
// ran out without its handshake ending
// counts one PASE failure when the slot is taken again — by anybody, the
// same initiator included — so twenty abandoned claims engage the lockout
// twenty failed passcode attempts engage ([Bridge.recordPaseFailure]).
// matter.js runs the timer once per pairing (PaseServer.ts:127-131 at
// db021da0), ignores every new exchange while it runs (:80-86), and counts
// every error of the pairing it accepted (:94-110); review II,
// R2-P6-sec-0304.
func (b *Bridge) claimPase(key HandshakeKey) paseClaim {
	b.mu.Lock()
	now := b.clk().Now()
	held := !b.paseInFlightSince.IsZero()
	if held && now.Sub(b.paseInFlightSince) <= pasePairingTimeout {
		ours := b.paseInFlight == key // read under the lock
		b.mu.Unlock()
		if ours {
			return paseClaimRepeated
		}
		return paseClaimBusy
	}
	expired := b.paseInFlight
	b.paseInFlight = key
	b.paseInFlightSince = now
	b.mu.Unlock()
	if held {
		// The handshake that held the slot ran out and counts as a failed
		// pairing. What it sent goes on being retransmitted until it is
		// acknowledged, spent or [unsecuredEntryLifetime] old (owner decision
		// 173) — unless the same initiator takes the slot again on the same
		// exchange: its old replies would read as the new handshake's.
		// Outside b.mu — the tracker resolves a session's interval through
		// the bridge.
		if expired == key {
			b.abandonHandshakeReplies(expired)
		}
		if b.logger != nil {
			b.logger.Info("matter.rx.sc.pase_claim_expired",
				slog.Int("exchange_id", int(expired.ExchangeID)),
				slog.Duration("timeout", pasePairingTimeout))
		}
		b.recordPaseFailure()
	}
	return paseClaimTaken
}

// endRepeatedPairing ends the PASE pairing after a further
// PBKDFParamRequest arrived on it: one PASE failure is counted, the claim is
// released, the handshake's replies are no longer retransmitted and its
// hold on a connection ends. The request is answered with a StatusReport
// InvalidParameter when the pairing's last reply was acknowledged, and only
// acknowledged while that reply still waits for its acknowledgement — as
// matter.js's cancelPairing sends its error unless the exchange's last
// message is unacknowledged (PaseServer.ts:103-104, :210 at db021da0).
// matter.js's PASE server ignores a NEW exchange while a
// pairing runs (PaseServer.ts:80-86 at db021da0), but a further
// PBKDFParamRequest on the accepted exchange reaches its typed read of the
// next message, is an unexpected message (SecureChannelMessenger.ts:95-98),
// and the pairing's catch counts it and cancels the pairing (PaseServer.ts
// :94-95); review II, R2-P6-sec-0304 (decision 65).
func (b *Bridge) endRepeatedPairing(src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader) {
	key := handshakeKeyOf(requestHdr, proto.ExchangeID)
	b.logger.Info("matter.rx.sc.pase_request_repeated",
		slog.String("src", srcString(src)),
		slog.Int("exchange_id", int(key.ExchangeID)))
	// Unreported while the pairing's last reply waits for its
	// acknowledgement ([Bridge.sendHandshakeFailureReport]).
	body := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), mrp.SCStatusProtocolInvalidParameter, nil)
	if err := b.sendHandshakeFailureReport(src, requestHdr, proto, body, handshakePeerTimingOf(nil, b.clk().Now())); err != nil {
		b.logger.Debug("matter.rx.sc.pase_request_repeated.status_report_send_failed", slog.Any("err", err))
	}
	b.recordPaseFailure()
	b.releasePaseInFlight(key)
	b.releaseHandshake(src, key)
}

// paseSessionIDLister is the optional capability of a [SessionRegistry] to
// name its established PASE sessions (secure/operational
// Manager.PASESessionIDs).
type paseSessionIDLister interface {
	PASESessionIDs() []uint16
}

// paseSessionExists reports whether the attached session registry holds an
// established PASE session that is not closing. A session whose close the
// bridge has scheduled ([Bridge.paseSessionsCloser]) is closing from that
// moment, as a matter.js session is from initiateClose on
// (packages/protocol/src/session/Session.ts:190-192 `isClosing`, :304-311
// at db021da0), though it stays in the table for its grace. A registry
// without the capability reports none.
func (b *Bridge) paseSessionExists() bool {
	b.mu.RLock()
	reg := b.sessionRegistry
	b.mu.RUnlock()
	r, ok := reg.(paseSessionIDLister)
	if !ok {
		return false
	}
	ids := r.PASESessionIDs()
	if len(ids) == 0 {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, id := range ids {
		if _, closing := b.paseClosing[id]; !closing {
			return true
		}
	}
	return false
}

// paseHandshakeOfAnother reports whether the single-active-PASE slot is
// held, within [pasePairingTimeout], by a handshake other than key's.
func (b *Bridge) paseHandshakeOfAnother(key HandshakeKey) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return !b.paseInFlightSince.IsZero() && b.paseInFlight != key &&
		b.clk().Now().Sub(b.paseInFlightSince) <= pasePairingTimeout
}

// abandonHandshakeReplies drops the replies of the handshake key names
// that still wait for their acknowledgement on the unsecured session: the
// handshake ended, or moved past them
// ([outboundReliableTracker.AbandonUnsecuredExchange]). A handshake that
// fails on a message does not call it: what it sent goes on being
// retransmitted until it is acknowledged or its retransmissions are spent
// ([Bridge.sendHandshakeFailureReport], owner decision 173). The replies
// of another initiator's handshake on the same exchange id stay.
//
// A reply that installed a secure session the peer has not acknowledged
// yet — a Sigma2_Resume — ends with its handshake, so the session is
// closed ([Bridge.closeUnacknowledgedResume]).
func (b *Bridge) abandonHandshakeReplies(key HandshakeKey) {
	ended, byEntry := b.endAwaitedResume(key)
	for _, id := range b.dropHandshakeReplies(key) {
		if byEntry && id == ended {
			continue
		}
		b.closeUnacknowledgedResume(id)
	}
}

// dropHandshakeReplies drops the replies of the handshake key names and
// returns the sessions they installed unacknowledged.
func (b *Bridge) dropHandshakeReplies(key HandshakeKey) []uint16 {
	b.mu.RLock()
	tracker := b.outboundReliable
	b.mu.RUnlock()
	if tracker == nil {
		return nil
	}
	cleared, installed := tracker.abandonUnsecuredExchange(key)
	if cleared > 0 {
		b.logger.Debug("matter.sc.handshake.abandon_replies",
			slog.Int("exchange_id", int(key.ExchangeID)),
			slog.Int("cleared", cleared))
	}
	return installed
}

// handshakeReplyUnacknowledged reports whether a message of the handshake
// key names still waits for its acknowledgement on the unsecured session.
func (b *Bridge) handshakeReplyUnacknowledged(key HandshakeKey) bool {
	b.mu.RLock()
	tracker := b.outboundReliable
	b.mu.RUnlock()
	return tracker != nil && tracker.holdsUnsecuredExchange(key)
}

// endHandshakeUnreported ends a failed handshake without its failure
// StatusReport, because a message the handshake sent still waits for its
// acknowledgement: nothing is sent, and that message is not dropped — it
// goes on being retransmitted until the peer acknowledges it or its
// retransmissions are spent ([Bridge.sendHandshakeFailureReport]); the
// failing message's own acknowledgement is left to the ACK pump. Review II,
// F-db021da0-7 and owner decision 173: before, the unacknowledged messages
// were dropped and the report sent as a reliable message.
//
// Since review II, phase 7 (decision 277) a failing message that does not
// acknowledge the waiting reply is refused on receive over UDP and never
// reaches the handshake, and one that acknowledges it clears it first; the
// guard ([Bridge.handshakeReplyUnacknowledged]) is reached over UDP only in
// a residue — two waiting messages on one handshake, or two dispatches of
// it that overlap — and never over TCP, where nothing is tracked
// (bridge/reply.go:262-263). It is kept as the stated second line (owner
// decision 282): matter.js keeps the same guard although its receive rules
// shield it the same way (packages/protocol/src/session/case/CaseServer.ts:82-86,
// packages/protocol/src/session/pase/PaseServer.ts:207-210 at db021da0),
// where it is reached by a give-up: the send threw and #sentMessageToAck is
// still set.
func (b *Bridge) endHandshakeUnreported(key HandshakeKey, stage string) {
	b.logger.Debug("matter.sc.handshake.failure_report_skipped",
		slog.String("stage", stage),
		slog.Int("exchange_id", int(key.ExchangeID)),
		slog.String("reason", "reply_unacknowledged"))
}

// failureReport reports whether a handshake reply is a failure
// StatusReport.
func failureReport(respOpcode uint8, respPayload []byte) bool {
	if respOpcode != mrp.SCOpcodeStatusReport {
		return false
	}
	general, _, _, ok := decodeStatusReport(respPayload)
	return ok && general != mrp.SCStatusGeneralSuccess
}

// releasePaseInFlight clears the single-active-PASE claim held by the
// handshake key. A claim held by a DIFFERENT handshake — another exchange,
// or another initiator on the same exchange id — stays untouched.
func (b *Bridge) releasePaseInFlight(key HandshakeKey) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.paseInFlight == key {
		b.paseInFlight = HandshakeKey{}
		b.paseInFlightSince = time.Time{}
	}
}

// recordPaseFailure increments the PASE failure counter and, when it
// reaches [paseMaxErrors], closes the open commissioning window and
// engages a timed PASE lockout. Mirrors matter.js PaseServer.ts:95-110
// (count → MaximumPasePairingErrorsReachedError) + DeviceCommissioner.ts:78-82
// at db021da0 (tooManyPaseErrors → endCommissioning). At b25ed960 that
// mechanism is gone: matter.js counts failed attempts per window — a PASE
// pairing that fails before its session is established (pairingFailed,
// wired at DeviceCommissioner.ts:251) and a fail-safe that ends without
// commissioning (:160-174) — and closes the window at
// MAX_FAILED_COMMISSIONING_ATTEMPTS = 20 (:44, :198); the fail-safe half is
// pin-lift unit P3-C, the timed lockout stays ahead of matter.js
// (BD-Matter-PASE-TimedLockout). The window closes
// as the end of commissioning closes it ([CommissioningWindow.EndCommissioning]),
// not as RevokeCommissioning does: matter.js's
// #closeWindow (DeviceCommissioner.ts:318-334) stops the timer,
// removes the PASE commissioner and leaves commissioning mode, and touches
// neither the fail-safe nor a PASE session — a commissioning already past
// PASE keeps its fail-safe and its pending fabric. Called from the
// handlePase error path for genuine pairing failures only (not
// missing-handler or state-replay retransmits).
func (b *Bridge) recordPaseFailure() {
	b.count(contract.MetricPASEFailures)
	if b.paseFailures.Add(1) != paseMaxErrors {
		// Below the cap, or a straggler arriving between the cap and the
		// lockout taking effect. Only the transition to exactly
		// paseMaxErrors engages the lockout, so the close + diagnostic
		// fire once per batch.
		return
	}
	cooldown := b.engagePaseLockout()
	b.count(contract.MetricPASELockouts)
	win := b.CommissioningWindow()
	b.logger.Warn("matter.rx.sc.pase_bruteforce",
		slog.Int("max_errors", paseMaxErrors),
		slog.Duration("lockout", cooldown),
		slog.String("hint", "too many PASE pairing failures; closing the commissioning window and refusing PASE for the lockout period"))
	b.diagRing().Record(diagevent.Event{
		Kind:     diagevent.KindPairing,
		Severity: diagevent.SeverityError,
		Message: "Pairing was locked after too many failed attempts. It unlocks " +
			"again after the lockout period; opening a new pairing window " +
			"unlocks it immediately.",
		Detail: map[string]string{
			"max_errors":       strconv.Itoa(paseMaxErrors),
			"lockout_duration": cooldown.String(),
		},
	})
	if win != nil {
		// EndCommissioning runs the window's restore closure synchronously, and
		// every production restore closure re-attaches the configured PASE
		// acceptor via AttachPaseHandler / AttachPaseHandlerProvider — each of
		// which calls resetPaseFailures(). Without this guard that reset
		// clears the lockout we just engaged, so the refusal lives only
		// microseconds and guessing continues at full rate with the backoff
		// streak stuck at 0. Preserve the in-force lockout across the internal
		// restore; a genuine operator-initiated window open takes a different
		// path and still starts clean.
		b.preserveLockoutOnReset.Store(true)
		win.EndCommissioning()
		b.preserveLockoutOnReset.Store(false)
	}
}

// engagePaseLockout refuses PASE for a bounded cooldown and returns the
// cooldown that was applied. Consecutive lockouts without an operator
// intervention double it, up to [paseLockoutMaxCooldown], so a host that
// keeps guessing pays an ever-growing price while a genuine operator
// waits the base cooldown at worst.
//
// The failure counter starts over: from here on the cooldown — not the
// counter — is what keeps PASE closed, and the next budget must be a
// full [paseMaxErrors] once it expires.
func (b *Bridge) engagePaseLockout() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.paseLockoutStreak++
	cooldown := paseLockoutCooldown
	for range b.paseLockoutStreak - 1 {
		if cooldown >= paseLockoutMaxCooldown {
			break
		}
		cooldown *= 2
	}
	cooldown = min(cooldown, paseLockoutMaxCooldown)
	b.paseLockoutUntil = b.clk().Now().Add(cooldown)
	b.paseFailures.Store(0)
	return cooldown
}

// paseLockedOut reports whether PASE is currently refused because the
// brute-force cap fired. Revoking the commissioning window is not enough
// on its own: an uncommissioned bridge keeps a long-lived
// configured-passcode acceptor armed with no window open, so without this
// refusal the guessing continues after the cap.
//
// The refusal expires by itself after the cooldown
// ([Bridge.engagePaseLockout]) — a permanent latch would hand any LAN
// host a pairing kill switch for the daemon's lifetime. Installing a
// fresh acceptor (opening a pairing window) clears it immediately, which
// is the operator-visible way back.
func (b *Bridge) paseLockedOut() bool {
	b.mu.RLock()
	until := b.paseLockoutUntil
	b.mu.RUnlock()
	return !until.IsZero() && b.clk().Now().Before(until)
}

// resetPaseFailures clears the per-window PASE state — the failure counter,
// any active lockout and its backoff streak. Called when a fresh PASE
// acceptor is installed ([Bridge.AttachPaseHandler] /
// [Bridge.AttachPaseHandlerProvider]) — a commissioning-window boundary —
// so each window gets its own [paseMaxErrors] budget and an unlocked PASE
// path. The unsecured duplicate-detection windows are not window state:
// each lives with its handshake ([unsecuredWindowTable]).
//
// Exception: when the brute-force cap's own RevokeWindow drives the
// re-attach (preserveLockoutOnReset is raised), an in-force lockout and its
// streak are kept. Otherwise the lockout would be cleared the instant it was
// engaged — see [Bridge.recordPaseFailure].
func (b *Bridge) resetPaseFailures() {
	b.paseFailures.Store(0)
	b.mu.Lock()
	// An in-force lockout survives the internal restore the brute-force cap
	// itself triggers (see [Bridge.recordPaseFailure]): clearing it there
	// would unlock PASE the instant it locked. Every other caller — a fresh
	// operator-initiated window open — leaves preserveLockoutOnReset clear
	// and gets a clean budget.
	if !b.preserveLockoutOnReset.Load() {
		b.paseLockoutUntil = time.Time{}
		b.paseLockoutStreak = 0
	}
	b.mu.Unlock()
}

// handlePase + handleCase share the handler-invocation + reply-send
// pattern. Pulled into helpers so the opcode switch stays a flat
// table and the reply-path bookkeeping (errors, log scoping) lives
// in one place per family.
func (b *Bridge) handlePase(
	src net.Addr,
	requestHdr *message.Header,
	proto message.ProtocolHeader,
	payload []byte,
	stage string,
	process func([]byte) (uint8, []byte, error),
	handler any,
) error {
	// Entry marker BEFORE the handler runs: a stage log with no
	// follow-up (no reply, no pase_err) pins a stall inside the
	// handler itself rather than in the dispatch or send path.
	b.logger.Debug("matter.rx.sc.pase",
		slog.String("stage", stage),
		slog.String("src", srcString(src)),
		slog.Int("exchange_id", int(proto.ExchangeID)))
	rxAt := b.clk().Now()
	// Read before the handler runs: a step may drop what came before it.
	replyUnacked := b.handshakeReplyUnacknowledged(handshakeKeyOf(requestHdr, proto.ExchangeID))
	respOpcode, respPayload, err := process(payload)
	if stage == "pake3" && err == nil {
		// The session is established: free the single-active-PASE slot
		// before the reply goes out. A commissioner that reads the reply
		// may close the session and open the next PASE at once (PASE, then
		// a fresh one to commission); released only after the send, the
		// slot was still held when that PBKDFParamRequest arrived on
		// another dispatch goroutine, which answered it pase_busy — dropped
		// after the MRP ack, so the commissioner waited out its whole
		// timeout. matter.js clears #pairingMessenger when the pairing
		// completes (PaseServer.ts:112-118), before its event loop handles
		// the next exchange.
		b.releasePaseInFlight(handshakeKeyOf(requestHdr, proto.ExchangeID))
	}
	b.settleHandshake(src, handshakeKeyOf(requestHdr, proto.ExchangeID), handler, respOpcode, respPayload, err)
	if err != nil {
		// Missing-handler is a routine pre-PASE-wiring condition
		// (commissioner pings before the daemon's PASE port is up);
		// log at debug so the warn channel stays clean. Real handler
		// errors (decode failures, crypto rejection) come through at
		// warn.
		level := slog.LevelWarn
		isMissing := errors.Is(err, ErrPaseHandlerMissing)
		isStateReplay := errors.Is(err, spake2.ErrSessionState)
		if isMissing {
			level = slog.LevelDebug
		}
		b.logger.Log(context.Background(), level, "matter.rx.sc.pase_err",
			slog.String("stage", stage),
			slog.String("src", srcString(src)),
			slog.Any("err", err))

		// Send a StatusReport on genuine PASE failures so the
		// commissioner learns the exchange is rejected rather than
		// waiting through full MRP retransmission timeouts. Skip for
		// the missing-handler condition (nothing wired yet) and for
		// state-machine replays (the original message was already
		// handled and replied to). Mirrors chip PASESession.cpp
		// error-path StatusReport emission.
		if !isMissing && !isStateReplay {
			// matter.js answers EVERY PASE pairing failure — a
			// wrong-passcode key-confirmation mismatch included — with
			// SecureChannelStatusCode.InvalidParam. NoSharedTrustRoots is
			// a CASE-only code (no fabric in common) and never rides a
			// PASE failure. Mirrors matter.js
			// packages/protocol/src/session/pase/PaseServer.ts:207-212
			// (cancelPairing → sendError(InvalidParam)).
			body := mrp.EncodeStatusReport(
				mrp.SCStatusGeneralFailure,
				uint32(mrp.SecureChannelProtocolID),
				mrp.SCStatusProtocolInvalidParameter,
				nil,
			)
			if replyUnacked {
				b.endHandshakeUnreported(handshakeKeyOf(requestHdr, proto.ExchangeID), stage)
			} else if sendErr := b.sendHandshakeFailureReport(src, requestHdr, proto, body, handshakePeerTimingOf(handler, rxAt)); sendErr != nil {
				b.logger.Debug("matter.rx.sc.pase_err.status_report_send_failed",
					slog.String("stage", stage),
					slog.Any("err", sendErr))
			}
			// Brute-force protection: a genuine pairing failure (bad Pake1
			// decode, confirmation mismatch, …). Count it and lock PASE
			// once too many accumulate.
			b.recordPaseFailure()
		}
		// The handshake is over: it died before Pake3, so the Pake3
		// branch's release never runs and the single-active-PASE slot
		// would stay held for the full pasePairingTimeout — an operator
		// retrying immediately after a failed attempt would be refused
		// with pase_busy for up to a minute. A state-machine replay is
		// the exception: the original handshake still owns the slot and
		// is still progressing. Mirrors matter.js PaseServer.ts:112-118,
		// where the finally block clears #pairingMessenger on every
		// termination, not only on success.
		if !isStateReplay {
			key := handshakeKeyOf(requestHdr, proto.ExchangeID)
			b.releasePaseInFlight(key)
			// Its replies go on waiting for their acknowledgement (owner
			// decision 173); only a failure for want of a handler drops
			// them — nobody is left to answer.
			if isMissing {
				b.abandonHandshakeReplies(key)
			}
		}
		return err
	}
	if respPayload == nil {
		// Handler completed without producing a wire reply (Pake3
		// happy-path, for instance). Nothing more to do.
		return nil
	}
	if replyUnacked && failureReport(respOpcode, respPayload) {
		b.endHandshakeUnreported(handshakeKeyOf(requestHdr, proto.ExchangeID), stage)
		return nil
	}
	// Reliable: PASE continuation messages (PBKDFParamResponse, Pake2)
	// are part of the reliable Secure-Channel exchange. A dropped Pake2
	// aborts commissioning — the commissioner waits for a reply that the
	// bridge, absent MRP tracking, would never rebroadcast. matter.js
	// makes every non-standalone-ack reply reliable (MessageExchange.ts:602);
	// the Sigma3/Pake3 piggyback ack (or a standalone ack) stops the
	// retransmit via the universal inbound Ack path (receive.go).
	if err := b.sendHandshakeReplyReliable(src, requestHdr, proto, respOpcode, respPayload, handshakePeerTimingOf(handler, rxAt), 0); err != nil {
		debugReplyError(b.logger, "send_"+stage, src, err)
		return err
	}
	b.dischargeOwedReplyAck(requestHdr, proto)
	return nil
}

func (b *Bridge) handleCase(
	src net.Addr,
	requestHdr *message.Header,
	proto message.ProtocolHeader,
	payload []byte,
	stage string,
	process func([]byte) (uint8, []byte, error),
	handler any,
) error {
	rxAt := b.clk().Now()
	// Read before the handler runs: a Sigma3 drops the Sigma2 before its
	// reply goes out (dispatchCase).
	replyUnacked := b.handshakeReplyUnacknowledged(handshakeKeyOf(requestHdr, proto.ExchangeID))
	respOpcode, respPayload, err := process(payload)
	b.settleHandshake(src, handshakeKeyOf(requestHdr, proto.ExchangeID), handler, respOpcode, respPayload, err)
	if err != nil {
		// Most "case_err"s on a healthy pair are MRP retransmissions of
		// an already-processed sigma message: the per-exchange responder
		// rejects them with `ErrSessionState` ("ProcessSigma1 already
		// called" / "ProcessSigma1 must run first"), which is by-design
		// — the original sigma was processed, this duplicate is just
		// redundant reliability traffic from the commissioner. Logging
		// those at WARN drowns out genuine failures (handler unwired,
		// payload decode error, signature verify mismatch). Keep WARN
		// for unexpected errors; downgrade the expected-state-rejects
		// and the missing-handler case to DEBUG.
		level := slog.LevelWarn
		isStateReject := errors.Is(err, sigma.ErrSessionState)
		switch {
		case errors.Is(err, ErrCaseHandlerMissing),
			errors.Is(err, errSigma1OverCompleted),
			isStateReject:
			level = slog.LevelDebug
		}
		b.logger.Log(context.Background(), level, "matter.rx.sc.case_err",
			slog.String("stage", stage),
			slog.String("src", srcString(src)),
			slog.Any("err", err))

		// Send a StatusReport so the peer learns the exchange is rejected
		// rather than waiting through MRP retransmission timeouts. Skip
		// for state-machine replays (the original Sigma was processed and
		// replied to), for the missing-handler case (nothing is wired
		// yet) and for a Sigma1 that began nothing on the key of a
		// handshake in progress or completed ([Bridge.handleSigma1]).
		// Mirrors chip CASESession.cpp error-path StatusReport emission
		// on Sigma-reject paths.
		if !isStateReject && !errors.Is(err, ErrCaseHandlerMissing) && !errors.Is(err, errSigma1OverCompleted) {
			// Map the error to the Secure-Channel protocol code: a
			// Sigma1 addressing no fabric this node holds is
			// NoSharedTrustRoots, every other failure (decode error,
			// bad ephemeral key, signature verify) InvalidParameter —
			// matter.js CaseServer.ts:88-95.
			body := mrp.EncodeStatusReport(
				mrp.SCStatusGeneralFailure,
				uint32(mrp.SecureChannelProtocolID),
				caseFailureProtocolCode(err),
				nil,
			)
			// The handshake is over. While the Sigma2 still waits for its
			// acknowledgement no report is sent and the Sigma2 goes on
			// being retransmitted (owner decision 173); otherwise the
			// report is sent reliably.
			if replyUnacked {
				b.endHandshakeUnreported(handshakeKeyOf(requestHdr, proto.ExchangeID), stage)
			} else if sendErr := b.sendHandshakeFailureReport(src, requestHdr, proto, body, handshakePeerTimingOf(handler, rxAt)); sendErr != nil {
				b.logger.Debug("matter.rx.sc.case_err.status_report_send_failed",
					slog.String("stage", stage),
					slog.Any("err", sendErr))
			}
		}
		return err
	}
	if respPayload == nil {
		return nil
	}
	if replyUnacked && failureReport(respOpcode, respPayload) {
		b.endHandshakeUnreported(handshakeKeyOf(requestHdr, proto.ExchangeID), stage)
		return nil
	}
	// Reliable: Sigma2 is a CASE continuation message on the reliable
	// Secure-Channel exchange. A dropped Sigma2 aborts CASE — the
	// commissioner waits for a reply the bridge would otherwise never
	// rebroadcast. matter.js makes every non-standalone-ack reply
	// reliable (MessageExchange.ts:602); the Sigma3 piggyback ack stops
	// the retransmit via the universal inbound Ack path (receive.go).
	//
	// A resumed session waits for the initiator's Success from before its
	// Sigma2_Resume goes out, so a Success that overtakes this send's
	// return finds it ([Bridge.settleAwaitedResume]).
	key := handshakeKeyOf(requestHdr, proto.ExchangeID)
	var installed uint16
	resumer, _ := handler.(*CaseAdapter)
	if resumer != nil && respOpcode == mrp.SCOpcodeSigma2Resume {
		if installed, _ = resumer.takeResumeInstall(); installed != 0 {
			resumer.awaitResumeSuccess(key, installed)
			b.armResumeDeadline(resumer, key, isConnRoute(src), true)
		}
	}
	establisher, establishing := sigma3EstablisherOf(handler)
	if err := b.sendHandshakeReplyReliable(src, requestHdr, proto, respOpcode, respPayload, handshakePeerTimingOf(handler, rxAt), installed); err != nil {
		debugReplyError(b.logger, "send_"+stage, src, err)
		if installed != 0 {
			// matter.js closes the session when the Sigma2_Resume cannot
			// be sent (CaseServer.ts:207-210 at b25ed960).
			if id, ok := resumer.takeAwaitedResume(key); ok && !b.failResume(resumer, id) {
				b.closeUnacknowledgedResume(id)
			}
		}
		if establisher != nil {
			// The Success did not go out: the session, still
			// establishing, is closed and the handshake fails (matter.js
			// CaseServer.ts:332-336 at b25ed960).
			establisher.completeEstablishment(establishing, false)
		}
		return err
	}
	if establisher != nil {
		// The Success went out for the first time: the session is
		// established, before its acknowledgement is awaited — the
		// initiator may use it from now on (matter.js CaseServer.ts:329-331
		// at b25ed960, the send's onTransmitted hook). A session closed
		// meanwhile stays closed (:344, :367-376).
		if !establisher.completeEstablishment(establishing, true) {
			b.logger.Debug("matter.rx.sc.case_closed_before_established",
				slog.String("src", srcString(src)),
				slog.Int("session_id", int(establishing)))
		}
	}
	b.dischargeOwedReplyAck(requestHdr, proto)
	return nil
}

// sigma3EstablisherOf returns the adapter whose Sigma3 established a
// session in this step and that session's id, read before the Success is
// sent; nil when the step established nothing or its handler is not a
// [CaseAdapter].
func sigma3EstablisherOf(handler any) (a *CaseAdapter, sessionID uint16) {
	step, ok := handler.(*sigma3Step)
	if !ok || step.outcome != sigma3Established {
		return nil, 0
	}
	a, ok = step.handler.(*CaseAdapter)
	if !ok {
		return nil, 0
	}
	if sessionID, ok = a.establishedSession(); !ok {
		return nil, 0
	}
	return a, sessionID
}

// settleAwaitedResume ends the wait of a resumed session for the
// initiator's StatusReport on the exchange key names: a Success
// (general and protocol code Success) establishes it, any other report
// fails its handshake and closes it. A report on an exchange no resumed
// session waits on changes nothing. The handler is resolved without
// building one: a provider is asked only for a key whose Sigma1 record it
// still holds. Mirrors matter.js
// packages/protocol/src/session/case/CaseServer.ts:204-210 at b25ed960
// (waitForSuccess, then completeEstablishmentOf; a failure force-closes the
// session) and SecureChannelMessenger.ts:247-272 (what counts as Success).
func (b *Bridge) settleAwaitedResume(key HandshakeKey, payload []byte) {
	a := b.awaitingResumeAdapter(key)
	if a == nil {
		return
	}
	id, ok := a.takeAwaitedResume(key)
	if !ok {
		return
	}
	general, _, protocolCode, decoded := decodeStatusReport(payload)
	success := decoded && general == mrp.SCStatusGeneralSuccess && protocolCode == mrp.SCStatusProtocolSessionEstablishmentSuccess
	if !a.completeEstablishment(id, success) {
		b.logger.Debug("matter.rx.sc.resume_not_established",
			slog.Int("session_id", int(id)),
			slog.Int("general_code", int(general)))
	}
}

// endsAwaitedResume reports whether a message of opcode on the unsecured
// session ends the wait of a resumed session on its exchange
// ([Bridge.endAwaitedResume]): every opcode but the StatusReport that
// answers it, an acknowledgement, and a Sigma1 — copies of the Sigma1 that
// began the resumption reach the router (Apple sends it over several
// interfaces; the router answers them from its Sigma1 record,
// [Bridge.handleSigma1]), and one of other bytes on the key of a completed
// handshake begins nothing (errSigma1OverCompleted).
func endsAwaitedResume(opcode uint8) bool {
	switch opcode {
	case mrp.SCOpcodeStatusReport, mrp.StandaloneAckOpcode, mrp.SCOpcodeSigma1:
		return false
	}
	return true
}

// awaitingResumeAdapter is the CASE adapter that may hold a resumed
// session waiting on the exchange key names, resolved without building
// one: a provider is asked only for a key whose Sigma1 record it still
// holds, otherwise the singleton handler; nil when neither is a
// [CaseAdapter].
func (b *Bridge) awaitingResumeAdapter(key HandshakeKey) *CaseAdapter {
	b.mu.RLock()
	_, held := b.sigma1Replied[key]
	provider, singleton := b.caseProvider, b.caseHandler
	b.mu.RUnlock()
	h := singleton
	if provider != nil && held {
		if p := provider(key); p != nil {
			h = p
		}
	}
	a, _ := h.(*CaseAdapter)
	return a
}

// armResumeDeadline (re)starts the deadline of a's wait for the initiator's
// Success on the exchange key names. matter.js reads that Success with the
// exchange's response timeout, which starts once its Sigma2_Resume's send
// returned — acknowledged over MRP, written over a connection — and on its
// expiry force-closes the session
// (packages/protocol/src/session/case/CaseServer.ts:204-210,
// MessageExchange.ts:894-908 at b25ed960), the timeout the responder's
// wait for Sigma3 has too ([Bridge.caseSigma3Wait]). Over a connection
// (overConn) it runs from the send. Over UDP it runs from the
// acknowledgement ([Bridge.unsecuredReplyAcknowledged]); armed at the send
// (atSend) it first covers the reply's whole time on the unsecured session
// ([unsecuredEntryLifetime]) as well, so a Sigma2_Resume whose entry lost
// its place in the retransmit table still ends the wait. Only a session a
// host installed establishing waits ([CaseAdapter.completesEstablishment]):
// one installed established has nothing to wait for.
func (b *Bridge) armResumeDeadline(a *CaseAdapter, key HandshakeKey, overConn, atSend bool) {
	if !a.completesEstablishment() {
		return
	}
	d := b.caseSigma3Wait(overConn)
	if atSend && !overConn {
		d += unsecuredEntryLifetime
	}
	a.armAwaitedResume(key, b.clk(), d, !atSend, func(id uint16) {
		b.logger.Info("matter.rx.sc.resume_success_timeout",
			slog.Int("session_id", int(id)))
		b.failResume(a, id)
	})
}

// endAwaitedResume ends the wait of a resumed session on the exchange key
// names without its Success: the handshake failed, and a session a host
// installed establishing is closed ([Bridge.failResume]). Nothing waits on
// key: nothing happens. It returns the session it ended and whether that
// was closed (by entry); one installed established is left to the callers
// that close by id, as before.
func (b *Bridge) endAwaitedResume(key HandshakeKey) (sessionID uint16, byEntry bool) {
	a := b.awaitingResumeAdapter(key)
	if a == nil {
		return 0, false
	}
	id, ok := a.takeAwaitedResume(key)
	if !ok {
		return 0, false
	}
	return id, b.failResume(a, id)
}

// failResume closes the resumed session id, a's, whose handshake failed
// before it was established: through a's host hook, which closes the
// installed session itself and not whatever holds its id by then (decision
// 159) — a force close, no CloseSession (matter.js CaseServer.ts:207-210 at
// b25ed960); for a host without the hook nothing here: it installed the
// session established, and the callers close it by id where they did
// before ([Bridge.closeUnacknowledgedResume]). It
// reports whether the session was closed by entry.
func (b *Bridge) failResume(a *CaseAdapter, id uint16) bool {
	if !a.completesEstablishment() {
		return false
	}
	a.completeEstablishment(id, false)
	return true
}

// closeUnacknowledgedResume closes the secure session a resumption installed
// whose Sigma2_Resume the initiator did not acknowledge: its
// retransmissions were spent, its time on the unsecured session ran out
// ([tickOutboundReliable]), it could not be sent ([Bridge.handleCase]) or
// its handshake was ended ([Bridge.abandonHandshakeReplies]). That is
// matter.js's outcome: its send returns only once the message is
// acknowledged (packages/protocol/src/protocol/MessageExchange.ts:822-830
// at db021da0), and a send that fails — its acknowledgement missing
// included — force-closes the session, which was registered before the
// send and is not yet established
// (packages/protocol/src/session/case/CaseServer.ts:177-190, :197-210 at
// b25ed960). A session a host installed establishing was closed by entry
// before, where the handshake key is known ([Bridge.endAwaitedResume]),
// and is not closed by id again here — except on the give-up of the
// retransmit table ([tickOutboundReliable]), which names only the id: that
// close finds the id free, as the allocator's cursor has moved past it
// (secure/operational allocateIDLocked) — short of a table with no other
// free id, a residual of the id-only report (Fable post-read L1). The controller's CASE
// responder closes such a session too (client/handshake). It is a liveness
// rule, not a defence: an acknowledgement authenticates nothing. An entry
// that lost its place in the retransmit table to a newer one
// ([unsecuredEntryLimit]) closes nothing — its datagram went out, and the
// peer's acknowledgement may still come; the session is left to the idle
// reaper. Review II, R2-P6-sec-3320.
func (b *Bridge) closeUnacknowledgedResume(sessionID uint16) {
	b.mu.RLock()
	reg := b.sessionRegistry
	b.mu.RUnlock()
	b.logger.Info("matter.rx.sc.resume_unacknowledged",
		slog.Int("session_id", int(sessionID)))
	if reg == nil {
		return
	}
	if err := reg.Close(sessionID); err != nil {
		b.logger.Debug("matter.rx.sc.resume_unacknowledged.miss",
			slog.Int("session_id", int(sessionID)),
			slog.Any("err", err))
	}
}

// sendHandshakeFailureReport ends the handshake on the request's exchange
// with the failure StatusReport body, as a reliable message retransmitted
// until the peer acknowledges it. While a message the handshake sent still
// waits for its acknowledgement no report is sent: the handshake ends
// unreported ([Bridge.endHandshakeUnreported]; review II, F-db021da0-7),
// and that message is not dropped but goes on being retransmitted until it
// is acknowledged or its retransmissions are spent (owner decision 173) —
// in both references the message that fails here does not reach the
// handshake, which goes on and keeps its reply in retransmission (matter.js
// packages/protocol/src/protocol/MessageExchange.ts:521-525 at db021da0;
// connectedhomeip src/messaging/ExchangeContext.cpp:585-595 at 6170af84),
// and since review II, phase 7 (decision 277) it does not here either: it
// is refused on receive, so over UDP the guard is reached only in a residue
// and over TCP never, nothing being tracked there (bridge/reply.go:262-263); it
// stays as matter.js's own guard stays (CaseServer.ts:82-86,
// PaseServer.ts:207-210; owner decision 282, [Bridge.endHandshakeUnreported]),
// and matter.js goes on retransmitting a message whose send still waits for
// its acknowledgement when its exchange is closed without a cause
// (MessageExchange.ts:1165-1193, :937-943), and connectedhomeip leaves it in the retransmit table
// unless the exchange is aborted (src/messaging/ExchangeContext.cpp:219-252,
// :261-270, src/messaging/ExchangeHolder.h:122-139 at 6170af84). The ACK
// pump's ticks are what retransmit, so it ends with [Bridge.Stop] too. A
// caller reads the state before its handler runs and calls
// endHandshakeUnreported itself ([Bridge.handlePase],
// [Bridge.handleCase]). Both references send the
// report that way: matter.js `sendError` is `exchange.send` without
// `suppressAck`, which sets the R flag, starts the retransmission timer and
// waits for the acknowledgement
// (packages/protocol/src/securechannel/SecureChannelMessenger.ts:170-172
// and :209-232, packages/protocol/src/protocol/MessageExchange.ts:692,
// :785-790 and :821-828 at db021da0; an exchange that is then closed keeps
// retransmitting, :1165-1190); connectedhomeip's `SendStatusReport` is a
// plain `SendMessage`, reliable on a session that uses MRP
// (src/protocols/secure_channel/PairingSession.h:157-190,
// src/messaging/ExchangeContext.cpp:111-114 at 6170af84), and releasing
// the pairing session afterwards aborts the exchange only while it still
// waits on a response (src/protocols/secure_channel/PairingSession.cpp:285-290,
// src/messaging/ExchangeHolder.h:122-139). Before review II the report was
// sent without the R flag, once (R2-P0-rv-A04).
func (b *Bridge) sendHandshakeFailureReport(
	src net.Addr,
	requestHdr *message.Header,
	proto message.ProtocolHeader,
	body []byte,
	peer *handshakePeerTiming,
) error {
	key := handshakeKeyOf(requestHdr, proto.ExchangeID)
	if b.handshakeReplyUnacknowledged(key) {
		b.endHandshakeUnreported(key, "failure_report")
		return nil
	}
	if err := b.sendHandshakeReplyReliable(src, requestHdr, proto, mrp.SCOpcodeStatusReport, body, peer, 0); err != nil {
		return err
	}
	// The report carried the acknowledgement of the request.
	b.dischargeOwedReplyAck(requestHdr, proto)
	return nil
}

// peerSessionParamsSource is the optional capability of a PASE or CASE
// handler that kept the initiator's SessionParameters from its
// PBKDFParamRequest / Sigma1 ([PaseAdapter.PeerSessionParams],
// [CaseAdapter.PeerSessionParams]).
type peerSessionParamsSource interface {
	PeerSessionParams() *sessionparams.Parameters
}

// handshakePeerTimingOf is the initiator's MRP timing for the reply to the
// request received at rxAt: the intervals the handler kept from the
// request (the spec defaults when it kept none or lacks the capability),
// the peer counted active from rxAt. matter.js creates the responder's
// unsecured session with the peer active (UnsecuredSession.ts:36 at
// db021da0, `setActiveTimestamp: !isInitiator`) and adopts the initiator's
// parameters before it replies (PaseServer.ts:153-156,
// CaseServer.ts:120-123), so its PBKDFParamResponse, Pake2 and Sigma2
// retransmit at the peer's active interval (MRP.ts:181).
//
// The initiator has not authenticated yet, so its intervals are clamped to
// [sessionparams.MaxUnauthenticatedInterval] here, where the bridge adopts
// them for the handshake's own replies (review II, owner decision 45; the
// controller side clamps at the same point, client/exchange). The secure
// session the handshake establishes keeps them as stated.
func handshakePeerTimingOf(handler any, rxAt time.Time) *handshakePeerTiming {
	var params *sessionparams.Parameters
	if s, ok := handler.(peerSessionParamsSource); ok {
		if stated := s.PeerSessionParams(); stated != nil {
			clamped := sessionparams.ClampUnauthenticated(*stated)
			params = &clamped
		}
	}
	return newHandshakePeerTiming(params, rxAt)
}

// markSigma1Replied records that we have produced a Sigma2 reply for
// `(key, hash)`. Returns true when an earlier Sigma1 arrival of the
// same handshake (initiator and exchange) with the SAME hash was
// already replied to — caller drops the duplicate. Returns false when
// the entry is fresh or the previous hash for the handshake differs
// (new Sigma1 round).
//
// Hash comparison ensures a Sigma1 retry after a state reset (e.g.
// Apple cancelled the first attempt and started a new one with a
// fresh ephemeral) is not mistaken for a replay.
//
// The record is made before the Sigma1 is judged, so concurrent copies of
// it are dropped; past [maxCaseHandshakes] records the oldest one loses
// its place only once the Sigma1 began a handshake
// ([Bridge.boundSigma1Records]), so a Sigma1 that begins nothing takes no
// handshake's place (review II, R2-P3-rr-dev-04). Until then the records
// exceed the bound by at most the Sigma1s being judged.
//
// The record of a handshake the router holds already — in progress or
// completed — stays while a Sigma1 of other bytes is judged: overRecord
// reports it, and the record is replaced only once that Sigma1 began a
// handshake ([Bridge.handleSigma1]). A Sigma1 that begins nothing ends no
// handshake: a completed one's record keeps a Sigma3 of it from a handler
// ([Bridge.dispatchSigma3]; review II, R2-P4-rr-sec-04, decision 284),
// and one in progress still takes its initiator's Sigma3 (R2-P4-fr-sec-03).
// The Sigma1 being judged is marked on the record, so its concurrent copies
// are dropped as those of a first Sigma1 are.
//
// A Sigma1 is held once across keys: bytes a record carries — answered, or
// being judged — under another key are a replay too. A Sigma1 travels in
// the clear and nothing in it binds it to the header's source node id or
// exchange id, so without this a node on the link that captured one could
// send it again under [maxCaseHandshakes] other keys, begin a handshake
// with each copy and take the place of every handshake in progress, the one
// it was captured from included. Copies of one initiator's Sigma1 arrive
// under its one key (Apple's multicast above) and stay a replay of that key
// (review II, R2-P6-sec-0301).
func (b *Bridge) markSigma1Replied(key HandshakeKey, hash [32]byte) (replay, overRecord bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	existing, ok := b.sigma1Replied[key]
	if ok && (existing.hash == hash || (existing.judged != nil && *existing.judged == hash)) {
		return true, false
	}
	for k, rec := range b.sigma1Replied {
		if k != key && (rec.hash == hash || (rec.judged != nil && *rec.judged == hash)) {
			return true, false
		}
	}
	if ok {
		existing.judged = &hash
		b.sigma1Replied[key] = existing
		return false, true
	}
	b.sigma1Seq++
	b.sigma1Replied[key] = sigma1Record{hash: hash, seq: b.sigma1Seq}
	return false, false
}

// sigma1BeganHandshake follows a Sigma1 of hash that began a handshake on
// key: over the record of another handshake ([Bridge.markSigma1Replied]) its
// record is made now, replacing that one; bySingleton records that the
// singleton handler runs the handshake ([Bridge.AttachCaseHandler]).
func (b *Bridge) sigma1BeganHandshake(key HandshakeKey, hash [32]byte, overRecord, bySingleton, overConn bool) {
	now := b.clk().Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	if overRecord {
		b.sigma1Seq++
		b.sigma1Replied[key] = sigma1Record{hash: hash, seq: b.sigma1Seq, bySingleton: bySingleton, answeredAt: now, overConn: overConn}
		return
	}
	if rec, ok := b.sigma1Replied[key]; ok && rec.hash == hash && rec.completed == nil {
		rec.bySingleton = bySingleton
		rec.answeredAt, rec.overConn = now, overConn
		b.sigma1Replied[key] = rec
	}
}

// endHandshakeOfReplacedHandler ends the handshake a Sigma1 of hash began on
// a singleton handler that was replaced while it judged it: its record
// goes, its hold on a connection is released and what it sent is no longer
// retransmitted.
func (b *Bridge) endHandshakeOfReplacedHandler(key HandshakeKey, hash [32]byte) {
	b.mu.Lock()
	rec, ok := b.sigma1Replied[key]
	ended := ok && rec.hash == hash && rec.completed == nil
	if ended {
		delete(b.sigma1Replied, key)
	}
	b.mu.Unlock()
	if ended {
		b.releaseCASEHandshakeEverywhere(key)
		b.abandonHandshakeReplies(key)
	}
}

// abortFabricHandshakes ends every CASE handshake this node answers whose
// responder resolved fabricIndex and that no Sigma3 completed yet: its
// record goes, what it still retransmits is dropped, and a provider's
// adapter leaves its provider and returns its staked id. A later Sigma3 of
// it is acknowledged and not answered ([Bridge.dropHandshakeNotBegun]).
// connectedhomeip aborts a pending establishment on the updated fabric
// (src/protocols/secure_channel/CASESession.h:149-153,
// CASESession.cpp:423-433 at 6170af84); matter.js has no listener that
// ends a handshake in flight: FabricManager.ts:401 at db021da0 emits
// `replaced`, and its listeners are OperationalCredentialsServer.ts:596
// (the Fabrics attribute and the commissioning state) and
// DeviceAdvertiser.ts:80 (the operational advertisement).
// Returns how many it ended. Review II, R2-P6-sec-2412.
func (b *Bridge) abortFabricHandshakes(fabricIndex uint8) int {
	type pending struct {
		key HandshakeKey
		seq uint64
	}
	var candidates []pending
	b.mu.RLock()
	for key, rec := range b.sigma1Replied {
		if rec.completed == nil {
			candidates = append(candidates, pending{key, rec.seq})
		}
	}
	b.mu.RUnlock()
	ended := 0
	for _, c := range candidates {
		h, provided := b.resolveCaseHandlerOf(c.key)
		a, ok := h.(*CaseAdapter)
		if !ok || !a.pendingOnFabric(fabricIndex) {
			continue
		}
		b.mu.Lock()
		rec, held := b.sigma1Replied[c.key]
		gone := held && rec.seq == c.seq && rec.completed == nil
		if gone {
			delete(b.sigma1Replied, c.key)
		}
		b.mu.Unlock()
		if !gone {
			continue
		}
		b.releaseCASEHandshakeEverywhere(c.key)
		b.abandonHandshakeReplies(c.key)
		if provided {
			a.handshakeFailed()
		}
		ended++
		b.logger.Info("matter.rx.sc.case_aborted_fabric_updated",
			slog.Int("fabric_index", int(fabricIndex)),
			slog.Int("exchange_id", int(c.key.ExchangeID)))
	}
	return ended
}

// sigma1BeganNothingOverRecord follows a Sigma1 of hash that began nothing
// over the record of another handshake: the record stays as it was, without
// the mark of that Sigma1 ([Bridge.markSigma1Replied]).
func (b *Bridge) sigma1BeganNothingOverRecord(key HandshakeKey, hash [32]byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if rec, ok := b.sigma1Replied[key]; ok && rec.judged != nil && *rec.judged == hash {
		rec.judged = nil
		b.sigma1Replied[key] = rec
	}
}

// boundSigma1Records takes their place from the oldest records past
// [maxCaseHandshakes], never key's — the handshake a Sigma1 just began —
// and ends their handshakes as evicted ones end
// ([Bridge.ForgetSigma1Replied]).
func (b *Bridge) boundSigma1Records(key HandshakeKey) {
	var evicted []HandshakeKey
	b.mu.Lock()
	for len(b.sigma1Replied) > maxCaseHandshakes {
		oldest, found := oldestSigma1Record(b.sigma1Replied, key)
		if !found {
			break
		}
		delete(b.sigma1Replied, oldest)
		evicted = append(evicted, oldest)
	}
	b.mu.Unlock()
	for _, k := range evicted {
		b.releaseCASEHandshakeEverywhere(k)
		b.abandonHandshakeReplies(k)
	}
}

// sigma1Record is the router's record of a Sigma1 it answered: the
// payload's hash, and its place in the order the records were made.
type sigma1Record struct {
	hash [32]byte
	seq  uint64
	// completed is set once a Sigma3 completed the handshake
	// ([Bridge.sigma3Handled]).
	completed *completedSigma3
	// bySingleton is set once the Sigma1 began a handshake the singleton
	// handler runs, not a provider's ([Bridge.AttachCaseHandler]).
	bySingleton bool
	// judged is the hash of a Sigma1 of other bytes while it is judged
	// over this record ([Bridge.markSigma1Replied]), else nil.
	judged *[32]byte
	// answeredAt is when the Sigma1 began the handshake, overConn whether
	// it arrived over a connection, and sigma2AckedAt when its Sigma2 was
	// acknowledged: the wait for Sigma3 runs from them
	// ([Bridge.sigma1RecordOver]).
	answeredAt    time.Time
	overConn      bool
	sigma2AckedAt time.Time
}

// runBySingleton reports whether the singleton handler runs, or completed,
// the handshake of the record ([Bridge.AttachCaseHandler]).
func (rec *sigma1Record) runBySingleton() bool {
	return rec.bySingleton || (rec.completed != nil && rec.completed.bySingleton)
}

// oldestSigma1Record returns the key of the record made first, other than
// except's.
func oldestSigma1Record(records map[HandshakeKey]sigma1Record, except HandshakeKey) (HandshakeKey, bool) {
	var (
		oldest HandshakeKey
		seq    uint64
		found  bool
	)
	for k, r := range records {
		if k == except {
			continue
		}
		if !found || r.seq < seq {
			oldest, seq, found = k, r.seq, true
		}
	}
	return oldest, found
}

// handleSigma1 runs a Sigma1 the router recorded through the handler of
// its handshake; a Sigma1 that left no handshake state leaves no record
// either (review II, R2-P3-dr-dev-03). On the key of a handshake the router
// holds — in progress or completed (overRecord, [Bridge.markSigma1Replied])
// — its record stays unless the Sigma1 began a handshake (review II,
// R2-P4-rr-sec-04, R2-P4-fr-sec-03), and a Sigma1 that began nothing there
// is acknowledged and not answered ([Bridge.dropHandshakeNotBegun]): the
// handler's failure report is not sent, and the reply waiting for the
// initiator's acknowledgement — the Sigma2, or the success report — stays
// the exchange's one reply. Mirrors matter.js
// packages/protocol/src/protocol/MessageExchange.ts:521-534 at db021da0: on
// the handshake's live exchange a message that does not acknowledge the
// pending reply throws a flow error before it reaches the CASE server,
// and nothing answers it — the exchange manager catches the error and
// logs it (ExchangeManager.ts:435-438, :538-549). matter.js throws before
// it takes up the message's acknowledgement
// (MessageExchange.ts:553-561); this module acknowledges it, as it
// acknowledges a Sigma3 or Sigma2_Resume no handshake takes (owner
// decision 70).
func (b *Bridge) handleSigma1(src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, payload []byte, key HandshakeKey, hash [32]byte, overRecord bool) error {
	ch, provided, gen := b.resolveCaseHandlerGen(key)
	begun, established := true, false
	processSigma1 := func(p []byte) (uint8, []byte, error) {
		op, reply, err := ch.ProcessSigma1(p)
		begun = sigma1Began(op, reply, err)
		established = err == nil && establishedBy(op, reply)
		if !begun && overRecord {
			return 0, nil, errSigma1OverCompleted
		}
		return op, reply, err
	}
	err := b.handleCase(src, requestHdr, proto, payload, "sigma1", processSigma1, ch)
	switch {
	case !begun && overRecord:
		b.sigma1BeganNothingOverRecord(key, hash)
		return b.dropHandshakeNotBegun(src, proto)
	case !begun:
		b.forgetSigma1Replied(key)
		return err
	}
	if overRecord {
		b.holdHandshake(src, key, false)
	}
	b.sigma1BeganHandshake(key, hash, overRecord, !provided, isConnRoute(src))
	if !provided && b.caseHandlerSwappedSince(gen) {
		// Another handler was attached while the replaced one judged
		// this Sigma1: the handshake ends as the swap ends the ones the
		// replaced handler began ([Bridge.AttachCaseHandler]), so its
		// Sigma3 never reaches a handler that did not see its Sigma1
		// (review II, U4-fr-sec-1).
		b.endHandshakeOfReplacedHandler(key, hash)
		return err
	}
	if established {
		// The reply established the session — a Sigma2_Resume: the
		// handshake is complete, and its record ends as a Sigma3-completed
		// one does ([Bridge.sigma1RecordOver]; review II,
		// R2-P4-fr-sec-02).
		b.sigma3Handled(key, sigma3Established, completedSigma3{bySingleton: !provided})
	}
	b.boundSigma1Records(key)
	return err
}

// sigma1Began reports whether a Sigma1's handling left a handshake in
// progress: not when the handler failed it — unless its state refused a
// repeated Sigma1, which leaves the handshake it already runs — nor when
// it answered with a failure report.
func sigma1Began(respOpcode uint8, respPayload []byte, err error) bool {
	if err != nil {
		return errors.Is(err, sigma.ErrSessionState)
	}
	if respOpcode == mrp.SCOpcodeStatusReport {
		general, _, _, ok := decodeStatusReport(respPayload)
		return ok && general == mrp.SCStatusGeneralSuccess
	}
	return true
}

// sigma3Handled follows a Sigma3 the handler of the handshake key names
// answered: a Sigma3 that failed the handshake ends the handshake's
// Sigma1 record, as Sigma3 closes the CASE handshake on the wire. A
// completed one keeps its record, completed by done, whatever the wiring:
// a Sigma3 of it that arrives again is acknowledged by the router and
// reaches no handler ([Bridge.dispatchSigma3]; review II, R2-P4-nw-01,
// phase 7, decision 284). The record ends
// when the provider lets its adapter go ([Bridge.ForgetSigma1Replied]), when
// another Sigma1 on the key begins a handshake and replaces it
// ([Bridge.markSigma1Replied]; one that begins nothing leaves it), when
// it loses its place past [maxCaseHandshakes]
// ([Bridge.boundSigma1Records]), and — for a handshake the singleton
// handler completed — when its success report is acknowledged or given up
// ([Bridge.sigma1RecordOver]) or [Bridge.AttachCaseHandler] replaces that
// handler. A Sigma3 of a handshake that had completed already leaves the
// record as it is. A resumption completes a handshake at its Sigma1
// ([Bridge.handleSigma1]) and records it here too.
func (b *Bridge) sigma3Handled(key HandshakeKey, outcome sigma3Outcome, done completedSigma3) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch outcome {
	case sigma3OfCompleted:
	case sigma3Established:
		if rec, ok := b.sigma1Replied[key]; ok {
			rec.completed = &done
			b.sigma1Replied[key] = rec
		}
	default:
		delete(b.sigma1Replied, key)
	}
}

// sigma1RecordHeld reports whether the router's Sigma1 record of the
// handshake key names holds that handshake's state on the unsecured
// session ([Bridge.unsecuredExchangeLive]), and ends a record whose
// handshake is over ([Bridge.sigma1RecordOver]).
func (b *Bridge) sigma1RecordHeld(key HandshakeKey) bool {
	b.mu.RLock()
	rec, ok := b.sigma1Replied[key]
	tracker := b.outboundReliable
	b.mu.RUnlock()
	if !ok {
		return false
	}
	if !b.sigma1RecordOver(key, rec, tracker) {
		return true
	}
	b.endSigma1Record(key, rec)
	return false
}

// sigma1RecordOver reports whether rec, the record of the handshake key
// names, has ended. The records of the handshakes the singleton handler
// runs end here, by the handshake's own events; a provider's record ends
// with the provider's adapter ([Bridge.ForgetSigma1Replied]), as before.
//
// A record holds while a reply of its handshake waits for its
// acknowledgement. Then:
//
//   - A completed handshake's record — by Sigma3, or by the resumption its
//     Sigma1 answered ([Bridge.handleSigma1]) — ends: the success report or
//     the Sigma2_Resume was acknowledged or given up, and the
//     retransmission the record serves is over (review II, R2-P4-rr-sec-02,
//     R2-P4-fr-sec-02). For a handshake Sigma3 completed matter.js lets
//     go of the handshake's unsecured session, its reception state
//     included, at the same point: the CASE server sends the success
//     report and waits for its acknowledgement
//     (packages/protocol/src/session/case/CaseServer.ts:332;
//     packages/protocol/src/protocol/MessageExchange.ts:830, given up at
//     :973-976 and thrown at :840 at db021da0), and then detaches and
//     closes that session (CaseServer.ts:95-98), which leaves the manager's
//     table (packages/protocol/src/session/UnsecuredSession.ts:94-102).
//     For a resumption matter.js holds the session longer: its
//     Sigma2_Resume sent and acknowledged (CaseServer.ts:197-202 at
//     b25ed960), it waits for the initiator's success report (:205) and
//     lets go when
//     that arrives or the wait runs out; the record here ends with the
//     Sigma2_Resume's acknowledgement, which that report is when it
//     carries it.
//   - A handshake whose Sigma2 was acknowledged waits for its Sigma3 for
//     [Bridge.caseSigma3Wait] from that acknowledgement and ends then
//     (R2-P4-fr-sec-02): matter.js's CASE server reads Sigma3 once
//     sendSigma2 returned — on the acknowledgement
//     (MessageExchange.ts:822-830) — with the exchange's timeout
//     (CaseServer.ts:267-279, packages/protocol/src/session/case/CaseMessenger.ts:35-36,
//     packages/protocol/src/securechannel/SecureChannelMessenger.ts:83-89,
//     MessageExchange.ts:872-897), and the timeout ends the handshake
//     (CaseServer.ts:61-99).
//   - Over TCP no acknowledgement comes, and the wait runs from the Sigma2
//     (matter.js sends it without waiting for an acknowledgement when the
//     session uses no MRP: MessageExchange.ts:692, :822).
//   - A Sigma2 given up ends the record at the give-up
//     ([Bridge.unsecuredReplyGivenUp]). One evicted from the full table,
//     which costs it its retransmissions and nothing else
//     ([unsecuredEntryLimit]), holds the record until
//     [unsecuredEntryLifetime] after it was sent, the longest the tracker
//     would have kept it.
func (b *Bridge) sigma1RecordOver(key HandshakeKey, rec sigma1Record, tracker *outboundReliableTracker) bool {
	if !rec.runBySingleton() {
		return false
	}
	if tracker != nil && tracker.holdsUnsecuredExchange(key) {
		return false
	}
	if rec.completed != nil {
		return true
	}
	now := b.clk().Now()
	switch {
	case !rec.sigma2AckedAt.IsZero():
		return !now.Before(rec.sigma2AckedAt.Add(b.caseSigma3Wait(false)))
	case rec.overConn:
		return !now.Before(rec.answeredAt.Add(b.caseSigma3Wait(true)))
	default:
		return !now.Before(rec.answeredAt.Add(unsecuredEntryLifetime))
	}
}

// endSigma1Record ends rec, the record of key, unless it changed since it
// was read; a handshake that did not complete ends as an evicted one does
// ([Bridge.boundSigma1Records]): its hold on a connection is released and
// what it still sends is no longer retransmitted.
func (b *Bridge) endSigma1Record(key HandshakeKey, rec sigma1Record) {
	b.mu.Lock()
	cur, held := b.sigma1Replied[key]
	ended := held && cur.seq == rec.seq && cur.completed == rec.completed
	if ended {
		delete(b.sigma1Replied, key)
	}
	b.mu.Unlock()
	if ended && rec.completed == nil {
		b.releaseCASEHandshakeEverywhere(key)
		b.abandonHandshakeReplies(key)
	}
}

// endOverSigma1Records ends every record whose handshake is over
// ([Bridge.sigma1RecordOver]), whatever exchange its source's unsecured
// window is bound to ([Bridge.sweepUnsecuredWindows] runs it).
func (b *Bridge) endOverSigma1Records() {
	type candidate struct {
		key HandshakeKey
		rec sigma1Record
	}
	var candidates []candidate
	b.mu.RLock()
	tracker := b.outboundReliable
	for key, rec := range b.sigma1Replied {
		if rec.runBySingleton() {
			candidates = append(candidates, candidate{key, rec})
		}
	}
	b.mu.RUnlock()
	for i := range candidates {
		if c := &candidates[i]; b.sigma1RecordOver(c.key, c.rec, tracker) {
			b.endSigma1Record(c.key, c.rec)
		}
	}
}

// unsecuredReplyGivenUp follows the give-up of a reply of the handshake key
// names — its retransmissions spent, or its [unsecuredEntryLifetime] over —
// in the sweep of the retransmission table that gave it up
// ([Bridge.tickOutboundReliable]): when nothing else of the handshake waits
// for its acknowledgement, the record of a handshake the singleton handler
// runs ends there, a completed one's included, unless its Sigma2 was
// acknowledged and it waits for Sigma3 ([Bridge.sigma1RecordOver]). A
// provider's record ends with its adapter, as before. Mirrors matter.js:
// the exchange gives the message up and the send throws
// (packages/protocol/src/protocol/MessageExchange.ts:973-976, :840 at
// db021da0), and the CASE server's error path closes the handshake's
// unsecured session (packages/protocol/src/session/case/CaseServer.ts:61-99).
func (b *Bridge) unsecuredReplyGivenUp(key HandshakeKey) {
	// A resumed session whose Sigma2_Resume was given up never establishes;
	// it is closed by entry before its record of the handshake ends.
	b.endAwaitedResume(key)
	b.mu.RLock()
	rec, ok := b.sigma1Replied[key]
	tracker := b.outboundReliable
	b.mu.RUnlock()
	if !ok || !rec.runBySingleton() {
		return
	}
	if tracker != nil && tracker.holdsUnsecuredExchange(key) {
		return
	}
	if rec.completed != nil || rec.sigma2AckedAt.IsZero() {
		b.endSigma1Record(key, rec)
	}
}

// unsecuredReplyAcknowledged follows the acknowledgement of a reply of the
// handshake key names, at the message that carried it
// ([Bridge.dispatch]): the time of its Sigma2's acknowledgement is recorded,
// and a record that ends with it ends here, not only once its source's
// window is looked at (review II, R2-P4-fr-sec-05).
func (b *Bridge) unsecuredReplyAcknowledged(key HandshakeKey) {
	// A resumed session's wait for the initiator's Success runs from its
	// Sigma2_Resume's acknowledgement ([Bridge.armResumeDeadline]).
	if a := b.awaitingResumeAdapter(key); a != nil {
		b.armResumeDeadline(a, key, false, false)
	}
	b.mu.Lock()
	if rec, ok := b.sigma1Replied[key]; ok && rec.bySingleton && rec.completed == nil && rec.sigma2AckedAt.IsZero() {
		rec.sigma2AckedAt = b.clk().Now()
		b.sigma1Replied[key] = rec
	}
	b.mu.Unlock()
	b.sigma1RecordHeld(key)
}

// caseExpectedCryptoProcessingTime, caseReliablePeerResponseTime and
// casePeerResponseTimeBuffer are the fixed terms of the responder's wait
// for Sigma3 ([Bridge.caseSigma3Wait]): matter.js's
// EXPECTED_CRYPTO_PROCESSING_TIME, the Secure Channel messenger's default
// (packages/protocol/src/securechannel/SecureChannelMessenger.ts:55, :65 at
// db021da0), the 30 s base of a channel without MRP (MRP.ts:90-92) and
// PEER_RESPONSE_TIME_BUFFER (MRP.ts:61).
const (
	caseExpectedCryptoProcessingTime = 35 * time.Second
	caseReliablePeerResponseTime     = 30 * time.Second
	casePeerResponseTimeBuffer       = 5 * time.Second
)

// caseSigma3Wait is how long the responder waits for Sigma3 once its Sigma2
// was acknowledged — over TCP (overConn) once it was sent. Mirrors matter.js
// MRP.maxPeerResponseTimeOf (packages/protocol/src/protocol/MRP.ts:79-120 at
// db021da0) as MessageExchange.nextMessage calls it for the CASE server's
// read of Sigma3 (MessageExchange.ts:872-886, MessageChannel.ts:231-249,
// with expectedProcessingTime EXPECTED_CRYPTO_PROCESSING_TIME,
// SecureChannelMessenger.ts:83-89): over UDP this node's own worst-case
// retransmission time on its own intervals, counted active
// (maxResponseTimeOf, MRP.ts:198-224, [maxScheduleTime]) with its fixed
// backoff, plus the expected processing time and the buffer — the peer's
// direction is left out (includeMaximumSendingTime undefined,
// MessageChannel.ts:240), and the additional delay is zero as the
// unsecured session's replies are tracked without a margin
// ([outboundReliableTracker.trackUnsecured]); over TCP the 30 s base or the
// expected processing time, whichever is longer, plus the buffer
// (MRP.ts:90-92).
func (b *Bridge) caseSigma3Wait(overConn bool) time.Duration {
	if overConn {
		return max(caseReliablePeerResponseTime, caseExpectedCryptoProcessingTime) + casePeerResponseTimeBuffer
	}
	local := b.cfg.SessionIntervals
	if local.Idle <= 0 {
		local.Idle = mrp.SessionIdleIntervalDefault
	}
	if local.Active <= 0 {
		local.Active = mrp.SessionActiveIntervalDefault
	}
	if local.ActiveThreshold <= 0 {
		local.ActiveThreshold = mrp.SessionActiveThresholdDefault
	}
	own := maxScheduleTime(local.Idle, local.Active, local.ActiveThreshold, true, b.LocalFixedMRPBackoff())
	return own + caseExpectedCryptoProcessingTime + casePeerResponseTimeBuffer
}

// completedSigma3 is how a handshake was completed: by the singleton
// handler or by a provider's adapter ([Bridge.AttachCaseHandler]).
type completedSigma3 struct {
	// bySingleton is set when the singleton handler completed the
	// handshake, not a provider's ([Bridge.AttachCaseHandler]).
	bySingleton bool
}

// handshakeCompleted reports whether the record of the handshake key names
// says it completed.
func (b *Bridge) handshakeCompleted(key HandshakeKey) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	rec, ok := b.sigma1Replied[key]
	return ok && rec.completed != nil
}

// sigma3Step is the handler a Sigma3 is settled with
// ([Bridge.settleHandshake]): it names the established session only when
// this Sigma3 established it, so a Sigma3 of a handshake that had
// completed already — the accepted one again included — binds no route.
type sigma3Step struct {
	handler CaseHandler
	outcome sigma3Outcome
}

// establishedSession implements [establishedSessionReporter].
func (s *sigma3Step) establishedSession() (uint16, bool) {
	rep, ok := s.handler.(establishedSessionReporter)
	if s.outcome != sigma3Established || !ok {
		return 0, false
	}
	return rep.establishedSession()
}

// PeerSessionParams implements [peerSessionParamsSource].
func (s *sigma3Step) PeerSessionParams() *sessionparams.Parameters {
	if p, ok := s.handler.(peerSessionParamsSource); ok {
		return p.PeerSessionParams()
	}
	return nil
}

// processSigma3With runs a Sigma3 through h and reports what it did to the
// handshake: a [CaseAdapter] says so itself; for any other handler a
// success report is an establishment and everything else a failure — a
// refusal comes back as a failure StatusReport reply, not as an error.
func processSigma3With(h CaseHandler, payload []byte) (opcode uint8, respPayload []byte, outcome sigma3Outcome, err error) {
	if a, ok := h.(*CaseAdapter); ok {
		return a.processSigma3(payload)
	}
	op, reply, err := h.ProcessSigma3(payload)
	if err == nil && establishedBy(op, reply) {
		return op, reply, sigma3Established, nil
	}
	return op, reply, sigma3Failed, err
}

// forgetSigma1Replied removes the dedupe entry for a handshake. Called
// from the Sigma3 receive path: the CASE handshake is done, and a later
// exchange-id rollover (uint16 wraps in long-lived daemons) must not see
// a stale hash.
func (b *Bridge) forgetSigma1Replied(key HandshakeKey) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sigma1Replied, key)
}

// ForgetSigma1Replied is the exported entry point the daemon wires into
// [PerExchangeCaseProvider.SetOnEvict]. It ends the record of a handshake
// a provider completed ([Bridge.sigma3Handled]); an ABORTED handshake (Sigma1 received,
// Sigma3 never) leaves its dedupe entry orphaned forever because the
// successful-path cleanup never runs. Routing the TTL reaper's eviction
// through here lets a per-exchange provider clean up aborted handshakes
// so the sigma1Replied map cannot grow without bound on a daemon that
// sees many half-completed CASE attempts.
//
// The eviction also ends the handshake's hold on a TCP connection it ran
// on ([Bridge.holdHandshake]): the handshake can no longer complete.
func (b *Bridge) ForgetSigma1Replied(key HandshakeKey) {
	b.forgetSigma1Replied(key)
	b.releaseCASEHandshakeEverywhere(key)
	// A Sigma2 or StatusReport still waiting for its acknowledgement
	// goes with the responder it came from.
	b.abandonHandshakeReplies(key)
}

// ---------------------------------------------------------------------------
// Graceful secure-session teardown (Secure-Channel CloseSession).
// ---------------------------------------------------------------------------

// scStatusProtocolCloseSession is the Secure-Channel StatusReport
// protocol code announcing that the sender will close the current
// session. Verbatim from matter.js packages/types/src/protocol/
// definitions/secure-channel.ts:76 (`CloseSession = 0x0003`).
const scStatusProtocolCloseSession uint16 = 0x0003

// stopSessionCloseBudget caps the total time [Bridge.Stop] spends on
// graceful secure-session teardown. Each per-session notification is a
// single best-effort UDP datagram, so the budget is generous — it
// exists so a wedged wire path can never stall daemon shutdown.
const stopSessionCloseBudget = 2500 * time.Millisecond

// SessionRegistry is the bridge-side seam to the operational session
// manager for session lifecycle actions the Secure-Channel wire path
// initiates: closing a single session on an inbound CloseSession
// StatusReport, and draining every session gracefully at shutdown.
// Backed by `secure/operational.Manager` in the production daemon via
// [Bridge.AttachSessionRegistry]; tests substitute a fake.
type SessionRegistry interface {
	// Close removes the session and runs the manager's close hooks
	// (subscription cascade). Returns an error when the id is unknown.
	Close(sessionID uint16) error
	// CloseAllGraceful notifies every peer (best-effort CloseSession
	// StatusReport via the registry's graceful-close notifier) and
	// closes all sessions. Notifications stop once deadline passes;
	// local teardown always completes. Returns the sessions closed.
	CloseAllGraceful(deadline time.Time) int
}

// gracefulCloseNotifierSetter is the optional capability a
// [SessionRegistry] implements to receive the bridge's outbound
// CloseSession sender. The operational manager fires it once per
// session before zeroising the keys on graceful teardown — the Go
// translation of matter.js ExchangeManager.ts:758 (at db021da0) observing
// session.gracefulClose and shipping the report via #sendCloseSession.
type gracefulCloseNotifierSetter interface {
	SetGracefulCloseNotifier(fn func(sessionID uint16, sess *channel.Session))
}

// reannounceTriggerSetter is the optional capability a
// [SessionRegistry] implements to receive the bridge's mDNS
// broadcast-resume trigger, fired with the fabric index after a reap /
// eviction leaves a (fabric, peer) pair with zero live sessions
// (matter.js packages/protocol/src/protocol/DeviceAdvertiser.ts:131-150
// at db021da0).
type reannounceTriggerSetter interface {
	SetReannounceTrigger(fn func(fabricIndex uint8))
}

// AttachSessionRegistry wires the operational session manager into the
// bridge's Secure-Channel path. Beyond storing the registry for the
// inbound-CloseSession and shutdown paths, it self-wires the reverse
// hooks when the registry supports them: the graceful-close notifier
// (outbound CloseSession StatusReport before key zeroise) and the
// mDNS reannounce trigger, the PASE close hook and the idle reaper's
// exemption for a session that carries a live subscription. Pass nil to
// detach.
//
// The registry's own close hook is the host's to wire (it also feeds the
// subscription manager): the host calls [Bridge.SessionClosed] from it,
// or a TCP connection whose sessions all closed by another path than an
// inbound or outbound CloseSession stays open until the peer drops it.
func (b *Bridge) AttachSessionRegistry(reg SessionRegistry) {
	b.mu.Lock()
	b.sessionRegistry = reg
	b.mu.Unlock()
	if c, ok := reg.(sessionCounter); ok {
		b.sessionCount.Store(&sessionCounterRef{c: c})
	} else {
		b.sessionCount.Store(nil)
	}
	if reg == nil {
		return
	}
	if setter, ok := reg.(gracefulCloseNotifierSetter); ok {
		setter.SetGracefulCloseNotifier(b.sendCloseSessionReport)
	}
	if setter, ok := reg.(reannounceTriggerSetter); ok {
		setter.SetReannounceTrigger(b.triggerSessionReannounce)
	}
	if setter, ok := reg.(paseCloseNotifierSetter); ok {
		setter.SetOnPaseSessionClose(b.triggerCommissioningReannounce)
	}
	b.wireReapExemption(reg)
}

// paseCloseNotifierSetter is the session registry's hook for a closed
// PASE session (secure/operational Manager.SetOnPaseSessionClose), which
// the bridge takes for the commissioning re-advertisement.
type paseCloseNotifierSetter interface {
	SetOnPaseSessionClose(fn func(sessionID uint16))
}

// abortPaseOnStatusReport ends the PASE handshake in progress when the
// commissioner answers it with a failure StatusReport — chip's initiator
// does so when Pake2's key confirmation fails, which is how a wrong
// passcode surfaces. matter.js PaseServer.onNewExchange sees the report as
// a ChannelStatusResponseError: it counts a pairing error toward
// PASE_COMMISSIONING_MAX_ERRORS, cancels the pairing without answering,
// and frees the server for the next attempt (PaseServer.ts:94-118).
// Without it the handshake kept the single-active-PASE slot until
// pasePairingTimeout, every retry in that minute was dropped as busy, and
// the twenty wrong-passcode attempts of TC-CADMIN-1.9 timed out instead
// of failing. Returns whether the report belonged to the PASE handshake.
func (b *Bridge) abortPaseOnStatusReport(src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, payload []byte) bool {
	if requestHdr.SessionID != 0 {
		return false
	}
	generalCode, _, protocolCode, ok := decodeStatusReport(payload)
	if !ok || generalCode == mrp.SCStatusGeneralSuccess {
		return false
	}
	key := handshakeKeyOf(requestHdr, proto.ExchangeID)
	b.mu.Lock()
	ours := !b.paseInFlightSince.IsZero() && b.paseInFlight == key
	b.mu.Unlock()
	if !ours {
		return false
	}
	b.logger.Info("matter.rx.sc.pase_aborted_by_peer",
		slog.String("src", srcString(src)),
		slog.Int("exchange_id", int(proto.ExchangeID)),
		slog.Int("general_code", int(generalCode)),
		slog.Int("protocol_code", int(protocolCode)))
	b.recordPaseFailure()
	b.releasePaseInFlight(key)
	b.abandonHandshakeReplies(key)
	// The report itself is acked by the pump, like any reliable message
	// that elicits no reply ([Bridge.AttachAckTracker]).
	return true
}

// decodeStatusReport splits a Secure-Channel StatusReport body into
// its fixed fields per Matter §4.10.1.1: LE uint16 generalCode ||
// uint32 protocolID || uint16 protocolCode (|| optional protocolData,
// ignored here). Mirrors matter.js SecureChannelStatusMessageSchema
// decode (packages/protocol/src/securechannel/
// SecureChannelStatusMessageSchema.ts). ok=false on a truncated body.
func decodeStatusReport(payload []byte) (generalCode uint16, protocolID uint32, protocolCode uint16, ok bool) {
	if len(payload) < 8 {
		return 0, 0, 0, false
	}
	return binary.LittleEndian.Uint16(payload[0:2]),
		binary.LittleEndian.Uint32(payload[2:6]),
		binary.LittleEndian.Uint16(payload[6:8]),
		true
}

// handleSecureChannelStatusReport services an inbound Secure-Channel
// StatusReport. The only initial StatusReport a peer legitimately
// opens an exchange with is CloseSession — the session it rides on is
// closed immediately (subscription cleanup cascades through the
// registry's close hooks); every other combination is a stray peer
// retransmit and is ignored with a debug log. Mirrors matter.js
// SecureChannelProtocol.ts:54-82 handleInitialStatusReport: non-close
// reports log + close the exchange, CloseSession resolves the session
// and calls session.handlePeerClose (SecureChannelProtocol.ts:81-82
// "Closed by peer").
func (b *Bridge) handleSecureChannelStatusReport(src net.Addr, requestHdr *message.Header, payload []byte) error {
	generalCode, protocolID, protocolCode, ok := decodeStatusReport(payload)
	isClose := ok &&
		generalCode == mrp.SCStatusGeneralSuccess &&
		protocolID == uint32(mrp.SecureChannelProtocolID) &&
		protocolCode == scStatusProtocolCloseSession
	if !isClose || requestHdr.SessionID == 0 {
		// matter.js ignores unexpected initial StatusReports with a
		// debug log (SecureChannelProtocol.ts:71-77); an unsecured
		// CloseSession is meaningless (session 0 has no keys to drop).
		b.logger.Debug("matter.rx.sc.status_report",
			slog.String("src", srcString(src)),
			slog.Int("session_id", int(requestHdr.SessionID)),
			slog.Int("payload_bytes", len(payload)))
		return nil
	}
	// Deferred: the route goes after the session, and with the session's
	// last route a TCP connection — matter.js closes the channel from
	// #deleteSession, after the session left the table
	// (ExchangeManager.ts:761-779 at db021da0).
	defer b.unbindSessionRoute(requestHdr.SessionID)
	// Every exchange the session carried dies with it, so any timed
	// deadline it registered can never be consumed. [Bridge.SessionClosed]
	// drops them again when the host's close hook reaches it; this call is
	// the only drop for a host that does not wire that hook, so it stays
	// (review II, N-sess-2).
	b.routing.dropSessionTimedDeadlines(requestHdr.SessionID)
	b.mu.RLock()
	reg := b.sessionRegistry
	b.mu.RUnlock()
	if reg == nil {
		b.logger.Debug("matter.rx.sc.close_session.no_registry",
			slog.Int("session_id", int(requestHdr.SessionID)))
		return nil
	}
	if err := reg.Close(requestHdr.SessionID); err != nil {
		// Already gone — racing a local reap / fabric teardown.
		b.logger.Debug("matter.rx.sc.close_session.miss",
			slog.Int("session_id", int(requestHdr.SessionID)),
			slog.Any("err", err))
		return nil
	}
	b.logger.Info("matter.rx.sc.close_session",
		slog.String("src", srcString(src)),
		slog.Int("session_id", int(requestHdr.SessionID)))
	b.diagRing().Record(diagevent.Event{
		Kind:     diagevent.KindSession,
		Severity: diagevent.SeverityInfo,
		Message:  "A controller closed its secure session with the bridge.",
		Detail: map[string]string{
			"peer":       srcString(src),
			"session_id": strconv.Itoa(int(requestHdr.SessionID)),
		},
	})
	return nil
}

// resolveSessionPeerAddr finds the last-known UDP address for a secure
// session so the graceful CloseSession StatusReport can be routed.
// Resolution order:
//
//  1. the authenticated Secure-Channel receive path's per-session
//     record (StandaloneAcks and other SC datagrams),
//  2. the per-subscription routing targets (a subscribed controller —
//     the common Apple Home case — always has one),
//  3. the owed-ack exchange table (a recent reliable IM request).
//
// Returns nil when the peer address was never observed; the caller
// skips the notification (best-effort, matching the try/catch around
// matter.js ExchangeManager.ts:781-791 #sendCloseSession at db021da0).
func (b *Bridge) resolveSessionPeerAddr(sessionID uint16) net.Addr {
	if raw, ok := b.sessionPeerAddrs.Load(sessionID); ok {
		if addr, ok := raw.(net.Addr); ok && !isNilRoute(addr) {
			return addr
		}
	}
	var addr net.Addr
	b.routing.subTargets.Range(func(_, v any) bool {
		if t, ok := v.(subTarget); ok && t.sessionID == sessionID && !isNilRoute(t.src) {
			addr = t.src
			return false
		}
		return true
	})
	if !isNilRoute(addr) {
		return addr
	}
	b.routing.exchangeSrcs.Range(func(k, v any) bool {
		key, ok := k.(mrp.ExchangeKey)
		if !ok || key.SessionID != sessionID {
			return true
		}
		if t, ok := v.(exchangeReplyTarget); ok && !isNilRoute(t.src) {
			addr = t.src
			return false
		}
		return true
	})
	return addr
}

// sendCloseSessionReport ships a Secure-Channel CloseSession
// StatusReport (GeneralCode=SUCCESS, ProtocolCode=CLOSE_SESSION) to
// the session's peer on a fresh bridge-initiated exchange. Wired as
// the operational manager's graceful-close notifier, so it runs while
// the session keys are still live. Strictly best-effort: any failure
// (unknown peer address, encrypt error, socket error) is logged at
// debug and swallowed — matter.js wraps the same send in a
// try/catch-and-warn (ExchangeManager.ts:781-791 #sendCloseSession at
// db021da0).
//
// Wire shape mirrors matter.js SecureChannelMessenger.ts:156-158
// sendCloseSession → #sendStatusReport(Success, CloseSession,
// requiresAck=false): the report is NOT MRP-tracked, the peer does not
// ack a farewell.
func (b *Bridge) sendCloseSessionReport(sessionID uint16, sess *channel.Session) {
	// The session is being torn down whether or not the farewell
	// reaches the peer, so its per-exchange timed deadlines and its
	// learned peer address can never be used again — neither of the
	// early returns below means the session survives. Teardown paths
	// that do not notify the peer fall back to the expiry sweep and to
	// overwrite-on-reuse respectively. As on the inbound CloseSession,
	// this drop stays beside the purge of [Bridge.SessionClosed]: a host
	// that does not wire the registry's close hook to it has no other
	// (review II, N-sess-2).
	b.routing.dropSessionTimedDeadlines(sessionID)
	// Deferred, not dropped here: this farewell still has to be routed
	// on that address, and every exit path must release it.
	// unbindSessionRoute also closes a TCP connection this was the last
	// session of — after the farewell went out on it.
	defer b.unbindSessionRoute(sessionID)
	b.mu.RLock()
	listener := b.listener
	b.mu.RUnlock()
	if sess == nil {
		return
	}
	dst := b.resolveSessionPeerAddr(sessionID)
	if listener == nil && !isConnRoute(dst) {
		return
	}
	if isNilRoute(dst) {
		b.logger.Debug("matter.tx.sc.close_session.no_peer_addr",
			slog.Int("session_id", int(sessionID)))
		return
	}
	proto := message.ProtocolHeader{
		Initiator:  true,
		Opcode:     mrp.SCOpcodeStatusReport,
		ExchangeID: b.nextOutboundExchangeID(),
		ProtocolID: mrp.SecureChannelProtocolID,
		NeedsAck:   false,
	}
	body := append(proto.Marshal(), mrp.EncodeStatusReport(
		mrp.SCStatusGeneralSuccess,
		uint32(mrp.SecureChannelProtocolID),
		scStatusProtocolCloseSession,
		nil,
	)...)
	// Stamp the peer's view of the SessionID so their inbound table
	// resolves the session — see [Bridge.sendReply] for the rationale.
	hdr := message.Header{SessionID: sess.PeerSessionID()}
	if hdr.SessionID == 0 {
		hdr.SessionID = sessionID
	}
	// Deliberately NOT [Bridge.encryptSecureOutbound]: the farewell seals
	// for a session that is being torn down — its manager entry is already
	// gone, and refreshing activity on a dying session would be
	// meaningless at best.
	enc, err := sess.Encrypt(&hdr, securityFlagsByte(&hdr), body)
	if err != nil {
		b.logger.Debug("matter.tx.sc.close_session.encrypt",
			slog.Int("session_id", int(sessionID)),
			slog.Any("err", err))
		return
	}
	datagram := append(hdr.Marshal(), enc.Ciphertext...) //nolint:gocritic // single-allocation join
	if err := b.sendRouted(listener, dst, sessionID, datagram); err != nil {
		b.logger.Debug("matter.tx.sc.close_session.send",
			slog.Int("session_id", int(sessionID)),
			slog.Any("err", err))
		return
	}
	b.logger.Debug("matter.tx.sc.close_session",
		slog.String("peer", srcString(dst)),
		slog.Int("session_id", int(sessionID)))
}

// serviceDisconnecter is the advertiser capability of the "reconnect"
// broadcast event (mdns.Responder.ServiceDisconnected).
type serviceDisconnecter interface {
	ServiceDisconnected(instanceName, serviceType string)
}

// serviceRetransmitter is the advertiser capability of the "retransmit"
// broadcast event (mdns.Responder.ServiceRetransmission).
type serviceRetransmitter interface {
	ServiceRetransmission(instanceName, serviceType string)
}

// triggerSessionReannounce resumes mDNS broadcast for fabricIndex after
// a session teardown left a peer of that fabric with zero live sessions,
// so the controller rediscovers the bridge and re-establishes CASE
// instead of showing it unresponsive. Mirrors matter.js
// packages/protocol/src/protocol/DeviceAdvertiser.ts:131-150 at db021da0
// (sessions.deleted → advertisementForFabric(fabric).serviceDisconnected(),
// Advertiser.ts:69-75, which starts the "reconnect" broadcast event,
// MdnsAdvertisement.ts:133-135; a running schedule is not reset,
// Advertisement.ts:78-91): an advertiser with ServiceDisconnected
// (mdns.Responder) runs the broadcast schedule again for that fabric's
// operational service only; an advertiser without the events re-publishes
// every active operational record instead. Index 0 — a PASE session, no
// fabric — reaches no operational service and returns: a closed PASE
// session's commissioning re-advertisement is
// [Bridge.triggerCommissioningReannounce]'s (DeviceAdvertiser.ts:152-161),
// fired by the session registry's PASE close hook for the session that
// was PASE, adopted onto a fabric or not. A terminated subscription takes
// this path for its fabric (DeviceAdvertiser.ts:174-179; see
// [Bridge.AttachSubscriptionManager]).
//
// The republish runs on its own goroutine bounded by the advertise
// timeout, because the trigger fires from the reaper / receive paths
// which must not block on the advertiser. The bridge owns
// it: it runs under the serve context, which Stop cancels, and Stop
// waits for it before the advertiser closes. No-op once the bridge
// stopped — matter.js shuts the advertiser down before removing
// sessions to prevent exactly this re-announce
// (ServerNetworkRuntime.ts:450-453 at b25ed960).
func (b *Bridge) triggerSessionReannounce(fabricIndex uint8) {
	if fabricIndex == 0 {
		return
	}
	instance, scoped := b.operationalInstanceFor(fabricIndex)
	b.mu.RLock()
	started := b.started
	advertiser := b.advertiser
	timeout := b.cfg.AdvertiseTimeout
	base := b.handlerCtx
	_, events := advertiser.(serviceDisconnecter)
	run := started && advertiser != nil && (scoped || !events)
	if run {
		// Added under mu while started: Stop's Wait follows its
		// started=false under the write lock, so no Add races it.
		b.reannounces.Add(1)
	}
	b.mu.RUnlock()
	if !run {
		return
	}
	if base == nil {
		base = context.Background()
	}
	go func() {
		defer b.reannounces.Done()
		ctx, cancel := clock.WithTimeout(base, b.clk(), timeout)
		defer cancel()
		// The responder takes the "reconnect" event; an advertiser
		// with only an on-demand trigger announces once; the rest
		// degrade to a per-record re-publish, which the
		// [mdns.Advertiser] contract defines as a re-announce.
		switch ev := advertiser.(type) {
		case serviceDisconnecter:
			ev.ServiceDisconnected(instance, mdns.ServiceTypeOperational)
		case interface{ TriggerReannounce(context.Context) }:
			ev.TriggerReannounce(ctx)
		default:
			active := advertiser.Active()
			for i := range active {
				if active[i].ServiceType != mdns.ServiceTypeOperational {
					continue
				}
				if err := advertiser.Publish(ctx, active[i]); err != nil {
					b.logger.Debug("matter.mdns.session_reannounce",
						slog.String("instance", active[i].InstanceName),
						slog.Any("err", err))
				}
			}
		}
		b.logger.Debug("matter.mdns.session_reannounce.done")
	}()
	// An mDNS advertisement change is exactly what an operator needs in
	// the trace when a controller stopped finding the bridge: the
	// re-announce says the records went back on the wire, and its
	// absence says they did not.
	b.diagRing().Record(diagevent.Event{
		Kind:     diagevent.KindDiscovery,
		Severity: diagevent.SeverityInfo,
		Message:  "The bridge re-announced itself on the network after a controller's last session or a subscription ended.",
	})
}

// triggerCommissioningReannounce starts a new announcement round of the
// commissionable record after a PASE session closed, while the record is
// published: the commissioner that comes next — after a fail-safe expiry,
// a revoked window, a commissioner that gave up — finds a fresh round
// instead of a record announced once at the window's open. Mirrors
// matter.js packages/protocol/src/protocol/DeviceAdvertiser.ts:152-161 at
// db021da0 (sessions.deleted → session.isPase →
// #startCommissioningAdvertisement, which returns without a commissioning
// service — after exitCommissioningMode — or while closing) and
// FailsafeCommissioningTest.ts:106-196, :198-303: exactly one round in the
// AddNOC case, whether the session was PASE alone or adopted onto the
// fabric by AddNOC — the CASE session's close takes
// triggerSessionReannounce's operational path and announces nothing
// commissionable. Publishing the record again restarts the responder's
// startup round (mdns.Responder.Publish, same fingerprint). Runs on its
// own goroutine, bounded and awaited as triggerSessionReannounce's; a
// no-op once the bridge stopped or the record is withdrawn.
func (b *Bridge) triggerCommissioningReannounce(uint16) {
	b.mu.RLock()
	started := b.started
	advertiser := b.advertiser
	svc, instance := b.commissioningService, b.commissioningInstanceName
	timeout := b.cfg.AdvertiseTimeout
	base := b.handlerCtx
	run := started && advertiser != nil && instance != ""
	if run {
		b.reannounces.Add(1)
	}
	b.mu.RUnlock()
	if !run {
		return
	}
	if base == nil {
		base = context.Background()
	}
	go func() {
		defer b.reannounces.Done()
		ctx, cancel := clock.WithTimeout(base, b.clk(), timeout)
		defer cancel()
		if err := advertiser.Publish(ctx, svc); err != nil {
			b.logger.Debug("matter.mdns.commissioning_reannounce",
				slog.String("instance", instance), slog.Any("err", err))
			return
		}
		b.logger.Info("matter.mdns.commissioning_reannounced", slog.String("instance", instance))
	}()
}

// closeSecureSessionsForShutdown drains every operational session with
// a best-effort CloseSession StatusReport per peer, bounded by
// [stopSessionCloseBudget]. Called by [Bridge.Stop] BEFORE the UDP
// listener is torn down — without the farewell, controllers keep their
// CASE sessions alive and show the bridge unresponsive for minutes
// after a restart while their retransmits time out. The overall select
// cap guarantees Stop never blocks on the teardown even if the
// registry wedges; the registry additionally enforces the same
// deadline per notification.
func (b *Bridge) closeSecureSessionsForShutdown() {
	b.mu.RLock()
	started := b.started
	reg := b.sessionRegistry
	b.mu.RUnlock()
	if !started || reg == nil {
		return
	}
	deadline := b.clk().Now().Add(stopSessionCloseBudget)
	done := make(chan int, 1)
	go func() { done <- reg.CloseAllGraceful(deadline) }()
	// The budget's timer is stopped on either branch, so none stays armed
	// on the bridge's clock after Stop.
	budget := b.clk().NewTimer(stopSessionCloseBudget)
	defer budget.Stop()
	select {
	case n := <-done:
		if n > 0 {
			b.logger.Info("matter.bridge.stop.sessions_closed", slog.Int("count", n))
		}
	case <-budget.C():
		b.logger.Warn("matter.bridge.stop.session_close_timeout",
			slog.Duration("budget", stopSessionCloseBudget))
	}
}

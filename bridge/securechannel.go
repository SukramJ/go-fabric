// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
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
	// resumption (commissioner falls back to full Sigma1).
	ProcessSigma2Resume(payload []byte) (uint8, []byte, error)
}

// AckHandler is the inbound MRP-ack port — the [mrp.AckTracker]
// typically backs it. The router calls Discharge for any datagram
// that carries HasAck=true regardless of opcode, so the IM and
// SecureChannel paths share this hook.
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
// The handler is shared across every PASE exchange — appropriate for
// the v1.1 single-Commissioner-window flow. For concurrent PASE
// dispatch (multiple commissioners pairing in parallel) use
// [Bridge.AttachPaseHandlerProvider] instead; when a provider is
// wired the singleton handler is ignored.
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
// CASE opcode (Sigma1, Sigma3, Sigma2Resume) and dispatches to the
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
	b.mu.RLock()
	provider := b.caseProvider
	singleton := b.caseHandler
	b.mu.RUnlock()
	if provider != nil {
		if h := provider(key); h != nil {
			return h
		}
	}
	return singleton
}

// AttachCaseHandler wires the CASE port. Pass nil to revert to noop.
func (b *Bridge) AttachCaseHandler(h CaseHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if h == nil {
		b.caseHandler = noopCaseHandler{}
		return
	}
	b.caseHandler = h
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
// datagrams. Replaces the v1.1.0 logging stub with a full opcode
// fan-out:
//
//   - 0x10 StandaloneAck         → AckHandler.Discharge (no reply)
//   - 0x20 PBKDFParamRequest     → PaseHandler.ProcessPBKDFParamRequest → 0x21 PBKDFParamResponse
//   - 0x22 Pake1                 → PaseHandler.ProcessPake1 → 0x23 Pake2
//   - 0x24 Pake3                 → PaseHandler.ProcessPake3 (no reply or status ack)
//   - 0x30 Sigma1                → CaseHandler.ProcessSigma1 → 0x31 Sigma2
//   - 0x32 Sigma3                → CaseHandler.ProcessSigma3 (no reply or status ack)
//   - 0x33 Sigma2Resume          → CaseHandler.ProcessSigma2Resume → 0x33 reply
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
	// Per-exchange owe-tracker discharge: when an inbound datagram
	// piggybacks an ACK we use the exchange ID to clear any pending
	// owe — the legacy AckHandler is keyed on exchange, distinct
	// from the outbound-reliable counter tracker handled in
	// receive.go.
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

	switch proto.Opcode {
	case mrp.StandaloneAckOpcode:
		// Pure StandaloneAck has no payload after the protocol header.
		// The HasAck path above already discharged the obligation;
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
		// (exchangeID, sigma1Hash) tuple. Mirrors matter.js's
		// `CaseServer.ts::onSigma1` which discards duplicate Sigma1
		// arrivals on the same exchange via `fabric.locked`.
		key := handshakeKeyOf(requestHdr, proto.ExchangeID)
		h := sha256.Sum256(payload)
		if b.markSigma1Replied(key, h) {
			b.logger.Debug("matter.rx.sc.sigma1.replay_dropped",
				slog.String("src", srcString(src)),
				slog.Int("exchange_id", int(proto.ExchangeID)))
			return nil
		}
		// The handshake holds a TCP connection it runs on open until it
		// ends (matter.js ExchangeManager.ts:634-648 counts the unsecured
		// session on the channel).
		b.holdHandshake(src, proto.ExchangeID, false)
		ch := b.resolveCaseHandler(key)
		return b.handleCase(src, requestHdr, proto, payload, "sigma1",
			ch.ProcessSigma1, ch)
	case mrp.SCOpcodeSigma3:
		// The initiator has moved past Sigma2: a Sigma2 still waiting for
		// its acknowledgement is done — its retransmissions are wasted
		// bandwidth at best and confuse Apple iOS's MRP layer at worst (it
		// drops a late Sigma2 with `Dropping message without piggyback ack
		// when we are waiting for an ack`, then aborts the handshake).
		// Before the handler runs, as the Pake3 case does, so the reply to
		// Sigma3 itself — the StatusReport that ends the handshake — stays
		// tracked and is retransmitted until the initiator acknowledges it:
		// matter.js sends it as an ordinary reliable message and waits for
		// its acknowledgement (packages/protocol/src/session/case/CaseServer.ts:332,
		// packages/protocol/src/securechannel/SecureChannelMessenger.ts:209-232,
		// packages/protocol/src/protocol/MessageExchange.ts:692, :785-790
		// and :821-828 at db021da0), connectedhomeip likewise
		// (src/protocols/secure_channel/PairingSession.h:157-190,
		// src/messaging/ExchangeMessageDispatch.cpp:54-75 at 6170af84).
		// Dropped after the handler, the report went out once: lost, it
		// left the initiator waiting out its timeout for a session this
		// node had already installed.
		key := handshakeKeyOf(requestHdr, proto.ExchangeID)
		b.abandonHandshakeReplies(key)
		ch := b.resolveCaseHandler(key)
		if err := b.handleCase(src, requestHdr, proto, payload, "sigma3",
			ch.ProcessSigma3, ch); err != nil {
			return err
		}
		// Sigma3 closes the CASE handshake on the wire — forget the
		// per-exchange Sigma1 dedupe entry so a later exchange-id
		// rollover starts clean.
		b.forgetSigma1Replied(key)
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
	case mrp.SCOpcodeSigma2Resume:
		ch := b.resolveCaseHandler(handshakeKeyOf(requestHdr, proto.ExchangeID))
		return b.handleCase(src, requestHdr, proto, payload, "sigma2_resume",
			ch.ProcessSigma2Resume, ch)

	case mrp.SCOpcodeStatusReport:
		if requestHdr.SessionID == 0 {
			// The peer ended a handshake on this exchange with a failure:
			// it no longer holds a connection open.
			if general, _, _, ok := decodeStatusReport(payload); ok && general != mrp.SCStatusGeneralSuccess {
				b.releaseHandshake(src, proto.ExchangeID)
			}
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
		if !b.claimPaseInFlight(key) {
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
		b.holdHandshake(src, proto.ExchangeID, true)
		h := b.resolvePaseHandler(key)
		return b.handlePase(src, requestHdr, proto, payload, "pbkdf_param_req",
			h.ProcessPBKDFParamRequest, h)
	case mrp.SCOpcodePake1:
		h := b.resolvePaseHandler(key)
		return b.handlePase(src, requestHdr, proto, payload, "pake1",
			h.ProcessPake1, h)
	case mrp.SCOpcodePake3:
		// The commissioner has moved past Pake2: whatever of the
		// handshake still waits for an acknowledgement is done. Before
		// the handler runs, so the reply to Pake3 itself stays tracked.
		b.abandonHandshakeReplies(key)
		h := b.resolvePaseHandler(key)
		err := b.handlePase(src, requestHdr, proto, payload, "pake3",
			h.ProcessPake3, h)
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
// matter.js throws MaximumPasePairingErrorsReachedError and lets the
// PaseServer die with the window (PaseServer.ts:106-110), chip calls
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

// claimPaseInFlight attempts to claim the single-active-PASE slot for
// the handshake key. Returns true when the claim succeeds: the slot was
// idle, expired, or already owned by the SAME handshake
// (PBKDFParamRequest retransmit). Mirrors matter.js PaseServer.ts:80-86
// onNewExchange.
func (b *Bridge) claimPaseInFlight(key HandshakeKey) bool {
	b.mu.Lock()
	now := b.clk().Now()
	idle := b.paseInFlightSince.IsZero() || now.Sub(b.paseInFlightSince) > pasePairingTimeout
	if !idle && b.paseInFlight != key {
		b.mu.Unlock()
		return false
	}
	timedOut, expired := !b.paseInFlightSince.IsZero() && b.paseInFlight != key, b.paseInFlight
	b.paseInFlight = key
	b.paseInFlightSince = now
	b.mu.Unlock()
	if timedOut {
		// The handshake that held the slot timed out: its replies stop
		// waiting for an acknowledgement with it. Outside b.mu — the
		// tracker resolves a session's interval through the bridge.
		b.abandonHandshakeReplies(expired)
	}
	return true
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
// (packages/protocol/src/session/Session.ts:190-192 `isClosing`, :304-309
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
// ([outboundReliableTracker.AbandonUnsecuredExchange]). A caller that goes
// on to send the StatusReport ending the handshake calls it first — the
// report is the one message that has to outlive the handshake. The replies
// of another initiator's handshake on the same exchange id stay.
func (b *Bridge) abandonHandshakeReplies(key HandshakeKey) {
	b.mu.RLock()
	tracker := b.outboundReliable
	b.mu.RUnlock()
	if tracker == nil {
		return
	}
	if cleared := tracker.AbandonUnsecuredExchange(key); cleared > 0 {
		b.logger.Debug("matter.sc.handshake.abandon_replies",
			slog.Int("exchange_id", int(key.ExchangeID)),
			slog.Int("cleared", cleared))
	}
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
// at db021da0 (tooManyPaseErrors → endCommissioning). The window closes
// as the end of commissioning closes it ([CommissioningWindow.EndCommissioning]),
// not as RevokeCommissioning does: matter.js's endCommissioning →
// #closeWindow (DeviceCommissioner.ts:319-324, :283-299) stops the timer,
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
	b.settleHandshake(src, proto.ExchangeID, handler, respOpcode, respPayload, err)
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
			if sendErr := b.sendHandshakeFailureReport(src, requestHdr, proto, body, handshakePeerTimingOf(handler, rxAt)); sendErr != nil {
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
			// And its replies stop waiting for an acknowledgement — all
			// but the StatusReport above, which was sent after them
			// ([Bridge.sendHandshakeFailureReport]); a failure that sent
			// none (no handler wired) drops them here.
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
	// Reliable: PASE continuation messages (PBKDFParamResponse, Pake2)
	// are part of the reliable Secure-Channel exchange. A dropped Pake2
	// aborts commissioning — the commissioner waits for a reply that the
	// bridge, absent MRP tracking, would never rebroadcast. matter.js
	// makes every non-standalone-ack reply reliable (MessageExchange.ts:602);
	// the Sigma3/Pake3 piggyback ack (or a standalone ack) stops the
	// retransmit via the universal inbound Ack path (receive.go).
	if err := b.sendHandshakeReplyReliable(src, requestHdr, proto, respOpcode, respPayload, handshakePeerTimingOf(handler, rxAt)); err != nil {
		debugReplyError(b.logger, "send_"+stage, src, err)
		return err
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
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
	respOpcode, respPayload, err := process(payload)
	b.settleHandshake(src, proto.ExchangeID, handler, respOpcode, respPayload, err)
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
		// replied to) and for the missing-handler case (nothing is wired
		// yet). Mirrors chip CASESession.cpp error-path StatusReport
		// emission on Sigma-reject paths.
		if !isStateReject && !errors.Is(err, ErrCaseHandlerMissing) {
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
			// The handshake is over: a Sigma2 still waiting for its
			// acknowledgement is not retransmitted to a peer whose
			// Sigma3 was refused; the report that tells it so is.
			if sendErr := b.sendHandshakeFailureReport(src, requestHdr, proto, body, handshakePeerTimingOf(handler, rxAt)); sendErr != nil {
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
	// Reliable: Sigma2 is a CASE continuation message on the reliable
	// Secure-Channel exchange. A dropped Sigma2 aborts CASE — the
	// commissioner waits for a reply the bridge would otherwise never
	// rebroadcast. matter.js makes every non-standalone-ack reply
	// reliable (MessageExchange.ts:602); the Sigma3 piggyback ack stops
	// the retransmit via the universal inbound Ack path (receive.go).
	if err := b.sendHandshakeReplyReliable(src, requestHdr, proto, respOpcode, respPayload, handshakePeerTimingOf(handler, rxAt)); err != nil {
		debugReplyError(b.logger, "send_"+stage, src, err)
		return err
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	return nil
}

// sendHandshakeFailureReport ends the handshake on the request's exchange
// with the failure StatusReport body: what the handshake sent and the peer
// has not acknowledged is dropped first, then the report goes out as a
// reliable message, so it is the one entry the exchange keeps and is
// retransmitted until the peer acknowledges it. Both references send the
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
	b.abandonHandshakeReplies(handshakeKeyOf(requestHdr, proto.ExchangeID))
	if err := b.sendHandshakeReplyReliable(src, requestHdr, proto, mrp.SCOpcodeStatusReport, body, peer); err != nil {
		return err
	}
	// The report carried the acknowledgement of the request.
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
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
// unsecured session with the peer active (UnsecuredSession.ts:30
// `setActiveTimestamp: !isInitiator`) and adopts the initiator's
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
func (b *Bridge) markSigma1Replied(key HandshakeKey, hash [32]byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing, ok := b.sigma1Replied[key]; ok && existing == hash {
		return true
	}
	b.sigma1Replied[key] = hash
	return false
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
// [PerExchangeCaseProvider.SetOnEvict]. The Sigma3 success path already
// calls [forgetSigma1Replied]; an ABORTED handshake (Sigma1 received,
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
	b.releaseCASEHandshakeEverywhere(key.ExchangeID)
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
// translation of matter.js ExchangeManager.ts:693 observing
// session.gracefulClose and shipping the report via #sendCloseSession.
type gracefulCloseNotifierSetter interface {
	SetGracefulCloseNotifier(fn func(sessionID uint16, sess *channel.Session))
}

// reannounceTriggerSetter is the optional capability a
// [SessionRegistry] implements to receive the bridge's mDNS
// broadcast-resume trigger, fired with the fabric index after a reap /
// eviction leaves a (fabric, peer) pair with zero live sessions
// (matter.js packages/protocol/src/protocol/DeviceAdvertiser.ts:131-150
// at 862ad75f).
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
	// that elicits no reply.
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
	// (ExchangeManager.ts:696-713).
	defer b.unbindSessionRoute(requestHdr.SessionID)
	// Every exchange the session carried dies with it, so any timed
	// deadline it registered can never be consumed.
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
// matter.js ExchangeManager.ts:716-726 #sendCloseSession at 862ad75f).
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
// try/catch-and-warn (ExchangeManager.ts:716-726 #sendCloseSession at
// 862ad75f).
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
	// overwrite-on-reuse respectively.
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
	if err := sendVia(listener, dst, datagram); err != nil {
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
// packages/protocol/src/protocol/DeviceAdvertiser.ts:131-150 at 862ad75f
// (sessions.deleted → advertisementForFabric(fabric).serviceDisconnected(),
// Advertiser.ts:69-75, which starts the "reconnect" broadcast event,
// MdnsAdvertisement.ts:132-134; a running schedule is not reset,
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
// (ServerNetworkRuntime.ts:450-453 at db021da0).
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
// 862ad75f (sessions.deleted → session.isPase →
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

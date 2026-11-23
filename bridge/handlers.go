// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/secure/sessionparams"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/im"

	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/secure/spake2"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// Errors specific to the production handler adapters.
var (
	// ErrPBKDFParamsMissing is returned by [PaseAdapter.ProcessPBKDFParamRequest]
	// when the adapter has not been configured with the bridge's PBKDF
	// salt + iterations via [PaseAdapter.SetPBKDFParams]. Without that
	// the adapter cannot answer a commissioner that asks for the
	// in-band copy of the PBKDF parameters (HasPBKDFParameters=false).
	ErrPBKDFParamsMissing = errors.New("bridge: PBKDF params not configured")

	// ErrUnsupportedPasscodeID is returned by
	// [PaseAdapter.ProcessPBKDFParamRequest] when the commissioner's
	// PBKDFParamRequest carries a non-zero PasscodeId. The default (and
	// only) commissioning passcode lives at PasscodeId 0
	// (DEFAULT_PASSCODE_ID per Matter §4.14.1.2); a non-zero id addresses
	// a multi-passcode onboarding slot the bridge does not implement.
	// Mirrors matter.js packages/protocol/src/session/pase/PaseServer.ts:144-146
	// (throw UnexpectedDataError when passcodeId !== DEFAULT_PASSCODE_ID).
	ErrUnsupportedPasscodeID = errors.New("bridge: unsupported PASE passcode id")
)

// PaseAdapter wraps a [spake2.Verifier] in the [PaseHandler] port the
// SecureChannel router consumes. The bridge constructs one adapter
// per pending PASE exchange (Verifier state is not safe for
// concurrent use across distinct exchanges, but the adapter itself
// has no mutable state — concurrency safety is the verifier's
// responsibility).
//
// PASE flow:
//
//  1. Commissioner sends PBKDFParamRequest (opcode 0x20). Bridge
//     responds with PBKDFParamResponse (0x21) carrying the salt +
//     iterations the bridge advertises. The response is assembled here:
//     the request is decoded, the PasscodeID validated against the
//     brute-force cap, and the answer built from the configured
//     parameters. Refusals are [ErrPBKDFParamsMissing] when the daemon
//     configured none and [ErrUnsupportedPasscodeID] otherwise.
//  2. Commissioner sends Pake1 (0x22) carrying pA. Bridge runs
//     [spake2.Verifier.ProcessPake1] and replies Pake2 (0x23) with
//     Y + cB.
//  3. Commissioner sends Pake3 (0x24) carrying cA. Bridge runs
//     [spake2.Verifier.ProcessPake3]; on success the optional
//     [PaseAdapter.OnSessionEstablished] callback fires with the
//     freshly-derived shared secret so the operational session
//     manager can register the new session in one atomic step.
type PaseAdapter struct {
	// mu serialises all PASE-state transitions (PBKDF capture →
	// Pake1 verifier allocation → Pake3 verifier consumption).
	// PASE is a strictly serial protocol per exchange, but the
	// SC router's PaseHandlerProvider may dispatch concurrent
	// exchanges to the same adapter instance; without this mutex
	// two commissioners pairing in parallel would race the
	// `verifier` pointer and `pbkdfReqBytes`/`pbkdfRespBytes`
	// slices. Holding the mutex across the verifier crypto is
	// fine — Pake1 takes < 50 ms even on Raspberry-Pi-class CPUs.
	mu              sync.Mutex
	verifierFactory func(context []byte) *spake2.Verifier
	verifier        *spake2.Verifier // set on Pake1, cleared on Pake3 / next Pake1
	// callerVerifier marks the verifier of [NewPaseAdapter]: the caller's,
	// reused for every Pake1, so the adapter does not close it.
	callerVerifier bool
	// verifierClosed, when set, observes each verifier the adapter closes
	// (in-package tests, assigned before the handshake starts).
	verifierClosed func(*spake2.Verifier)
	onEstablished  PaseSessionEstablished

	// PBKDF response config — populated via [PaseAdapter.SetPBKDFParams].
	pbkdfIterations    uint32
	pbkdfSalt          []byte
	responderSessionID uint16
	// established is set once Pake3 verified and the pickup callback
	// registered the session under responderSessionID.
	established bool
	// onUnfinished and releasedStaked: see [PaseAdapter.SetOnUnfinished].
	onUnfinished   func(sessionID uint16)
	releasedStaked bool
	// randomSource feeds ResponderRandom in ProcessPBKDFParamRequest.
	// Nil means crypto/rand. In-package tests assign a deterministic
	// source directly, before the handshake starts (no concurrent
	// reader exists until the first Process* call).
	randomSource func() [spake2.PBKDFRandomSize]byte

	// pbkdfReqBytes + pbkdfRespBytes are the wire-bytes of the most
	// recent PBKDFParamRequest / PBKDFParamResponse exchange. Matter
	// §4.13.4 mandates that the SPAKE2+ context fed into the
	// transcript is SHA-256("CHIP PAKE V1 Commissioning" || request
	// || response); both sides hash these into their context to bind
	// the PASE session to the negotiated parameters. Captured by
	// ProcessPBKDFParamRequest, consumed by ProcessPake1, dropped
	// when a step of the handshake is refused ([PaseAdapter.endHandshakeLocked]).
	pbkdfReqBytes  []byte
	pbkdfRespBytes []byte

	// peerSessionID is the InitiatorSessionID the commissioner
	// supplied in PBKDFParamRequest. Captured here so the
	// post-Pake3 OnSessionEstablished callback can hand it to
	// operational.Manager.OpenFromPase — the bridge needs to stamp
	// this value (NOT its own local session id) into outbound
	// Header.SessionID for any encrypted reply.
	peerSessionID uint16

	// peerSessionParams retains the commissioner's SessionParameters
	// (PBKDFParamRequest tag 5, nil when absent) so the PASE session
	// opener can size retransmissions to the peer's advertised
	// intervals and learn its transports. Mirrors matter.js
	// PaseServer.ts:155-157.
	peerSessionParams *sessionparams.Parameters

	// responderSessionParams is emitted as PBKDFParamResponse tag 5.
	// Nil means [DefaultSessionParameters]: matter.js answers every
	// PBKDFParamRequest with the node's session parameters, the same
	// ones CASE advertises (PaseServer.ts:151 `responderSessionParams =
	// this.sessions.sessionParameters`; CaseServer.ts:272).
	responderSessionParams *sessionparams.Parameters
}

// PaseSessionEstablished fires after a successful Pake3 verification.
// sharedSecret is the raw 16-byte Spake2+ Ke; peerSessionID is the
// commissioner's local session ID (InitiatorSessionID from
// PBKDFParamRequest) — the bridge MUST stamp this into outbound
// Header.SessionID so the commissioner can resolve the session in
// its own table. Implementations typically hand sharedSecret +
// peerSessionID to [operational.Manager.OpenFromPase] which derives
// the I2R/R2I session keys and registers the new [channel.Session].
// A non-nil error from the callback is wrapped and returned from
// ProcessPake3 (the SecureChannel router logs it and the
// commissioner falls back).
//
// The slice is the callback's own: the adapter holds no other reference to
// it and does not read it after the call. An implementation that has
// derived its keys overwrites it, as the node facade does (review II,
// W1-sec-05); the adapter does not do so itself, because an implementation
// may keep the slice.
type PaseSessionEstablished func(sharedSecret []byte, peerSessionID uint16) error

// NewPaseAdapter wraps v as a single-shot adapter — the verifier is
// reused for every Pake1 the adapter sees. Suitable for tests and
// single-PASE-attempt scenarios. The factory ignores the per-Pake1
// context override and always returns the same verifier; this is OK
// for tests that don't exercise the PBKDFParamRequest/Response
// round, but production should use [NewPaseAdapterWithFactory] so
// the verifier's context can be derived from the actual exchanged
// PBKDFParam wire bytes (Matter §4.13.4).
//
// A nil verifier produces an adapter that returns errors from every
// Pake* call — useful for tests of the missing-verifier defensive
// path.
//
// v stays the caller's: the adapter does not close it when a handshake
// ends, as it closes the verifiers a factory makes; the caller closes it
// ([spake2.Verifier.Close]).
func NewPaseAdapter(v *spake2.Verifier) *PaseAdapter {
	return &PaseAdapter{
		verifierFactory: func([]byte) *spake2.Verifier { return v },
		verifier:        v,
		callerVerifier:  true,
	}
}

// NewPaseAdapterWithFactory wraps a verifier factory. The adapter
// invokes the factory at every Pake1 with the **per-exchange
// SPAKE2+ context** — SHA-256("CHIP PAKE V1 Commissioning" ||
// PBKDFParamRequest_bytes || PBKDFParamResponse_bytes) per Matter
// §4.13.4 — and the returned verifier MUST initialise its transcript
// with that 32-byte digest, not the literal context string.
//
// The factory MUST return a fresh verifier each call. Returning a
// shared verifier defeats the purpose; tests that need observable
// state across calls should use [NewPaseAdapter] directly.
//
// Concurrent PASE attempts (multiple commissioners pairing in
// parallel) are NOT supported — the adapter still tracks one
// "current" verifier between Pake1 and Pake3. Per-exchange
// concurrent dispatch requires a per-exchange-id PaseHandler-Provider
// on the bridge router; in practice operators open the commissioning
// window for one commissioner at a time so serial-with-retry covers
// the realistic case.
func NewPaseAdapterWithFactory(factory func(context []byte) *spake2.Verifier) *PaseAdapter {
	return &PaseAdapter{verifierFactory: factory}
}

// SetOnSessionEstablished installs a callback that fires after a
// successful Pake3 verification. Pass nil to clear.
func (a *PaseAdapter) SetOnSessionEstablished(cb PaseSessionEstablished) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onEstablished = cb
}

// SetOnUnfinished installs fn, called once with the responder session id
// the adapter announces when its provider drops the adapter
// ([PerExchangePaseProvider]: the 60 s exchange timeout, the reaper,
// Forget, Reset) without a session established under that id, so the host
// returns the id it staked for the handshake (operational
// Manager.ReleaseID). matter.js reserves no id ahead of the session
// (packages/protocol/src/session/SessionManager.ts:581-598 at db021da0);
// an id staked for the PBKDFParamResponse goes back as soon as the
// handshake can no longer finish. Pass nil to clear.
func (a *PaseAdapter) SetOnUnfinished(fn func(sessionID uint16)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onUnfinished = fn
}

// dropped ends the adapter's handshake when its provider lets the adapter
// go ([PerExchangePaseProvider]: the 60 s exchange timeout, the reaper,
// Forget, Reset): a verifier still held is closed, as at every other end
// of a handshake ([PaseAdapter.endHandshakeLocked]), and the staked
// session id is returned ([PaseAdapter.releaseStakedID]).
func (a *PaseAdapter) dropped() {
	a.mu.Lock()
	a.endHandshakeLocked()
	a.mu.Unlock()
	a.releaseStakedID()
}

// releaseStakedID calls the [PaseAdapter.SetOnUnfinished] hook with the
// announced session id, unless a session was established under it or the
// id was returned already.
func (a *PaseAdapter) releaseStakedID() {
	a.mu.Lock()
	fn, id := a.onUnfinished, a.responderSessionID
	if fn == nil || a.established || a.releasedStaked {
		a.mu.Unlock()
		return
	}
	a.releasedStaked = true
	a.mu.Unlock()
	fn(id)
}

// SetPBKDFParams configures the PBKDF salt + iteration count the
// adapter advertises in the PBKDFParamResponse, plus the responder
// session ID that goes into the response and into the bridge's
// inbound encrypted Header.SessionID once the PASE round completes.
// The values must match what was passed to
// [spake2.NewVerifierContext] — otherwise the commissioner's (w0,w1)
// derivation diverges from the bridge's and Pake1 verification fails.
func (a *PaseAdapter) SetPBKDFParams(iterations uint32, salt []byte, responderSessionID uint16) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pbkdfIterations = iterations
	a.pbkdfSalt = append([]byte(nil), salt...) // defensive copy
	a.responderSessionID = responderSessionID
}

// SetResponderSessionParams configures the SessionParameters the adapter
// advertises as PBKDFParamResponse tag 5. Nil restores
// [DefaultSessionParameters]; an empty structure omits the field.
// Mirrors matter.js PaseServer.ts:151 responderSessionParams.
func (a *PaseAdapter) SetResponderSessionParams(p *sessionparams.Parameters) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.responderSessionParams = p
}

// PeerSessionParams returns the commissioner's SessionParameters from
// the most recent PBKDFParamRequest, or nil when it sent none. The PASE
// session opener stamps them onto the operational entry (matter.js
// PaseServer.ts:155-157).
func (a *PaseAdapter) PeerSessionParams() *sessionparams.Parameters {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.peerSessionParams == nil {
		return nil
	}
	cp := *a.peerSessionParams
	return &cp
}

// ProcessPBKDFParamRequest decodes the commissioner's request, picks
// a fresh ResponderRandom, and assembles the PBKDFParamResponse.
// The response carries the bridge's PBKDF salt + iterations only when
// the request flagged HasPBKDFParameters=false; otherwise the
// commissioner already knows them (e.g. from the QR / manual code).
//
// The wire-bytes of both the inbound request and the outbound
// response are captured on the adapter so the subsequent
// ProcessPake1 can build the SPAKE2+ context per Matter §4.13.4
// (SHA-256("CHIP PAKE V1 Commissioning" || req || resp)).
//
// Returns [ErrPBKDFParamsMissing] when [SetPBKDFParams] hasn't been
// called yet — the SecureChannel router degrades to a debug log so
// the bridge's pre-commissioner boot phase doesn't flood warnings.
func (a *PaseAdapter) ProcessPBKDFParamRequest(payload []byte) (opcode uint8, respPayload []byte, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pbkdfIterations == 0 || len(a.pbkdfSalt) == 0 {
		return 0, nil, ErrPBKDFParamsMissing
	}
	req, err := spake2.DecodePBKDFParamRequest(payload)
	if err != nil {
		a.endHandshakeLocked()
		return 0, nil, fmt.Errorf("bridge: PBKDFParamRequest decode: %w", err)
	}
	// Reject a non-default PasscodeId before assembling the response.
	// Commissioning uses DEFAULT_PASSCODE_ID (0); a non-zero id names a
	// multi-passcode onboarding slot the bridge does not support.
	// Surfacing the error routes handlePase to emit
	// StatusReport(InvalidParam) and count the attempt toward the PASE
	// brute-force cap, exactly as matter.js throws UnexpectedDataError →
	// #pairingErrors++. Mirrors matter.js
	// packages/protocol/src/session/pase/PaseServer.ts:144-146 (passcodeId
	// check) → :94-95 (catch → #pairingErrors++).
	if req.PasscodeID != 0 {
		a.endHandshakeLocked()
		return 0, nil, fmt.Errorf("%w: %d", ErrUnsupportedPasscodeID, req.PasscodeID)
	}
	source := a.randomSource
	if source == nil {
		source = randPBKDFRandom
	}
	respRand := source()
	resp := spake2.PBKDFParamResponse{
		InitiatorRandom:    req.InitiatorRandom,
		ResponderRandom:    respRand[:],
		ResponderSessionID: a.responderSessionID,
		// Advertise the node's SessionParameters (tag 5) — matter.js
		// PaseServer.ts:151/:160 responderSessionParams.
		ResponderSessionParams: a.responderSessionParamsLocked(),
	}
	if !req.HasPBKDFParameters {
		resp.Parameters = &spake2.PBKDFParameters{
			Iterations: a.pbkdfIterations,
			Salt:       a.pbkdfSalt,
		}
	}
	respBytes := resp.Marshal()
	// Capture both the inbound + outbound bytes for the upcoming
	// SPAKE2+ context derivation. Defensive copies — the SC router
	// recycles the inbound payload buffer between datagrams.
	a.pbkdfReqBytes = append(a.pbkdfReqBytes[:0], payload...)
	a.pbkdfRespBytes = append(a.pbkdfRespBytes[:0], respBytes...)
	// Capture the commissioner's local session id so the post-Pake3
	// session pickup can hand it to the operational manager.
	a.peerSessionID = req.InitiatorSessionID
	// Retain the commissioner's SessionParameters (tag 5) for the
	// session opener — matter.js PaseServer.ts:155-157.
	a.peerSessionParams = req.InitiatorSessionParams
	return mrp.SCOpcodePBKDFParamResponse, respBytes, nil
}

// randPBKDFRandom is the production random source for ResponderRandom.
// crypto/rand.Read panics on extreme failure modes (entropy pool
// drained beyond recovery); we propagate via a zero-fill to keep the
// adapter signature error-free, and let the commissioner reject the
// downstream session if the response somehow lands with all-zero
// bytes. In practice crypto/rand.Read on Linux never fails.
func randPBKDFRandom() [spake2.PBKDFRandomSize]byte {
	var out [spake2.PBKDFRandomSize]byte
	_, _ = rand.Read(out[:])
	return out
}

// ProcessPake1 decodes the wire payload, allocates a fresh verifier
// from the factory (so a retry after a failed previous Pake3 starts
// clean), runs it, and encodes the Pake2 reply.
//
// The verifier-context passed to the factory is the SPAKE2+ context
// hash per Matter §4.13.4: SHA-256("CHIP PAKE V1 Commissioning" ||
// PBKDFParamRequest_bytes || PBKDFParamResponse_bytes). Both sides
// derive this hash independently from the wire-bytes that crossed
// the channel during the PBKDF round. Without it the prover and
// verifier compute different transcript hashes and the cB tag
// verification on the prover side fails.
func (a *PaseAdapter) ProcessPake1(payload []byte) (opcode uint8, respPayload []byte, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.verifierFactory == nil {
		return 0, nil, errors.New("bridge: PaseAdapter without verifier factory")
	}
	context, err := a.computePaseContextLocked()
	if err != nil {
		return 0, nil, err
	}
	v := a.verifierFactory(context)
	if v == nil {
		return 0, nil, errors.New("bridge: PaseAdapter factory returned nil verifier")
	}
	if a.verifier != v {
		// A verifier of an earlier Pake1 on this adapter is dropped here.
		a.closeVerifierLocked()
	}
	a.verifier = v
	pA, err := spake2.DecodePake1(payload)
	if err != nil {
		// Clear the freshly-allocated verifier and surface the decode
		// error so handlePase emits the single StatusReport(FAILURE) AND
		// counts the attempt toward the PASE brute-force cap. matter.js
		// counts every PASE pairing failure — a malformed Pake1 included —
		// toward PASE_COMMISSIONING_MAX_ERRORS; swallowing the error into a
		// self-emitted StatusReport (returning a nil Go error) let an
		// attacker hammer Pake1 without ever tripping the cap. Mirrors
		// matter.js packages/protocol/src/session/pase/PaseServer.ts:172
		// (readPasePake1) → :94-95 (catch → #pairingErrors++).
		a.endHandshakeLocked()
		return 0, nil, fmt.Errorf("bridge: Pake1 decode: %w", err)
	}
	out, err := v.ProcessPake1(pA)
	if err != nil {
		// Clear the verifier and surface the crypto error (invalid pA
		// curve point, RNG failure) so handlePase emits the single
		// StatusReport(FAILURE) and counts the attempt toward the
		// brute-force cap. Mirrors matter.js
		// packages/protocol/src/session/pase/PaseServer.ts:174
		// (computeSecretAndVerifiersFromX) → :94-95 (catch → #pairingErrors++).
		a.endHandshakeLocked()
		return 0, nil, fmt.Errorf("bridge: Pake1 verify: %w", err)
	}
	return mrp.SCOpcodePake2, spake2.EncodePake2(out), nil
}

// endHandshakeLocked drops the state of the handshake a refused step — or
// a confirmed Pake3 — ended: the verifier and the PBKDFParamRequest/Response
// transcript. The
// router ends the handshake on every refusal — failure report, count, the
// single-active-PASE claim freed ([Bridge.handlePase]) — and drops a Pake1
// no PBKDFParamRequest began ([Bridge.resolvePaseContinuation]); a Pake1
// that still reaches the adapter — from a host that drives it directly —
// must not run against the ended handshake either, which a transcript kept
// here would let it do (R2-P5 Fable F3). The next handshake starts with
// its own PBKDFParamRequest. The verifier
// is closed, so what it still holds of the handshake — Ke after a success,
// the confirmation keys, Ke and the ephemeral secrets after a refusal — is
// overwritten when the handshake ends ([spake2.Verifier.Close]). Caller
// holds a.mu.
func (a *PaseAdapter) endHandshakeLocked() {
	a.closeVerifierLocked()
	a.pbkdfReqBytes = nil
	a.pbkdfRespBytes = nil
}

// closeVerifierLocked closes and drops the handshake's verifier, unless it
// is the caller's ([NewPaseAdapter]), which is only dropped. Caller holds
// a.mu.
func (a *PaseAdapter) closeVerifierLocked() {
	v := a.verifier
	a.verifier = nil
	if v == nil || a.callerVerifier {
		return
	}
	v.Close()
	if a.verifierClosed != nil {
		a.verifierClosed(v)
	}
}

// computePaseContextLocked derives the SPAKE2+ context per Matter
// §4.13.4. Caller MUST hold a.mu — the function reads pbkdfReqBytes /
// pbkdfRespBytes which the unlocked write path in
// ProcessPBKDFParamRequest mutates. Returns an error when
// ProcessPBKDFParamRequest hasn't run yet — commissioners are
// required to send PBKDFParamRequest before Pake1 per Spec §4.13, so
// missing capture state is a protocol violation the bridge surfaces
// as a wrapped error.
func (a *PaseAdapter) computePaseContextLocked() ([]byte, error) {
	if len(a.pbkdfReqBytes) == 0 || len(a.pbkdfRespBytes) == 0 {
		return nil, errors.New("bridge: Pake1 before PBKDFParamRequest/Response — context cannot be derived")
	}
	h := sha256.New()
	h.Write([]byte(spake2.MatterContext))
	h.Write(a.pbkdfReqBytes)
	h.Write(a.pbkdfRespBytes)
	return h.Sum(nil), nil
}

// establishedSession names the PASE session the adapter's handshake
// established, for the bridge to bind it to the connection the handshake
// ran on.
func (a *PaseAdapter) establishedSession() (uint16, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.responderSessionID, a.established
}

// ProcessPake3 decodes the wire payload and runs the verifier
// allocated during the matching ProcessPake1. On success the
// optional [PaseAdapter.SetOnSessionEstablished] callback fires
// with the freshly-derived Spake2+ shared secret and the adapter
// returns a Secure-Channel StatusReport with
// SESSION_ESTABLISHMENT_SUCCESS so the commissioner can complete
// the handshake (Matter §4.13.4 step 11). The verifier is cleared
// after Pake3 so a stray follow-up Pake3 (e.g. retransmit) cannot
// accidentally succeed against stale state.
//
// On a Pake3 failure — a malformed payload OR a key-confirmation
// mismatch (the on-wire symptom of a wrong passcode) — the adapter
// clears the verifier and RETURNS the underlying error (a decode
// error, or spake2.ErrConfirmationFailed) instead of emitting its own
// StatusReport. The SecureChannel router's handlePase path then sends
// exactly one StatusReport(FAILURE) AND counts the attempt toward the
// cap of failed commissioning attempts. matter.js counts every PASE
// pairing that fails before its session is established — an incorrect key
// confirmation included — as a failed attempt of the window; swallowing the
// error here let an attacker guess passcodes without ever tripping the cap.
// Mirrors matter.js packages/protocol/src/session/pase/PaseServer.ts:180
// at b25ed960 (throw UnexpectedDataError on incorrect key confirmation) →
// :100-106 (catch, cancelPairing) → :116-118 (pairingFailed, counted by
// packages/protocol/src/protocol/DeviceCommissioner.ts:251).
func (a *PaseAdapter) ProcessPake3(payload []byte) (opcode uint8, respPayload []byte, err error) {
	cb, sharedSecret, peerSessionID, err := a.consumePake3(payload)
	if err != nil {
		return 0, nil, err
	}
	// The pickup callback runs OUTSIDE a.mu: the daemon's production
	// callback calls back into adapter getters (PeerSessionParams), and a
	// non-reentrant mutex held across the callback self-deadlocks the
	// PASE receive goroutine — the commissioner then never receives
	// the closing StatusReport and times out after Pake3. Mirrors
	// matter.js packages/protocol/src/session/pase/PaseServer.ts
	// :151-157, where the pickup reads initiator session params after
	// verification with no lock held. Running unlocked is safe: the
	// verifier state was already torn down under the lock, and the
	// SC router's single-active-PASE claim serialises handshakes.
	if cb != nil {
		if err := cb(sharedSecret, peerSessionID); err != nil {
			return 0, nil, fmt.Errorf("bridge: PASE session pickup: %w", err)
		}
		a.mu.Lock()
		a.established = true
		a.mu.Unlock()
	}
	// Per Matter §4.13.4 step 11 the verifier MUST close the PASE
	// handshake with a StatusReport(SUCCESS, SESSION_ESTABLISHMENT_SUCCESS).
	// chip-tool retransmits Pake3 until it sees this; a bare MRP
	// StandaloneAck is not enough.
	body := mrp.EncodeStatusReport(
		mrp.SCStatusGeneralSuccess,
		uint32(mrp.SecureChannelProtocolID),
		mrp.SCStatusProtocolSessionEstablishmentSuccess,
		nil,
	)
	return mrp.SCOpcodeStatusReport, body, nil
}

// consumePake3 decodes + verifies the commissioner's cA under a.mu
// and tears down the per-handshake verifier state either way. It
// returns the pickup callback plus its inputs so [ProcessPake3] can
// fire the callback after the lock is released.
func (a *PaseAdapter) consumePake3(payload []byte) (cb PaseSessionEstablished, sharedSecret []byte, peerSessionID uint16, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := a.verifier
	if v == nil {
		// A Pake3 without a preceding Pake1 is a refused step like any
		// other: the router counts it and ends the handshake, so the
		// transcript goes with it (R2-P5 Fable F5).
		a.endHandshakeLocked()
		return nil, nil, 0, errors.New("bridge: PaseAdapter Pake3 without preceding Pake1")
	}
	cA, err := spake2.DecodePake3(payload)
	if err != nil {
		// Clear the verifier — a retry after any Pake3 rejection MUST
		// start fresh from Pake1 — and surface the decode error so
		// handlePase emits the single StatusReport(FAILURE) and counts
		// the attempt toward the brute-force cap.
		a.endHandshakeLocked()
		return nil, nil, 0, fmt.Errorf("bridge: Pake3 decode: %w", err)
	}
	if err := v.ProcessPake3(cA); err != nil {
		// Clear the verifier so the next Pake1 re-starts cleanly, and
		// surface the confirmation error (spake2.ErrConfirmationFailed
		// for a wrong-passcode cA that does not match the verifier's
		// expected hAY) so handlePase emits the single
		// StatusReport(FAILURE) and increments the brute-force counter.
		a.endHandshakeLocked()
		return nil, nil, 0, err
	}
	sharedSecret = v.SharedSecret()
	peerSessionID = a.peerSessionID
	// A confirmed Pake3 ends the handshake too: the transcript has served
	// its one context, and every exit of Pake3 drops it, so no later Pake1
	// on this adapter runs against it (R2-P5 Fable F6).
	a.endHandshakeLocked()
	return a.onEstablished, sharedSecret, peerSessionID, nil
}

// CaseAdapter wraps a [sigma.Responder] in the [CaseHandler] port.
// The responder owns its own state machine — Sigma1 → Sigma2 →
// Sigma3 advance through the responder's internal phase tracking.
//
// Sigma2_Resume (Matter §4.13.2.4) is supported when the Responder
// has been wired with a ResumptionStore via
// [sigma.Responder.SetResumptionStore]: a Sigma1 carrying valid
// resumptionId + initiatorResumeMic triggers a one-round-trip resume
// instead of Full Sigma.
type CaseAdapter struct {
	// mu guards established, establishedSessionID, lastSigma2 and
	// responder.
	mu            sync.RWMutex
	responder     *sigma.Responder
	onEstablished CaseSessionEstablished
	// established tracks whether onEstablished has fired for the
	// current responder. Apple Home's MRP layer retransmits Sigma3
	// when our Sigma3-Success StatusReport ACK is in-flight; the
	// retransmit lands in `Responder.ProcessSigma3` which returns nil
	// without re-deriving keys (idempotent path). Without this guard
	// `onEstablished` would also fire on
	// every retransmit — registering the SAME operational session
	// multiple times in `OpenFromSigmaWithStake` and confusing Apple's
	// session table. Reset on `SetResponder` so a fresh CASE
	// handshake (post-AddNOC identity swap) gets a clean trigger, and
	// on a Sigma1 that restarts the handshake — but not on the
	// responder's idempotent Sigma1 replay, which restarts nothing.
	established bool
	// establishedSessionID is the responder session id the last
	// onEstablished registered. A resume that announces a different id
	// is a new session whose keys must be installed even though the
	// gate is already closed (the responder takes a fresh id for a
	// resume on a finished handshake, matter.js CaseServer.ts:173).
	establishedSessionID uint16
	// lastSigma2 is the Sigma2 wire reply of the current handshake, so
	// a Sigma1 that yields the same bytes is recognised as the
	// responder's idempotent replay rather than a restart.
	lastSigma2 []byte
	// stepPeer is the initiator's SessionParameters as the Sigma1 that
	// began the current handshake stated them (nil for none), set under mu
	// in the same step that records the handshake, so a Sigma3 takes the
	// ones of the handshake it completes ([CaseAdapter.processSigma3]) —
	// never another Sigma1's that ran on the responder since (R4-sec-4401).
	stepPeer *sessionparams.Parameters
	// resumeInstalled is the session the last Sigma1 installed by
	// resumption, until the router takes it for the Sigma2_Resume's
	// retransmission entry ([CaseAdapter.takeResumeInstall]); zero for
	// none.
	resumeInstalled uint16
	// onUnfinished and releasedStaked: see [CaseAdapter.SetOnUnfinished].
	onUnfinished   func(sessionID uint16)
	releasedStaked bool
	// detach, set by the [PerExchangeCaseProvider] that built the adapter,
	// drops the adapter from the provider once its handshake failed.
	detach func()
	// onBegun, set by the [PerExchangeCaseProvider] that built the
	// adapter, is told that a Sigma1 began a handshake on it
	// ([CaseAdapter.sigma1BeganHandshake]).
	onBegun func()
	// onCompletion, set by the host that installs its sessions
	// establishing (node, through internal/bridgeseam), is told when the
	// handshake of a session onEstablished installed completed or failed
	// ([CaseAdapter.completeEstablishment]); nil installs established, and
	// nothing is told.
	onCompletion func(sessionID uint16, completed bool) bool
	// awaitedResume is the resumed session whose Sigma2_Resume went out on
	// the exchange awaitedResumeKey names and that waits for the
	// initiator's Success ([CaseAdapter.awaitResumeSuccess]); zero for
	// none.
	awaitedResume    uint16
	awaitedResumeKey HandshakeKey
	// awaitedResumeTimer, once armed, ends the wait at its deadline
	// ([CaseAdapter.armAwaitedResume]); stopped when the wait ends.
	awaitedResumeTimer clock.Timer
	// awaitedResumeAcked is set once the deadline was rearmed at the
	// Sigma2_Resume's acknowledgement, which happens once.
	awaitedResumeAcked bool
}

// setOnCompletion installs fn, the host's hook that completes or ends the
// establishment of a session onEstablished installed (internal/bridgeseam
// SetCaseEstablishmentCompletion).
func (a *CaseAdapter) setOnCompletion(fn func(sessionID uint16, completed bool) bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onCompletion = fn
}

// completeEstablishment tells the host that the handshake of the session
// sessionID completed — completed true: a new session's Success went out
// for the first time, or a resumed one's initiator sent its Success — or
// failed before it did — completed false: the host closes the session. It
// reports whether the session is established: false when it was closed
// while establishing, or the handshake failed. Without a host hook the
// session was installed established, and the report is completed.
// Mirrors matter.js packages/protocol/src/session/case/CaseServer.ts:204-210
// and :329-344 at b25ed960 (completeEstablishmentOf, and the force close of
// a session whose handshake failed while it was establishing).
func (a *CaseAdapter) completeEstablishment(sessionID uint16, completed bool) bool {
	a.mu.RLock()
	fn := a.onCompletion
	a.mu.RUnlock()
	if fn == nil {
		return completed
	}
	return fn(sessionID, completed)
}

// awaitResumeSuccess records that the resumed session sessionID waits for
// the initiator's Success on the exchange key names: set before the
// Sigma2_Resume goes out, so a Success that arrives before the send
// returned finds it ([Bridge.handleCase]). A wait it replaces is returned
// (prev, true) for the caller to fail: no session is left waiting without
// its deadline.
func (a *CaseAdapter) awaitResumeSuccess(key HandshakeKey, sessionID uint16) (prev uint16, replaced bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopAwaitedResumeTimerLocked()
	prev, replaced = a.awaitedResume, a.awaitedResume != 0 && a.awaitedResume != sessionID
	a.awaitedResume, a.awaitedResumeKey, a.awaitedResumeAcked = sessionID, key, false
	return prev, replaced
}

// armAwaitedResume (re)starts the deadline of the wait for the initiator's
// Success on the exchange key names: expire runs with the waiting session
// once d passed on clk, unless the wait ended before. A deadline armed
// before is replaced, at the acknowledgement (atAck) once only; without a
// wait on key nothing is armed.
func (a *CaseAdapter) armAwaitedResume(key HandshakeKey, clk clock.Clock, d time.Duration, atAck bool, expire func(sessionID uint16)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.awaitedResume == 0 || a.awaitedResumeKey != key || (atAck && a.awaitedResumeAcked) {
		return
	}
	a.awaitedResumeAcked = a.awaitedResumeAcked || atAck
	a.stopAwaitedResumeTimerLocked()
	a.awaitedResumeTimer = clk.AfterFunc(d, func() {
		if id, ok := a.takeAwaitedResume(key); ok {
			expire(id)
		}
	})
}

// stopAwaitedResumeTimerLocked stops the wait's deadline. Caller holds a.mu.
func (a *CaseAdapter) stopAwaitedResumeTimerLocked() {
	if a.awaitedResumeTimer != nil {
		a.awaitedResumeTimer.Stop()
		a.awaitedResumeTimer = nil
	}
}

// completesEstablishment reports whether a host hook completes or ends the
// establishment of the adapter's sessions ([CaseAdapter.setOnCompletion]).
func (a *CaseAdapter) completesEstablishment() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.onCompletion != nil
}

// takeAwaitedResume returns, once, the resumed session that waits for the
// initiator's Success on the exchange key names.
func (a *CaseAdapter) takeAwaitedResume(key HandshakeKey) (uint16, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := a.awaitedResume
	if id == 0 || a.awaitedResumeKey != key {
		return 0, false
	}
	a.stopAwaitedResumeTimerLocked()
	a.awaitedResume, a.awaitedResumeKey = 0, HandshakeKey{}
	return id, true
}

// SetOnUnfinished installs fn, called once with the session id the
// adapter's responder announces when the adapter's handshake ends without
// a session established under that id — a failed Sigma1 or Sigma3 on an
// adapter a [PerExchangeCaseProvider] manages, or the provider dropping
// the adapter (the reaper, Forget, Reset) — so the host returns the id it
// staked for the handshake (operational Manager.ReleaseID). matter.js
// reserves no id ahead of the session (packages/protocol/src/session/SessionManager.ts:581-598
// at db021da0). A failed adapter leaves its provider at once, so the id
// it returned is never announced again. Pass nil to clear.
func (a *CaseAdapter) SetOnUnfinished(fn func(sessionID uint16)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onUnfinished = fn
}

// setDetach is the provider's hook that drops the adapter
// ([PerExchangeCaseProvider.Resolve]).
func (a *CaseAdapter) setDetach(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.detach = fn
}

// setOnBegun is the provider's hook told of a Sigma1 that began a
// handshake on the adapter ([PerExchangeCaseProvider.Resolve]).
func (a *CaseAdapter) setOnBegun(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onBegun = fn
}

// sigma1BeganHandshake follows a Sigma1 the responder accepted — answered
// with a Sigma2 or a Sigma2_Resume: the provider that built the adapter
// gives it its place now (review II, R2-P6-sec-0301).
func (a *CaseAdapter) sigma1BeganHandshake() {
	a.mu.RLock()
	fn := a.onBegun
	a.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// releaseStakedID calls the [CaseAdapter.SetOnUnfinished] hook with the
// session id the responder announces, unless a session was established
// under it or the id was returned already.
func (a *CaseAdapter) releaseStakedID() {
	r := a.snapshotResponder()
	if r == nil {
		return
	}
	id := r.SessionID()
	a.mu.Lock()
	fn := a.onUnfinished
	if fn == nil || a.releasedStaked || (a.established && a.establishedSessionID == id) {
		a.mu.Unlock()
		return
	}
	a.releasedStaked = true
	a.mu.Unlock()
	fn(id)
}

// handshakeFailed ends a failed handshake on an adapter its provider
// manages: the adapter leaves the provider (a later message of the same
// initiator and exchange meets a fresh one) and returns its staked id. An
// adapter without a provider's detach hook keeps its id: whoever holds it
// may run the next handshake on it, under that id.
func (a *CaseAdapter) handshakeFailed() {
	a.mu.RLock()
	detach := a.detach
	a.mu.RUnlock()
	if detach == nil {
		return
	}
	a.releaseStakedID()
	detach()
}

// CaseSessionEstablished fires after a successful Sigma3 verification.
// keys are the derived I2R/R2I/AttestationChallenge bundle from
// [sigma.Responder.SessionKeys]; peerSessionID is the
// InitiatorSessionID the commissioner sent in Sigma1 — the bridge
// stamps it into outbound Header.SessionID so the peer resolves the
// session in its own table. Implementations typically hand both to
// [operational.Manager.OpenFromSigmaWithStake].
type CaseSessionEstablished func(keys sigma.SessionKeys, peerSessionID uint16) error

// NewCaseAdapter wraps r.
//
// A responder without session parameters of its own is given
// [DefaultSessionParameters]: matter.js always answers Sigma1 with its
// SessionParameters (CaseServer.ts:258-264 `responderSessionParams:
// this.#sessions.sessionParameters`), and a controller that finds none
// assumes MaxPathsPerInvoke 1 and the oldest revisions — the CHIP Python
// harness then batches two commands into a device it believes accepts one
// and fails TC-IDM-1.4 when the device, rightly by its own
// BasicInformation, accepts them.
func NewCaseAdapter(r *sigma.Responder) *CaseAdapter {
	if r != nil && r.SessionParameters() == nil {
		p := DefaultSessionParameters()
		r.SetSessionParameters(&p)
	}
	return &CaseAdapter{responder: r}
}

// responderSessionParamsLocked is the structure PBKDFParamResponse tag 5
// carries, with the adapter's mutex held: the host's choice, else
// [DefaultSessionParameters] — matter.js sends the node's session
// parameters on every PBKDFParamResponse (PaseServer.ts:151).
func (a *PaseAdapter) responderSessionParamsLocked() *sessionparams.Parameters {
	if a.responderSessionParams != nil {
		return a.responderSessionParams
	}
	p := DefaultSessionParameters()
	return &p
}

// SessionParametersFor is [DefaultSessionParameters] with the node's
// transports stated: the SupportedTransports bitmap
// ([sessionparams.TransportTCPClient], [sessionparams.TransportTCPServer])
// and, when it names TCP, MaxTCPMessageSize — matter.js's fallback of
// 64000 bytes unless the host passes its own bound (SessionParameters.ts:58-63,
// :152). A node without TCP states the bitmap as 0 and no size, as matter.js
// does ([sessionparams.Parameters.Encode]; review II, R2-P6-par-2905).
func SessionParametersFor(transports uint16, maxTCPMessageSize uint32) sigma.SessionParameters {
	p := DefaultSessionParameters()
	p.SupportedTransports = transports
	if p.SupportsTCP() {
		p.MaxTCPMessageSize = maxTCPMessageSize
		if p.MaxTCPMessageSize == 0 {
			p.MaxTCPMessageSize = sessionparams.DefaultMaxTCPMessageSize
		}
	}
	return p
}

// DefaultSessionParameters is what a node advertises in Sigma2 when the
// host chose nothing: matter.js SessionParameters.defaults
// (packages/protocol/src/session/SessionParameters.ts) — the MRP intervals
// of SessionIntervals.defaults (idle 500 ms, active 300 ms, active
// threshold 4000 ms), this module's DataModelRevision, Interaction Model
// revision and SpecificationVersion, and MaxPathsPerInvoke equal to what
// BasicInformation advertises and the invoke path enforces
// ([im.DefaultMaxPathsPerInvoke]).
func DefaultSessionParameters() sigma.SessionParameters {
	return sigma.SessionParameters{
		SessionIdleInterval:      500,
		SessionActiveInterval:    300,
		SessionActiveThreshold:   4000,
		DataModelRevision:        defaultDataModelRevision,
		InteractionModelRevision: uint16(im.MatterInteractionModelRevision),
		SpecificationVersion:     cluster.SpecificationVersion,
		MaxPathsPerInvoke:        im.DefaultMaxPathsPerInvoke,
	}
}

// defaultDataModelRevision is the BasicInformation DataModelRevision the
// root advertises by default (cluster/core NewBasicInformation: 21, matter.js
// Specification.ts DATA_MODEL_REVISION for 1.6.x).
const defaultDataModelRevision uint16 = 21

// SetResponder swaps the underlying sigma responder. Called after
// AddNOC installs a real fabric so subsequent CASE handshakes use the
// persisted NOC + ICAC instead of the empty ephemeral identity the
// daemon boots with.
func (a *CaseAdapter) SetResponder(r *sigma.Responder) {
	a.mu.Lock()
	a.responder = r
	a.established = false
	a.establishedSessionID = 0
	a.lastSigma2 = nil
	a.stepPeer = nil
	a.mu.Unlock()
}

func (a *CaseAdapter) snapshotResponder() *sigma.Responder {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.responder
}

// SnapshotResponder is the public read-only accessor for the wrapped
// responder. Callers use it from inside the OnSessionEstablished
// callback to lift state (peer node id, peer session id, …) the
// callback signature does not expose directly.
func (a *CaseAdapter) SnapshotResponder() *sigma.Responder { return a.snapshotResponder() }

// PeerSessionParams returns the initiator's SessionParameters from the
// Sigma1 the adapter's responder processed, or nil when it stated none or
// no Sigma1 was processed yet. The bridge paces the Sigma2 /
// Sigma2_Resume retransmissions by them (matter.js CaseServer.ts:120-123
// `session.timingParameters = sigma1.initiatorSessionParams`).
func (a *CaseAdapter) PeerSessionParams() *sessionparams.Parameters {
	r := a.snapshotResponder()
	if r == nil {
		return nil
	}
	p, ok := r.PeerSessionParameters()
	if !ok {
		return nil
	}
	return &p
}

// establishedSession names the secure session the adapter's handshake
// established (Sigma3 or a resume), for the bridge to bind it to the
// connection the handshake ran on.
func (a *CaseAdapter) establishedSession() (uint16, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.establishedSessionID, a.established
}

// SetOnSessionEstablished installs a callback that fires after a
// successful Sigma3 verification. Pass nil to clear. It may be called
// while a handshake runs; the handshake reads the callback once, under the
// adapter's lock (review II, R2-C2-conc-0402).
func (a *CaseAdapter) SetOnSessionEstablished(cb CaseSessionEstablished) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onEstablished = cb
}

// ProcessSigma1 hands the wire payload to [sigma.Responder.ProcessSigma1WithResume].
// When the Sigma1 carries a valid resumptionId + initiatorResumeMic pair AND the
// responder has a ResumptionStore wired, the fast Sigma2_Resume path is taken:
// the reply uses opcode SCOpcodeSigma2Resume and the session is established
// immediately — no Sigma3 is expected. Otherwise Full Sigma runs as before.
//
// Mirrors matter.js packages/protocol/src/session/case/CaseServer.ts::#handleSigma1.
func (a *CaseAdapter) ProcessSigma1(payload []byte) (opcode uint8, respPayload []byte, err error) {
	opcode, respPayload, _, err = a.processSigma1(payload)
	return opcode, respPayload, err
}

// processSigma1 is [CaseAdapter.ProcessSigma1] with what this one call
// took: the initiator's SessionParameters as this Sigma1 states them, and,
// when it established a session by resumption, that session's id as this
// call set it under a.mu. The bridge binds the resumed session's route and
// paces the reply by them: read back from the adapter afterwards either
// could belong to another Sigma1 or a Sigma3 that ran on the adapter since
// (R4-sec-4401, the shape of R3-sec-3404).
func (a *CaseAdapter) processSigma1(payload []byte) (opcode uint8, respPayload []byte, taken caseStepTaken, err error) {
	taken.peer = statedSigma1Params(payload)
	r := a.snapshotResponder()
	if r == nil {
		return 0, nil, taken, errors.New("bridge: CaseAdapter without responder")
	}
	result, err := r.ProcessSigma1WithResume(payload)
	if err != nil {
		// chip CASESession.cpp sends a SecureChannel StatusReport on every
		// Sigma failure so the commissioner learns the round was actively
		// rejected and stops MRP-retransmitting. Without this report Apple
		// retries for ~30 s before logging a timeout. A Sigma1 addressing
		// a fabric this node does not hold earns NoSharedTrustRoots;
		// everything else is InvalidParameter — matter.js CaseServer.ts:
		// 88-95 (FabricNotFoundError → NoSharedTrustRoots, else
		// InvalidParam).
		body := mrp.EncodeStatusReport(
			mrp.SCStatusGeneralFailure,
			uint32(mrp.SecureChannelProtocolID),
			caseFailureProtocolCode(err),
			nil,
		)
		// A refused Sigma1 changes nothing of the responder
		// ([sigma.Responder.ProcessSigma1WithResume] commits only a
		// Sigma1 it accepts), so it ends no handshake of this adapter:
		// an adapter whose handshake is in progress or established its
		// session stays with its provider, and the handshake's record
		// with it (review II, R2-P4-rr-sec-04, R2-P4-fr-sec-03). An
		// adapter without a handshake was made for this Sigma1 and goes.
		a.mu.RLock()
		begun := a.established || a.lastSigma2 != nil
		a.mu.RUnlock()
		if !begun {
			a.handshakeFailed()
		}
		return mrp.SCOpcodeStatusReport, body, taken, nil //nolint:nilerr // Sigma1 failure is converted to a StatusReport wire frame; the caller receives nil err by design
	}

	if result.IsResume() {
		// Sigma2_Resume fast path: the session is installed, the host told
		// and the resumption record replaced before the reply is sent; the
		// record is consumed at its acceptance, where the responder claims
		// its id. A host that installs establishing (node) installs the
		// session establishing, as matter.js creates it before its
		// Sigma2_Resume (packages/protocol/src/session/case/CaseServer.ts:177-190
		// at b25ed960); it is established once the initiator's Success
		// arrives (:204-206, [Bridge.settleAwaitedResume]) and closed when
		// the initiator reports a failure instead (:207-210). matter.js
		// saves the record after that Success (:216-217); the host saves it
		// at the establishment too (node caseEstablishment.complete). A
		// session whose Sigma2_Resume is not acknowledged is closed — by
		// entry for a host that installs establishing
		// ([Bridge.endAwaitedResume]), by id otherwise
		// ([Bridge.closeUnacknowledgedResume]; review II, R2-P6-sec-3320).
		out := sigma.MarshalSigma2Resume(*result.Sigma2Resume)

		// Every resume that yields a session id the adapter has not
		// registered installs its keys: the first one on a fresh adapter,
		// and any later one for which the responder took a fresh id (a
		// resume on a finished handshake). A resume re-announcing the id
		// already established is the MRP replay case and stays quiet. The
		// id is the one this call's Sigma2_Resume announces — the responder
		// set it in the step that built the reply — not read back from the
		// responder, which another Sigma1 may have renewed since.
		sessionID := result.Sigma2Resume.ResponderSessionID
		a.mu.Lock()
		install := !a.established || sessionID != a.establishedSessionID
		prevEstablished, prevSessionID, prevPeer := a.established, a.establishedSessionID, a.stepPeer
		a.established = true
		a.establishedSessionID = sessionID
		a.stepPeer = taken.peer
		onEstablished := a.onEstablished
		a.mu.Unlock()
		if !install || onEstablished == nil {
			// No session is installed for this resume, so nobody replaces
			// the record it presented: the claim goes back (review II,
			// R2-P6-sec-3320, decision 243).
			result.Claim.Release()
		}
		if install && onEstablished != nil {
			// The callback's second argument is the PEER's session id
			// (Sigma1.initiatorSessionID) — the id the initiator expects
			// stamped into Header.SessionID of every outbound packet.
			// matter.js CaseServer.ts:179 `peerSessionId: cx.peerSessionId`.
			// Passing our own ResponderSessionID here registered the
			// resumed session under a peer id the initiator never
			// allocated, so it dropped every reply.
			if err := onEstablished(result.ResumeKeys, r.PeerSessionID()); err != nil {
				// No session was installed — the host's stake was lost
				// (operational.ErrStakeLost) or its table refused it: the
				// resume began nothing. The adapter's state goes back to
				// what it was, and an adapter whose handshake had not
				// begun ends as a failed Sigma1 ends it — it leaves its
				// provider and returns its staked id — rather than wait
				// for the reaper (review II, R2-P6-sec-0301).
				a.mu.Lock()
				a.established, a.establishedSessionID, a.stepPeer = prevEstablished, prevSessionID, prevPeer
				begun := a.established || a.lastSigma2 != nil
				a.mu.Unlock()
				if !begun {
					a.handshakeFailed()
				}
				return 0, nil, taken, fmt.Errorf("bridge: CASE resume session pickup: %w", err)
			}
			a.mu.Lock()
			a.resumeInstalled = sessionID
			a.mu.Unlock()
		}
		a.sigma1BeganHandshake()
		taken.sessionID, taken.named = sessionID, true
		return mrp.SCOpcodeSigma2Resume, out, taken, nil
	}

	// Full Sigma path — reset established so the Sigma3 completion
	// fires onEstablished correctly even if a prior handshake or resume
	// on this adapter had already set it to true. Only a Sigma1 that
	// restarted the handshake counts: while the handshake is in progress
	// the responder answers an MRP retransmit of the same Sigma1 with its
	// cached Sigma2 (secure/sigma/protocol.go processSigma1Locked; the
	// cache is this module's own), and reopening the gate there would let
	// the Sigma3 retransmit that follows re-register the session and
	// displace the live one. Once the handshake finished, the same Sigma1
	// gets a fresh Sigma2 and reopens the gate; the finished handshake's
	// Sigma3 then fails against it (review II, R2-P4-fr-sec-01).
	out := result.Sigma2.Marshal()
	a.mu.Lock()
	if !bytes.Equal(out, a.lastSigma2) {
		a.established = false
		a.lastSigma2 = out
		a.stepPeer = taken.peer
	}
	a.mu.Unlock()
	a.sigma1BeganHandshake()
	return mrp.SCOpcodeSigma2, out, taken, nil
}

// statedSigma1Params returns the initiator's SessionParameters a Sigma1
// states, nil when it states none or does not decode.
func statedSigma1Params(payload []byte) *sessionparams.Parameters {
	s1, err := sigma.UnmarshalSigma1(payload)
	if err != nil || s1.InitiatorSessionParams == nil {
		return nil
	}
	p := *s1.InitiatorSessionParams
	return &p
}

// pendingOnFabric reports whether the adapter's handshake resolved
// fabricIndex and has not established its session
// ([Bridge.abortFabricHandshakes]).
func (a *CaseAdapter) pendingOnFabric(fabricIndex uint8) bool {
	r := a.snapshotResponder()
	if r == nil {
		return false
	}
	fi, _, ok := r.SessionIdentity()
	if !ok || fi != fabricIndex {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return !a.established
}

// takeResumeInstall returns, once, the session the last Sigma1 installed by
// resumption: the router ties it to the Sigma2_Resume's retransmission
// entry ([Bridge.handleCase]).
func (a *CaseAdapter) takeResumeInstall() (uint16, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := a.resumeInstalled
	a.resumeInstalled = 0
	return id, id != 0
}

// caseFailureProtocolCode maps a CASE handshake failure to the
// Secure-Channel protocol code the StatusReport carries: a Sigma1 that
// addresses no fabric this node holds is NoSharedTrustRoots, any other
// failure InvalidParameter. Mirrors matter.js CaseServer.ts:88-95.
func caseFailureProtocolCode(err error) uint16 {
	if errors.Is(err, sigma.ErrNoSharedTrustRoots) {
		return mrp.SCStatusProtocolNoSharedTrustRoots
	}
	return mrp.SCStatusProtocolInvalidParameter
}

// ProcessSigma3 hands the wire payload to [sigma.Responder.ProcessSigma3].
// On success the optional [CaseAdapter.SetOnSessionEstablished]
// callback fires with the derived [sigma.SessionKeys] and the
// adapter returns a Secure-Channel StatusReport(Success/Success)
// for the caller to ship — Apple Home (and matter.js by extension)
// only starts operational reads on the freshly-established CASE
// session AFTER receiving this success report. Without it Apple
// retransmits Sigma1 every ~30s and the pairing UI hangs at
// "verbinden …".
//
// Once the handshake established its session, a Sigma3 — the bytes that
// established it included — is not answered: it is acknowledged, as the
// router acknowledges a Sigma3 of a completed handshake, and it neither
// reaches the responder nor changes the session (review II,
// R2-P3-rr-dev-01, R2-P4-nv-03, R2-P4-nw-01; phase 7, decisions 284, 286;
// [Bridge.dispatchSigma3]).
func (a *CaseAdapter) ProcessSigma3(payload []byte) (opcode uint8, respPayload []byte, err error) {
	opcode, respPayload, _, _, err = a.processSigma3(payload)
	return opcode, respPayload, err
}

// sigma3Outcome is what one Sigma3 did to the handshake it names.
type sigma3Outcome uint8

const (
	// sigma3Failed: the Sigma3 ended the handshake without a session —
	// refused, or failed with an error.
	sigma3Failed sigma3Outcome = iota
	// sigma3Established: this Sigma3 established the session.
	sigma3Established
	// sigma3OfCompleted: the handshake had established its session
	// before; the Sigma3 changed nothing and is not answered.
	sigma3OfCompleted
)

// processSigma3 is [CaseAdapter.ProcessSigma3] with the outcome of this
// one call, the initiator's SessionParameters of the handshake it ran in
// and, when it established the session, the session's id as this call set
// it under a.mu — the bridge binds the session's route, records and
// completes its establishment and paces the reply by them: read back from
// shared state either could belong to another, concurrent Sigma3 or a
// Sigma1 that ran on the adapter since (R3-sec-3404, R4-sec-4401,
// R4-sec-4402).
func (a *CaseAdapter) processSigma3(payload []byte) (opcode uint8, respPayload []byte, outcome sigma3Outcome, taken caseStepTaken, err error) {
	// Held across the responder's step so the check of an established
	// handshake and the establishment itself are one step for concurrent
	// Sigma3 datagrams; released before the callbacks.
	a.mu.Lock()
	taken.peer = a.stepPeer
	r := a.responder
	if r == nil {
		a.mu.Unlock()
		return 0, nil, sigma3Failed, taken, errors.New("bridge: CaseAdapter without responder")
	}
	if a.established {
		// Every Sigma3 after establishment is acknowledged and not
		// answered, the bytes that established the session included
		// (review II, phase 7, decision 286): matter.js's CASE server reads
		// one Sigma3 per handshake
		// (packages/protocol/src/session/case/CaseServer.ts:275-279 at
		// db021da0), and a repeat on its exchange is a duplicate that does
		// not reach it (packages/protocol/src/protocol/MessageExchange.ts:501-511).
		a.mu.Unlock()
		return 0, nil, sigma3OfCompleted, taken, nil
	}
	if err := r.ProcessSigma3(payload); err != nil {
		a.mu.Unlock()
		// Emit StatusReport(FAILURE, InvalidParameter) so the commissioner
		// terminates the CASE exchange immediately instead of retransmitting
		// Sigma3. Mirrors chip CASESession.cpp error-exit pattern.
		a.handshakeFailed()
		return mrp.SCOpcodeStatusReport, caseSigma3Refusal(), sigma3Failed, taken, nil //nolint:nilerr // Sigma3 failure is converted to a StatusReport wire frame; the caller receives nil err by design
	}
	// onEstablished must fire EXACTLY ONCE per CASE handshake. Apple's
	// MRP-layer retransmits Sigma3 when our StatusReport ACK is
	// in-flight; such a retransmission meets the established branch
	// above and does not re-register the operational session — that
	// double-registers the same session-id in opMgr and confuses Apple's
	// session table.
	a.established = true
	sessionID := r.SessionID()
	a.establishedSessionID = sessionID
	onEstablished := a.onEstablished
	a.mu.Unlock()
	// A host that installs establishing (node) installs the session
	// establishing here, before the Success is sent; it is established at
	// the Success's first transmission ([Bridge.handleCase],
	// [CaseAdapter.completeEstablishment]). Mirrors matter.js
	// packages/protocol/src/session/case/CaseServer.ts:314-331 at b25ed960.
	if onEstablished != nil {
		keys, ok := r.SessionKeys()
		if !ok {
			a.sigma3PickupFailed()
			return 0, nil, sigma3Failed, taken, errors.New("bridge: CASE keys missing after Sigma3 success")
		}
		if err := onEstablished(keys, r.PeerSessionID()); err != nil {
			a.sigma3PickupFailed()
			return 0, nil, sigma3Failed, taken, fmt.Errorf("bridge: CASE session pickup: %w", err)
		}
	}
	taken.sessionID, taken.named = sessionID, true
	return mrp.SCOpcodeStatusReport, caseEstablishedReport(), sigma3Established, taken, nil
}

// sigma3PickupFailed ends a handshake whose Sigma3 the responder verified
// but whose session was not installed (no keys, or the host refused it):
// the adapter no longer reads established — a later Sigma3 is not taken
// for one of a completed handshake — and, as a failed Sigma3 ends it, it
// leaves its provider and returns its staked id. The resume arm of
// [CaseAdapter.ProcessSigma1] rolls back the same way (review II,
// R2-C2-fsm-0401; R2-P6-sec-0301).
func (a *CaseAdapter) sigma3PickupFailed() {
	a.mu.Lock()
	a.established, a.establishedSessionID = false, 0
	a.mu.Unlock()
	a.handshakeFailed()
}

// caseEstablishedReport is the StatusReport(Success, SessionEstablishmentSuccess)
// that answers the Sigma3 establishing a CASE session. Per Matter Core
// §4.13.2.3 + matter.js's CaseServer.#generateSigma2: after Sigma3
// verifies, the responder ships it so the commissioner knows the CASE
// session is live and operational reads may begin. Without this reply
// Apple Home keeps retransmitting Sigma1 and never sends a single packet
// on the freshly-allocated session.
func caseEstablishedReport() []byte {
	return mrp.EncodeStatusReport(
		mrp.SCStatusGeneralSuccess,
		uint32(mrp.SecureChannelProtocolID),
		mrp.SCStatusProtocolSessionEstablishmentSuccess,
		nil,
	)
}

// caseSigma3Refusal is the StatusReport(FAILURE, InvalidParameter) that
// refuses a Sigma3.
func caseSigma3Refusal() []byte {
	return mrp.EncodeStatusReport(
		mrp.SCStatusGeneralFailure,
		uint32(mrp.SecureChannelProtocolID),
		mrp.SCStatusProtocolInvalidParameter,
		nil,
	)
}

// ProcessSigma2Resume handles an inbound Sigma2_Resume (opcode 0x33)
// which would arrive only if the bridge were acting as CASE initiator
// resuming a prior session. The bridge is always a CASE responder, so
// in practice this path is never reached — the bridge SENDS opcode 0x33
// (via ProcessSigma1 on the resume fast path) and does NOT receive it.
// Implemented as a stub that rejects with an error; the commissioner
// (if it ever sends 0x33 in error) falls back to a fresh Sigma1.
func (a *CaseAdapter) ProcessSigma2Resume(_ []byte) (opcode uint8, respPayload []byte, err error) {
	return 0, nil, errors.New("bridge: inbound Sigma2_Resume not supported (bridge is CASE responder)")
}

// MRPAckAdapter wraps an [mrp.AckTracker] in the [AckHandler] port.
// Trivial passthrough — the MRP layer owns the obligation
// bookkeeping; the adapter exists so the bridge can stay decoupled
// from the concrete tracker type (tests substitute a fake).
//
// Deprecated: an AckTracker holds the acknowledgements this node owes,
// which a peer's acknowledgement does not fulfil, so it is not an
// [AckHandler]; [Bridge.AttachAckTracker] no longer wires one. There is
// no replacement. Removable in v0.25.0.
type MRPAckAdapter struct {
	tracker *mrp.AckTracker
}

// NewMRPAckAdapter wraps t.
//
// Deprecated: see [MRPAckAdapter]. Removable in v0.25.0.
func NewMRPAckAdapter(t *mrp.AckTracker) *MRPAckAdapter {
	return &MRPAckAdapter{tracker: t}
}

// Discharge forwards to [mrp.AckTracker.Discharge]. initiator is the
// LOCAL side's role on the exchange.
func (a *MRPAckAdapter) Discharge(sessionID, exchangeID uint16, initiator bool) bool {
	if a.tracker == nil {
		return false
	}
	return a.tracker.Discharge(sessionID, exchangeID, initiator)
}

// OperationalSessionLookup wraps a session-lookup function from the
// `secure/operational` layer in the [SessionLookup] port the
// receive pipeline depends on. Decouples bridge from the concrete
// `*operational.Manager` type so tests can substitute a fake.
//
// The function shape mirrors [operational.Manager.Get] — passing
// `manager.Get` directly would fail because Get returns
// `(*Entry, error)`. The adapter unwraps to `(*channel.Session, bool)`.
//
// Implements the optional [SessionFabricResolver] when wired with a
// non-nil `fabricFor` closure — Subscribe, ACL checks and any other
// fabric-scoped logic can then resolve `(sessionID → fabricIndex)`.
type OperationalSessionLookup struct {
	get           func(sessionID uint16) (*channel.Session, bool)
	fabricFor     func(sessionID uint16) (uint8, bool)
	subjectFor    func(sessionID uint16) (uint64, []uint32, bool)
	paseFor       func(sessionID uint16) (bool, bool)
	intervalFor   func(sessionID uint16, now time.Time) (time.Duration, bool)
	transportsFor func(sessionID uint16) (uint16, uint32, bool)
	scheduleFor   func(sessionID uint16) (idle, active, threshold time.Duration, peerActive, ok bool)
	markRx        func(sessionID uint16)
	markTx        func(sessionID uint16)
}

// SessionFabricResolver is an optional capability a [SessionLookup]
// can implement so the bridge can resolve the operational FabricIndex
// of an active session. Used by Subscribe (for fabric-scoped quotas),
// the ACL access check, and the AdministratorCommissioning admin
// fabric attribute. PASE / pre-fabric sessions yield (0, true).
//
// Returning (_, false) signals "session not known" — callers that
// need a fabric usually fall back to 0 (pre-fabric semantics).
type SessionFabricResolver interface {
	FabricFor(sessionID uint16) (uint8, bool)
}

// SessionRetransmitIntervalResolver is an optional capability a
// [SessionLookup] can implement so the outbound-reliable tracker can
// size its retransmission backoff to the peer's advertised MRP
// session parameters (active/idle interval selected by peer
// activity — matter.js MRP.ts:181 in retransmissionIntervalOf,
// :162-190 at db021da0). Sessions
// the resolver does not know (session 0 / PASE) return (_, false)
// and the tracker falls back to the spec idle default.
type SessionRetransmitIntervalResolver interface {
	RetransmitBaseInterval(sessionID uint16, now time.Time) (time.Duration, bool)
}

// SessionActivityMarker is an optional capability a [SessionLookup] can
// implement so the wire paths can refresh a session's activity
// timestamps: MarkActiveRx after every successfully decrypted and
// authenticated inbound message, MarkActiveTx after every outbound
// secure send. The operational manager's idle reaper reads the
// timestamp to evict dead sessions; the Rx half additionally feeds the
// MRP peer-active determination. Mirrors matter.js
// packages/protocol/src/session/Session.ts:127-133
// notifyActivity(messageReceived) — `timestamp` refreshes in both
// directions, `activeTimestamp` (peer activity) only on receive —
// invoked from MessageExchange.ts:429 (receive, duplicates included)
// and :562/:814 (sends).
type SessionActivityMarker interface {
	MarkActiveRx(sessionID uint16)
	MarkActiveTx(sessionID uint16)
}

// SessionSubjectResolver is an optional capability a [SessionLookup]
// can implement so the bridge can resolve the requesting peer subject
// (operational NodeID + CASE Authenticated Tags) of an active CASE
// session. Used by the IM dispatcher's ACL gate to evaluate
// per-subject ACEs (Matter §9.10.5.6). Returning (_, _, false) signals
// "session not known" — the ACL gate then matches only ACEs whose
// Subjects list is empty (the fabric-wide wildcard).
type SessionSubjectResolver interface {
	SubjectFor(sessionID uint16) (nodeID uint64, cats []uint32, ok bool)
}

// SessionPASEResolver is an optional capability a [SessionLookup] can
// implement so the bridge can tell whether a session was established
// over PASE. It cannot be inferred from the FabricIndex: AddNOC adopts
// the commissioner's PASE session onto the freshly built fabric
// (matter.js packages/node/src/behaviors/operational-credentials/
// OperationalCredentialsServer.ts:279-283 `session.fabric = fabric`),
// after which the session reports a non-zero fabric while still being
// PASE and still carrying the implicit Administer grant
// (FabricAccessControl.ts:189-191). Returning (_, false) signals
// "session not known" — the request is then evaluated by the ordinary
// ACL path, never handed Administer.
type SessionPASEResolver interface {
	IsPASE(sessionID uint16) (pase, ok bool)
}

// NewOperationalSessionLookup builds the adapter. The `get` closure
// typically wraps an [operational.Manager.Get] call:
//
//	NewOperationalSessionLookup(func(id uint16) (*channel.Session, bool) {
//	    e, err := mgr.Get(id)
//	    if err != nil {
//	        return nil, false
//	    }
//	    return e.Session, true
//	})
func NewOperationalSessionLookup(get func(sessionID uint16) (*channel.Session, bool)) *OperationalSessionLookup {
	return &OperationalSessionLookup{get: get}
}

// WithFabricResolver wires the optional FabricFor side of the
// adapter. Pass a closure returning `(entry.FabricIndex, true)` for
// known sessions and `(0, false)` for unknown ones. Returns the
// receiver so callers can chain.
func (l *OperationalSessionLookup) WithFabricResolver(fabricFor func(sessionID uint16) (uint8, bool)) *OperationalSessionLookup {
	if l == nil {
		return nil
	}
	l.fabricFor = fabricFor
	return l
}

// Lookup implements [SessionLookup].
func (l *OperationalSessionLookup) Lookup(sessionID uint16) (*channel.Session, bool) {
	if l == nil || l.get == nil {
		return nil, false
	}
	return l.get(sessionID)
}

// FabricResolutionWired reports whether a fabric-resolver closure was
// actually supplied.
//
// Implementing [SessionFabricResolver] is not the same as being able to
// resolve: this adapter carries the method unconditionally, so a type
// assertion says nothing about whether [WithFabricResolver] was ever called.
// Without it every FabricFor answers (0, false), CheckACL reads that 0 as
// "PASE, still commissioning" and waves the request through -- so the shape
// that passes a type check is exactly the shape that bypasses access control.
// Bridge.Start asks this question instead of asking the type system.
func (l *OperationalSessionLookup) FabricResolutionWired() bool {
	return l != nil && l.fabricFor != nil
}

// FabricFor implements [SessionFabricResolver]. Returns (0, false)
// when the adapter was built without a fabric resolver closure.
func (l *OperationalSessionLookup) FabricFor(sessionID uint16) (uint8, bool) {
	if l == nil || l.fabricFor == nil {
		return 0, false
	}
	return l.fabricFor(sessionID)
}

// WithSubjectResolver wires the optional SubjectFor side of the
// adapter. Pass a closure returning `(peerNodeID, peerCATs, true)`
// for known CASE sessions and `(0, nil, false)` for unknown ones.
// Returns the receiver so callers can chain.
func (l *OperationalSessionLookup) WithSubjectResolver(subjectFor func(sessionID uint16) (uint64, []uint32, bool)) *OperationalSessionLookup {
	if l == nil {
		return nil
	}
	l.subjectFor = subjectFor
	return l
}

// WithRetransmitIntervalResolver wires the optional
// RetransmitBaseInterval side of the adapter. Pass a closure that
// resolves the peer-appropriate MRP base interval (typically
// `operational.Entry.RetransmitBaseInterval`) for known sessions and
// returns `(0, false)` for unknown ones. Returns the receiver so
// callers can chain.
func (l *OperationalSessionLookup) WithRetransmitIntervalResolver(intervalFor func(sessionID uint16, now time.Time) (time.Duration, bool)) *OperationalSessionLookup {
	if l == nil {
		return nil
	}
	l.intervalFor = intervalFor
	return l
}

// WithTransportResolver installs the closure [OperationalSessionLookup.TransportParametersFor]
// answers from: the peer's SupportedTransports bitmap and MaxTcpMessageSize
// a host keeps on its operational entry ([operational.Entry.PeerTransportParameters]).
func (l *OperationalSessionLookup) WithTransportResolver(transportsFor func(sessionID uint16) (uint16, uint32, bool)) *OperationalSessionLookup {
	if l == nil {
		return nil
	}
	l.transportsFor = transportsFor
	return l
}

// TransportParametersFor implements [SessionTransportResolver]. Returns
// (0, 0, false) when no resolver closure was supplied or the session is
// unknown.
func (l *OperationalSessionLookup) TransportParametersFor(sessionID uint16) (transports uint16, maxTCPMessageSize uint32, ok bool) {
	if l == nil || l.transportsFor == nil {
		return 0, 0, false
	}
	return l.transportsFor(sessionID)
}

// SessionPeerScheduleResolver is an optional capability a [SessionLookup]
// can implement so a wait for the peer's answer can be bounded by the
// peer's whole MRP schedule: its idle and active intervals, its active
// threshold and whether it is active now (typically
// [operational.Entry.PeerMRPSchedule]). The bound of a chunk's
// StatusResponse wait sums the peer's retransmissions on it, switching
// from the active to the idle interval past the threshold, as matter.js's
// maxResponseTimeOf does (review II, F-db021da0-13). Sessions the
// resolver does not know answer ok false; the bound then sums one base
// interval ([SessionRetransmitIntervalResolver]).
type SessionPeerScheduleResolver interface {
	PeerMRPSchedule(sessionID uint16) (idle, active, threshold time.Duration, peerActive, ok bool)
}

// WithPeerScheduleResolver wires the optional PeerMRPSchedule side of the
// adapter ([SessionPeerScheduleResolver]). Pass a closure answering a known
// session's schedule with ok true. Returns the receiver so callers can
// chain.
func (l *OperationalSessionLookup) WithPeerScheduleResolver(scheduleFor func(sessionID uint16) (idle, active, threshold time.Duration, peerActive, ok bool)) *OperationalSessionLookup {
	if l == nil {
		return nil
	}
	l.scheduleFor = scheduleFor
	return l
}

// PeerMRPSchedule implements [SessionPeerScheduleResolver]. ok is false
// when the adapter was built without a schedule closure or the session
// is unknown.
func (l *OperationalSessionLookup) PeerMRPSchedule(sessionID uint16) (idle, active, threshold time.Duration, peerActive, ok bool) {
	if l == nil || l.scheduleFor == nil {
		return 0, 0, 0, false, false
	}
	return l.scheduleFor(sessionID)
}

// RetransmitBaseInterval implements [SessionRetransmitIntervalResolver].
// Returns (0, false) when the adapter was built without an interval
// resolver closure.
func (l *OperationalSessionLookup) RetransmitBaseInterval(sessionID uint16, now time.Time) (time.Duration, bool) {
	if l == nil || l.intervalFor == nil {
		return 0, false
	}
	return l.intervalFor(sessionID, now)
}

// SubjectFor implements [SessionSubjectResolver]. Returns (0, nil,
// false) when the adapter was built without a subject resolver
// closure or the session is unknown.
func (l *OperationalSessionLookup) SubjectFor(sessionID uint16) (nodeID uint64, cats []uint32, ok bool) {
	if l == nil || l.subjectFor == nil {
		return 0, nil, false
	}
	return l.subjectFor(sessionID)
}

// WithPASEResolver wires the optional IsPASE side of the adapter. Pass
// a closure returning `(entry.IsPASE(), true)` for known sessions and
// `(false, false)` for unknown ones — typically
// `operational.Entry.IsPASE`. Returns the receiver so callers can
// chain. Without it every IsPASE answers (false, false) and a PASE
// session adopted onto a fabric by AddNOC loses its implicit Administer
// grant on the next request.
func (l *OperationalSessionLookup) WithPASEResolver(paseFor func(sessionID uint16) (bool, bool)) *OperationalSessionLookup {
	if l == nil {
		return nil
	}
	l.paseFor = paseFor
	return l
}

// IsPASE implements [SessionPASEResolver]. Returns (false, false) when
// the adapter was built without a PASE resolver closure or the session
// is unknown.
func (l *OperationalSessionLookup) IsPASE(sessionID uint16) (pase, ok bool) {
	if l == nil || l.paseFor == nil {
		return false, false
	}
	return l.paseFor(sessionID)
}

// WithActivityMarkers wires the optional [SessionActivityMarker] side of
// the adapter. Pass closures that resolve the operational entry and call
// its MarkActiveRx / MarkActiveTx (unknown sessions are a no-op).
// Returns the receiver so callers can chain.
func (l *OperationalSessionLookup) WithActivityMarkers(markRx, markTx func(sessionID uint16)) *OperationalSessionLookup {
	if l == nil {
		return nil
	}
	l.markRx = markRx
	l.markTx = markTx
	return l
}

// MarkActiveRx implements [SessionActivityMarker]. No-op when the
// adapter was built without activity-marker closures.
func (l *OperationalSessionLookup) MarkActiveRx(sessionID uint16) {
	if l == nil || l.markRx == nil {
		return
	}
	l.markRx(sessionID)
}

// MarkActiveTx implements [SessionActivityMarker]. No-op when the
// adapter was built without activity-marker closures.
func (l *OperationalSessionLookup) MarkActiveTx(sessionID uint16) {
	if l == nil || l.markTx == nil {
		return
	}
	l.markTx(sessionID)
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"

	"thingfabric.dev/go-fabric/secure/aesccm"
)

// ErrFabricIDMismatch is returned by [Responder.ProcessSigma3] when the
// peer's operational certificate is scoped to a different fabric than
// the one the responder selected for this exchange — its NOC subject
// fabric-id does not equal [Identity.FabricID]. Mirrors matter.js
// packages/protocol/src/session/case/CaseServer.ts:304-306
// (`if (fabric.fabricId !== peerFabricId) throw new UnexpectedDataError`).
var ErrFabricIDMismatch = errors.New("sigma: peer NOC fabric-id does not match responder fabric")

// PeerFabricIDExtractor is an OPTIONAL interface a [PeerVerifier]
// implementation may also satisfy. When it does, [Responder.ProcessSigma3]
// calls it after NOC-chain verification to lift the peer NOC subject's
// fabric-id and reject the handshake when it does not match the
// responder-selected fabric's [Identity.FabricID].
//
// The chain verification in [PeerVerifier.VerifyAndExtractPubKey] only
// proves the peer NOC links back to the fabric root; it does NOT bind
// the NOC subject fabric-id, which Matter §6.5.6.1 carries as a
// distinct subject DN attribute. Two fabrics provisioned from one
// trust root would otherwise accept each other's operational
// certificates. matter.js validates the two separately and rejects a
// fabric-id mismatch before the transcript signature check.
//
// The daemon's production verifier implements this surface; only
// protocol-layer test rigs that hand the responder a certificate-less
// verifier leave it unimplemented, and those skip the check. A
// verifier that DOES implement it but fails to read the fabric-id
// rejects the handshake — matter.js has no "unavailable" path here.
// Mirrors matter.js packages/protocol/src/session/case/CaseServer.ts:299-306.
type PeerFabricIDExtractor interface {
	PeerFabricIDFromNOC(noc []byte) (uint64, error)
}

// sigma2Salt builds the per-spec HKDF salt for the S2K key:
// `IPK || responderRandom || responderEphPubKey || SHA256(sigma1)`
// (Matter Core §4.13.2.5 / §4.14.2.3). Without the IPK prefix the
// responder derives a different S2K than the initiator and Apple
// surfaces SecureChannel/INVALID_PARAMETER on Sigma2.
func sigma2Salt(ipk, responderRandom, responderEphPub, sigma1Bytes []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(ipk) + len(responderRandom) + len(responderEphPub) + sha256.Size)
	buf.Write(ipk)
	buf.Write(responderRandom)
	buf.Write(responderEphPub)
	t := sha256.Sum256(sigma1Bytes)
	buf.Write(t[:])
	return buf.Bytes()
}

// sigma3Salt builds the per-spec HKDF salt for the S3K key:
// `IPK || SHA256(sigma1 || sigma2_full)`. Note: spec mandates the
// *complete* Sigma2 wire bytes (including encrypted2), not the
// pre-encryption partial form we used during the early TLV bring-up.
func sigma3Salt(ipk, sigma1Bytes, sigma2Bytes []byte) []byte {
	h := sha256.New()
	h.Write(sigma1Bytes)
	h.Write(sigma2Bytes)
	digest := h.Sum(nil)
	out := make([]byte, 0, len(ipk)+sha256.Size)
	out = append(out, ipk...)
	out = append(out, digest...)
	return out
}

// sessionKeysSalt builds the secure-session salt:
// `IPK || SHA256(sigma1 || sigma2 || sigma3)`.
func sessionKeysSalt(ipk, sigma1Bytes, sigma2Bytes, sigma3Bytes []byte) []byte {
	h := sha256.New()
	h.Write(sigma1Bytes)
	h.Write(sigma2Bytes)
	h.Write(sigma3Bytes)
	digest := h.Sum(nil)
	out := make([]byte, 0, len(ipk)+sha256.Size)
	out = append(out, ipk...)
	out = append(out, digest...)
	return out
}

// nonceForTBE returns the 13-byte AES-CCM nonce used to encrypt
// TBE2 / TBE3. Per Matter Core Spec §4.13.2.3 the nonces are
// hard-coded constants — the transcript binding rides in the AAD,
// not the nonce.
func nonceForTBE(label string) []byte {
	n := make([]byte, aesccm.NonceSize)
	copy(n, label)
	return n
}

var (
	nonceTBE2 = nonceForTBE("NCASE_Sigma2N")
	nonceTBE3 = nonceForTBE("NCASE_Sigma3N")
)

// ResumptionRecord is the per-peer CASE resumption state the protocol
// layer needs to attempt Sigma2_Resume. The store package embeds a
// superset of this; the protocol layer takes only what it uses so the
// sigma package stays free of the store import.
type ResumptionRecord struct {
	// SharedSecret is the 32-byte ECDH shared secret from the prior CASE
	// session. Acts as the HKDF IKM on the resume path.
	SharedSecret []byte
	// ResumptionID is the 16-byte id stored in TBE2 of the original Sigma2
	// and presented back by the initiator in Sigma1 tag 6.
	ResumptionID []byte
	// FabricIndex scopes the record to the fabric the original CASE
	// session was established under. The resumed session is registered
	// under this fabric — matter.js CaseServer.ts:155 destructures
	// `fabric` straight from the record; there is no Sigma3 on the
	// resume path, so the fabric can come from nowhere else.
	FabricIndex uint8
	// PeerNodeID is the peer's operational NodeID from the original
	// session. It feeds the AES-CCM nonce of the resumed session
	// (Matter §4.5.1.4 builds the nonce from the source NodeID);
	// leaving it zero makes every inbound decrypt fail.
	PeerNodeID uint64
	// PeerCATs carries the CASE Authenticated Tags from the peer's NOC
	// captured at the original Sigma3. The resume path re-grants them
	// without re-validating the NOC — matter.js CaseServer.ts:155
	// `caseAuthenticatedTags` from the record.
	PeerCATs []uint32
}

// sharedSecretSize is the length of a P-256 ECDH shared secret, the
// only secret a [ResumptionRecord] can carry.
const sharedSecretSize = 32

// wellFormed reports whether r has the shape a record of a finished CASE
// session has: a 32-byte shared secret and a 16-byte resumption id. A
// record of another shape is treated as absent on both sides of the
// handshake.
func (r *ResumptionRecord) wellFormed() bool {
	return r != nil && len(r.SharedSecret) == sharedSecretSize && len(r.ResumptionID) == ResumptionIDSize
}

// ResumptionStore is the read side the Responder requires during
// ProcessSigma1 to attempt Sigma2_Resume. GetByID must return a
// ResumptionRecord and nil when the id is known; it must return nil
// record and a non-nil error when the id is unknown so the responder can
// fall through to Full Sigma. Implementations must be safe for concurrent use.
// A record of another shape than a finished session leaves (a 32-byte
// secret, a 16-byte id) is treated as unknown.
type ResumptionStore interface {
	GetByID(resumptionID []byte) (*ResumptionRecord, error)
}

// resumptionClaimer is the optional capability of a [ResumptionStore] to
// hand a resumption id to one resume at a time: ClaimResumptionID claims
// id for the resume about to be accepted, false when another resume holds
// it; release gives it back when this one does not take place. A claim
// that is not released ends with the record the resume replaces — the
// store's owner drops it (review II, R2-P6-sec-3320, owner decision 231).
type resumptionClaimer interface {
	ClaimResumptionID(id []byte) (release func(), ok bool)
}

// resolutionCommitter is the optional capability of an [IdentityResolver]
// to learn when the Sigma1 its last resolution served is accepted: the
// responder calls CommitResolution at its commit point, once that Sigma1
// can no longer be refused, so a resolution made for a Sigma1 the
// responder then refuses — a bad ephemeral key, no session id — never
// stands for the handshake in progress (review II, decision 244). It runs
// under the responder's lock and must not call back into it.
type resolutionCommitter interface {
	CommitResolution()
}

// Sigma1ProcessResult is the discriminated union returned by
// [Responder.ProcessSigma1WithResume]. Callers check IsResume to
// decide which opcode to send.
type Sigma1ProcessResult struct {
	// Sigma2 is non-nil when Full Sigma was performed.
	Sigma2 *Sigma2
	// Sigma2Resume is non-nil when the fast-path resume was performed.
	Sigma2Resume *Sigma2Resume
	// ResumeKeys holds the derived session keys after a Sigma2_Resume.
	// Populated only when Sigma2Resume != nil.
	ResumeKeys SessionKeys
	// Claim, set on a resume whose [ResumptionStore] claims the presented
	// resumption id, is that claim. A caller that installs no session for
	// the resume — it re-announces a session the caller holds already —
	// releases it ([ResumeClaim.Release]), so the record may resume again;
	// a caller that installs one leaves the claim to the store's owner,
	// who ends it with the record the resume replaces (review II,
	// R2-P6-sec-3320, decision 243). When the caller's install fails
	// (bridge's established-callback returns an error), the bridge
	// releases nothing: the callback that failed releases the claim
	// itself, as node's does, and a host with a claiming store of its
	// own releases it in its callback, or the id stays claimed. nil on a
	// full handshake and on a store that claims nothing.
	Claim *ResumeClaim
}

// ResumeClaim is the responder's claim of a resumption id
// ([Sigma1ProcessResult.Claim]).
type ResumeClaim struct{ release func() }

// Release gives the claim back; a nil claim, and every release after the
// first, does nothing.
func (c *ResumeClaim) Release() {
	if c == nil || c.release == nil {
		return
	}
	c.release()
	c.release = nil
}

// IsResume reports whether the result requires a Sigma2_Resume reply.
func (r Sigma1ProcessResult) IsResume() bool { return r.Sigma2Resume != nil }

// --- Responder ---

// Responder drives the device-side of CASE. Construct with
// [NewResponder], call [Responder.ProcessSigma1] to consume the
// commissioner's Sigma1 and produce Sigma2, then [Responder.ProcessSigma3]
// to finalise. Optionally wire [ResumptionStore] via
// [Responder.SetResumptionStore] to enable the Sigma2_Resume fast path.
type Responder struct {
	// mu serialises every state-mutating method on Responder. Apple
	// iOS sends Sigma1 as a multicast on every interface (IPv4,
	// IPv6-Link-Local, IPv6-Global), so the bridge UDP listener
	// receives identical Sigma1 datagrams in parallel. Without the
	// mutex, all parallel ProcessSigma1 invocations race past the
	// state==responderStateInit check, each generates fresh ECDH key
	// material, and emits a different Sigma2 wire payload. The
	// initiator picks the first received and drops the rest with
	// `Dropping message without piggyback ack` (Bug A).
	//
	// Mirrors matter.js packages/protocol/src/session/case/CaseServer.ts:
	// the per-exchange CaseHandler lives behind a single async-await
	// chain so handlers are sequential by construction.
	mu sync.Mutex

	identity         *Identity
	verifier         PeerVerifier
	sessionID        uint16
	resumptionStore  ResumptionStore
	identityResolver IdentityResolver
	// sessionIDRenewer, when wired, supplies a fresh local session id
	// every time the responder resets for a NEW Sigma1 — see
	// [Responder.SetSessionIDRenewer].
	sessionIDRenewer func(previous uint16) (uint16, bool)
	// idPending: the handshake has no local session id of its own yet —
	// the responder was built without one, or starts over after a
	// handshake — and takes one from sessionIDRenewer once its Sigma1 is
	// accepted ([Responder.nextSessionIDLocked]).
	idPending bool
	// sessionParams, when non-nil and non-empty, is emitted as Sigma2
	// context-tag 5 (responderSessionParams).
	sessionParams *SessionParameters

	// ephPubBytes is the handshake's ephemeral public key; its private
	// key never leaves the Sigma1 step's locals.
	ephPubBytes []byte

	random       [RandomSize]byte
	resumptionID []byte
	// resume records what the Sigma2_Resume fast path did for the
	// session the responder currently holds. Observability only —
	// nothing in the handshake reads it back. See [ResumeInfo].
	resume ResumeInfo

	sigma1Bytes   []byte
	initEphPub    []byte   // initiator ephemeral pub key parsed from Sigma1
	peerSessionID uint16   // initiatorSessionID from Sigma1; the value we stamp into outbound Header.SessionID
	peerNodeID    uint64   // NodeID extracted from the peer's NOC at Sigma3 verification; feeds the AES-CCM nonce
	peerCATs      []uint32 // CASE Authenticated Tags from the peer's NOC subject; feeds the ACL gate's per-subject match
	// peerSessionParams retains the initiator's MRP tuning hints from
	// Sigma1 tag 5 so the operational session can size its
	// retransmission backoff to the peer (matter.js MRP.ts:129).
	peerSessionParams *SessionParameters
	sigma2            Sigma2
	sigma2Bytes       []byte // full Sigma2 wire bytes; reused for S3K + final salts
	shared            []byte
	// sigma3Digest is the SHA-256 of the Sigma3 that finished the
	// handshake, nil before and for a handshake a resumption finished:
	// only that Sigma3 is accepted again ([Responder.ProcessSigma3]).
	sigma3Digest *[32]byte

	keys  SessionKeys
	state responderState
}

// PeerSessionID returns the InitiatorSessionID extracted from the
// peer's Sigma1. CASE callers route this into
// [channel.Config.PeerSessionID] so outbound packets stamp the
// SessionID the commissioner expects in its lookup table; without
// it Apple Home / chip-tool drops every reply on the encrypted
// path with `secure session not found`.
func (r *Responder) PeerSessionID() uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peerSessionID
}

// ResumeInfo describes the CASE resumption fast path
// ([Responder.ProcessSigma1WithResume]) for the session a responder
// currently holds. Resumed is false for a session established through
// Full Sigma and for a responder that has processed nothing yet.
//
// It exists because the resume branch is the one part of CASE whose
// correctness cannot be settled without a live controller. matter.js
// takes a fresh session id for a resumed session
// (packages/protocol/src/session/case/CaseServer.ts:#resume calls
// `getNextAvailableSessionId()` before creating the secure session); we
// keep the id the responder already announced. Renewing it here would
// allocate one id per MRP retransmit of a resume Sigma1, reusing it
// risks conflating the peer's previous message counters with the new
// session — and which failure a certified controller actually provokes
// is an interop question, not a unit-test one. Recording both ids makes
// the current behaviour readable from an operator report instead of
// only from the source. matter.js logs a resumed session for the same
// reason (NodeSession.ts:437 `logNew(logger, "Resumed", …)`).
type ResumeInfo struct {
	// Resumed reports whether the session came from Sigma2_Resume
	// rather than Full Sigma.
	Resumed bool
	// PresentedResumptionID is the id the initiator sent in Sigma1 —
	// it names the cached record the controller resumed from.
	PresentedResumptionID []byte
	// IssuedResumptionID is the fresh id shipped in Sigma2_Resume for
	// the initiator's next resume.
	IssuedResumptionID []byte
	// SessionIDBefore and SessionIDAfter are the responder's local
	// session id around the resume. They are equal while the resume
	// path reuses the id; keeping both means a future change to that
	// choice shows up in the record without touching the caller.
	SessionIDBefore uint16
	SessionIDAfter  uint16
}

// ResumeInfo returns a copy of the resumption record for the session the
// responder currently holds. Safe to call from a session-established
// callback: the record is stamped before the fast path returns.
func (r *Responder) ResumeInfo() ResumeInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.resume
	out.PresentedResumptionID = append([]byte(nil), r.resume.PresentedResumptionID...)
	out.IssuedResumptionID = append([]byte(nil), r.resume.IssuedResumptionID...)
	return out
}

// PeerSessionParameters returns the initiator's MRP tuning hints from
// Sigma1 (tag 5) and whether the initiator supplied any. Session-open
// callers copy them onto the operational entry so the retransmission
// backoff honours the peer's advertised idle/active intervals
// (matter.js MRP.ts:129 retransmissionIntervalOf).
func (r *Responder) PeerSessionParameters() (SessionParameters, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.peerSessionParams == nil {
		return SessionParameters{}, false
	}
	return *r.peerSessionParams, true
}

// PeerNodeID returns the operational NodeID of the peer extracted
// from the verified NOC. The Matter AES-CCM nonce per §4.5.1.4 is
// `securityFlags || messageCounter || sourceNodeID` and the peer's
// outbound nonce uses ITS OWN NodeID — so to verify those packets
// we must hand the peer's NodeID into [channel.Config.PeerNodeID].
// Defaulting it to 0 (the previous behaviour) made every operational
// inbound from Apple Home fail AES-CCM tag verification with
// `aesccm: authentication failed`.
func (r *Responder) PeerNodeID() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peerNodeID
}

// PeerCATs returns a copy of the CASE Authenticated Tags lifted out
// of the peer's NOC subject at Sigma3 verification. Returns nil when
// the verifier did not implement [PeerCATsExtractor] or the peer's
// NOC carried no CATs. CASE callers route this into
// [channel.Config.PeerCATs] so the IM dispatcher's ACL gate can match
// CAT-bearing ACEs (Matter §9.10.5.6).
func (r *Responder) PeerCATs() []uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.peerCATs) == 0 {
		return nil
	}
	out := make([]uint32, len(r.peerCATs))
	copy(out, r.peerCATs)
	return out
}

// SessionIdentity returns the (FabricIndex, NodeID) the multi-fabric
// destination resolver landed on for the current exchange. ok==false
// before Sigma1 has been processed. Bridge callers feed these values
// into [operational.Manager.OpenFromSigmaWithStake] so the secure session
// is registered under the actually-selected fabric — without this, a
// Sigma1 that the resolver routed to fabric #1 would still be tagged
// with the factory-time fabric #2 (the bridge's last-installed default),
// and Apple's reads under fabric #1 would target a session table entry
// scoped to the wrong fabric.
//
// The fabric_index is read from [Identity.FabricIndex]; daemons that
// don't stamp it leave a zero (which is also the pre-resolver
// fallback). Returns the operational FabricIndex (0 = unknown / not
// set by daemon) and NodeID. Callers tolerant of fabric_index==0
// fall back to their factory-time default fabric.
func (r *Responder) SessionIdentity() (fabricIndex uint8, nodeID uint64, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.identity == nil || r.state == responderStateInit {
		return 0, 0, false
	}
	return r.identity.FabricIndex, r.identity.NodeID, true
}

type responderState uint8

const (
	responderStateInit responderState = iota
	responderStateSigma2Sent
	responderStateFinished
	// responderStateFailed is terminal for the handshake attempt that
	// reached it: a Sigma3 that did not authenticate must not be
	// followed by a second Sigma3 against the same Sigma2. Only a fresh
	// Sigma1 clears it, which is how a controller retries anyway.
	responderStateFailed
)

// NewResponder returns a Responder ready to consume Sigma1. A sessionID
// of 0 — no secure session's id — leaves the id to the renewer
// ([Responder.SetSessionIDRenewer]), which supplies it once a Sigma1 is
// accepted.
func NewResponder(identity *Identity, verifier PeerVerifier, sessionID uint16) *Responder {
	return &Responder{identity: identity, verifier: verifier, sessionID: sessionID, idPending: sessionID == 0}
}

// SetResumptionStore wires the optional resumption-record lookup. When
// non-nil and Sigma1 carries tags 6+7, [Responder.ProcessSigma1WithResume]
// attempts Sigma2_Resume before falling through to Full Sigma.
func (r *Responder) SetResumptionStore(s ResumptionStore) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resumptionStore = s
}

// SessionID returns the local session id the responder announces in
// Sigma2 for the handshake it is currently serving. Callers that open
// the operational session after Sigma3 MUST read it here rather than
// remember the id the responder was constructed with: a second Sigma1
// on the same exchange renews it (see
// [Responder.SetSessionIDRenewer]).
func (r *Responder) SessionID() uint16 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID
}

// SetSessionIDRenewer wires the local session-id source used when a
// NEW Sigma1 arrives on a responder that already served one, and for the
// first Sigma1 of a responder built with session id 0. It is called with
// the id currently announced and returns the replacement; ok==false keeps
// the current id, and fails the handshake of a responder that has none.
// It is called only once the Sigma1 is accepted — it parsed, addressed a
// fabric this node holds (or presented a resumption record whose MIC
// verified) and carried a valid ephemeral key — so a Sigma1 that starts
// no handshake stakes nothing (review II, R2-P3-dr-dev-08): matter.js
// takes the id after the destination check and the key agreement
// (packages/protocol/src/session/case/CaseServer.ts:239, :266 at
// db021da0) and, resuming, after the resume MIC decrypted (:161-173).
//
// matter.js takes the responder session id inside each Sigma1
// handling (packages/protocol/src/session/case/CaseServer.ts:266
// `getNextAvailableSessionId`), so every handshake gets its own id
// even when several run over one exchange. Our per-exchange
// CaseAdapter instead allocates once at construction, which made the
// second handshake register its session in the slot the first peer's
// session still occupied. Daemons therefore wire this to the
// operational manager's allocator; leaving it nil keeps the
// constructor-time id (single-handshake test rigs).
//
// The callback runs while the responder's own lock is held, so it must
// not call back into the responder.
func (r *Responder) SetSessionIDRenewer(fn func(previous uint16) (uint16, bool)) {
	r.mu.Lock()
	r.sessionIDRenewer = fn
	r.mu.Unlock()
}

// commitResolutionLocked tells the resolver, when it can be told, that the
// Sigma1 its last resolution served is accepted. Caller holds r.mu.
func (r *Responder) commitResolutionLocked() {
	if c, ok := r.identityResolver.(resolutionCommitter); ok {
		c.CommitResolution()
	}
}

// errNoSessionID fails the handshake of a responder that has no local
// session id and whose renewer supplied none.
var errNoSessionID = errors.New("sigma: no local session id for the handshake")

// nextSessionIDLocked is the local session id the handshake of an accepted
// Sigma1 takes: a fresh one from the renewer when pending — the handshake
// has none of its own yet ([Responder.idPending]) — else the one the
// responder holds; a responder without a renewer keeps the id it was
// built with. It changes nothing of the responder: the caller commits the
// id. Caller holds r.mu.
func (r *Responder) nextSessionIDLocked(pending bool) (uint16, error) {
	if !pending || r.sessionIDRenewer == nil {
		return r.sessionID, nil
	}
	next, ok := r.sessionIDRenewer(r.sessionID)
	if !ok {
		if r.sessionID == 0 {
			return 0, errNoSessionID
		}
		return r.sessionID, nil
	}
	return next, nil
}

// SetSessionParameters wires the responder's preferred MRP tuning
// hints. When set and non-empty, Sigma2 carries the value as
// context-tag 5 (`responderSessionParams`) so the commissioner can
// align its retransmit budget with the bridge's mDNS-advertised
// SII/SAI/SAT.
//
// Mirrors matter.js packages/protocol/src/session/case/CaseServer.ts:258-264.
// Pass nil to detach.
func (r *Responder) SetSessionParameters(p *SessionParameters) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionParams = p
}

// SessionParameters returns the parameters Sigma2 carries, nil when none
// are set.
func (r *Responder) SessionParameters() *SessionParameters {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionParams
}

// SetIdentityResolver wires a multi-fabric identity selector. When
// set, every fresh Sigma1 arrival runs the resolver against the
// inbound `(destinationID, initiatorRandom)` pair; the matching
// fabric's `(Identity, PeerVerifier)` replaces the baseline identity
// for Sigma2 generation. Pass nil to revert to the constructor-time
// single identity (test / single-fabric path).
//
// Required for Apple Home Multi-Admin pairing: the iPhone reconnects
// under the primary fabric while the iCloud system commissioner uses
// a different fabric, and both target the same bridge — without
// per-Sigma1 fabric selection, our Sigma2 carries the wrong NOC and
// the initiator drops with InvalidParam. Mirrors matter.js
// `FabricManager.findFabricFromDestinationId`.
func (r *Responder) SetIdentityResolver(resolver IdentityResolver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.identityResolver = resolver
}

// ProcessSigma1WithResume is the resumption-aware entry point.
// It first attempts the Sigma2_Resume fast path (Matter §4.13.2.4)
// when Sigma1 carries resumptionId + initiatorResumeMic AND a
// ResumptionStore is wired; on any miss or MIC failure it falls
// through to Full Sigma automatically.
//
// The result's IsResume() flag tells the caller which opcode to use
// (SCOpcodeSigma2Resume vs SCOpcodeSigma2). After a resume the session
// is fully established — no Sigma3 is expected.
//
// Mirrors matter.js packages/protocol/src/session/case/CaseServer.ts::#resume.
func (r *Responder) ProcessSigma1WithResume(sigma1Bytes []byte) (Sigma1ProcessResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Parse the incoming Sigma1 so we can inspect the optional resume
	// fields. Like the full path ([Responder.processSigma1Locked]), the
	// resume path judges the Sigma1 on locals and commits only a
	// resumption it accepts: a miss falls through to the full path, a
	// refusal leaves the responder as it was.
	sigma1, err := UnmarshalSigma1(sigma1Bytes)
	if err != nil {
		return Sigma1ProcessResult{}, err
	}

	// Attempt resume path only when Sigma1 carries both optional fields
	// AND a store is wired. Mirrors matter.js CaseServer.ts:#handleSigma1:
	// `sigma1.resumptionId !== undefined && sigma1.initiatorResumeMic !== undefined`.
	if r.resumptionStore != nil &&
		len(sigma1.ResumptionID) == ResumptionIDSize &&
		len(sigma1.InitiatorResumeMIC) == ResumptionIDSize {
		result, ok, resumeErr := r.tryResume(sigma1)
		if resumeErr != nil {
			// Crypto / KDF failure on an otherwise matching resumption
			// record. Propagate — do not silently fall through to Full
			// Sigma. A resumption-store lookup failure is NOT in this
			// class: tryResume folds it into a miss so a transient
			// store error downgrades to a Full Sigma handshake instead
			// of aborting the peer's CASE attempt.
			return Sigma1ProcessResult{}, resumeErr
		}
		if ok {
			return result, nil
		}
		// MIC miss / unknown id → fall through to Full Sigma below.
	}

	// Full Sigma path. Use processSigma1Locked rather than ProcessSigma1
	// because we already hold r.mu (sync.Mutex is not re-entrant).
	sigma2, err := r.processSigma1Locked(sigma1Bytes)
	if err != nil {
		return Sigma1ProcessResult{}, err
	}
	return Sigma1ProcessResult{Sigma2: &sigma2}, nil
}

// tryResume attempts the Sigma2_Resume path for a Sigma1 that carries
// resumptionId (tag 6) + initiatorResumeMic (tag 7). Returns
// (result, true, nil) on success, (_, false, nil) when the MIC misses
// (caller should fall through to Full Sigma), or (_, false, err) on a
// hard failure.
//
// KDF / MIC logic mirrors matter.js CaseServer.ts::#resume lines 139-213.
func (r *Responder) tryResume(sigma1 Sigma1) (Sigma1ProcessResult, bool, error) { //nolint:funlen // single-purpose resumption: judge on locals, then commit
	// Captured before anything can change it so [ResumeInfo] can report
	// what the fast path did with the session id rather than only what
	// it ended up being.
	sessionIDBefore := r.sessionID
	// The resumption keys and the session-key material end here on every
	// way out ([zero]); the record's shared secret is the store's.
	var sc scrub
	defer sc.wipe()
	rec, err := r.resumptionStore.GetByID(sigma1.ResumptionID)
	if err != nil || rec == nil {
		// Unknown id — or a store lookup that failed — both fall
		// through to Full Sigma. A lookup failure is deliberately not
		// distinguished from a miss: the fall-through is always safe
		// (the peer completes a Full Sigma handshake), while
		// propagating would turn a transient store error into a failed
		// CASE establishment.
		return Sigma1ProcessResult{}, false, nil //nolint:nilerr // miss is non-fatal; caller retries with full Sigma
	}
	// A record of another shape is no record: with an empty or short
	// secret the resume MICs are computable from the public random and
	// resumption id. The module's store checks the shape on write and
	// read; a [ResumptionStore] of another kind may not (R2-P6-sec-0113).
	if !rec.wellFormed() {
		return Sigma1ProcessResult{}, false, nil
	}

	// KDFSR1: peerResumeKey = HKDF(sharedSecret,
	//     initiatorRandom || peerResumptionId, "Sigma1_Resume", 16)
	// matter.js CaseServer.ts line 146-149.
	sr1Salt := make([]byte, 0, RandomSize+ResumptionIDSize)
	sr1Salt = append(sr1Salt, sigma1.InitiatorRandom[:]...)
	sr1Salt = append(sr1Salt, sigma1.ResumptionID...)
	peerResumeKey, err := hkdfDerive(rec.SharedSecret, sr1Salt, HKDFInfoSigma1Resume, SessionKeySize)
	if err != nil {
		return Sigma1ProcessResult{}, false, fmt.Errorf("sigma resume: KDFSR1: %w", err)
	}
	sc.add("resume.s1rk", peerResumeKey)

	// Verify initiatorResumeMIC = AES-CCM-Encrypt(peerResumeKey, empty,
	//     nonce="NCASE_SigmaS1"). The MIC is over zero-length plaintext;
	// matter.js uses crypto.decrypt which verifies the tag on decrypt.
	// We verify by re-encrypting and comparing — equivalent since AES-CCM
	// with empty plaintext is deterministic under a fixed key+nonce.
	// matter.js CaseServer.ts line 153.
	if !verifyResumeMIC(peerResumeKey, sigma1.InitiatorResumeMIC, nonceResume1MIC) {
		// Bad MIC → fall through to Full Sigma per spec §4.13.2.4.
		return Sigma1ProcessResult{}, false, nil
	}

	// Resolve the responder identity by the record's FabricIndex. The
	// resume path has no Sigma3 (no NOC exchange), so the fabric is
	// authoritative from the record alone — matter.js CaseServer.ts:155
	// `const { sharedSecret, fabric, peerNodeId, caseAuthenticatedTags }
	// = cx.resumptionRecord`. A resolver miss means the fabric was
	// removed after the record was written; treat like an unknown id
	// and fall through to Full Sigma.
	identity, verifier := r.identity, r.verifier
	byIndex := false
	if r.identityResolver != nil {
		if fir, ok := r.identityResolver.(FabricIndexResolver); ok {
			id, ver, found := fir.ResolveFabricIndex(rec.FabricIndex)
			if !found {
				return Sigma1ProcessResult{}, false, nil
			}
			identity, verifier = id, ver
			byIndex = true
		}
	}
	// Without the lookup by fabric index the baseline identity stands in
	// only for a record of its own fabric: a record naming another fabric
	// would resume that fabric's peer under the baseline's fabric
	// (SessionIdentity) — fail closed, a miss (R2-C2-sec-0101). A zero
	// on either side is "not stamped" (SessionIdentity's doc), the same
	// unknown a full handshake reports, and is not compared.
	if !byIndex && rec.FabricIndex != 0 && identity != nil && identity.FabricIndex != 0 && identity.FabricIndex != rec.FabricIndex {
		return Sigma1ProcessResult{}, false, nil
	}

	// Generate a fresh 16-byte local resumption id for the response.
	freshResumptionID := make([]byte, ResumptionIDSize)
	if _, err := rand.Read(freshResumptionID); err != nil {
		return Sigma1ProcessResult{}, false, fmt.Errorf("sigma resume: fresh resumptionID: %w", err)
	}

	// KDFSR2: resumeKey = HKDF(sharedSecret,
	//     initiatorRandom || localResumptionId, "Sigma2_Resume", 16)
	// matter.js CaseServer.ts line 182-183.
	sr2Salt := make([]byte, 0, RandomSize+ResumptionIDSize)
	sr2Salt = append(sr2Salt, sigma1.InitiatorRandom[:]...)
	sr2Salt = append(sr2Salt, freshResumptionID...)
	resumeKey, err := hkdfDerive(rec.SharedSecret, sr2Salt, HKDFInfoSigma2Resume, SessionKeySize)
	if err != nil {
		return Sigma1ProcessResult{}, false, fmt.Errorf("sigma resume: KDFSR2: %w", err)
	}
	sc.add("resume.s2rk", resumeKey)

	// Sigma2ResumeMIC = AES-CCM-Encrypt(resumeKey, empty,
	//     nonce="NCASE_SigmaS2"). matter.js CaseServer.ts line 184.
	sigma2ResumeMIC, err := sealResumeMIC(resumeKey, nonceResume2MIC)
	if err != nil {
		return Sigma1ProcessResult{}, false, fmt.Errorf("sigma resume: Sigma2ResumeMIC: %w", err)
	}

	// Derive operational session keys via HKDF(sharedSecret,
	//     initiatorRandom || peerResumptionId, "SessionResumptionKeys", 48).
	// Salt uses the PEER's resumption id (the one they presented in Sigma1),
	// mirroring matter.js CaseServer.ts line 165:
	// `secureSessionSalt = Bytes.concat(peerRandom, peerResumptionId)`.
	keySalt := make([]byte, 0, RandomSize+ResumptionIDSize)
	keySalt = append(keySalt, sigma1.InitiatorRandom[:]...)
	keySalt = append(keySalt, sigma1.ResumptionID...)
	finalMat, err := hkdfDerive(rec.SharedSecret, keySalt, HKDFInfoSessionResumptionKeys, FinalKeyMaterialSize)
	if err != nil {
		return Sigma1ProcessResult{}, false, fmt.Errorf("sigma resume: session keys: %w", err)
	}
	sc.add("resume.sessionKeyMaterial", finalMat)
	var keys SessionKeys
	copy(keys.I2RKey[:], finalMat[0:SessionKeySize])
	copy(keys.R2IKey[:], finalMat[SessionKeySize:2*SessionKeySize])
	copy(keys.AttestationChallenge[:], finalMat[2*SessionKeySize:3*SessionKeySize])

	// The record goes to one resume: a store that can claims its id now,
	// and a Sigma1 whose id another resume holds falls through to Full
	// Sigma, as a miss does (review II, R2-P6-sec-3320).
	release := func() {}
	var claim *ResumeClaim
	if c, ok := r.resumptionStore.(resumptionClaimer); ok {
		var claimed bool
		if release, claimed = c.ClaimResumptionID(sigma1.ResumptionID); !claimed {
			return Sigma1ProcessResult{}, false, nil
		}
		// Looked up again under the claim: a resume that held the id
		// and gave it back once its record was replaced leaves no record
		// under it.
		if again, err := r.resumptionStore.GetByID(sigma1.ResumptionID); err != nil || again == nil {
			release()
			return Sigma1ProcessResult{}, false, nil //nolint:nilerr // a record gone under the claim is a miss
		}
		claim = &ResumeClaim{release: release}
	}

	// The resumption is accepted: its session takes its id now, the last
	// step that can refuse it. A resume that lands on a responder which
	// already completed a handshake is a second session on this
	// exchange: the id it announced is occupied by the session the first
	// handshake established, and announcing it again would hand the peer
	// a slot whose keys are the old ones. It takes a fresh id, as the
	// full path does for its second handshake and as matter.js
	// CaseServer.ts:175 does per Sigma1 (getNextAvailableSessionId).
	sessionID, err := r.nextSessionIDLocked(r.idPending || r.state == responderStateFinished)
	if err != nil {
		release()
		return Sigma1ProcessResult{}, false, err
	}
	r.sessionID, r.idPending = sessionID, false
	r.commitResolutionLocked()
	r.identity, r.verifier = identity, verifier
	r.peerSessionParams = sigma1.InitiatorSessionParams
	r.sigma3Digest = nil

	// Adopt the resumed session's identity so the post-resume accessors
	// (PeerSessionID / PeerNodeID / PeerCATs / SessionIdentity /
	// ECDHSharedSecret / ResumptionID) describe THIS session, exactly as
	// they would after a full Sigma3. matter.js CaseServer.ts:171-186
	// creates the secure session from `peerNodeId`, `peerSessionId:
	// cx.peerSessionId`, `sharedSecret` and `caseAuthenticatedTags`,
	// then persists the fresh resumption id (`:212`). Without these the
	// resumed session registers with peerNodeID=0 (every inbound
	// AES-CCM verify fails, Matter §4.5.1.4 nonce) and peerSessionID=0
	// (every outbound reply stamps a session id the initiator never
	// allocated) — a dead session until the controller gives up on
	// resumption and re-runs Full Sigma.
	r.peerSessionID = sigma1.InitiatorSessionID
	r.peerNodeID = rec.PeerNodeID
	r.peerCATs = append([]uint32(nil), rec.PeerCATs...)
	// The secret of the session a second handshake on this responder
	// replaces goes before the new one is taken.
	r.dropSharedLocked()
	r.shared = append([]byte(nil), rec.SharedSecret...)
	r.resumptionID = freshResumptionID
	r.keys = keys
	r.state = responderStateFinished
	// Stamp the record BEFORE returning: the caller fires its
	// session-established callback on the way out of the fast path, and
	// that callback is where the daemon reads it.
	r.resume = ResumeInfo{
		Resumed:               true,
		PresentedResumptionID: append([]byte(nil), sigma1.ResumptionID...),
		IssuedResumptionID:    append([]byte(nil), freshResumptionID...),
		SessionIDBefore:       sessionIDBefore,
		SessionIDAfter:        r.sessionID,
	}

	s2r := Sigma2Resume{
		ResumptionID:       freshResumptionID,
		Sigma2ResumeMIC:    sigma2ResumeMIC,
		ResponderSessionID: r.sessionID,
		// Propagate the responder's MRP tuning hints so the resuming peer
		// can align its retransmit budget. Mirrors matter.js CaseServer.ts:186-191.
		SessionParams: r.sessionParams,
	}
	return Sigma1ProcessResult{
		Sigma2Resume: &s2r,
		ResumeKeys:   keys,
		Claim:        claim,
	}, true, nil
}

// verifyResumeMIC checks the initiator's 16-byte AES-CCM MIC: encrypt
// empty plaintext under key+nonce and compare (constant-time) to mic.
// AES-CCM of empty plaintext is deterministic, so seal==verify.
func verifyResumeMIC(key, mic []byte, nonceStr string) bool {
	computed, err := sealResumeMIC(key, nonceStr)
	if err != nil {
		return false
	}
	return constantTimeEqual(computed, mic)
}

// sealResumeMIC AES-CCM-seals empty plaintext under key and nonce
// (zero-padded to NonceSize), returning the 16-byte tag only.
// Corresponds to matter.js crypto.encrypt(key, new Uint8Array(0), nonce).
func sealResumeMIC(key []byte, nonceStr string) ([]byte, error) {
	cipher, err := aesccm.New(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aesccm.NonceSize)
	copy(nonce, nonceStr)
	// Seal: AES-CCM over empty plaintext → 16-byte tag appended to empty ciphertext.
	sealed, err := cipher.Seal(nil, nonce, nil, nil)
	if err != nil {
		return nil, err
	}
	// sealed is the 16-byte tag only (no ciphertext for empty input).
	return sealed, nil
}

// ProcessSigma1 consumes the initiator's Sigma1 bytes and produces
// the Sigma2 reply.
//
// Idempotency: Apple Home retransmits Sigma1 over the post-Commissioning
// CASE channel (same ExchangeID, same Sigma1 payload) when its
// MRP-layer has not yet seen our Sigma2 ACK. Without idempotency,
// allocating fresh ephemeral keys + random + Sigma2 on every
// retransmit yields a NEW Sigma2 each time — Apple sticks with the
// FIRST Sigma2 it received, derives session keys against that
// Sigma2's ephemerals, and ships its Sigma3 with those keys. Our
// last-generated Responder state then fails to AES-CCM-decrypt the
// Sigma3 payload (`encrypted payload authentication failed`) and
// Apple logs `still not subscribed, marking the device as
// unreachable` ~16 s later. The cache holds while the handshake is in
// progress ([Responder.processSigma1Locked]) and is this module's own:
// matter.js's CASE server keeps no Sigma2 to send again
// (packages/protocol/src/session/case/CaseServer.ts:102-146 at
// db021da0); its exchange layer acknowledges a duplicate
// (packages/protocol/src/protocol/MessageExchange.ts:501-511; the
// retransmission it sends in the acknowledgement's place is for BDX
// alone, :580-583), and answers a message whose id the pending reply
// acknowledges with that reply again (:512-520).
func (r *Responder) ProcessSigma1(sigma1Bytes []byte) (Sigma2, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.processSigma1Locked(sigma1Bytes)
}

// processSigma1Locked is the lock-already-held variant used by both
// ProcessSigma1 (which acquires the mutex) and ProcessSigma1WithResume
// (which holds it across the resume probe + Full-Sigma fallback so the
// state transition stays atomic against parallel Sigma1 datagrams).
//
// A Sigma1 is judged completely on locals — decode, destination id,
// ephemeral point, the whole Sigma2 and, last, the local session id —
// and the responder's state is committed only once nothing can refuse
// it any more: a Sigma1 that is refused, or whose step fails, leaves
// the responder exactly as it was, the state, the cached Sigma2 and the
// keys of a handshake in progress or finished included (review II,
// R2-P4-fr-sec-03). In matter.js such a message changes nothing about a
// handshake whose Sigma2 is unacknowledged: it never reaches the CASE
// server on the handshake's live exchange
// (packages/protocol/src/protocol/MessageExchange.ts:521-534 at
// db021da0), and a Sigma1 that opens an exchange runs a CASE server of
// its own (packages/protocol/src/session/case/CaseServer.ts:61-63).
// Once the Sigma2 was acknowledged matter.js hands the message to the
// CASE server's read of Sigma3, which refuses its type and ends the
// handshake (packages/protocol/src/session/case/CaseMessenger.ts:35-36,
// packages/protocol/src/securechannel/SecureChannelMessenger.ts:95-98,
// CaseServer.ts:65-99); this responder keeps the handshake there too,
// the module's own rule (BD-Matter-CASE-Sigma1OverCompletedAcknowledgedOnly).
func (r *Responder) processSigma1Locked(sigma1Bytes []byte) (_ Sigma2, err error) { //nolint:funlen // single-purpose SIGMA-1 processing with many crypto/validation branches
	if r.state == responderStateSigma2Sent && bytes.Equal(r.sigma1Bytes, sigma1Bytes) && r.sigma2Bytes != nil {
		// Idempotent replay while the handshake is in progress: Apple
		// Home retransmits Sigma1 over MRP when its layer hasn't yet
		// observed our Sigma2 ACK. Same Sigma1 bytes → same Sigma2 wire
		// reply. Returning a freshly-derived Sigma2 here would hand the
		// initiator new ephemerals; its first-received Sigma2 wins so
		// the Sigma3 would AES-CCM-fail against our last-derived keys.
		// The cache is this module's own: matter.js answers the
		// retransmission on the exchange layer — a duplicate is
		// acknowledged
		// (packages/protocol/src/protocol/MessageExchange.ts:501-511 at
		// db021da0; the retransmission sent in the acknowledgement's
		// place is for BDX alone, :580-583), and a message whose id the
		// pending reply acknowledges gets that reply again (:512-520).
		return r.sigma2, nil
	}
	// Any other Sigma1 — a different one, or any once the handshake
	// finished or failed — is taken as a new handshake on this slot. A
	// finished handshake's Sigma1 again is taken as any other: its
	// Sigma2 is not sent again, since its Sigma3 can complete nothing
	// any more (review II, R2-P4-fr-sec-01). matter.js never sends the
	// first Sigma2 again: on the handshake's live exchange the message
	// is a duplicate or a flow error (MessageExchange.ts:501-511,
	// :521-534 at db021da0), and once that exchange is gone
	// (CaseServer.ts:95-98) the CASE server begins afresh with a new
	// random and key pair (packages/protocol/src/session/case/CaseServer.ts:61-63,
	// :102-146, :236, :241). Apple Home opens new CASE sessions on the
	// SAME ExchangeID after CommissioningComplete (the iCloud Hub
	// Companion fabric grafts onto the existing exchange).
	//
	// S2K and its salt, which carries the IPK, end with this step; so
	// does the new shared secret when the step fails.
	var (
		sc     scrub
		shared []byte
	)
	defer func() {
		sc.wipe()
		if err != nil && shared != nil {
			secretHook("sharedSecret", shared)
			zero(shared)
		}
	}()
	sigma1, err := UnmarshalSigma1(sigma1Bytes)
	if err != nil {
		return Sigma2{}, err
	}
	// Multi-fabric identity resolution. Apple Home Multi-Admin keeps
	// two fabrics live concurrently; Sigma1's DestinationID =
	// HMAC(opIPK, random||rootPub||fabricID||nodeID) tells us which
	// fabric the initiator is addressing. The daemon-side resolver
	// iterates every persisted fabric, computes the candidate
	// destinationID, and returns the matching identity. A miss is a
	// refusal: the initiator addressed a fabric this node does not
	// hold, so no Sigma2 it could decrypt exists, and answering one
	// under the constructor-time identity costs a full ECDH/ECDSA/AES
	// round per datagram for a reply the peer only ever fails to open.
	// Mirrors matter.js CaseServer.ts:239 (findFabricFromDestinationId
	// throws before any key generation) and :88-90 (StatusReport
	// NoSharedTrustRoots). A responder without a resolver keeps its
	// single constructor-time identity (single-fabric / test path).
	identity, verifier := r.identity, r.verifier
	if r.identityResolver != nil {
		id, ver, ok := r.identityResolver.ResolveSigma1Destination(sigma1.DestinationID, sigma1.InitiatorRandom)
		if !ok {
			return Sigma2{}, ErrNoSharedTrustRoots
		}
		identity, verifier = id, ver
	}

	initEphBytes := sigma1.InitiatorEphPubKey
	if err := validatePoint(initEphBytes); err != nil {
		return Sigma2{}, err
	}
	initECDH, err := ecdh.P256().NewPublicKey(initEphBytes)
	if err != nil {
		return Sigma2{}, fmt.Errorf("%w: %v", ErrInvalidPoint, err) //nolint:errorlint // intentional double-wrap
	}

	var random [RandomSize]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Sigma2{}, fmt.Errorf("sigma: random: %w", err)
	}
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return Sigma2{}, fmt.Errorf("sigma: ephemeral keygen: %w", err)
	}
	ephPubBytes := priv.PublicKey().Bytes()
	shared, err = ecdhSharedSecret(priv, initECDH)
	if err != nil {
		return Sigma2{}, err
	}
	// The ephemeral private key has served: nothing after the shared
	// secret reads it, and it is never kept past this step ([zero]).

	// Per Matter §4.14.2.3 the responder signs over a TLV-encoded
	// `signed_data` carrying (responderNOC, responderICAC?,
	// responderEph, initiatorEph). The initiator's Sigma3 signature
	// later flips the eph order to (initiatorEph, responderEph) —
	// asymmetry intentional, locks each party's signature to its role.
	mySig, err := signTranscript(identity.signingKey(), identity.NOC, identity.ICAC, ephPubBytes, initEphBytes)
	if err != nil {
		return Sigma2{}, err
	}

	// Generate a 16-byte resumptionID for the optional
	// session-resumption surface.
	resumptionID := make([]byte, 16)
	if _, err := rand.Read(resumptionID); err != nil {
		return Sigma2{}, err
	}

	tbe2Bytes := marshalTBE2(TBE2Plaintext{
		ResponderNOC:  identity.NOC,
		ResponderICAC: identity.ICAC,
		Signature:     mySig,
		ResumptionID:  resumptionID,
	})

	// S2K derivation per Matter §4.14.2.3:
	// salt = IPK || responderRandom || responderEphPubKey || SHA256(sigma1).
	salt2 := sc.add("sigma2.salt", sigma2Salt(identity.IPK[:], random[:], ephPubBytes, sigma1Bytes))
	s2k, err := hkdfDerive(shared, salt2, HKDFInfoSigma2, SessionKeySize)
	if err != nil {
		return Sigma2{}, err
	}
	sc.add("sigma2.s2k", s2k)
	cipher, err := aesccm.New(s2k)
	if err != nil {
		return Sigma2{}, err
	}
	// AES-CCM AAD = empty; transcript binding rides in the salt only.
	enc2, err := cipher.Seal(nil, nonceTBE2, tbe2Bytes, nil)
	if err != nil {
		return Sigma2{}, err
	}

	// The Sigma1 is accepted: its handshake takes its id now, the last
	// step that can refuse it. A handshake that replaces another on
	// this slot takes a fresh one: the id already announced may be
	// occupied by the session the first handshake established, and
	// registering the second peer under it would displace the first
	// without tearing it down.
	sessionID, err := r.nextSessionIDLocked(r.idPending || r.state != responderStateInit)
	if err != nil {
		return Sigma2{}, err
	}
	r.commitResolutionLocked()

	// Commit. The previous handshake's shared secret and session keys
	// end here ([zero]); whoever needed them took a copy when it
	// finished. The resume record, the CASE Authenticated Tags, the
	// peer's node id and the MRP hints describe the peer this responder
	// served before: they are replaced or cleared for the whole span of
	// the new handshake, not just from its Sigma3 onwards.
	r.dropSharedLocked()
	r.keys = SessionKeys{}
	r.resume = ResumeInfo{}
	r.peerNodeID = 0
	r.peerCATs = nil
	r.sigma3Digest = nil
	r.identity, r.verifier = identity, verifier
	r.peerSessionParams = sigma1.InitiatorSessionParams
	r.sigma1Bytes = append([]byte(nil), sigma1Bytes...)
	r.initEphPub = append([]byte(nil), initEphBytes...)
	r.peerSessionID = sigma1.InitiatorSessionID
	r.random = random
	r.ephPubBytes = ephPubBytes
	r.shared, shared = shared, nil
	r.resumptionID = resumptionID
	r.sessionID, r.idPending = sessionID, false
	r.sigma2 = Sigma2{
		ResponderRandom:    random,
		ResponderSessionID: sessionID,
		ResponderEphPubKey: ephPubBytes,
		Encrypted2:         enc2,
		SessionParams:      r.sessionParams,
	}
	// Pre-compute the full Sigma2 wire bytes once for both the S3K
	// salt (next step) and the secure-session salt (after Sigma3).
	r.sigma2Bytes = r.sigma2.Marshal()
	r.state = responderStateSigma2Sent
	return r.sigma2, nil
}

// ProcessSigma3 verifies the initiator's Sigma3 envelope, validates
// the operational signature, and finalises the session keys.
//
// Idempotency: Apple Home retransmits Sigma3 over MRP when its layer
// has not yet observed our Sigma3-Success StatusReport ACK. A
// retransmit lands here with `state == Finished`; returning an error
// drops the StatusReport reply and Apple's pair times out 9 s later
// with `HMMTRErrorDomain Code=9`. Treat a retransmit of the same
// Sigma3 bytes as success — the caller (CaseAdapter.ProcessSigma3)
// then re-emits the cached StatusReport(Success). The convergence is
// this module's own: matter.js's CASE server reads Sigma3 once
// (packages/protocol/src/session/case/CaseServer.ts:279 at db021da0),
// and a retransmission meets its exchange layer, where a duplicate is
// acknowledged (packages/protocol/src/protocol/MessageExchange.ts:501-511;
// the retransmission sent in the acknowledgement's place is for BDX
// alone, :580-583), and a message whose id the pending reply
// acknowledges gets that reply again (:512-520).
//
// Once the handshake finished, only the bytes of the Sigma3 that finished
// it are accepted again; any other Sigma3 — and every Sigma3 after a
// resumption, which had none — is refused with [ErrSessionState] and
// changes nothing (review II, phase 4 fr-sec).
//
// A Sigma3 that does not authenticate is terminal for this handshake:
// the responder accepts no further Sigma3 until a new Sigma1 restarts
// it, and commits nothing out of the rejected message.
func (r *Responder) ProcessSigma3(sigma3Bytes []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == responderStateFinished {
		// Already verified this exchange — re-ack via cached state, for
		// the retransmission of the finishing Sigma3 only.
		if r.sigma3Digest == nil || *r.sigma3Digest != sha256.Sum256(sigma3Bytes) {
			return fmt.Errorf("%w: a Sigma3 other than the one that finished the handshake", ErrSessionState)
		}
		return nil
	}
	if r.state != responderStateSigma2Sent {
		return fmt.Errorf("%w: ProcessSigma1 must run first", ErrSessionState)
	}
	if err := r.verifySigma3Locked(sigma3Bytes); err != nil {
		// A Sigma3 that does not authenticate ends the handshake.
		// Leaving the responder in Sigma2Sent would let a second Sigma3
		// be verified against the same Sigma2 — the shape that turns any
		// per-attempt state into cross-attempt state. matter.js has no
		// such path: CaseServer reads exactly one Sigma3 and the first
		// failure ends the exchange
		// (packages/protocol/src/session/case/CaseServer.ts:275-309).
		// Only a fresh Sigma1 restarts this responder, and it derives its
		// own shared secret: this one ends with the handshake ([zero]).
		r.dropSharedLocked()
		r.keys = SessionKeys{}
		r.state = responderStateFailed
		return err
	}
	digest := sha256.Sum256(sigma3Bytes)
	r.sigma3Digest = &digest
	r.state = responderStateFinished
	return nil
}

// verifySigma3Locked runs the Sigma3 crypto and, once the transcript
// signature has authenticated the peer, commits the peer identity and
// the session keys. Caller holds r.mu and owns the state transition.
func (r *Responder) verifySigma3Locked(sigma3Bytes []byte) error {
	sigma3, err := UnmarshalSigma3(sigma3Bytes)
	if err != nil {
		return err
	}
	// S3K derivation per Matter §4.14.2.3:
	// salt = IPK || SHA256(sigma1 || sigma2_full).
	var sc scrub
	defer sc.wipe()
	salt3 := sc.add("sigma3.salt", sigma3Salt(r.identity.IPK[:], r.sigma1Bytes, r.sigma2Bytes))
	s3k, err := hkdfDerive(r.shared, salt3, HKDFInfoSigma3, SessionKeySize)
	if err != nil {
		return err
	}
	sc.add("sigma3.s3k", s3k)
	cipher, err := aesccm.New(s3k)
	if err != nil {
		return err
	}
	// AES-CCM AAD = empty (transcript binding via salt).
	plain, err := cipher.Open(nil, nonceTBE3, sigma3.Encrypted3, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}
	tbe3, err := unmarshalTBE3(plain)
	if err != nil {
		return err
	}
	initOpPub, err := r.verifier.VerifyAndExtractPubKey(tbe3.InitiatorNOC, tbe3.InitiatorICAC)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSignatureInvalid, err)
	}
	// Bind the peer's NOC to the responder-selected fabric: the chain
	// verification above only proves the NOC links to the fabric root,
	// not that its subject fabric-id equals the fabric we signed Sigma2
	// under. matter.js checks the two separately and rejects a mismatch
	// BEFORE the transcript signature check. A verifier that does not
	// expose the fabric-id, or cannot read it out of a NOC it just
	// accepted, fails closed: matter.js has no path that proceeds without
	// the comparison. Mirrors matter.js
	// packages/protocol/src/session/case/CaseServer.ts:299-306.
	if r.identity != nil {
		extractor, ok := r.verifier.(PeerFabricIDExtractor)
		if !ok {
			return fmt.Errorf("%w: the peer verifier exposes no NOC fabric-id", ErrFabricIDMismatch)
		}
		peerFabricID, ferr := extractor.PeerFabricIDFromNOC(tbe3.InitiatorNOC)
		if ferr != nil {
			return fmt.Errorf("%w: peer NOC fabric-id unreadable: %w", ErrFabricIDMismatch, ferr)
		}
		if peerFabricID != r.identity.FabricID {
			return fmt.Errorf("%w: NOC fabric-id 0x%016X != responder fabric-id 0x%016X",
				ErrFabricIDMismatch, peerFabricID, r.identity.FabricID)
		}
	}
	// Nothing lifted out of the peer's certificate may reach responder
	// state before the transcript signature proves the sender owns it:
	// every step up to here is reproducible by any node that holds the
	// fabric IPK, so an unauthenticated Sigma3 could otherwise install
	// another member's identity on this exchange. matter.js keeps the
	// decoded subject in locals across verifyEcdsa and only then builds
	// the secure session from peerNodeId + caseAuthenticatedTags
	// (packages/protocol/src/session/case/CaseServer.ts:302-327).
	if err := verifyTranscript(initOpPub, tbe3.Signature, tbe3.InitiatorNOC, tbe3.InitiatorICAC, r.initEphPub, r.ephPubBytes); err != nil {
		return err
	}

	// Lift the peer's NodeID out of the now-authenticated NOC
	// (PeerNodeIDExtractor). The value is the session's subject and rides
	// into the secure channel's AES-CCM nonce (Matter §4.5.1.4) so we can
	// authenticate the peer's outbound packets. A responder with an
	// identity fails closed when the verifier exposes no node id or
	// cannot read it: matter.js reads it from the NOC unconditionally
	// (CaseServer.ts:298-307), and a session with subject 0 would match no
	// ACL entry it should.
	if extractor, ok := r.verifier.(PeerNodeIDExtractor); ok {
		pid, perr := extractor.PeerNodeIDFromNOC(tbe3.InitiatorNOC)
		switch {
		case perr == nil:
			r.peerNodeID = pid
		case r.identity != nil:
			return fmt.Errorf("%w: peer NOC node id unreadable: %w", ErrPeerIdentityMismatch, perr)
		}
	} else if r.identity != nil {
		return fmt.Errorf("%w: the peer verifier exposes no NOC node id", ErrPeerIdentityMismatch)
	}
	// Lift CASE Authenticated Tags out of the same NOC subject for the
	// ACL gate's per-subject match (Matter §9.10.5.6). The tag set is
	// replaced, never merged: a peer whose NOC carries no CATs must end
	// up with none, otherwise it inherits every CAT-scoped ACE written
	// for whoever held this responder before it. An extractor that
	// errors or reports no CATs leaves r.peerCATs nil — the ACL gate
	// then only matches operational-node-id ACEs.
	r.peerCATs = nil
	if extractor, ok := r.verifier.(PeerCATsExtractor); ok {
		if cats, cerr := extractor.PeerCATsFromNOC(tbe3.InitiatorNOC); cerr == nil && len(cats) > 0 {
			r.peerCATs = append([]uint32(nil), cats...)
		}
	}

	// Secure-session salt = IPK || SHA256(sigma1 || sigma2 || sigma3).
	finalSalt := sc.add("sessionKeys.salt", sessionKeysSalt(r.identity.IPK[:], r.sigma1Bytes, r.sigma2Bytes, sigma3Bytes))
	final, err := hkdfDerive(r.shared, finalSalt, HKDFInfoSessionKeys, FinalKeyMaterialSize)
	if err != nil {
		return err
	}
	sc.add("sessionKeyMaterial", final)
	copy(r.keys.I2RKey[:], final[0:SessionKeySize])
	copy(r.keys.R2IKey[:], final[SessionKeySize:2*SessionKeySize])
	copy(r.keys.AttestationChallenge[:], final[2*SessionKeySize:3*SessionKeySize])
	return nil
}

// dropSharedLocked overwrites the responder's copy of the ECDH shared
// secret and lets it go ([zero]). Caller holds r.mu.
func (r *Responder) dropSharedLocked() {
	secretHook("sharedSecret", r.shared)
	zero(r.shared)
	r.shared = nil
}

// SessionKeys returns the derived I2R / R2I / AttestationChallenge.
// Valid only after [Responder.ProcessSigma3] succeeded.
func (r *Responder) SessionKeys() (SessionKeys, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != responderStateFinished {
		return SessionKeys{}, false
	}
	return r.keys, true
}

// ECDHSharedSecret returns a defensive copy of the ECDH-derived shared
// secret produced during Sigma1/2 (the value used as HKDF IKM for S2K,
// S3K, SessionKeys, and all resumption KDFs). Returns nil before
// [Responder.ProcessSigma1] has been called.
//
// The caller (daemon.go CASE onEstablished) must pass this value to
// [operational.Manager.PersistResumption] so that returning peers can
// resume via Sigma2_Resume. Mirrors matter.js
// packages/protocol/src/session/case/CaseServer.ts:210 —
// `cx.resumptionRecord.sharedSecret`.
func (r *Responder) ECDHSharedSecret() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.shared) == 0 {
		return nil
	}
	return append([]byte(nil), r.shared...)
}

// ResumptionID returns a defensive copy of the 16-byte resumption ID
// the responder placed in TBE2 (Sigma2 tag 4). Returns nil before
// [Responder.ProcessSigma1] has been called.
//
// The caller (daemon.go CASE onEstablished) must pass this value to
// [operational.Manager.PersistResumption] together with
// [Responder.ECDHSharedSecret] so that the resumption record is
// complete and a future Sigma1 carrying this ID can trigger
// Sigma2_Resume. Mirrors matter.js
// packages/protocol/src/session/case/CaseServer.ts:210 —
// `cx.resumptionRecord.resumptionId`.
func (r *Responder) ResumptionID() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.resumptionID) == 0 {
		return nil
	}
	return append([]byte(nil), r.resumptionID...)
}

// constantTimeKeysEqual is a debugging aid the test code uses to
// compare initiator/responder keys in constant time. Only exposed for
// testing — production code should compare via crypto/subtle directly.
func constantTimeKeysEqual(a, b SessionKeys) bool {
	return constantTimeEqual(a.I2RKey[:], b.I2RKey[:]) &&
		constantTimeEqual(a.R2IKey[:], b.R2IKey[:]) &&
		constantTimeEqual(a.AttestationChallenge[:], b.AttestationChallenge[:])
}

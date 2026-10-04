// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/secure/aesccm"
)

// ErrPeerIdentityMismatch is returned by [Initiator.ProcessSigma2] when the
// responder's operational certificate names a different node than the one
// the initiator set out to reach. Mirrors matter.js
// packages/protocol/src/session/case/CaseClient.ts:#doPair, which throws
// UnexpectedDataError when `peerNodeIdNOCert !== peerNodeId`.
var ErrPeerIdentityMismatch = errors.New("sigma: responder NOC does not name the expected peer node")

// ErrUnexpectedSigma2Resume is returned by [Initiator.ProcessSigma2Resume]
// when the responder answers with Sigma2_Resume although the initiator
// offered no resumption. Mirrors matter.js CaseClient.ts:#doPair
// ("Received an unexpected sigma2Resume.").
var ErrUnexpectedSigma2Resume = errors.New("sigma: unexpected Sigma2_Resume")

// Initiator drives the initiator side of CASE.
//
// Two constructors exist. [NewInitiator] is the original protocol-layer
// fixture: it takes a precomputed destination id and checks nothing about
// the peer beyond its signature. [NewPeerInitiator] is the one a node uses
// to reach a peer on its own fabric — it derives the destination id from
// the Sigma1 random as Matter §4.13.2.4.2 requires, offers resumption when
// it holds a record, and binds the responder's NOC to the node and fabric
// it meant to reach. go-fabric uses it for one purpose only: re-establishing
// former subscriptions after a restart (docs/adr/0008).
//
// Mirrors matter.js packages/protocol/src/session/case/CaseClient.ts:pair.
type Initiator struct {
	mu sync.Mutex

	identity    *Identity
	verifier    PeerVerifier
	ephPriv     *ecdh.PrivateKey
	ephPubBytes []byte
	sessionID   uint16
	dest        [32]byte
	random      [RandomSize]byte

	// Peer mode (NewPeerInitiator). peerMode false keeps the
	// fixture-compatible behaviour of NewInitiator.
	peerMode      bool
	peerNodeID    uint64
	rootPublicKey []byte
	resumption    *ResumptionRecord
	sessionParams *SessionParameters

	sigma1Bytes []byte
	sigma3Bytes []byte
	keys        SessionKeys
	state       initiatorState

	// Outcome, valid once state == initiatorStateFinished.
	peerSessionID     uint16
	peerSessionParams *SessionParameters
	peerCATs          []uint32
	shared            []byte
	resumptionID      []byte
	resumed           bool
}

type initiatorState uint8

const (
	initiatorStateInit initiatorState = iota
	initiatorStateSigma1Sent
	initiatorStateFinished
	initiatorStateFailed
)

// NewInitiator returns an Initiator ready to emit Sigma1 towards a fixed
// destination id. It performs no peer node-id binding and offers no
// resumption; use [NewPeerInitiator] to reach a real peer.
func NewInitiator(identity *Identity, verifier PeerVerifier, sessionID uint16, destinationID [32]byte) *Initiator {
	return &Initiator{
		identity:  identity,
		verifier:  verifier,
		sessionID: sessionID,
		dest:      destinationID,
	}
}

// InitiatorConfig configures [NewPeerInitiator].
type InitiatorConfig struct {
	// Identity is this node's operational identity on the shared fabric.
	Identity *Identity
	// Verifier checks the responder's NOC chain back to the fabric root.
	// If it also implements [PeerNodeIDExtractor], [PeerFabricIDExtractor]
	// or [PeerCATsExtractor], the responder's NOC is bound to PeerNodeID
	// and Identity.FabricID, and its CATs are lifted for the session.
	Verifier PeerVerifier
	// SessionID is the local session id announced in Sigma1.
	SessionID uint16
	// PeerNodeID is the operational node id of the responder.
	PeerNodeID uint64
	// RootPublicKey is the 65-byte uncompressed fabric root key; it is an
	// input of the destination id (Matter §4.13.2.4.2).
	RootPublicKey []byte
	// Resumption, when non-nil, makes Sigma1 offer session resumption with
	// the record's id and shared secret. matter.js looks the record up by
	// peer address (SessionManager.findResumptionRecordByAddress).
	Resumption *ResumptionRecord
	// SessionParams are this node's session parameters, sent as Sigma1
	// tag 5 (matter.js sends `this.#sessions.sessionParameters`).
	SessionParams *SessionParameters
}

// NewPeerInitiator returns an Initiator for reaching PeerNodeID on the
// fabric of cfg.Identity.
func NewPeerInitiator(cfg InitiatorConfig) (*Initiator, error) {
	if cfg.Identity == nil || cfg.Identity.PrivateKey == nil {
		return nil, errors.New("sigma: initiator needs an operational identity")
	}
	if cfg.Verifier == nil {
		return nil, errors.New("sigma: initiator needs a peer verifier")
	}
	if len(cfg.RootPublicKey) == 0 {
		return nil, errors.New("sigma: initiator needs the fabric root public key")
	}
	i := &Initiator{
		identity:      cfg.Identity,
		verifier:      cfg.Verifier,
		sessionID:     cfg.SessionID,
		peerMode:      true,
		peerNodeID:    cfg.PeerNodeID,
		rootPublicKey: append([]byte(nil), cfg.RootPublicKey...),
	}
	if cfg.Resumption != nil && len(cfg.Resumption.SharedSecret) > 0 && len(cfg.Resumption.ResumptionID) == ResumptionIDSize {
		rec := *cfg.Resumption
		rec.SharedSecret = append([]byte(nil), rec.SharedSecret...)
		rec.ResumptionID = append([]byte(nil), rec.ResumptionID...)
		rec.PeerCATs = append([]uint32(nil), rec.PeerCATs...)
		i.resumption = &rec
	}
	if cfg.SessionParams != nil && !cfg.SessionParams.isEmpty() {
		sp := *cfg.SessionParams
		i.sessionParams = &sp
	}
	return i, nil
}

// SessionID returns the local session id announced in Sigma1.
func (i *Initiator) SessionID() uint16 { return i.sessionID }

// PeerNodeID returns the node the initiator was built to reach (zero for
// a [NewInitiator] fixture).
func (i *Initiator) PeerNodeID() uint64 { return i.peerNodeID }

// GenerateSigma1 produces the Sigma1 message bytes for transmission.
//
// In peer mode the destination id is computed from the freshly drawn
// random, and a held resumption record adds resumptionId +
// initiatorResumeMIC — matter.js CaseClient.ts:#doPair, KDFSR1 over
// `initiatorRandom || resumptionId` and an AES-CCM tag over empty
// plaintext under nonce "NCASE_SigmaS1".
func (i *Initiator) GenerateSigma1() ([]byte, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state != initiatorStateInit {
		return nil, fmt.Errorf("%w: GenerateSigma1 already called", ErrSessionState)
	}
	if _, err := rand.Read(i.random[:]); err != nil {
		return nil, fmt.Errorf("sigma: random: %w", err)
	}
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("sigma: ephemeral keygen: %w", err)
	}
	i.ephPriv = priv
	i.ephPubBytes = priv.PublicKey().Bytes()

	if i.peerMode {
		i.dest = ComputeDestinationID(i.identity.IPK, i.random, i.rootPublicKey, i.identity.FabricID, i.peerNodeID)
	}
	msg := Sigma1{
		InitiatorRandom:        i.random,
		InitiatorSessionID:     i.sessionID,
		DestinationID:          i.dest,
		InitiatorEphPubKey:     i.ephPubBytes,
		InitiatorSessionParams: i.sessionParams,
	}
	if i.resumption != nil {
		key, err := hkdfDerive(i.resumption.SharedSecret, resumeSalt(i.random, i.resumption.ResumptionID), HKDFInfoSigma1Resume, SessionKeySize)
		if err != nil {
			return nil, fmt.Errorf("sigma resume: KDFSR1: %w", err)
		}
		mic, err := sealResumeMIC(key, nonceResume1MIC)
		if err != nil {
			return nil, fmt.Errorf("sigma resume: initiatorResumeMIC: %w", err)
		}
		msg.ResumptionID = append([]byte(nil), i.resumption.ResumptionID...)
		msg.InitiatorResumeMIC = mic
	}
	i.sigma1Bytes = msg.Marshal()
	i.state = initiatorStateSigma1Sent
	return i.sigma1Bytes, nil
}

// resumeSalt concatenates the initiator random and a resumption id, the
// salt of every resumption KDF (Matter §4.13.2.4).
func resumeSalt(random [RandomSize]byte, resumptionID []byte) []byte {
	out := make([]byte, 0, RandomSize+len(resumptionID))
	out = append(out, random[:]...)
	return append(out, resumptionID...)
}

// ProcessSigma2 consumes a decoded Sigma2 and produces the Sigma3 bytes.
//
// The Sigma3 key salt hashes the Sigma2 *as received*; this entry point
// re-encodes the struct, which is only byte-exact for a Sigma2 this module
// produced. A transport handing over wire bytes uses
// [Initiator.ProcessSigma2Bytes].
func (i *Initiator) ProcessSigma2(sigma2 Sigma2) ([]byte, error) {
	return i.processSigma2(sigma2, sigma2.Marshal())
}

// ProcessSigma2Bytes decodes the responder's Sigma2 wire payload and
// produces the Sigma3 bytes.
func (i *Initiator) ProcessSigma2Bytes(raw []byte) ([]byte, error) {
	sigma2, err := UnmarshalSigma2(raw)
	if err != nil {
		return nil, err
	}
	return i.processSigma2(sigma2, raw)
}

// processSigma2 mirrors matter.js CaseClient.ts:#doPair (full handshake
// branch): derive S2K, decrypt TBE2, verify the responder's NOC and its
// transcript signature, bind the NOC to the expected node and fabric,
// then build Sigma3 and the session keys.
func (i *Initiator) processSigma2(sigma2 Sigma2, sigma2Bytes []byte) ([]byte, error) { //nolint:funlen // one linear handshake step; splitting it scatters the transcript
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state != initiatorStateSigma1Sent {
		return nil, fmt.Errorf("%w: GenerateSigma1 must run first", ErrSessionState)
	}
	fail := func(err error) ([]byte, error) {
		i.state = initiatorStateFailed
		return nil, err
	}
	if err := validatePoint(sigma2.ResponderEphPubKey); err != nil {
		return fail(err)
	}

	respECDH, err := ecdh.P256().NewPublicKey(sigma2.ResponderEphPubKey)
	if err != nil {
		return fail(fmt.Errorf("%w: %v", ErrInvalidPoint, err)) //nolint:errorlint // intentional double-wrap
	}
	shared, err := ecdhSharedSecret(i.ephPriv, respECDH)
	if err != nil {
		return fail(err)
	}

	// S2K per Matter §4.14.2.3:
	// salt = IPK || responderRandom || responderEphPubKey || SHA256(sigma1)
	salt2 := sigma2Salt(i.identity.IPK[:], sigma2.ResponderRandom[:], sigma2.ResponderEphPubKey, i.sigma1Bytes)
	s2k, err := hkdfDerive(shared, salt2, HKDFInfoSigma2, SessionKeySize)
	if err != nil {
		return fail(err)
	}
	// AES-CCM AAD is empty for Sigma2/Sigma3; the transcript binding rides
	// in the salt (matter.js `crypto.decrypt(key, data, nonce)`).
	cipher, err := aesccm.New(s2k)
	if err != nil {
		return fail(err)
	}
	plain, err := cipher.Open(nil, nonceTBE2, sigma2.Encrypted2, nil)
	if err != nil {
		return fail(fmt.Errorf("%w: %w", ErrUnauthenticated, err))
	}
	tbe2, err := unmarshalTBE2(plain)
	if err != nil {
		return fail(err)
	}

	respOpPub, err := i.verifier.VerifyAndExtractPubKey(tbe2.ResponderNOC, tbe2.ResponderICAC)
	if err != nil {
		return fail(fmt.Errorf("%w: %w", ErrSignatureInvalid, err))
	}
	if err := verifyTranscript(respOpPub, tbe2.Signature, tbe2.ResponderNOC, tbe2.ResponderICAC, sigma2.ResponderEphPubKey, i.ephPubBytes); err != nil {
		return fail(err)
	}

	var peerCATs []uint32
	if i.peerMode {
		// matter.js CaseClient.ts:#doPair checks the NOC subject's node id
		// and fabric id against the peer it set out to reach, after the
		// signature and before Sigma3.
		if extractor, ok := i.verifier.(PeerNodeIDExtractor); ok {
			nodeID, nerr := extractor.PeerNodeIDFromNOC(tbe2.ResponderNOC)
			if nerr != nil {
				return fail(fmt.Errorf("%w: node id unreadable: %w", ErrPeerIdentityMismatch, nerr))
			}
			if nodeID != i.peerNodeID {
				return fail(fmt.Errorf("%w: NOC node id 0x%016X, expected 0x%016X", ErrPeerIdentityMismatch, nodeID, i.peerNodeID))
			}
		}
		if extractor, ok := i.verifier.(PeerFabricIDExtractor); ok {
			fabricID, ferr := extractor.PeerFabricIDFromNOC(tbe2.ResponderNOC)
			if ferr != nil {
				return fail(fmt.Errorf("%w: peer NOC fabric-id unreadable: %w", ErrFabricIDMismatch, ferr))
			}
			if fabricID != i.identity.FabricID {
				return fail(fmt.Errorf("%w: NOC fabric-id 0x%016X != fabric-id 0x%016X", ErrFabricIDMismatch, fabricID, i.identity.FabricID))
			}
		}
		// The responder's CATs, lifted from its now-authenticated NOC, are
		// the subject CATs of requests it sends over this session
		// (Matter §6.6.2.1.2; chip CASESession::HandleSigma2 extracts them
		// the same way). See notes/parity/by_design.md
		// BD-Matter-CaseInitiatorPeerCATs for matter.js's narrower source.
		if extractor, ok := i.verifier.(PeerCATsExtractor); ok {
			if cats, cerr := extractor.PeerCATsFromNOC(tbe2.ResponderNOC); cerr == nil && len(cats) > 0 {
				peerCATs = append([]uint32(nil), cats...)
			}
		}
	}

	// TBE3: initiator signed data carries (initNOC, initICAC, initEph,
	// respEph) per Matter §4.14.2.3.
	mySig, err := signTranscript(i.identity.PrivateKey, i.identity.NOC, i.identity.ICAC, i.ephPubBytes, sigma2.ResponderEphPubKey)
	if err != nil {
		return fail(err)
	}
	tbe3Bytes := marshalTBE3(TBE3Plaintext{
		InitiatorNOC:  i.identity.NOC,
		InitiatorICAC: i.identity.ICAC,
		Signature:     mySig,
	})

	// S3K: salt = IPK || SHA256(sigma1 || sigma2_as_received).
	salt3 := sigma3Salt(i.identity.IPK[:], i.sigma1Bytes, sigma2Bytes)
	s3k, err := hkdfDerive(shared, salt3, HKDFInfoSigma3, SessionKeySize)
	if err != nil {
		return fail(err)
	}
	enc, err := aesccm.New(s3k)
	if err != nil {
		return fail(err)
	}
	enc3, err := enc.Seal(nil, nonceTBE3, tbe3Bytes, nil)
	if err != nil {
		return fail(err)
	}
	i.sigma3Bytes = Sigma3{Encrypted3: enc3}.Marshal()

	// Secure-session salt = IPK || SHA256(sigma1 || sigma2 || sigma3).
	finalSalt := sessionKeysSalt(i.identity.IPK[:], i.sigma1Bytes, sigma2Bytes, i.sigma3Bytes)
	final, err := hkdfDerive(shared, finalSalt, HKDFInfoSessionKeys, FinalKeyMaterialSize)
	if err != nil {
		return fail(err)
	}
	copy(i.keys.I2RKey[:], final[0:SessionKeySize])
	copy(i.keys.R2IKey[:], final[SessionKeySize:2*SessionKeySize])
	copy(i.keys.AttestationChallenge[:], final[2*SessionKeySize:3*SessionKeySize])

	i.peerSessionID = sigma2.ResponderSessionID
	if sigma2.SessionParams != nil {
		sp := *sigma2.SessionParams
		i.peerSessionParams = &sp
	}
	i.peerCATs = peerCATs
	i.shared = shared
	i.resumptionID = append([]byte(nil), tbe2.ResumptionID...)
	i.resumed = false
	i.state = initiatorStateFinished
	return i.sigma3Bytes, nil
}

// ProcessSigma2Resume consumes the responder's Sigma2_Resume wire payload.
// On success the session is established and no Sigma3 follows; the caller
// answers with a StatusReport(Success).
//
// Mirrors matter.js CaseClient.ts:#doPair (sigma2Resume branch): verify
// the responder's MIC under KDFSR2 over `initiatorRandom ||
// newResumptionId`, then derive the session keys with
// "SessionResumptionKeys" over `initiatorRandom || presentedResumptionId`.
func (i *Initiator) ProcessSigma2Resume(raw []byte) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state != initiatorStateSigma1Sent {
		return fmt.Errorf("%w: GenerateSigma1 must run first", ErrSessionState)
	}
	if i.resumption == nil {
		i.state = initiatorStateFailed
		return ErrUnexpectedSigma2Resume
	}
	s2r, err := UnmarshalSigma2Resume(raw)
	if err != nil {
		i.state = initiatorStateFailed
		return err
	}
	rec := i.resumption
	resumeKey, err := hkdfDerive(rec.SharedSecret, resumeSalt(i.random, s2r.ResumptionID), HKDFInfoSigma2Resume, SessionKeySize)
	if err != nil {
		i.state = initiatorStateFailed
		return fmt.Errorf("sigma resume: KDFSR2: %w", err)
	}
	if !verifyResumeMIC(resumeKey, s2r.Sigma2ResumeMIC, nonceResume2MIC) {
		i.state = initiatorStateFailed
		return fmt.Errorf("%w: sigma2ResumeMIC", ErrUnauthenticated)
	}
	final, err := hkdfDerive(rec.SharedSecret, resumeSalt(i.random, rec.ResumptionID), HKDFInfoSessionResumptionKeys, FinalKeyMaterialSize)
	if err != nil {
		i.state = initiatorStateFailed
		return fmt.Errorf("sigma resume: session keys: %w", err)
	}
	copy(i.keys.I2RKey[:], final[0:SessionKeySize])
	copy(i.keys.R2IKey[:], final[SessionKeySize:2*SessionKeySize])
	copy(i.keys.AttestationChallenge[:], final[2*SessionKeySize:3*SessionKeySize])

	i.peerSessionID = s2r.ResponderSessionID
	if s2r.SessionParams != nil {
		sp := *s2r.SessionParams
		i.peerSessionParams = &sp
	}
	i.peerCATs = append([]uint32(nil), rec.PeerCATs...)
	i.shared = append([]byte(nil), rec.SharedSecret...)
	i.resumptionID = append([]byte(nil), s2r.ResumptionID...)
	i.resumed = true
	i.state = initiatorStateFinished
	return nil
}

// SessionKeys returns the derived I2R / R2I / AttestationChallenge.
// Valid only after the handshake finished.
func (i *Initiator) SessionKeys() (SessionKeys, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state != initiatorStateFinished {
		return SessionKeys{}, false
	}
	return i.keys, true
}

// InitiatorResult is what a finished initiator hands to the session layer.
type InitiatorResult struct {
	// Keys are the session keys. The initiator encrypts with I2RKey and
	// decrypts with R2IKey.
	Keys SessionKeys
	// PeerSessionID is the responder's session id (Sigma2 tag 2 or
	// Sigma2_Resume tag 3).
	PeerSessionID uint16
	// PeerNodeID is the responder's operational node id.
	PeerNodeID uint64
	// PeerCATs are the responder's CASE Authenticated Tags: lifted from
	// its NOC on a full handshake, taken from the record on resumption.
	PeerCATs []uint32
	// PeerSessionParams are the responder's session parameters, when sent.
	PeerSessionParams *SessionParameters
	// Resumed reports that the session came from Sigma2_Resume.
	Resumed bool
	// SharedSecret and ResumptionID form the resumption record for the
	// next handshake with this peer (matter.js saves it after every
	// successful pair).
	SharedSecret []byte
	ResumptionID []byte
}

// Result returns the handshake outcome. The second return is false until
// the handshake finished.
func (i *Initiator) Result() (InitiatorResult, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state != initiatorStateFinished {
		return InitiatorResult{}, false
	}
	res := InitiatorResult{
		Keys:          i.keys,
		PeerSessionID: i.peerSessionID,
		PeerNodeID:    i.peerNodeID,
		PeerCATs:      append([]uint32(nil), i.peerCATs...),
		Resumed:       i.resumed,
		SharedSecret:  append([]byte(nil), i.shared...),
		ResumptionID:  append([]byte(nil), i.resumptionID...),
	}
	if i.peerSessionParams != nil {
		sp := *i.peerSessionParams
		res.PeerSessionParams = &sp
	}
	return res, true
}

// UnmarshalSigma2 decodes a Matter TLV Sigma2 (Matter §4.14.2.3). Unknown
// optional tags are skipped. Mirrors matter.js
// packages/protocol/src/session/case/CaseMessages.ts:TlvCaseSigma2.
func UnmarshalSigma2(b []byte) (Sigma2, error) {
	var s Sigma2
	dec := sigmaTLVDecoder(b)
	if err := dec.openStruct(); err != nil {
		return s, fmt.Errorf("%w: sigma2 open struct: %w", ErrSessionState, err)
	}
	seen := 0
	for {
		tag, val, end, err := dec.next()
		if err != nil {
			return s, fmt.Errorf("%w: sigma2 next: %w", ErrSessionState, err)
		}
		if end {
			break
		}
		switch tag {
		case 1:
			if len(val.octets) != RandomSize {
				return s, fmt.Errorf("%w: sigma2 random length=%d", ErrSessionState, len(val.octets))
			}
			copy(s.ResponderRandom[:], val.octets)
			seen |= 1
		case 2:
			s.ResponderSessionID = uint16(val.u & 0xFFFF)
			seen |= 2
		case 3:
			if len(val.octets) != EphPubKeySize {
				return s, fmt.Errorf("%w: sigma2 ephPub length=%d", ErrSessionState, len(val.octets))
			}
			s.ResponderEphPubKey = append([]byte(nil), val.octets...)
			seen |= 4
		case 4:
			s.Encrypted2 = append([]byte(nil), val.octets...)
			seen |= 8
		case 5:
			if val.container {
				sp, err := parseSessionParameters(dec)
				if err != nil {
					return s, fmt.Errorf("%w: sigma2 sessionParams: %w", ErrSessionState, err)
				}
				s.SessionParams = &sp
			}
		default:
			if val.container {
				if err := dec.skipContainer(); err != nil {
					return s, fmt.Errorf("%w: sigma2 skip tag=%d: %w", ErrSessionState, tag, err)
				}
			}
		}
	}
	if seen != 15 {
		return s, fmt.Errorf("%w: sigma2 missing mandatory field (mask=%04b)", ErrSessionState, seen)
	}
	return s, nil
}

// UnmarshalSigma2Resume decodes a Matter TLV Sigma2_Resume (Matter
// §4.14.2.3). Mirrors matter.js CaseMessages.ts:TlvCaseSigma2Resume.
func UnmarshalSigma2Resume(b []byte) (Sigma2Resume, error) {
	var s Sigma2Resume
	dec := sigmaTLVDecoder(b)
	if err := dec.openStruct(); err != nil {
		return s, fmt.Errorf("%w: sigma2resume open struct: %w", ErrSessionState, err)
	}
	seen := 0
	for {
		tag, val, end, err := dec.next()
		if err != nil {
			return s, fmt.Errorf("%w: sigma2resume next: %w", ErrSessionState, err)
		}
		if end {
			break
		}
		switch tag {
		case 1:
			if len(val.octets) != ResumptionIDSize {
				return s, fmt.Errorf("%w: sigma2resume resumptionId length=%d", ErrSessionState, len(val.octets))
			}
			s.ResumptionID = append([]byte(nil), val.octets...)
			seen |= 1
		case 2:
			if len(val.octets) != aesccm.TagSize {
				return s, fmt.Errorf("%w: sigma2resume mic length=%d", ErrSessionState, len(val.octets))
			}
			s.Sigma2ResumeMIC = append([]byte(nil), val.octets...)
			seen |= 2
		case 3:
			s.ResponderSessionID = uint16(val.u & 0xFFFF)
			seen |= 4
		case 4:
			if val.container {
				sp, err := parseSessionParameters(dec)
				if err != nil {
					return s, fmt.Errorf("%w: sigma2resume sessionParams: %w", ErrSessionState, err)
				}
				s.SessionParams = &sp
			}
		default:
			if val.container {
				if err := dec.skipContainer(); err != nil {
					return s, fmt.Errorf("%w: sigma2resume skip tag=%d: %w", ErrSessionState, tag, err)
				}
			}
		}
	}
	if seen != 7 {
		return s, fmt.Errorf("%w: sigma2resume missing mandatory field (mask=%03b)", ErrSessionState, seen)
	}
	return s, nil
}

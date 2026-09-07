// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// Verifier validates a NOC chain against a fabric root.
//
// Two roles:
//
//   - Sigma: the responder side of CASE looks up the local root
//     for the fabric the initiator claims, then asks the verifier
//     to validate the initiator's NOC + optional ICAC chain.
//
//   - Commissioning (Stufe 6): after AddNOC the bridge inspects
//     its own newly-issued NOC + ICAC against the trusted root
//     supplied by AddTrustedRootCertificate.
//
// The verifier carries a clock so tests can pin "now" via a
// [TimeSource] implementation; production code uses [SystemTime].
type Verifier struct {
	root *ecdsa.PublicKey
	now  TimeSource
}

// TimeSource returns the current time for NotBefore/NotAfter checks.
type TimeSource interface {
	Now() time.Time
}

// SystemTime delegates to time.Now.
type SystemTime struct{}

// Now implements [TimeSource].
func (SystemTime) Now() time.Time { return time.Now() }

// FixedTime returns a [TimeSource] that always reports t — for tests.
type FixedTime struct{ T time.Time }

// Now implements [TimeSource].
func (f FixedTime) Now() time.Time { return f.T }

// Verification errors.
var (
	// ErrSignatureInvalid is returned when an ECDSA verify fails.
	ErrSignatureInvalid = errors.New("mattercert: signature invalid")
	// ErrChainBroken is returned when the chain does not link up
	// (e.g. NOC issuer != ICAC subject, or ICAC issuer != Root subject).
	ErrChainBroken = errors.New("mattercert: certificate chain broken")
	// ErrExpired is returned when the certificate's validity window
	// excludes the current time.
	ErrExpired = errors.New("mattercert: certificate expired or not yet valid")
	// ErrInvalidRCAC is returned when a Root CA certificate fails the
	// structural checks that go beyond signature verification.
	ErrInvalidRCAC = errors.New("mattercert: RCAC structural validation failed")
)

// KeyUsage bit masks per Matter §6.5.1.4 (mirrors RFC 5280 §4.2.1.3).
const (
	// KeyUsageKeyCertSign must be set on every CA certificate.
	// Mirrors chip src/credentials/CHIPCert.cpp ValidateChipRCAC keyCertSign check.
	KeyUsageKeyCertSign uint16 = 1 << 5
	// KeyUsageCRLSign is defined for completeness; chip does NOT require it
	// on an RCAC — only keyCertSign is mandatory per CHIPCert.cpp:1141.
	// Kept as a named constant so callers can inspect the bit without
	// hard-coding the mask, but ValidateRCAC intentionally omits it.
	KeyUsageCRLSign uint16 = 1 << 6
)

// ValidateRCAC checks the structural invariants that chip enforces on Root
// CA certificates beyond what the chain verifier already covers:
//
//   - Subject == Issuer (self-signed: the RCAC-ID in both must match).
//   - SubjectKeyIdentifier == AuthorityKeyIdentifier (self-signed key binding).
//   - KeyUsage extension present with keyCertSign set (cRLSign is NOT required).
//   - BasicConstraints PathLenConstraint, when PRESENT, must be ≤ 1 (Matter
//     restricts depth to one ICAC). chip CHIPCert.cpp:1136-1139 wraps the
//     bound in `if (mCertFlags.Has(kPathLenConstraintPresent))` — an RCAC
//     without an explicit PathLenConstraint passes (a missing constraint
//     means "no caller-imposed depth limit"), and PathLen=0 is also
//     accepted because chip imposes no lower bound.
//   - ECDSA self-signature verification: the RCAC must be signed by its own key.
//
// Mirrors connectedhomeip/src/credentials/CHIPCert.cpp:1116-1144
// ValidateChipRCAC verbatim — the previous "PathLen must be present AND > 0"
// guards were stricter than chip and rejected the chip-tool / openssl
// commissioner default RCAC (Path­LenConstraint absent), breaking
// commissioning at SendTrustedRootCert with `BasicConstraints
// PathLenConstraint absent on RCAC`.
// Returns [ErrInvalidRCAC] (wrapping a descriptive message) when any check fails.
func ValidateRCAC(c *Certificate) error {
	if !c.IsRoot() {
		return fmt.Errorf("%w: cert is not a Root CA", ErrInvalidRCAC)
	}
	// Self-signed: Subject RCAC-ID must equal Issuer RCAC-ID.
	// Mirrors chip CHIPCert.cpp:1131 mSubjectDN.IsEqual(mIssuerDN).
	if c.Subject.MatterRCACID != c.Issuer.MatterRCACID {
		return fmt.Errorf("%w: Subject RCAC-ID (0x%016X) != Issuer RCAC-ID (0x%016X); root must be self-signed",
			ErrInvalidRCAC, c.Subject.MatterRCACID, c.Issuer.MatterRCACID)
	}
	// SubjectKeyId must equal AuthorityKeyId on a self-signed certificate.
	// Mirrors chip CHIPCert.cpp:1133 mSubjectKeyId.data_equal(mAuthKeyId).
	if c.Extensions.HasSubjectKeyID && c.Extensions.HasAuthorityKeyID {
		if !bytesEqual(c.Extensions.SubjectKeyID, c.Extensions.AuthorityKeyID) {
			return fmt.Errorf("%w: SubjectKeyId != AuthorityKeyId on self-signed RCAC", ErrInvalidRCAC)
		}
	}
	// KeyUsage must include keyCertSign. chip checks ONLY kKeyCertSign;
	// cRLSign is NOT required by the Matter spec or chip.
	// Mirrors chip CHIPCert.cpp:1141 kKeyUsage_KeyCertSign.
	if !c.Extensions.HasKeyUsage {
		return fmt.Errorf("%w: KeyUsage extension absent", ErrInvalidRCAC)
	}
	if c.Extensions.KeyUsage&KeyUsageKeyCertSign == 0 {
		return fmt.Errorf("%w: KeyUsage=0x%04X missing keyCertSign (bit 5)",
			ErrInvalidRCAC, c.Extensions.KeyUsage)
	}
	// BasicConstraints PathLenConstraint, when present, must be ≤ 1
	// (Matter restricts depth to at most one ICAC per §6.5.2.3).
	// chip CHIPCert.cpp:1136-1139 ONLY checks the upper bound and ONLY
	// when the constraint is present — an RCAC without an explicit
	// PathLenConstraint (chip-tool / openssl default) is accepted.
	if c.Extensions.BasicConstraintsHasPathLen && c.Extensions.BasicConstraintsPathLen > 1 {
		return fmt.Errorf("%w: BasicConstraints PathLenConstraint=%d exceeds 1 (Matter allows at most one ICAC)",
			ErrInvalidRCAC, c.Extensions.BasicConstraintsPathLen)
	}
	// ECDSA self-signature: the RCAC must be signed by its own public key.
	// A commissioner that submits a foreign or unsigned root could later
	// break the CASE chain if the bridge accepts it silently.
	// Mirrors chip CHIPCert.cpp:1144 VerifyCertSignature(certData, certData).
	selfKey, err := c.PublicKeyECDSA()
	if err != nil {
		return fmt.Errorf("%w: cannot decode RCAC public key: %w", ErrInvalidRCAC, err)
	}
	if err := verifySignature(c, selfKey); err != nil {
		return fmt.Errorf("%w: RCAC self-signature invalid: %w", ErrInvalidRCAC, err)
	}
	return nil
}

// bytesEqual returns true when a and b have identical length and contents.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// NewVerifier returns a verifier rooted at rootPubKey. The key must be
// a P-256 public key in uncompressed (0x04 prefix, 65 bytes) form —
// the canonical Matter root-CA public-key shape.
func NewVerifier(rootPubKey []byte, clock TimeSource) (*Verifier, error) {
	if len(rootPubKey) != 65 || rootPubKey[0] != 0x04 {
		return nil, fmt.Errorf("%w: root pub key length=%d prefix=%#x", ErrMalformed, len(rootPubKey), rootPubKey[0])
	}
	root, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), rootPubKey)
	if err != nil {
		return nil, fmt.Errorf("%w: root pub key off-curve: %w", ErrMalformed, err)
	}
	if clock == nil {
		clock = SystemTime{}
	}
	return &Verifier{
		root: root,
		now:  clock,
	}, nil
}

// PeerNodeIDFromNOC implements the [sigma.PeerNodeIDExtractor]
// optional surface. It decodes noc and returns the operational
// NodeID from the certificate subject. The Sigma responder pipes
// this into [channel.Config.PeerNodeID] so the AES-CCM nonce on
// the inbound secure channel (which uses the peer's own NodeID)
// matches.
func (v *Verifier) PeerNodeIDFromNOC(noc []byte) (uint64, error) {
	c, err := Decode(noc)
	if err != nil {
		return 0, fmt.Errorf("noc decode: %w", err)
	}
	if !c.Subject.HasNodeID {
		return 0, fmt.Errorf("%w: NOC subject missing NodeID", ErrMalformed)
	}
	return c.Subject.MatterNodeID, nil
}

// PeerFabricIDFromNOC implements the [sigma.PeerFabricIDExtractor]
// optional surface. It decodes noc and returns the matter-fabric-id
// carried by the certificate subject (Matter §6.5.6.1).
//
// The chain walk in [Verifier.VerifyAndExtractPubKey] proves only that
// the peer NOC links back to this fabric's root — it never compares
// the subject's fabric-id. Two fabrics provisioned from one trust root
// would therefore accept each other's operational certificates, and a
// controller holding a fabric-B NOC could open a session tagged with
// fabric A and read fabric A's ACL-scoped data. The Sigma responder
// makes that comparison, but only for verifiers exposing this method,
// so dropping it here turns the check back into dead code. Mirrors
// matter.js packages/protocol/src/session/case/CaseServer.ts
// (`if (fabric.fabricId !== peerFabricId) throw new UnexpectedDataError`).
func (v *Verifier) PeerFabricIDFromNOC(noc []byte) (uint64, error) {
	c, err := Decode(noc)
	if err != nil {
		return 0, fmt.Errorf("noc decode: %w", err)
	}
	if !c.Subject.HasFabricID {
		return 0, fmt.Errorf("%w: NOC subject missing FabricID", ErrMalformed)
	}
	return c.Subject.MatterFabricID, nil
}

// PeerCATsFromNOC implements the [sigma.PeerCATsExtractor] optional
// surface. It decodes noc and returns the list of CASE Authenticated
// Tags from the certificate subject. The Sigma responder pipes this
// into [channel.Config.PeerCATs] so the IM dispatcher's ACL gate can
// evaluate per-subject ACEs that target administrator groups (Matter
// §9.10.5.6 + chip src/access/AccessControl.cpp:481).
//
// Returns nil + no error when the NOC subject carries no CATs — that
// is the common case (single-admin fabrics). chip
// src/lib/core/CASEAuthTag.h enforces (identifier, version) packing
// inside a 32-bit CAT; the values stored here match that layout
// verbatim so [matchesCATSubject] decoding in the dispatcher round-trips.
func (v *Verifier) PeerCATsFromNOC(noc []byte) ([]uint32, error) {
	c, err := Decode(noc)
	if err != nil {
		return nil, fmt.Errorf("noc decode: %w", err)
	}
	if len(c.Subject.CASEAuthTags) == 0 {
		return nil, nil
	}
	out := make([]uint32, len(c.Subject.CASEAuthTags))
	copy(out, c.Subject.CASEAuthTags)
	return out, nil
}

// VerifyAndExtractPubKey is the [sigma.PeerVerifier] surface. It
// decodes the supplied NOC bytes, optionally walks an ICAC, and
// verifies the chain back to the verifier's root. Returns the
// operational public key embedded in the NOC.
//
// Errors translate into [sigma.ErrSignatureInvalid] at the Sigma
// layer; this method preserves the more specific error inside the
// returned wrapper for diagnostics.
func (v *Verifier) VerifyAndExtractPubKey(noc, icac []byte) (*ecdsa.PublicKey, error) {
	if len(noc) > MaxOperationalCertTLVBytes {
		return nil, fmt.Errorf("%w: NOC is %d bytes, over the %d-byte cap", ErrMalformed, len(noc), MaxOperationalCertTLVBytes)
	}
	if len(icac) > MaxOperationalCertTLVBytes {
		return nil, fmt.Errorf("%w: ICAC is %d bytes, over the %d-byte cap", ErrMalformed, len(icac), MaxOperationalCertTLVBytes)
	}
	nocCert, err := Decode(noc)
	if err != nil {
		return nil, fmt.Errorf("noc: %w", err)
	}
	if !nocCert.IsNOC() {
		return nil, fmt.Errorf("%w: cert is not a NOC", ErrMalformed)
	}
	if err := v.checkValidity(nocCert); err != nil {
		return nil, fmt.Errorf("noc: %w", err)
	}
	if err := checkNOCShape(nocCert); err != nil {
		return nil, fmt.Errorf("noc: %w", err)
	}

	var issuerKey *ecdsa.PublicKey
	if len(icac) > 0 {
		icacCert, err := Decode(icac)
		if err != nil {
			return nil, fmt.Errorf("icac: %w", err)
		}
		if !icacCert.IsICA() {
			return nil, fmt.Errorf("%w: cert is not an ICAC", ErrMalformed)
		}
		if err := v.checkValidity(icacCert); err != nil {
			return nil, fmt.Errorf("icac: %w", err)
		}
		if err := checkICACShape(icacCert, nocCert); err != nil {
			return nil, fmt.Errorf("icac: %w", err)
		}
		// ICAC must be signed by the root.
		if err := verifySignature(icacCert, v.root); err != nil {
			return nil, fmt.Errorf("icac: %w", err)
		}
		// NOC must be signed by ICAC.
		icacPub, err := icacCert.PublicKeyECDSA()
		if err != nil {
			return nil, fmt.Errorf("icac: %w", err)
		}
		issuerKey = icacPub
		// Chain link check: NOC.Issuer.MatterICACID == ICAC.Subject.MatterICACID.
		if nocCert.Issuer.MatterICACID != icacCert.Subject.MatterICACID || !icacCert.Subject.HasICACID {
			return nil, fmt.Errorf("%w: NOC issuer ICAC-ID does not match ICAC subject", ErrChainBroken)
		}
	} else {
		// NOC is signed directly by the root.
		issuerKey = v.root
	}

	if err := verifySignature(nocCert, issuerKey); err != nil {
		return nil, fmt.Errorf("noc: %w", err)
	}
	return nocCert.PublicKeyECDSA()
}

// MaxOperationalCertTLVBytes caps a Matter operational certificate (NOC,
// ICAC, RCAC) in its TLV form — §6.1.3; mirrors matter.js
// OperationalBase.ts:19 MAX_TLV_BYTES.
const MaxOperationalCertTLVBytes = 400

// Operational node ids occupy 0x0000_0000_0000_0001 ..
// 0xFFFF_FFEF_FFFF_FFFF (§2.5.5); mirrors matter.js NodeId.isOperationalNodeId.
const maxOperationalNodeID = 0xFFFF_FFEF_FFFF_FFFF

// keyUsage bits as the Matter TLV cert carries them (RFC 5280 bit
// positions in a little-endian uint16 — matter.js ExtensionKeyUsageSchema).
const (
	keyUsageDigitalSignature   uint16 = 1 << 0
	keyUsageKeyCertSign        uint16 = 1 << 5
	keyUsageCRLSign            uint16 = 1 << 6
	extendedKeyUsageServerAuth uint8  = 1
	extendedKeyUsageClientAuth uint8  = 2
	maxCASEAuthTags                   = 3
)

// checkNOCShape applies the structural predicates a NOC must satisfy
// beyond carrying a node id and a fabric id. Mirrors matter.js
// packages/protocol/src/certificate/kinds/Noc.ts (validateFields + verify):
// not a CA, only the four operational identifiers in the subject, an
// operational node id, a non-zero fabric id, at most three CATs each with
// a non-zero version, keyUsage digitalSignature, extendedKeyUsage carrying
// serverAuth or clientAuth, and a 20-byte subjectKeyIdentifier. Every one
// of them is what keeps a cryptographically valid certificate from
// standing in for an identity it does not name.
func checkNOCShape(c *Certificate) error {
	if c.Subject.HasICACID || c.Subject.HasRCACID {
		return fmt.Errorf("%w: NOC subject must not carry an ICAC-ID or RCAC-ID", ErrMalformed)
	}
	if c.Subject.MatterNodeID == 0 || c.Subject.MatterNodeID > maxOperationalNodeID {
		return fmt.Errorf("%w: NOC node id 0x%016X is not an operational node id", ErrMalformed, c.Subject.MatterNodeID)
	}
	if c.Subject.MatterFabricID == 0 {
		return fmt.Errorf("%w: NOC fabric id must not be 0", ErrMalformed)
	}
	if len(c.Subject.CASEAuthTags) > maxCASEAuthTags {
		return fmt.Errorf("%w: NOC carries %d CATs, at most %d allowed", ErrMalformed, len(c.Subject.CASEAuthTags), maxCASEAuthTags)
	}
	seen := make(map[uint32]struct{}, len(c.Subject.CASEAuthTags))
	for _, cat := range c.Subject.CASEAuthTags {
		if cat&0xFFFF == 0 {
			return fmt.Errorf("%w: CAT 0x%08X has version 0", ErrMalformed, cat)
		}
		id := cat >> 16
		if _, dup := seen[id]; dup {
			return fmt.Errorf("%w: CAT identifier 0x%04X appears twice", ErrMalformed, id)
		}
		seen[id] = struct{}{}
	}
	ext := c.Extensions
	if ext.HasBasicConstraints && ext.BasicConstraintsIsCA {
		return fmt.Errorf("%w: NOC must not be a CA", ErrMalformed)
	}
	if !ext.HasKeyUsage || ext.KeyUsage&keyUsageDigitalSignature == 0 {
		return fmt.Errorf("%w: NOC keyUsage must carry digitalSignature", ErrMalformed)
	}
	if !ext.HasExtendedKeyUsage || !hasEKU(ext.ExtendedKeyUsage, extendedKeyUsageServerAuth, extendedKeyUsageClientAuth) {
		return fmt.Errorf("%w: NOC extendedKeyUsage must carry serverAuth or clientAuth", ErrMalformed)
	}
	if !ext.HasSubjectKeyID || len(ext.SubjectKeyID) != 20 {
		return fmt.Errorf("%w: NOC subjectKeyIdentifier must be 160 bit", ErrMalformed)
	}
	return nil
}

// checkICACShape applies the ICAC predicates of matter.js Icac.ts
// (validateFields + verify): a CA with keyCertSign and cRLSign (optionally
// digitalSignature), no extendedKeyUsage, no node id, no CATs, a non-zero
// fabric id when it carries one, and — the predicate with teeth — a fabric
// id equal to the NOC's when both carry one. Without that last check an
// ICAC minted for fabric A could issue a NOC for fabric B under the same
// root, and the responder would open the session on B.
func checkICACShape(icac, noc *Certificate) error {
	if icac.Subject.HasRCACID {
		return fmt.Errorf("%w: ICAC subject must not carry an RCAC-ID", ErrMalformed)
	}
	if len(icac.Subject.CASEAuthTags) > 0 {
		return fmt.Errorf("%w: ICAC must not carry CATs", ErrMalformed)
	}
	if icac.Subject.HasFabricID {
		if icac.Subject.MatterFabricID == 0 {
			return fmt.Errorf("%w: ICAC fabric id must not be 0", ErrMalformed)
		}
		if noc.Subject.HasFabricID && icac.Subject.MatterFabricID != noc.Subject.MatterFabricID {
			return fmt.Errorf("%w: ICAC fabric id 0x%016X does not match NOC fabric id 0x%016X",
				ErrChainBroken, icac.Subject.MatterFabricID, noc.Subject.MatterFabricID)
		}
	}
	ext := icac.Extensions
	if !ext.HasBasicConstraints || !ext.BasicConstraintsIsCA {
		return fmt.Errorf("%w: ICAC must be a CA", ErrMalformed)
	}
	if !ext.HasKeyUsage || ext.KeyUsage&^keyUsageDigitalSignature != keyUsageKeyCertSign|keyUsageCRLSign {
		return fmt.Errorf("%w: ICAC keyUsage must be keyCertSign and cRLSign (optionally digitalSignature), got %#04x", ErrMalformed, ext.KeyUsage)
	}
	if ext.HasExtendedKeyUsage && len(ext.ExtendedKeyUsage) > 0 {
		return fmt.Errorf("%w: ICAC must not carry an extendedKeyUsage", ErrMalformed)
	}
	if !ext.HasSubjectKeyID || len(ext.SubjectKeyID) != 20 {
		return fmt.Errorf("%w: ICAC subjectKeyIdentifier must be 160 bit", ErrMalformed)
	}
	return nil
}

func hasEKU(list []uint8, wanted ...uint8) bool {
	for _, have := range list {
		for _, w := range wanted {
			if have == w {
				return true
			}
		}
	}
	return false
}

// checkValidity confirms the certificate's NotBefore/NotAfter fields name
// representable times. It does NOT reject on the wall clock: matter.js
// OperationalBase.ts:82-89 only warns when notBefore lies in the future
// and never checks notAfter ("TODO: implement real checks when we add
// Last known Good UTC time"), and its own CA backdates every NOC by a
// year for the same reason — a bridge whose clock is behind the
// commissioner's (an RTC-less board before its first NTP sync, or a
// controller that mints the NOC with NotBefore = its own now) must not
// refuse AddNOC and every later CASE until the clock catches up. The
// spec's answer is Last-Known-Good-Time (§11.18.5); until that exists the
// gold standard's behaviour is the one to mirror. NotBefore / NotAfter
// are Matter-epoch seconds (offsets from 2000-01-01T00:00:00Z per
// §6.5.1.5).
func matterTimeRepresentable(matterSecs uint64) bool {
	const epoch = uint64(matterEpochUTCSeconds)
	return matterSecs <= ^uint64(0)-epoch
}

func (v *Verifier) checkValidity(c *Certificate) error {
	if !matterTimeRepresentable(c.NotBefore) {
		return fmt.Errorf("%w: NotBefore=%d does not name a representable time", ErrMalformed, c.NotBefore)
	}
	if c.NotAfter != 0 {
		if !matterTimeRepresentable(c.NotAfter) {
			return fmt.Errorf("%w: NotAfter=%d does not name a representable time", ErrMalformed, c.NotAfter)
		}
	}
	return nil
}

// verifySignature ECDSA-verifies the certificate's Signature field
// against issuerKey. Per Matter Core §6.5 / matter.js
// Certificate.verifyChain the signature covers the certificate's
// **X.509 DER form** with the signatureAlgorithm + signature
// stripped — i.e. the TBSCertificate. Hashing the raw Matter TLV
// (our previous approach) only validates self-issued test certs and
// rejects every real-world commissioner's NOC, including Apple Home.
func verifySignature(c *Certificate, issuerKey *ecdsa.PublicKey) error {
	if len(c.Signature) != 64 {
		return fmt.Errorf("%w: signature length=%d", ErrMalformed, len(c.Signature))
	}
	r := new(big.Int).SetBytes(c.Signature[:32])
	s := new(big.Int).SetBytes(c.Signature[32:])

	tbs, err := TBSToDER(c)
	if err != nil {
		return fmt.Errorf("rebuild tbs der: %w", err)
	}
	hash := sha256.Sum256(tbs)
	if !ecdsa.Verify(issuerKey, hash[:], r, s) {
		return ErrSignatureInvalid
	}
	return nil
}

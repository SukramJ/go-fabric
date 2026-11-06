// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/hex"
	"strings"
	"time"

	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// PAA is one trusted Product Attestation Authority — what matter.js's
// DclCertificateService keeps of a root certificate: its DER, whether it
// is a production root, and when it was fetched.
type PAA struct {
	// DER is the X.509 certificate.
	DER []byte
	// Production is false for a test or development root.
	Production bool
	// FetchedAt is when the store obtained the root; the zero time is
	// unknown and skips the PaaTrustStoreTimeMismatch check (DAV:253-266).
	FetchedAt time.Time
}

// PAAStore is the trust store [Validate] consults — the part of matter.js
// DclCertificateService the validator uses (DeviceAttestationValidator.ts:
// 321-486 at db021da0): getCertificate / getCertificateAsDer by subject key
// identifier with test certificates considered, acceptsTestCertificates,
// lookupCdSigner.
type PAAStore interface {
	// PAA returns the root whose subject key identifier is skid.
	PAA(skid []byte) (PAA, bool)
	// AcceptsTestCertificates reports whether a test root is trusted as
	// one; when false a test root is an error finding (DAV:243-251).
	AcceptsTestCertificates() bool
	// CDSigner returns the public key of the Certification Declaration
	// signer whose subject key identifier is skid. [Validate] asks only
	// for a signer that is not built in ([KnownCDSigner]); the answer is
	// trusted as a CSA-issued signer: a ledger-backed store answers only
	// with a certificate [VerifyCDSignerCertificate] accepts
	// (DclCertificateService.lookupCdSigner, dcl/DclCertificateService.ts:
	// 901-970 at db021da0), never with a PAA or PAI key; a [StaticStore]
	// answers with the keys its caller vouches for.
	CDSigner(skid []byte) (*ecdsa.PublicKey, bool)
}

// Revocations is the optional revocation check of a [PAAStore] —
// matter.js DclCertificateService.isRevoked(authorityKeyId, serialNumber,
// issuerDnHex) (DAV:342-363). A store without it reports nothing revoked.
type Revocations interface {
	// IsRevoked reports whether the certificate with serial (the DER
	// INTEGER content) issued under akid by the issuer whose DER DN is
	// issuerDN (uppercase hex) is revoked.
	IsRevoked(akid, serial []byte, issuerDN string) (bool, error)
}

// StaticStore is a [PAAStore] over a fixed set of roots and CD signers. Its
// CD signers are the caller's: unlike a DCL answer they are not checked
// against the CSA CD root, so the caller vouches that each is a CD signer.
type StaticStore struct {
	// PAAs are the trusted roots.
	PAAs []PAA
	// CDSigners maps a CD signer's subject key identifier, as uppercase
	// hex, to its public key.
	CDSigners map[string]*ecdsa.PublicKey
	// AcceptTest trusts test roots as such.
	AcceptTest bool
}

// PAA implements [PAAStore]: the first root whose certificate parses and
// carries skid as its subject key identifier.
func (s *StaticStore) PAA(skid []byte) (PAA, bool) {
	for _, p := range s.PAAs {
		c, err := x509.ParseCertificate(p.DER)
		if err == nil && bytes.Equal(c.SubjectKeyId, skid) {
			return p, true
		}
	}
	return PAA{}, false
}

// AcceptsTestCertificates implements [PAAStore].
func (s *StaticStore) AcceptsTestCertificates() bool { return s.AcceptTest }

// CDSigner implements [PAAStore].
func (s *StaticStore) CDSigner(skid []byte) (*ecdsa.PublicKey, bool) {
	k, ok := s.CDSigners[strings.ToUpper(hex.EncodeToString(skid))]
	return k, ok
}

// TestStore is the CSA test trust store: the two test PAAs of
// secure/attestation (the roots the reference node's chain and
// chip-tool's test chains end in) as test roots, and test certificates
// accepted, which also trusts the Appendix F test CD signer — built in
// like the CSA CD signing keys ([KnownCDSigner]), so the store lists no CD
// signer.
func TestStore() *StaticStore {
	return &StaticStore{
		PAAs: []PAA{
			{DER: secattestation.TestPAAFFF1Cert},
			{DER: secattestation.TestPAANoVIDCert},
		},
		AcceptTest: true,
	}
}

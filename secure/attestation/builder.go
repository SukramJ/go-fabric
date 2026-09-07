// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // G505: blocklisted import; the SKID derivation in computeSKID is the only use and states why SHA-1 is not ours to choose.
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"time"
)

// Matter Attestation Certificate Subject DN attributes
// (Matter §6.5.6.1).
//
// Both attributes are UTF8String holding the upper-case 4-hex-character
// representation of the unsigned 16-bit ID. Commissioners parse them
// out of the Subject DN to bind the certificate to a specific
// (vendor, product) pair — Apple Home rejects bridges whose VID/PID
// does not appear in the DAC.
var (
	oidMatterVendorID  = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 1}
	oidMatterProductID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 2}
)

// matterAttCertName composes the Subject DN for a Matter attestation
// certificate: a CommonName plus the matter-oid-vid attribute and
// optionally matter-oid-pid for DAC subjects.
//
// crypto/x509 only consults `ExtraNames` (not `Names`) when emitting
// a fresh DN — `Names` is the parsed surface. ExtraNames is therefore
// the single source of truth for the encoded order: CN, VID, [PID].
func matterAttCertName(cn string, vid, pid uint16, includePID bool) pkix.Name {
	extra := []pkix.AttributeTypeAndValue{
		{Type: oidCommonName, Value: cn},
		{Type: oidMatterVendorID, Value: fmt.Sprintf("%04X", vid)},
	}
	if includePID {
		extra = append(extra, pkix.AttributeTypeAndValue{
			Type: oidMatterProductID, Value: fmt.Sprintf("%04X", pid),
		})
	}
	return pkix.Name{ExtraNames: extra}
}

// oidCommonName is RFC 5280 id-at-commonName (2.5.4.3). Spelled out
// here so [matterAttCertName] can place it in the same ExtraNames
// slice as the Matter-specific OIDs and control the encoding order.
var oidCommonName = asn1.ObjectIdentifier{2, 5, 4, 3}

// computeSKID derives the 20-byte SubjectKeyIdentifier from an EC
// public key per the SHA-1(uncompressed point) convention used by
// chip-tool / matter.js. Apple's commissioner verifies the AKI on the
// child certificate matches this SKID byte-for-byte.
//
// The exact bytes are fixed by chip's certificate generator, which
// hashes the P256PublicKey's raw uncompressed point
// (connectedhomeip/src/credentials/GenerateChipX509Cert.cpp:103
// EncodeSubjectKeyIdentifierExtension, and :75 for the matching AKI).
// Both suppressions below keep this an exact reproduction of that
// input.
func computeSKID(pub *ecdsa.PublicKey) []byte {
	// SA1019 (elliptic.Marshal deprecated since Go 1.21) is suppressed
	// because the hash input has to be the bare 65-byte uncompressed
	// point, not a key object. The non-deprecated spelling,
	// pub.Bytes(), returns bytes plus an error that this signature has
	// nowhere to put, so replacing it is a caller-visible change rather
	// than a comment fix.
	raw := elliptic.Marshal(pub.Curve, pub.X, pub.Y) //nolint:staticcheck // SA1019: see above.
	// G401 (weak primitive) is suppressed because SHA-1 is used here to
	// derive an identifier, not to sign, MAC or detect tampering:
	// nothing downstream relies on it being collision-resistant. Nor is
	// the algorithm ours to pick — RFC 5280 §4.2.1.2 method (1)
	// prescribes it, and a commissioner byte-compares the child
	// certificate's AKI against this value, so any other hash makes the
	// chain unverifiable.
	sum := sha1.Sum(raw) //nolint:gosec // G401: see above.
	return sum[:]
}

// Chain bundles the materials the OperationalCredentials
// cluster surfaces during PASE: the DAC private key (signs
// AttestationResponse) plus the DAC and PAI certificates the cluster
// emits via CertificateChainResponse.
type Chain struct {
	DACKey *ecdsa.PrivateKey
	DAC    []byte
	PAI    []byte
}

// BuildTestChain returns a fresh DAC + PAI rooted at the embedded
// CSA Test PAA (VID 0xFFF1). The PAI is generated once per call with
// a random key and the requested vendor ID; the DAC is generated
// from a freshly-minted P-256 key with the given (vendor, product)
// pair. Apple Home, Google Home, and chip-tool all accept this chain
// without operator-supplied attestation material.
//
// The returned PAI key is discarded — only the certificate ships;
// signing capability stays with the bridge via the DAC key.
func BuildTestChain(vid, pid uint16) (*Chain, error) {
	paiKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("pai keygen: %w", err)
	}
	dacKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("dac keygen: %w", err)
	}

	// The PAI is rooted at the VID-less CSA test PAA, never at the
	// VID-0xFFF1 one: the attestation validator compares the PAA's
	// subject VID with the PAI's when the PAA carries one (matter.js
	// DeviceAttestationValidator.ts:328-334 VendorIdMismatch, Matter
	// §6.2.3.1 step 4), so a PAI whose subject VID is the operator's
	// vendor id under the FFF1 PAA fails attestation for every
	// vendor_id != 0xFFF1 — exactly the value a production operator is
	// told to set. matter.js's own device-side generator roots every PAI
	// at the NoVID PAA for this reason (AttestationCertificateManager.ts:
	// 38-44 #paaKeyPair / #paaKeyIdentifier, :121-123 issuer without
	// vendorId, :136-139 signed by the NoVID key).
	paaCert, err := x509.ParseCertificate(TestPAANoVIDCert)
	if err != nil {
		return nil, fmt.Errorf("parse PAA: %w", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	notBefore := now.Add(-time.Hour)
	notAfter := now.Add(20 * 365 * 24 * time.Hour) // 20 years.

	paiSerial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, fmt.Errorf("pai serial: %w", err)
	}
	paiSubject := matterAttCertName(fmt.Sprintf("Matter Dev PAI 0x%04X", vid), vid, 0, false)
	paiSKID := computeSKID(&paiKey.PublicKey)
	paiTmpl := &x509.Certificate{
		SerialNumber:          paiSerial,
		Subject:               paiSubject,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		SubjectKeyId:          paiSKID,
		AuthorityKeyId:        TestPAANoVIDSKID,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
	}
	paiDER, err := x509.CreateCertificate(rand.Reader, paiTmpl, paaCert, &paiKey.PublicKey, TestPAANoVIDPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("pai sign: %w", err)
	}
	// Re-parse to obtain an Issuer DN structure for the DAC template.
	paiCert, err := x509.ParseCertificate(paiDER)
	if err != nil {
		return nil, fmt.Errorf("re-parse PAI: %w", err)
	}

	dacSerial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, fmt.Errorf("dac serial: %w", err)
	}
	dacSubject := matterAttCertName(fmt.Sprintf("Matter Dev DAC 0x%04X/0x%04X", vid, pid), vid, pid, true)
	dacSKID := computeSKID(&dacKey.PublicKey)
	dacTmpl := &x509.Certificate{
		SerialNumber: dacSerial,
		Subject:      dacSubject,
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		// Matter §6.2.2.1 mandates Extended Key Usage = clientAuth on
		// the DAC. chip's DefaultDeviceAttestationVerifier verifies it
		// strictly in 2026; Apple's verifier currently tolerates the
		// absence but logs the gap. Mirror the spec to stay future-
		// proof against strict-mode controllers.
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		SubjectKeyId:          dacSKID,
		AuthorityKeyId:        paiSKID,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
	}
	dacDER, err := x509.CreateCertificate(rand.Reader, dacTmpl, paiCert, &dacKey.PublicKey, paiKey)
	if err != nil {
		return nil, fmt.Errorf("dac sign: %w", err)
	}
	return &Chain{
		DACKey: dacKey,
		DAC:    dacDER,
		PAI:    paiDER,
	}, nil
}

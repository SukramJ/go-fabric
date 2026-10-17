// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// The X.509 side of the trust store: what matter.js Paa.fromAsn1,
// Pai.fromAsn1 and Certificate.parseAsn1Certificate (packages/protocol/src/
// certificate/kinds/AttestationCertificates.ts:23-50, Certificate.ts at
// 0ceb202d) read of a trust-store, CRL signer or CD signer certificate,
// on crypto/x509. The subject's ids, the P-256 key and the signature check
// are controller/attestation's (MatterIDs, P256Key, VerifySignedBy).

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/SukramJ/go-fabric/controller/attestation"
)

// maxDERCertificateSize is MAX_DER_CERTIFICATE_SIZE (kinds/common.ts:13),
// the limit Certificate.parseAsn1Certificate applies (Certificate.ts:
// 617-622).
const maxDERCertificateSize = 600

// Kind is the kind of a trust-store entry — DclCertificateService.
// CertificateKind (DclCertificateService.ts:2089).
type Kind string

// The kinds.
const (
	KindPAA      Kind = "PAA"
	KindCDSigner Kind = "CDSigner"
)

// trustCert is a parsed trust-store or CRL signer certificate.
type trustCert struct {
	x509      *x509.Certificate
	vendorID  *uint16
	productID *uint16
}

// parseCert is Certificate.parseAsn1Certificate with requiredExtensions:
// the size limit, the DER parse and basicConstraints, keyUsage,
// subjectKeyIdentifier and — unless paa — authorityKeyIdentifier
// (Certificate.ts:397-401, :549-551), then the subject's Matter vendor and
// product id (Certificate.ts:345-352, :379-392).
func parseCert(der []byte, paa bool) (*trustCert, error) {
	if len(der) > maxDERCertificateSize {
		return nil, fmt.Errorf("DER certificate of %d bytes exceeds the %d byte limit", len(der), maxDERCertificateSize)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	if !c.BasicConstraintsValid || c.KeyUsage == 0 || len(c.SubjectKeyId) == 0 || !paa && len(c.AuthorityKeyId) == 0 {
		return nil, errors.New("Missing required extensions in certificate") //nolint:staticcheck // matter.js's message
	}
	t := &trustCert{x509: c}
	if t.vendorID, t.productID, err = attestation.MatterIDs(c); err != nil {
		return nil, err
	}
	return t, nil
}

// parseTrustStoreCertificate is DclCertificateService.ts:48-52: a PAA as
// Paa.fromAsn1, a CD signer as Certificate.parseAsn1Certificate with the
// required extensions.
func parseTrustStoreCertificate(der []byte, kind Kind) (*trustCert, error) {
	return parseCert(der, kind == KindPAA)
}

// pemToDER is Pem.asDer for text (packages/general/src/crypto/Pem.ts:
// 35-73): the base64 between the first "-----BEGIN " line and the next
// "-----END " line, whitespace removed.
func pemToDER(pem string) ([]byte, error) {
	lines := strings.Split(pem, "\n")
	start := 0
	for start < len(lines) && !strings.HasPrefix(lines[start], "-----BEGIN ") {
		start++
	}
	if start >= len(lines) {
		return nil, errors.New("No BEGIN line in PEM file") //nolint:staticcheck // matter.js's message
	}
	end := start + 1
	for end < len(lines) && !strings.HasPrefix(lines[end], "-----END ") {
		end++
	}
	if end >= len(lines) {
		return nil, errors.New("No END line in PEM file") //nolint:staticcheck // matter.js's message
	}
	b64 := strings.Join(strings.Fields(strings.Join(lines[start+1:end], "")), "")
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("Error in PEM base64 encoding: %w", err) //nolint:staticcheck // matter.js's message
	}
	return der, nil
}

// PEMToDER is matter.js Pem.asDer for a certificate the ledger returns PEM
// encoded (Certificate.PEMCert) — packages/general/src/crypto/Pem.ts:35-73.
func PEMToDER(pem string) ([]byte, error) { return pemToDER(pem) }

// ErrSKIDMismatch is a certificate whose own subject key identifier is not
// the one its source states for it.
var ErrSKIDMismatch = errors.New("dcl: subject key identifier mismatch")

// CheckTrustStoreCertificate is #parseAndCheckSkid (DclCertificateService.ts:
// 492-515) for a source outside the store, a snapshot: der parses as kind
// (parseTrustStoreCertificate, :48-52) and carries statedSKID — hex, colons
// allowed, any case — as its own subject key identifier, else
// [ErrSKIDMismatch]. It returns the parsed certificate.
func CheckTrustStoreCertificate(der []byte, kind Kind, statedSKID string) (*x509.Certificate, error) {
	c, err := parseTrustStoreCertificate(der, kind)
	if err != nil {
		return nil, err
	}
	if derived, stated := hexUpper(c.x509.SubjectKeyId), normalizeKeyID(statedSKID); derived != stated {
		return nil, fmt.Errorf("%w: entry=%s der-derived=%s", ErrSKIDMismatch, stated, derived)
	}
	return c.x509, nil
}

// normalizeKeyID is #normalizeSubjectKeyId (DclCertificateService.ts:
// 368-373): uppercase hex without colons.
func normalizeKeyID(s string) string { return strings.ToUpper(strings.ReplaceAll(s, ":", "")) }

// serialContent is a certificate's serialNumber as matter.js keeps it: the
// DER INTEGER content (Certificate.ts:650), a leading zero byte when the
// top bit is set.
func serialContent(n *big.Int) []byte {
	b := n.Bytes()
	if len(b) == 0 || b[0]&0x80 != 0 {
		b = append([]byte{0}, b...)
	}
	return b
}

// hexUpper is Bytes.toHex(…).toUpperCase().
func hexUpper(b []byte) string { return strings.ToUpper(hex.EncodeToString(b)) }

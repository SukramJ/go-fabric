// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// The X.509 side of the attestation certificates: what matter.js
// Certificate.parseAsn1Certificate (packages/protocol/src/certificate/
// kinds/Certificate.ts at 0ceb202d) reads of a DAC, PAI or PAA that the
// validator uses, on top of crypto/x509's parser.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxDERCertificateSize is matter.js MAX_DER_CERTIFICATE_SIZE
// (kinds/common.ts:13), the limit of a traditional certificate
// (Certificate.ts:617-622, :752-757).
const maxDERCertificateSize = 600

// matterEpochOffset is matter.js MATTER_EPOCH_OFFSET_S, the Unix time of
// 2000-01-01T00:00:00Z.
const matterEpochOffset int64 = 946684800

// The Matter attestation DN attributes: matter-oid-vid and matter-oid-pid
// (Matter §6.2.2.2; secure/attestation/builder.go:39-50 writes them).
var (
	oidMatterVendorID  = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 1}
	oidMatterProductID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 2}
)

// nonWellDefined is X520.NON_WELL_DEFINED_DATE, 99991231235959Z, which
// matter.js reads as 0 (Certificate.ts:589-593).
var nonWellDefined = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

// attCert is a parsed attestation certificate.
type attCert struct {
	x509      *x509.Certificate
	vendorID  *uint16 // nil when the DN carries none
	productID *uint16
	notBefore int64 // Matter epoch seconds, 0 for the non-well-defined date
	notAfter  int64
}

// requiredExtensions are Certificate.REQUIRED_PAA_EXTENSIONS and
// REQUIRED_EXTENSIONS (Certificate.ts:397-401): basicConstraints,
// keyUsage and subjectKeyIdentifier, plus authorityKeyIdentifier for a
// PAI and a DAC; a missing one is a CertificateError (:549-551).
func checkRequiredExtensions(c *x509.Certificate, authorityKeyID bool) error {
	switch {
	case !c.BasicConstraintsValid:
		return errors.New("attestation: missing required extension basicConstraints")
	case c.KeyUsage == 0:
		return errors.New("attestation: missing required extension keyUsage")
	case len(c.SubjectKeyId) == 0:
		return errors.New("attestation: missing required extension subjectKeyIdentifier")
	case authorityKeyID && len(c.AuthorityKeyId) == 0:
		return errors.New("attestation: missing required extension authorityKeyIdentifier")
	}
	return nil
}

// parseAttCert is Paa/Pai/Dac.fromAsn1 (kinds/AttestationCertificates.ts:
// 34-72): the DER parse with the size limit and the required extensions,
// the Matter vendor and product id of the subject, the validity in Matter
// epoch seconds. ML-DSA certificates (PQC Phase 1, disabled by default,
// :22-32) do not parse here.
func parseAttCert(der []byte, authorityKeyID bool) (*attCert, error) {
	if len(der) > maxDERCertificateSize {
		return nil, fmt.Errorf("attestation: DER certificate of %d bytes exceeds the %d byte limit", len(der), maxDERCertificateSize)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	if err := checkRequiredExtensions(c, authorityKeyID); err != nil {
		return nil, err
	}
	a := &attCert{x509: c, notBefore: matterTime(c.NotBefore), notAfter: matterTime(c.NotAfter)}
	if a.vendorID, a.productID, err = MatterIDs(c); err != nil {
		return nil, err
	}
	return a, nil
}

// matterTime is Certificate.ts:587-596 (parseDate): 0 for the
// non-well-defined date, else Matter epoch seconds.
func matterTime(t time.Time) int64 {
	if t.Equal(nonWellDefined) {
		return 0
	}
	return t.Unix() - matterEpochOffset
}

// MatterIDs reads the vendor and product id of the subject DN —
// Certificate.ts:345-352 (the OID attributes, hex strings parsed with
// parseInt(_, 16)) and :379-392 (the Mvid:/Mpid: commonName fallback,
// only when the DN carries neither OID).
func MatterIDs(c *x509.Certificate) (vid, pid *uint16, err error) {
	for _, n := range c.Subject.Names {
		s, ok := n.Value.(string)
		switch {
		case !ok:
			continue
		case n.Type.Equal(oidMatterVendorID):
			vid = parseHexID(s)
		case n.Type.Equal(oidMatterProductID):
			pid = parseHexID(s)
		}
	}
	if vid != nil || pid != nil {
		return vid, pid, nil
	}
	cn := c.Subject.CommonName
	if vid, err = fallbackID(cn, "Mvid:"); err != nil {
		return nil, nil, err
	}
	if pid, err = fallbackID(cn, "Mpid:"); err != nil {
		return nil, nil, err
	}
	return vid, pid, nil
}

// parseHexID is parseInt(hexString, 16) as matter.js applies it to a
// vendor or product id attribute (Certificate.ts:345-352): the leading
// hex digits; a value without one is absent. A value above 16 bits is
// absent here too — matter.js keeps the number, which then matches no
// 16-bit id either.
func parseHexID(s string) *uint16 {
	end := 0
	for end < len(s) && strings.ContainsRune("0123456789abcdefABCDEF", rune(s[end])) {
		end++
	}
	if end == 0 {
		return nil
	}
	v, err := strconv.ParseUint(s[:end], 16, 16)
	if err != nil {
		return nil
	}
	id := uint16(v)
	return &id
}

var fallbackPatterns = map[string]*regexp.Regexp{
	"Mvid:": regexp.MustCompile(`Mvid:([0-9A-F]{4})`),
	"Mpid:": regexp.MustCompile(`Mpid:([0-9A-F]{4})`),
}

// fallbackID is parseMatterFallbackVidPid (Certificate.ts:249-258): the
// leftmost prefix followed by 4 uppercase hex digits; the prefix without
// them is an error.
func fallbackID(cn, prefix string) (*uint16, error) {
	if m := fallbackPatterns[prefix].FindStringSubmatch(cn); m != nil {
		v, _ := strconv.ParseUint(m[1], 16, 16)
		id := uint16(v)
		return &id, nil
	}
	if strings.Contains(cn, prefix) {
		return nil, fmt.Errorf("attestation: commonName contains %s not followed by 4 uppercase hex digits", prefix)
	}
	return nil, nil
}

// P256Key is the certificate's key when it is EC P-256 — matter.js's
// "ECDSA-P256" key algorithm (kinds/CertificateSignature.ts:55-57).
func P256Key(c *x509.Certificate) (*ecdsa.PublicKey, error) {
	k, ok := c.PublicKey.(*ecdsa.PublicKey)
	if !ok || k.Curve != elliptic.P256() {
		return nil, errors.New("attestation: public key is not EC P-256")
	}
	return k, nil
}

// VerifySignedBy is Certificate.verifySignature with an ECDSA P-256 signer
// (kinds/CertificateSignature.ts:101-120): the certificate's
// ecdsa-with-SHA256 signature over its TBS DER verifies with key.
func VerifySignedBy(c *x509.Certificate, key *ecdsa.PublicKey) error {
	if c.SignatureAlgorithm != x509.ECDSAWithSHA256 {
		return fmt.Errorf("attestation: signature algorithm %v is not ecdsa-with-SHA256", c.SignatureAlgorithm)
	}
	digest := sha256.Sum256(c.RawTBSCertificate)
	if !ecdsa.VerifyASN1(key, digest[:], c.Signature) {
		return errors.New("attestation: signature does not verify")
	}
	return nil
}

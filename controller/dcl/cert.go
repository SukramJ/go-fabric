// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// The X.509 side of the trust store: what matter.js Paa.fromAsn1,
// Pai.fromAsn1 and Certificate.parseAsn1Certificate (packages/protocol/src/
// certificate/kinds/AttestationCertificates.ts:23-50, Certificate.ts at
// 0ceb202d) read of a trust-store, CRL signer or CD signer certificate,
// on crypto/x509. The same reading as controller/attestation's
// (cert.go there), which keeps it unexported.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// maxDERCertificateSize is MAX_DER_CERTIFICATE_SIZE (kinds/common.ts:13),
// the limit Certificate.parseAsn1Certificate applies (Certificate.ts:
// 617-622).
const maxDERCertificateSize = 600

var (
	oidMatterVendorID  = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 1}
	oidMatterProductID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 2, 2}
)

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
	if t.vendorID, t.productID, err = matterIDs(c); err != nil {
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

// matterIDs reads the vendor and product id of the subject DN: the OID
// attributes as hex (parseInt(_, 16)), and the Mvid:/Mpid: commonName
// fallback only when the DN carries neither (Certificate.ts:345-352,
// :379-392, parseMatterFallbackVidPid :249-258).
func matterIDs(c *x509.Certificate) (vid, pid *uint16, err error) {
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

func fallbackID(cn, prefix string) (*uint16, error) {
	if m := fallbackPatterns[prefix].FindStringSubmatch(cn); m != nil {
		v, _ := strconv.ParseUint(m[1], 16, 16)
		id := uint16(v)
		return &id, nil
	}
	if strings.Contains(cn, prefix) {
		return nil, fmt.Errorf("commonName contains %s not followed by 4 uppercase hex digits", prefix)
	}
	return nil, nil
}

// p256Key is the certificate's key when it is EC P-256, matter.js's
// "ECDSA-P256" (kinds/CertificateSignature.ts:55-57).
func p256Key(c *x509.Certificate) (*ecdsa.PublicKey, error) {
	k, ok := c.PublicKey.(*ecdsa.PublicKey)
	if !ok || k.Curve != elliptic.P256() {
		return nil, errors.New("public key is not EC P-256")
	}
	return k, nil
}

// verifySignedBy is Certificate.verifySignature with an ECDSA P-256 key
// (kinds/CertificateSignature.ts:101-120): the ecdsa-with-SHA256
// signature over the TBS DER verifies with key.
func verifySignedBy(c *x509.Certificate, key *ecdsa.PublicKey) error {
	if c.SignatureAlgorithm != x509.ECDSAWithSHA256 {
		return fmt.Errorf("signature algorithm %v is not ecdsa-with-SHA256", c.SignatureAlgorithm)
	}
	digest := sha256.Sum256(c.RawTBSCertificate)
	if !ecdsa.VerifyASN1(key, digest[:], c.Signature) {
		return errors.New("signature does not verify")
	}
	return nil
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

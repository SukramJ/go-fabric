// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// The X.509 side of the attestation certificates: what matter.js
// Certificate.parseAsn1Certificate (packages/protocol/src/certificate/
// kinds/Certificate.ts at db021da0) reads of a DAC, PAI or PAA that the
// validator uses, on top of crypto/x509's parser; the profile is
// profile.go.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/internal/forward"
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

// parseAttCert is Paa/Pai/Dac.fromAsn1 (kinds/AttestationCertificates.ts:
// 50-233 at db021da0): the DER parse with the size limit and the required
// extensions of the kind, the Matter vendor and product id of the subject,
// the validity in Matter epoch seconds; a DAC's key is EC P-256 (the Dac
// constructor, :235-247 at db021da0), and a DAC or PAI is held to its
// profile (:107-196 at db021da0), a violation being a [*profileError]. It
// parses with PQC Phase 1's size limit (parseAttestationCertificate,
// :50-63 at db021da0); a traditional certificate stays bound to 600 bytes,
// and one that uses ML-DSA while the feature is off is
// [ErrMLDSAAttestationDisabled] (pqc.go). The PAA profile is not checked.
//
// acceptProfileViolation is AttestationParseOptions.acceptProfileViolation
// (:29-48 at db021da0): no profile check, and basic constraints not
// required of a PAA either; key usage and the key identifiers stay
// required, as a certificate without them is unusable.
func parseAttCert(der []byte, kind certKind, acceptProfileViolation bool) (*attCert, error) {
	if len(der) > maxPQCDERCertificateSize {
		return nil, fmt.Errorf("attestation: DER certificate of %d bytes exceeds the %d byte limit", len(der), maxPQCDERCertificateSize)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		// crypto/x509 refuses a certificate that carries an extension
		// twice (RFC 5280 §4.2) or a version field above 2, which
		// matter.js parses and refuses in the DAC and PAI profile
		// (AttestationCertificates.ts:122-124, :130-139 at db021da0) — after
		// the required extensions of the parse (Certificate.ts:589-593),
		// which come first here too.
		if (kind == kindDAC || kind == kindPAI) && !acceptProfileViolation {
			if enc, encErr := readEncoding(der); encErr == nil {
				if reqErr := checkRequiredExtensions(kind, false, &enc); reqErr != nil {
					return nil, reqErr
				}
				if !enc.isV3() || enc.hasDuplicateSingleExtension() {
					return nil, assertCommonProfile(kind, &enc)
				}
			}
		}
		return nil, err
	}
	// The issuer's and the subject's vendor and product id are read as
	// the DER parse reads them, issuer first, before the required
	// extensions and the traditional size limit (Certificate.ts:729, :740,
	// :792, :812 at db021da0) and before the ML-DSA check of
	// parseAttestationCertificate (AttestationCertificates.ts:56-61).
	if _, _, err := IssuerMatterIDs(c); err != nil {
		return nil, err
	}
	vid, pid, err := MatterIDs(c)
	if err != nil {
		return nil, err
	}
	switch {
	case usesMLDSA(c) && !forward.Enabled(forward.PQCPhase1):
		return nil, fmt.Errorf("%w: this certificate uses %v", ErrMLDSAAttestationDisabled, c.SignatureAlgorithm)
	case !usesMLDSA(c) && len(der) > maxDERCertificateSize:
		return nil, fmt.Errorf("attestation: DER certificate of %d bytes exceeds the %d byte limit", len(der), maxDERCertificateSize)
	}
	enc, err := readEncoding(der)
	if err != nil {
		return nil, err
	}
	if err := checkRequiredExtensions(kind, acceptProfileViolation, &enc); err != nil {
		return nil, err
	}
	// A PAA or CD signer below v3 carries no extensions crypto/x509 reads
	// (RFC 5280 §4.1.2.9), so its key identifiers would stay empty and the
	// chain fail later with a misleading mismatch; refuse it here. A DAC or
	// PAI gets the profile's version rule below.
	if (kind == kindPAA || kind == kindCDSigner) && !enc.isV3() {
		return nil, fmt.Errorf("attestation: %s version field must be 2 (v3), not %s", kind, enc.version)
	}
	a := &attCert{x509: c, vendorID: vid, productID: pid, notBefore: matterTime(c.NotBefore), notAfter: matterTime(c.NotAfter)}
	switch kind {
	case kindDAC:
		if _, err := P256Key(c); err != nil {
			return nil, fmt.Errorf("attestation: DAC public key must be EC P-256: %w", err)
		}
		if !acceptProfileViolation {
			err = assertDacProfile(&enc)
		}
	case kindPAI:
		if !acceptProfileViolation {
			err = assertPaiProfile(&enc)
		}
	case kindPAA, kindCDSigner:
	}
	if err != nil {
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

// MatterIDs reads the vendor and product id of the subject DN by
// matter.js's rule for a parse without acceptMalformedVidPid (db021da0;
// the rule is [matterIDsOf]'s).
func MatterIDs(c *x509.Certificate) (vid, pid *uint16, err error) {
	return matterIDsOf(c.Subject)
}

// IssuerMatterIDs reads the vendor and product id of the issuer DN the
// same way — matter.js parses the issuer with parseSubjectOrIssuer too
// (Certificate.ts:729 at db021da0), so a DAC whose PAI carries its vendor
// id only in a `Mvid:` commonName still names its issuer's vendor
// (CertificatesTest.ts:319-324, the Nuki DAC).
func IssuerMatterIDs(c *x509.Certificate) (vid, pid *uint16, err error) {
	return matterIDsOf(c.Issuer)
}

// oidCommonName is X.520 commonName (2.5.4.3).
var oidCommonName = asn1.ObjectIdentifier{2, 5, 4, 3}

// matterIDsOf is the vendor and product id of one DN, as
// parseSubjectOrIssuer and decodeDistinguishedName read them without
// acceptMalformedVidPid (Certificate.ts:302-382, DistinguishedName.ts
// :252-328 at db021da0):
//
//   - a matter-oid-vid or matter-oid-pid attribute must be exactly 4
//     uppercase hex digits (parseMatterRdnHex, Certificate.ts:242-249,
//     :341-349) and occur once (DistinguishedName.ts:261-270, :277-281);
//     either violation is a [*profileError], CertificateProfileError;
//   - the commonName fallback is read only when the DN carries neither
//     attribute, a malformed one counting (Certificate.ts:343, :377-381):
//     the first commonName, in DN order, that carries a VendorID or
//     ProductID supplies both, searched in its first 64 bytes
//     (DistinguishedName.ts:345-374).
func matterIDsOf(name pkix.Name) (vid, pid *uint16, err error) {
	hasAttribute := false
	var commonNames []string
	for _, n := range name.Names {
		var field string
		var out **uint16
		switch {
		case n.Type.Equal(oidMatterVendorID):
			field, out = "vendorId", &vid
		case n.Type.Equal(oidMatterProductID):
			field, out = "productId", &pid
		case n.Type.Equal(oidCommonName):
			if s, ok := n.Value.(string); ok {
				commonNames = append(commonNames, s)
			}
			continue
		default:
			continue
		}
		hasAttribute = true
		s, _ := n.Value.(string)
		id, ok := parseHexID(s)
		if !ok {
			return nil, nil, profileErrorf("Invalid %s %q: expected exactly 4 uppercase hexadecimal digits", field, s)
		}
		if *out != nil {
			return nil, nil, profileErrorf("Distinguished name contains %s more than once", field)
		}
		*out = &id
	}
	if hasAttribute {
		return vid, pid, nil
	}
	return fallbackIDs(commonNames)
}

// maxFallbackCommonNameBytes is MAX_FALLBACK_COMMON_NAME_BYTES
// (DistinguishedName.ts:345-346 at db021da0).
const maxFallbackCommonNameBytes = 64

// fallbackIDs is fallbackVidPid (DistinguishedName.ts:354-374 at
// db021da0): the first commonName whose first 64 bytes carry a VendorID
// or ProductID supplies both; a later one is not read, an earlier one with
// a malformed prefix refuses the DN.
func fallbackIDs(commonNames []string) (vid, pid *uint16, err error) {
	for _, cn := range commonNames {
		if len(cn) > maxFallbackCommonNameBytes {
			cn = cn[:maxFallbackCommonNameBytes]
		}
		if vid, err = fallbackID(cn, "Mvid:"); err != nil {
			return nil, nil, err
		}
		if pid, err = fallbackID(cn, "Mpid:"); err != nil {
			return nil, nil, err
		}
		if vid != nil || pid != nil {
			return vid, pid, nil
		}
	}
	return nil, nil, nil
}

// parseHexID is parseMatterRdnHex(field, text, 2) (Certificate.ts:242-249
// at db021da0): exactly 4 uppercase hex digits, nothing else.
func parseHexID(s string) (uint16, bool) {
	if !hexIDPattern.MatchString(s) {
		return 0, false
	}
	v, _ := strconv.ParseUint(s, 16, 16)
	return uint16(v), true
}

var hexIDPattern = regexp.MustCompile(`^[0-9A-F]{4}$`)

var fallbackPatterns = map[string]*regexp.Regexp{
	"Mvid:": regexp.MustCompile(`Mvid:([0-9A-F]{4})`),
	"Mpid:": regexp.MustCompile(`Mpid:([0-9A-F]{4})`),
}

// fallbackID is parseMatterFallbackVidPid (DistinguishedName.ts:63-72 at
// db021da0): the leftmost prefix followed by 4 uppercase hex digits; the
// prefix without them is an error.
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

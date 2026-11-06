// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// The DAC and PAI certificate profile (Matter Core §6.2.2.3, §6.2.2.4) as
// matter.js enforces it: what the DER of a certificate states beyond its
// values — the version, each interpreted extension with its criticality,
// all key usage bits — read as matter.js Certificate.parseExtensions keeps
// it in MatterCertificate.encoding (kinds/Certificate.ts:385-596 and
// :698-706, kinds/definitions/base.ts:78-104 at db021da0), and the rules
// Dac.fromAsn1 and Pai.fromAsn1 check against it
// (kinds/AttestationCertificates.ts:65-233 at db021da0). crypto/x509 reads
// the rest of the certificate; it drops the version's consequences (a v2
// certificate's extensions are not read) and does not keep bits beyond
// decipherOnly, so the profile reads the DER itself.

import (
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
)

// certKind is the attestation certificate a DER is parsed as.
type certKind uint8

const (
	kindPAA certKind = iota
	kindPAI
	kindDAC
	// kindCDSigner is a CD signer certificate (cdsigners.go), which
	// CdSigners.verifyCertificate parses with all of
	// Certificate.REQUIRED_EXTENSIONS (CdSigners.ts:104-105 at db021da0).
	kindCDSigner
)

func (k certKind) String() string {
	switch k {
	case kindPAI:
		return "PAI"
	case kindDAC:
		return "DAC"
	case kindCDSigner:
		return "CD signer"
	default:
		return "PAA"
	}
}

// profileError is matter.js CertificateProfileError: the certificate parses
// but breaks the X.509 profile of its kind. The validator reports it as
// DacFormatInvalid or PaiFormatInvalid (DeviceAttestationValidator.ts:
// 160-194 at db021da0).
type profileError struct{ msg string }

func (e *profileError) Error() string { return e.msg }

func profileErrorf(format string, args ...any) error {
	return &profileError{msg: fmt.Sprintf(format, args...)}
}

// The interpreted extensions, by name as MatterCertificate.ExtensionName
// spells them (Certificate.ts:465-484 at db021da0).
const (
	extBasicConstraints = "basicConstraints"
	extKeyUsage         = "keyUsage"
	extExtendedKeyUsage = "extendedKeyUsage"
	extSubjectKeyID     = "subjectKeyIdentifier"
	extAuthorityKeyID   = "authorityKeyIdentifier"
)

// interpretedExtensions maps an extension OID's content octets to its name:
// 2.5.29.19, .15, .37, .14 and .35.
var interpretedExtensions = map[string]string{
	"\x55\x1d\x13": extBasicConstraints,
	"\x55\x1d\x0f": extKeyUsage,
	"\x55\x1d\x25": extExtendedKeyUsage,
	"\x55\x1d\x0e": extSubjectKeyID,
	"\x55\x1d\x23": extAuthorityKeyID,
}

// The DER identifier octets the encoding reader tests.
const (
	derBooleanTag     = 0x01
	derIntegerTag     = 0x02
	derOctetStringTag = 0x04
)

// extensionEncoding is one interpreted extension as the DER carries it.
type extensionEncoding struct {
	name     string
	critical bool
}

// certEncoding is MatterCertificate.Encoding (kinds/definitions/base.ts:
// 88-104 at db021da0) with the basic constraints values
// (Certificate.ts:496-513 at db021da0).
type certEncoding struct {
	// version is the X.509 version field; 2 means v3, an absent field 0.
	version *big.Int
	// extensions are the interpreted extensions in encoding order,
	// duplicates included.
	extensions []extensionEncoding
	// keyUsage holds all key usage bits, bit n as X.509 numbers it, a set
	// bit beyond 15 as bit 16; nil without the extension.
	keyUsage *uint32
	// keyUsageBytes is the length of the key usage BIT STRING without its
	// padding byte; 0 leaves matter.js's keyUsage value undefined.
	keyUsageBytes int
	// isCA and pathLen are the last basic constraints extension's values;
	// without one, not a CA.
	isCA    bool
	pathLen *big.Int
	// skid and akid are the key identifiers as matter.js reads them
	// (Certificate.ts:573-585 at db021da0), nil when it reads none; the
	// encoding reader keeps them because crypto/x509 reads no extension of
	// a v1 or v2 certificate.
	skid, akid []byte
}

// has reports whether the DER carries the extension.
func (e *certEncoding) has(name string) bool {
	return slices.ContainsFunc(e.extensions, func(x extensionEncoding) bool { return x.name == name })
}

// count is how often the DER carries the extension.
func (e *certEncoding) count(name string) int {
	n := 0
	for _, x := range e.extensions {
		if x.name == name {
			n++
		}
	}
	return n
}

// critical reports whether the first instance of the extension is marked
// critical — extensions.find(...)?.critical (AttestationCertificates.ts:
// 130-139 at db021da0).
func (e *certEncoding) critical(name string) bool {
	for _, x := range e.extensions {
		if x.name == name {
			return x.critical
		}
	}
	return false
}

// isV3 reports a version field of 2 (an X.509 v3 certificate).
func (e *certEncoding) isV3() bool { return e.version != nil && e.version.Cmp(big.NewInt(2)) == 0 }

// readEncoding reads a certificate's encoding — the version
// parseAsn1Certificate reads (Certificate.ts:667-706 at db021da0) and parseExtensions
// (:431-596 at db021da0): an extension it does not interpret is skipped
// whatever its encoding, an interpreted one needs its value OCTET STRING,
// a critical flag or cA is a one-byte BOOLEAN, a pathLen an INTEGER with
// content.
func readEncoding(der []byte) (certEncoding, error) {
	cert, err := decodeDER(der)
	if err != nil {
		return certEncoding{}, err
	}
	if len(cert.elements) < 1 {
		return certEncoding{}, errors.New("attestation: invalid certificate structure")
	}
	tbs := cert.elements[0].elements
	if len(tbs) < 7 {
		return certEncoding{}, errors.New("attestation: invalid TBSCertificate structure")
	}
	enc := certEncoding{version: new(big.Int)}
	idx := 0
	if tbs[0].tag == 0xa0 {
		if len(tbs[0].elements) == 0 || tbs[0].elements[0].tag != derIntegerTag {
			return certEncoding{}, errors.New("attestation: invalid certificate version structure")
		}
		if enc.version, err = derUnsignedInteger(tbs[0].elements[0], "Certificate version"); err != nil {
			return certEncoding{}, err
		}
		idx++
	}
	// Serial, signature algorithm, issuer, validity, subject, public key,
	// then the extensions [3] (Certificate.ts:708-792 at db021da0).
	idx += 6
	if idx >= len(tbs) || tbs[idx].tag != 0xa3 || len(tbs[idx].elements) == 0 {
		return certEncoding{}, errors.New("attestation: missing required extensions in certificate")
	}
	for _, ext := range tbs[idx].elements[0].elements {
		if err := enc.readExtension(ext); err != nil {
			return certEncoding{}, err
		}
	}
	return enc, nil
}

// readExtension is one pass of parseExtensions' loop (Certificate.ts:
// 450-586 at db021da0).
func (e *certEncoding) readExtension(ext derNode) error {
	if len(ext.elements) < 2 {
		return nil
	}
	oid := ext.elements[0].bytes
	valueIndex, critical := 1, false
	if len(ext.elements) > 2 && ext.elements[1].tag == derBooleanTag {
		valueIndex = 2
		var err error
		if critical, err = derBoolean(ext.elements[1], fmt.Sprintf("Critical flag of extension %x", oid)); err != nil {
			return err
		}
	}
	name, ok := interpretedExtensions[string(oid)]
	if !ok {
		return nil
	}
	value := ext.elements[valueIndex]
	if value.tag != derOctetStringTag {
		return fmt.Errorf("attestation: extension %x is missing its value octet string", oid)
	}
	e.extensions = append(e.extensions, extensionEncoding{name: name, critical: critical})
	node, err := decodeDER(value.bytes)
	if err != nil {
		return err
	}
	switch name {
	case extBasicConstraints:
		// RFC 5280 §4.2.1.9: SEQUENCE { cA BOOLEAN DEFAULT FALSE,
		// pathLenConstraint INTEGER OPTIONAL }; pathLen is read also when
		// cA is omitted.
		isCA, i := false, 0
		var pathLen *big.Int
		if i < len(node.elements) && node.elements[i].tag == derBooleanTag {
			if isCA, err = derBoolean(node.elements[i], "Basic constraints cA"); err != nil {
				return err
			}
			i++
		}
		if i < len(node.elements) && node.elements[i].tag == derIntegerTag {
			if pathLen, err = derUnsignedInteger(node.elements[i], "Basic constraints pathLenConstraint"); err != nil {
				return err
			}
		}
		e.isCA, e.pathLen = isCA, pathLen
	case extKeyUsage:
		// The BIT STRING's content without its padding byte.
		bits := node.bytes
		if len(bits) > 0 {
			bits = bits[1:]
		}
		v := keyUsageBits(bits)
		e.keyUsage, e.keyUsageBytes = &v, len(bits)
	case extSubjectKeyID:
		e.skid = append([]byte{}, node.bytes...)
	case extAuthorityKeyID:
		if len(node.elements) > 0 {
			e.akid = append([]byte{}, node.elements[0].bytes...)
		}
	}
	return nil
}

// derBoolean is Certificate.ts:390-397 at db021da0: exactly one content
// byte, any non-zero value true.
func derBoolean(n derNode, field string) (bool, error) {
	if len(n.bytes) != 1 {
		return false, fmt.Errorf("attestation: %s is a BOOLEAN of %d bytes", field, len(n.bytes))
	}
	return n.bytes[0] != 0, nil
}

// derUnsignedInteger is Certificate.ts:399-406 at db021da0: the content
// bytes as an unsigned big-endian number; none is an error.
func derUnsignedInteger(n derNode, field string) (*big.Int, error) {
	if len(n.bytes) == 0 {
		return nil, fmt.Errorf("attestation: %s is an INTEGER without content", field)
	}
	return new(big.Int).SetBytes(n.bytes), nil
}

// keyUsageBits is Certificate.ts:408-423 at db021da0: bit n as X.509
// numbers it (0 is digitalSignature, 8 decipherOnly), a set bit beyond 15
// as bit 16.
func keyUsageBits(bitString []byte) uint32 {
	var bits uint32
	for bit := 0; bit < 16 && bit>>3 < len(bitString); bit++ {
		if bitString[bit>>3]&(0x80>>(bit&7)) != 0 {
			bits |= 1 << bit
		}
	}
	if len(bitString) > 2 && slices.ContainsFunc(bitString[2:], func(b byte) bool { return b != 0 }) {
		bits |= 1 << 16
	}
	return bits
}

// requiredExtensions lists the extensions each kind requires. They are
// Certificate.REQUIRED_PAA_EXTENSIONS and REQUIRED_EXTENSIONS
// (Certificate.ts:385-388 at db021da0), except that a DAC and a PAI
// leave basic constraints out, whose absence is a profile violation
// (DAC_PAI_REQUIRED_EXTENSIONS, AttestationCertificates.ts:65-73 at
// db021da0).
func requiredExtensions(kind certKind, acceptProfileViolation bool) []string {
	switch {
	case kind == kindPAA && acceptProfileViolation:
		return []string{extKeyUsage, extSubjectKeyID}
	case kind == kindPAA:
		return []string{extBasicConstraints, extKeyUsage, extSubjectKeyID}
	case kind == kindCDSigner:
		return []string{extBasicConstraints, extKeyUsage, extSubjectKeyID, extAuthorityKeyID}
	default:
		return []string{extKeyUsage, extSubjectKeyID, extAuthorityKeyID}
	}
}

// checkRequiredExtensions is the required extensions check that closes
// parseExtensions (Certificate.ts:431-596 at db021da0): an extension is
// missing unless the DER carries it and its value reads — a key usage
// BIT STRING with content, a key identifier.
func checkRequiredExtensions(kind certKind, acceptProfileViolation bool, enc *certEncoding) error {
	var missing []string
	for _, name := range requiredExtensions(kind, acceptProfileViolation) {
		ok := enc.has(name)
		switch name {
		case extKeyUsage:
			ok = ok && enc.keyUsageBytes > 0
		case extSubjectKeyID:
			ok = ok && enc.skid != nil
		case extAuthorityKeyID:
			ok = ok && enc.akid != nil
		}
		if !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return errors.New("attestation: missing required extensions in certificate: " + strings.Join(missing, ", "))
	}
	return nil
}

// singleExtensions are DAC_PAI_SINGLE_EXTENSIONS
// (AttestationCertificates.ts:92-98 at db021da0), the extensions a DAC or
// PAI carries at most once, as the CHIP SDK checks.
var singleExtensions = []string{extBasicConstraints, extKeyUsage, extSubjectKeyID, extAuthorityKeyID}

// hasDuplicateSingleExtension reports whether the DER carries one of the
// single extensions twice.
func (e *certEncoding) hasDuplicateSingleExtension() bool {
	return slices.ContainsFunc(singleExtensions, func(name string) bool { return e.count(name) > 1 })
}

// assertCommonProfile is AttestationCertificates.ts:107-140 at db021da0:
// version 2 (v3), no single extension twice, basic constraints present
// and critical, key usage critical.
func assertCommonProfile(kind certKind, enc *certEncoding) error {
	if enc.version.Cmp(big.NewInt(2)) != 0 {
		return profileErrorf("%s version field must be 2 (v3), not %s", kind, enc.version)
	}
	for _, name := range singleExtensions {
		if enc.count(name) > 1 {
			return profileErrorf("%s carries more than one %s extension", kind, name)
		}
	}
	if !enc.has(extBasicConstraints) {
		return profileErrorf("%s must carry the basic constraints extension", kind)
	}
	if !enc.critical(extBasicConstraints) {
		return profileErrorf("%s basic constraints extension must be marked critical", kind)
	}
	if !enc.critical(extKeyUsage) {
		return profileErrorf("%s key usage extension must be marked critical", kind)
	}
	return nil
}

// assertPaiProfile is AttestationCertificates.ts:142-168 at db021da0: cA
// TRUE, pathLen 0, key usage keyCertSign and cRLSign with digitalSignature
// optional and no other bit (0x0060 or 0x0061).
func assertPaiProfile(enc *certEncoding) error {
	if err := assertCommonProfile(kindPAI, enc); err != nil {
		return err
	}
	if !enc.isCA {
		return profileErrorf("PAI basic constraints must have cA set to TRUE")
	}
	if enc.pathLen == nil || enc.pathLen.Sign() != 0 {
		return profileErrorf("PAI basic constraints must have pathLen 0, not %s", pathLenText(enc.pathLen))
	}
	if ku := enc.keyUsage; ku == nil || (*ku != 0x0060 && *ku != 0x0061) {
		return profileErrorf("PAI key usage must have keyCertSign and cRLSign set, may have digitalSignature set, and no other bit")
	}
	return nil
}

// assertDacProfile is AttestationCertificates.ts:170-196 at db021da0: cA
// FALSE, no pathLen (RFC 5280 §4.2.1.9 allows none without cA, and the
// CHIP SDK refuses one), key usage digitalSignature only.
func assertDacProfile(enc *certEncoding) error {
	if err := assertCommonProfile(kindDAC, enc); err != nil {
		return err
	}
	if enc.isCA {
		return profileErrorf("DAC basic constraints must have cA set to FALSE")
	}
	if enc.pathLen != nil {
		return profileErrorf("DAC basic constraints must not have a pathLen, found %s", enc.pathLen)
	}
	if ku := enc.keyUsage; ku == nil || *ku != 0x0001 {
		return profileErrorf("DAC key usage must have only digitalSignature set")
	}
	return nil
}

// pathLenText is how matter.js's template literal prints an absent pathLen.
func pathLenText(p *big.Int) string {
	if p == nil {
		return "undefined"
	}
	return p.String()
}

// isProfileError reports whether err is a profile violation.
func isProfileError(err error) bool {
	var pe *profileError
	return errors.As(err, &pe)
}

// names are the interpreted extensions' names in encoding order.
func (e *certEncoding) names() []string {
	out := make([]string, len(e.extensions))
	for i, x := range e.extensions {
		out[i] = x.name
	}
	return out
}

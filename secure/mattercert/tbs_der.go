// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

// TBS-DER reconstruction for Matter operational certificates.
//
// Per Matter Core Spec §6.5 the operational certificates ride on the
// wire as Matter-TLV but are *signed over their X.509 DER form*. The
// matter.js reference implementation (Certificate.verifyChain) calls
// `verifyEcdsa(issuerKey, this.asUnsignedDer(), this.signature)` —
// `asUnsignedDer()` converts the in-memory cert back to ASN.1 DER and
// hashes that. Hashing the raw TLV (our previous approach) only works
// for our own self-issued test certs; every real-world commissioner
// (Apple Home, chip-tool, Google Home) signs the DER form, so its
// signature does not validate against a TLV-derived hash.
//
// This file rebuilds the X.509 TBSCertificate (ASN.1 SEQUENCE) bytes
// from a decoded Matter [Certificate] and returns them ready for
// SHA-256 + ECDSA verification.

import (
	"crypto/elliptic"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// matterEpochUTCSeconds is the Matter epoch (2000-01-01T00:00:00Z)
// expressed in Unix seconds. Matter cert NotBefore / NotAfter fields
// are unsigned-uint32 second offsets from this epoch (§6.5.1.5).
const matterEpochUTCSeconds int64 = 946684800

// matterEpochToTime converts a Matter epoch timestamp into a Go
// time.Time. matterToJsDate in matter.js does the same.
func matterEpochToTime(matterSecs uint64) time.Time {
	if matterSecs == 0 {
		// Per Matter §6.5.1.5 NotAfter = 0 means "no well-defined
		// expiration"; X.509 encodes that as 99991231235959Z (RFC
		// 5280 §4.1.2.5).
		return time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	}
	// matterSecs comes straight off the wire — Decode stores the TLV
	// integer unchanged and bounds only the ordering of NotBefore /
	// NotAfter, not their magnitude — so this addition is reachable with
	// an attacker-chosen value and can overflow int64. The conversion is
	// suppressed rather than range-checked because every wrapped result
	// is fail-closed downstream: a value near 2^63 lands on a year
	// encoding/asn1 refuses to encode, so asn1.Marshal in [TBSToDER]
	// returns an error, and a value that wraps onto a representable date
	// only changes the DER bytes verifySignature hashes, which makes the
	// issuer signature fail to verify. Validity is never decided here —
	// checkValidity (verify.go) compares the raw uint64 fields.
	//nolint:gosec // G115: see above; the overflow is reachable but cannot widen acceptance.
	return time.Unix(matterEpochUTCSeconds+int64(matterSecs), 0).UTC()
}

// Matter Subject DN OIDs (Matter Core §6.5.6.1 "Distinguished Name
// Encoding"). These are 1.3.6.1.4.1.37244.{1,2,5,6} for the per-Matter
// attribute types — matter.js encodes the same constants.
var (
	oidMatterNodeID      = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 1}
	oidMatterICACID      = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 3}
	oidMatterRCACID      = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 4}
	oidMatterFabricID    = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 5}
	oidMatterCASEAuthTag = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 37244, 1, 6}
	// oidDomainComponent is domainComponent (0.9.2342.19200300.100.1.25),
	// an IA5String — matter.js X520.DomainComponent.
	oidDomainComponent = asn1.ObjectIdentifier{0, 9, 2342, 19200300, 100, 1, 25}

	oidEcdsaWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidEcPublicKey     = asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
	oidPrime256v1      = asn1.ObjectIdentifier{1, 2, 840, 10045, 3, 1, 7}

	oidExtBasicConstraints = asn1.ObjectIdentifier{2, 5, 29, 19}
	oidExtKeyUsage         = asn1.ObjectIdentifier{2, 5, 29, 15}
	oidExtSubjectKeyID     = asn1.ObjectIdentifier{2, 5, 29, 14}
	oidExtAuthorityKeyID   = asn1.ObjectIdentifier{2, 5, 29, 35}
	oidExtExtendedKeyUsage = asn1.ObjectIdentifier{2, 5, 29, 37}

	oidEKUServerAuth      = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 1}
	oidEKUClientAuth      = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 2}
	oidEKUCodeSigning     = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 3}
	oidEKUEmailProtection = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 4}
	oidEKUTimeStamping    = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 8}
	oidEKUOCSPSigning     = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 9}
)

// TBSToDER reconstructs the X.509 TBSCertificate bytes for c. The
// result is the byte form matter.js's Certificate.verifyChain hashes
// with SHA-256; ECDSA verification of c.Signature against the issuer
// public key over SHA-256(TBSToDER(c)) reproduces the
// chip-spec-compliant chain check.
func TBSToDER(c *Certificate) ([]byte, error) {
	tbs, err := buildTBS(c)
	if err != nil {
		return nil, err
	}
	out, err := asn1.Marshal(tbs)
	if err != nil {
		return nil, fmt.Errorf("matter cert: TBS asn1.Marshal: %w", err)
	}
	return out, nil
}

// rdnAttribute is one (OID, string value) pair; the value carries its own
// ASN.1 string type (UTF8String, PrintableString or IA5String). It
// marshals as the AttributeTypeAndValue SEQUENCE described in RFC 5280
// §4.1.2.4.
type rdnAttribute struct {
	Type  asn1.ObjectIdentifier
	Value asn1.RawValue
}

// x520AttributeTypes are the 2.5.4.n attribute types of the DN TLV tags
// 1-15 in tag order: commonName, surName, serialNumber, countryName,
// localityName, stateOrProvinceName, organizationName,
// organizationalUnitName, title, name, givenName, initials,
// generationQualifier, dnQualifier, pseudonym. Mirrors matter.js
// packages/general/src/crypto/X520.ts:46-76 at 862ad75f.
var x520AttributeTypes = [...]int{3, 4, 5, 6, 7, 8, 10, 11, 12, 41, 42, 43, 44, 46, 65}

// standardDNAttribute maps a standard X.520 DN TLV tag to its OID and its
// ASN.1 string tag: 1-15 are 2.5.4.n as UTF8String, 16 is domainComponent
// as IA5String, 129-143 are the attributes 1-15 as PrintableString; ok is
// false for any other tag. Mirrors matter.js
// packages/protocol/src/certificate/kinds/Certificate.ts:895-964 at
// 862ad75f (astOfDistinguishedName with the X520 encoders,
// packages/general/src/crypto/X520.ts:28-79).
func standardDNAttribute(tag uint8) (oid asn1.ObjectIdentifier, asn1Tag int, ok bool) {
	switch {
	case tag == dnTagStandardLast:
		return oidDomainComponent, asn1.TagIA5String, true
	case tag >= dnTagStandardFirst && tag < dnTagStandardLast:
		return asn1.ObjectIdentifier{2, 5, 4, x520AttributeTypes[tag-dnTagStandardFirst]}, asn1.TagUTF8String, true
	case tag >= dnTagPrintableFirst && tag <= dnTagPrintableLast:
		return asn1.ObjectIdentifier{2, 5, 4, x520AttributeTypes[tag-dnTagPrintableFirst]}, asn1.TagPrintableString, true
	}
	return nil, 0, false
}

// buildDN encodes a Matter Distinguished Name as an X.509 RDNSequence
// — a SEQUENCE OF SET OF AttributeTypeAndValue. Matter §6.5.6 places
// each attribute in its own SET (one attribute per RDN), so we emit
// one SET per present field.
//
// Matter IDs 64-bit are uppercase hex (16 chars). The 32-bit
// case-authenticated-tags are uppercase hex (8 chars). The standard
// X.520 attributes go out in their arrival position with their own string
// type ([standardDNAttribute]) — matter.js
// packages/protocol/src/certificate/kinds/Certificate.ts:895-964 at
// 862ad75f (astOfDistinguishedName); they used to be left out, which
// failed every chain whose DNs carry them (Google's roots).
func buildDN(dn DistinguishedName) []asn1.RawValue {
	var rdns []asn1.RawValue
	emitValue := func(oid asn1.ObjectIdentifier, value asn1.RawValue) {
		attr := rdnAttribute{Type: oid, Value: value}
		// SET OF AttributeTypeAndValue — single attribute, but still
		// in a SET container per RFC 5280.
		raw, err := asn1.Marshal(attr)
		if err != nil {
			// invariant: buildDN encodes the bridge's own hex identifiers
			// and, as a RawValue whose bytes asn1.Marshal copies without
			// looking at them, a peer's attribute strings into a fixed
			// rdnAttribute shape, so a marshal failure here can only be a
			// code defect (e.g. a bad OID), not remote-triggerable.
			panic(fmt.Sprintf("matter cert: buildDN attr marshal: %v", err))
		}
		set := asn1.RawValue{
			Class:      asn1.ClassUniversal,
			Tag:        asn1.TagSet,
			IsCompound: true,
			Bytes:      raw,
		}
		setBytes, err := asn1.Marshal(set)
		if err != nil {
			// invariant: same reasoning as the attr marshal above — set
			// wraps bytes we just produced ourselves.
			panic(fmt.Sprintf("matter cert: buildDN set marshal: %v", err))
		}
		var setRaw asn1.RawValue
		if _, err := asn1.Unmarshal(setBytes, &setRaw); err != nil {
			// invariant: re-unmarshalling bytes this function marshalled
			// two lines above; a failure here means asn1.Marshal itself
			// produced non-round-trippable output, not a remote input
			// problem.
			panic(fmt.Sprintf("matter cert: buildDN set re-unmarshal: %v", err))
		}
		rdns = append(rdns, setRaw)
	}
	emit := func(oid asn1.ObjectIdentifier, value string) {
		emitValue(oid, asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagUTF8String, Bytes: []byte(value)})
	}
	// Walk the DN in the original TLV arrival order — matter.js's
	// `Object.entries(data).forEach(...)` iterates insertion order
	// and a re-ordered DN produces a different TBS-DER, so this
	// loop is part of signature-verification correctness, not just
	// canonicalisation.
	catIdx, attrIdx := 0, 0
	for _, tag := range dn.Order {
		switch tag {
		case dnTagMatterRCACID:
			emit(oidMatterRCACID, hex16(dn.MatterRCACID))
		case dnTagMatterICACID:
			emit(oidMatterICACID, hex16(dn.MatterICACID))
		case dnTagMatterNodeID:
			emit(oidMatterNodeID, hex16(dn.MatterNodeID))
		case dnTagMatterFabricID:
			emit(oidMatterFabricID, hex16(dn.MatterFabricID))
		case dnTagMatterCASEAuth:
			if catIdx < len(dn.CASEAuthTags) {
				emit(oidMatterCASEAuthTag, hex8(dn.CASEAuthTags[catIdx]))
				catIdx++
			}
		default:
			// A standard X.520 attribute in its arrival position; decodeDN
			// collected Attributes in the same sequence as Order.
			if oid, asn1Tag, ok := standardDNAttribute(tag); ok && attrIdx < len(dn.Attributes) {
				emitValue(oid, asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1Tag, Bytes: []byte(dn.Attributes[attrIdx].Value)})
				attrIdx++
			}
		}
	}
	// Fallback path: caller built a DN without populating Order
	// (e.g. test code constructing a synthetic DistinguishedName by
	// hand). Emit in spec-canonical order so test fixtures still
	// produce a consistent TBS.
	if len(dn.Order) == 0 {
		if dn.HasRCACID {
			emit(oidMatterRCACID, hex16(dn.MatterRCACID))
		}
		if dn.HasICACID {
			emit(oidMatterICACID, hex16(dn.MatterICACID))
		}
		if dn.HasNodeID {
			emit(oidMatterNodeID, hex16(dn.MatterNodeID))
		}
		if dn.HasFabricID {
			emit(oidMatterFabricID, hex16(dn.MatterFabricID))
		}
		for _, cat := range dn.CASEAuthTags {
			emit(oidMatterCASEAuthTag, hex8(cat))
		}
	}
	return rdns
}

// hex16 returns an uppercase 16-character hex string of v (Matter §6.5.6.1
// uses 16-char hex for 64-bit attributes).
func hex16(v uint64) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return strings.ToUpper(hex.EncodeToString(b[:]))
}

// hex8 returns an uppercase 8-character hex string of v (32-bit CASE
// authenticated tag).
func hex8(v uint32) string {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return strings.ToUpper(hex.EncodeToString(b[:]))
}

// validityWindow is the X.509 Validity SEQUENCE per RFC 5280 §4.1.2.5.
// Each Time field auto-selects UTCTime (years 1950..2049) or
// GeneralizedTime (≥2050) based on Go's encoding/asn1 default rules
// — no context tag. matter.js emits the same shape via DerCodec.
type validityWindow struct {
	NotBefore time.Time
	NotAfter  time.Time
}

// algorithmIdentifier covers both the AlgorithmIdentifier of the
// signature (no parameters) and the AlgorithmIdentifier inside
// SubjectPublicKeyInfo (parameters carry the curve OID).
type algorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

// subjectPublicKeyInfo is the SEQUENCE inside the TBS that publishes
// the cert's public key.
type subjectPublicKeyInfo struct {
	Algorithm        algorithmIdentifier
	SubjectPublicKey asn1.BitString
}

// tbsCertificate is the X.509 TBSCertificate SEQUENCE we hash for
// signature verification.
type tbsCertificate struct {
	Version            int `asn1:"explicit,tag:0,default:0"`
	SerialNumber       *big.Int
	SignatureAlgorithm algorithmIdentifier
	Issuer             asn1.RawValue
	Validity           validityWindow
	Subject            asn1.RawValue
	PublicKey          subjectPublicKeyInfo
	Extensions         []asn1.RawValue `asn1:"explicit,tag:3,omitempty"`
}

// buildTBS turns the decoded Matter [Certificate] into the
// [tbsCertificate] structure ready for asn1.Marshal.
func buildTBS(c *Certificate) (tbsCertificate, error) {
	var tbs tbsCertificate
	tbs.Version = 2 // X.509 v3

	serial := new(big.Int).SetBytes(c.SerialNumber)
	if serial.Sign() == 0 {
		// Avoid the asn1 package emitting an empty INTEGER for a
		// zero-valued serial — Matter mandates 1..20 octets, but a
		// zero would still wire-encode.
		serial = big.NewInt(0)
	}
	tbs.SerialNumber = serial

	tbs.SignatureAlgorithm = algorithmIdentifier{
		Algorithm:  oidEcdsaWithSHA256,
		Parameters: asn1.RawValue{}, // ECDSA AlgorithmIdentifier carries no parameters
	}

	issuerRaw, err := wrapDN(buildDN(c.Issuer))
	if err != nil {
		return tbs, fmt.Errorf("issuer: %w", err)
	}
	tbs.Issuer = issuerRaw
	subjectRaw, err := wrapDN(buildDN(c.Subject))
	if err != nil {
		return tbs, fmt.Errorf("subject: %w", err)
	}
	tbs.Subject = subjectRaw

	tbs.Validity = validityWindow{
		NotBefore: matterEpochToTime(c.NotBefore),
		NotAfter:  matterEpochToTime(c.NotAfter),
	}

	x, _ := elliptic.Unmarshal(elliptic.P256(), c.PublicKey) //nolint:staticcheck // SA1019: required for raw point decode
	if x == nil {
		return tbs, fmt.Errorf("%w: pubkey off-curve", ErrMalformed)
	}
	pkParams, err := asn1.Marshal(oidPrime256v1)
	if err != nil {
		return tbs, fmt.Errorf("pubkey params: %w", err)
	}
	var pkParamsRaw asn1.RawValue
	if _, err := asn1.Unmarshal(pkParams, &pkParamsRaw); err != nil {
		return tbs, fmt.Errorf("pubkey params unmarshal: %w", err)
	}
	tbs.PublicKey = subjectPublicKeyInfo{
		Algorithm: algorithmIdentifier{
			Algorithm:  oidEcPublicKey,
			Parameters: pkParamsRaw,
		},
		SubjectPublicKey: asn1.BitString{
			Bytes:     append([]byte(nil), c.PublicKey...),
			BitLength: len(c.PublicKey) * 8,
		},
	}

	exts, err := buildExtensions(&c.Extensions)
	if err != nil {
		return tbs, fmt.Errorf("extensions: %w", err)
	}
	tbs.Extensions = exts
	return tbs, nil
}

// wrapDN takes the per-RDN raw values and wraps them into the X.509
// Name SEQUENCE OF.
func wrapDN(rdns []asn1.RawValue) (asn1.RawValue, error) {
	body, err := asn1.Marshal(rdns)
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("dn marshal: %w", err)
	}
	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(body, &raw); err != nil {
		return asn1.RawValue{}, fmt.Errorf("dn re-unmarshal: %w", err)
	}
	return raw, nil
}

// extension is one X.509 v3 Extension SEQUENCE.
type extension struct {
	ID       asn1.ObjectIdentifier
	Critical bool `asn1:"optional"`
	Value    []byte
}

// buildExtensions encodes the Matter cert extensions as the X.509
// extension list (Matter §6.5.1.4 → RFC 5280 §4.2) in the order they
// arrived in the TLV (ext.Order). This mirrors matter.js's
// `extensionsToAsn1` (packages/protocol/src/certificate/kinds/Certificate.ts:208),
// which walks Object.entries(extensions) in insertion order — the wire
// order TlvObject.decodeTlvInternalValue assigned the fields in. The
// order is part of the signed bytes: Amazon's and SmartThings' roots
// carry basic-constraints, subject-key-id, key-usage, authority-key-id,
// and emitting them in tag order fails their self-signature.
//
// An ext without Order (built by hand, not decoded) is emitted in tag
// order: basic-constraints, key-usage, extended-key-usage,
// subject-key-id, authority-key-id, future-extensions.
func buildExtensions(ext *CertificateExtensions) ([]asn1.RawValue, error) {
	order := ext.Order
	if len(order) == 0 {
		order = canonicalExtensionOrder(ext)
	}
	out := make([]asn1.RawValue, 0, len(order))
	futureIdx := 0
	for _, tag := range order {
		var (
			raw asn1.RawValue
			err error
		)
		switch tag {
		case extTagBasicConstraints:
			raw, err = buildBasicConstraints(ext)
		case extTagKeyUsage:
			raw, err = buildKeyUsage(ext)
		case extTagExtendedKeyUsage:
			raw, err = buildExtendedKeyUsage(ext)
		case extTagSubjectKeyID:
			raw, err = buildSubjectKeyID(ext)
		case extTagAuthorityKeyID:
			raw, err = buildAuthorityKeyID(ext)
		case extTagFutureExtension:
			if futureIdx >= len(ext.FutureExtensions) {
				return nil, fmt.Errorf("%w: future-extension order entry without value", ErrMalformed)
			}
			raw, err = buildFutureExtension(ext.FutureExtensions[futureIdx])
			futureIdx++
		default:
			return nil, fmt.Errorf("%w: unknown extension tag %d in order", ErrMalformed, tag)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, nil
}

// canonicalExtensionOrder lists the present extensions in tag order,
// for an ext constructed without Order.
func canonicalExtensionOrder(ext *CertificateExtensions) []uint8 {
	var order []uint8
	if ext.HasBasicConstraints {
		order = append(order, extTagBasicConstraints)
	}
	if ext.HasKeyUsage {
		order = append(order, extTagKeyUsage)
	}
	if ext.HasExtendedKeyUsage {
		order = append(order, extTagExtendedKeyUsage)
	}
	if ext.HasSubjectKeyID {
		order = append(order, extTagSubjectKeyID)
	}
	if ext.HasAuthorityKeyID {
		order = append(order, extTagAuthorityKeyID)
	}
	for range ext.FutureExtensions {
		order = append(order, extTagFutureExtension)
	}
	return order
}

func buildBasicConstraints(ext *CertificateExtensions) (asn1.RawValue, error) {
	// BasicConstraints ::= SEQUENCE { cA BOOLEAN DEFAULT FALSE,
	// pathLenConstraint INTEGER OPTIONAL }. cA is omitted when false
	// (RFC 5280 appendix B; matter.js X509.ts:156 deletes isCa === false).
	// pathLenConstraint is written whenever the TLV carried it — also
	// when it is 0: matter.js passes the number through DerCodec
	// (DerCodec.ts:224 encodes every number as INTEGER) and chip's
	// DecodeConvertBasicConstraintsExtension (CHIPCertToX509.cpp:243-249)
	// writes the INTEGER on tag presence, not on value. The Amazon and
	// SmartThings ICACs carry path-len 0 and their signatures cover the
	// `02 01 00`; an `optional` int that drops the zero fails them.
	var (
		val []byte
		err error
	)
	if ext.BasicConstraintsHasPathLen {
		val, err = asn1.Marshal(struct {
			IsCA       bool `asn1:"optional"`
			MaxPathLen int
		}{IsCA: ext.BasicConstraintsIsCA, MaxPathLen: int(ext.BasicConstraintsPathLen)})
	} else {
		val, err = asn1.Marshal(struct {
			IsCA bool `asn1:"optional"`
		}{IsCA: ext.BasicConstraintsIsCA})
	}
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("basic-constraints: %w", err)
	}
	return marshalExt(oidExtBasicConstraints, true, val)
}

func buildKeyUsage(ext *CertificateExtensions) (asn1.RawValue, error) {
	// KeyUsage is a BIT STRING; Matter's uint16 maps directly to
	// RFC 5280 §4.2.1.3 bit positions (digitalSignature=0, …).
	val, err := asn1.Marshal(encodeKeyUsageBits(ext.KeyUsage))
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("key-usage: %w", err)
	}
	return marshalExt(oidExtKeyUsage, true, val)
}

func buildExtendedKeyUsage(ext *CertificateExtensions) (asn1.RawValue, error) {
	oids, err := mapEKU(ext.ExtendedKeyUsage)
	if err != nil {
		return asn1.RawValue{}, err
	}
	val, err := asn1.Marshal(oids)
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("extended-key-usage: %w", err)
	}
	return marshalExt(oidExtExtendedKeyUsage, true, val)
}

func buildSubjectKeyID(ext *CertificateExtensions) (asn1.RawValue, error) {
	val, err := asn1.Marshal(ext.SubjectKeyID)
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("subject-key-id: %w", err)
	}
	return marshalExt(oidExtSubjectKeyID, false, val)
}

func buildAuthorityKeyID(ext *CertificateExtensions) (asn1.RawValue, error) {
	// AuthorityKeyIdentifier ::= SEQUENCE { keyIdentifier [0] OCTET STRING OPTIONAL, … }.
	aki := struct {
		KeyIdentifier []byte `asn1:"tag:0,optional"`
	}{KeyIdentifier: ext.AuthorityKeyID}
	val, err := asn1.Marshal(aki)
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("authority-key-id: %w", err)
	}
	return marshalExt(oidExtAuthorityKeyID, false, val)
}

// buildFutureExtension re-emits one future extension verbatim: it rides
// as raw DER, per matter.js's `RawBytes(Bytes.concat(...future))`.
func buildFutureExtension(fe []byte) (asn1.RawValue, error) {
	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(fe, &raw); err != nil {
		return asn1.RawValue{}, fmt.Errorf("future-extension: unmarshal: %w", err)
	}
	return raw, nil
}

// marshalExt wraps val into an [asn1.RawValue] holding the X.509
// extension SEQUENCE { id, critical?, value OCTET STRING }.
func marshalExt(id asn1.ObjectIdentifier, critical bool, val []byte) (asn1.RawValue, error) {
	e := extension{ID: id, Critical: critical, Value: val}
	body, err := asn1.Marshal(e)
	if err != nil {
		return asn1.RawValue{}, fmt.Errorf("extension %v: %w", id, err)
	}
	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(body, &raw); err != nil {
		return asn1.RawValue{}, fmt.Errorf("extension %v unmarshal: %w", id, err)
	}
	return raw, nil
}

// encodeKeyUsageBits turns the Matter UINT16 KeyUsage value into a
// minimum-length BIT STRING per RFC 5280. Bit 0 (digitalSignature) is
// the most significant bit of byte 0; matter.js's bit assignment is
// the same.
func encodeKeyUsageBits(flags uint16) asn1.BitString {
	if flags == 0 {
		return asn1.BitString{Bytes: []byte{0}, BitLength: 0}
	}
	// Find the highest set bit to size the BIT STRING tightly.
	highest := -1
	for i := range 16 {
		if flags&(1<<i) != 0 {
			highest = i
		}
	}
	bytes := []byte{0, 0}
	for i := 0; i <= highest; i++ {
		if flags&(1<<i) != 0 {
			byteIdx := i / 8
			bitIdx := i % 8
			// RFC 5280: bit i is the (7-i mod 8)-th bit (MSB) of byte (i div 8).
			bytes[byteIdx] |= 1 << (7 - bitIdx)
		}
	}
	if highest < 8 {
		bytes = bytes[:1]
	}
	return asn1.BitString{Bytes: bytes, BitLength: highest + 1}
}

// mapEKU translates the Matter ExtendedKeyUsage enum (1..6) to the
// standard X.509 EKU OIDs (RFC 5280 §4.2.1.12).
func mapEKU(values []uint8) ([]asn1.ObjectIdentifier, error) {
	out := make([]asn1.ObjectIdentifier, 0, len(values))
	for _, v := range values {
		switch v {
		case 1:
			out = append(out, oidEKUServerAuth)
		case 2:
			out = append(out, oidEKUClientAuth)
		case 3:
			out = append(out, oidEKUCodeSigning)
		case 4:
			out = append(out, oidEKUEmailProtection)
		case 5:
			out = append(out, oidEKUTimeStamping)
		case 6:
			out = append(out, oidEKUOCSPSigning)
		default:
			return nil, fmt.Errorf("%w: extended-key-usage value %d", ErrUnsupportedAlgorithm, v)
		}
	}
	return out, nil
}

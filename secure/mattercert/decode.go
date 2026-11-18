// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/tlv"
)

// Top-level certificate field tags (Matter §6.5.1.1).
const (
	tagSerialNumber       uint8 = 1
	tagSignatureAlgorithm uint8 = 2
	tagIssuer             uint8 = 3
	tagNotBefore          uint8 = 4
	tagNotAfter           uint8 = 5
	tagSubject            uint8 = 6
	tagPublicKeyAlgorithm uint8 = 7
	tagEllipticCurveID    uint8 = 8
	tagEllipticCurvePub   uint8 = 9
	tagExtensions         uint8 = 10
	tagSignature          uint8 = 11
)

// DN attribute tags inside Issuer / Subject (Matter Core Spec §6.5.6.1
// Table 60). Every tag identifies a single 64-bit Matter ID; matter-noc-cat
// is uint32 and may appear multiple times.
const (
	dnTagMatterNodeID         uint8 = 17 // matter-node-id (uint64)
	dnTagMatterFirmwareSignID uint8 = 18 // matter-firmware-signing-id (uint64)
	dnTagMatterICACID         uint8 = 19 // matter-icac-id (uint64)
	dnTagMatterRCACID         uint8 = 20 // matter-rcac-id (uint64)
	dnTagMatterFabricID       uint8 = 21 // matter-fabric-id (uint64)
	dnTagMatterVVSID          uint8 = 23 // matter-vvs-id (uint64), a VVSC's
	dnTagMatterCASEAuth       uint8 = 22 // matter-noc-cat (uint32, multiple)
	// The standard X.520 attributes (Core § 6.5.6.1): tags 1-16 carry them
	// as UTF8String — domainComponent (16) as IA5String — and tags 129-143
	// carry the attributes 1-15 as PrintableString. Mirrors matter.js
	// packages/protocol/src/certificate/kinds/definitions/operational.ts:71-111
	// at db021da0 (TlvGenericMatterSubjectOrIssuerTaggedList).
	dnTagStandardFirst  uint8 = 1
	dnTagStandardLast   uint8 = 16
	dnTagPrintableFirst uint8 = 129
	dnTagPrintableLast  uint8 = 143
)

// SignatureAlgorithm values (Matter §6.5.1.2). Only ECDSA-with-SHA256
// is defined.
const (
	SigAlgoECDSAWithSHA256 uint64 = 1
)

// PublicKeyAlgorithm values (Matter §6.5.1.5). Only EC is defined.
const (
	PubKeyAlgoEC uint64 = 1
)

// EllipticCurveIdentifier values (Matter §6.5.1.6). Only P-256.
const (
	CurvePrime256v1 uint64 = 1
)

// Errors returned during decoding / structural validation.
var (
	// ErrTruncated is wrapped onto truncation surfaced by the TLV
	// codec.
	ErrTruncated = errors.New("mattercert: certificate truncated")
	// ErrMalformed is returned when the TLV structure violates the
	// Matter §6.5 cert schema (missing mandatory fields, wrong types).
	ErrMalformed = errors.New("mattercert: malformed certificate")
	// ErrNonOperationalNodeID is returned, alongside [ErrMalformed], when a
	// NOC's matter-node-id lies outside the operational node ID range
	// 0x0000_0000_0000_0001 .. 0xFFFF_FFEF_FFFF_FFFF. AddNOC and UpdateNOC
	// answer it with NOCResponse InvalidNodeOpId instead of InvalidNOC.
	// Mirrors matter.js 0ceb202d
	// packages/protocol/src/certificate/kinds/Noc.ts:20 (OperationalNodeIdError
	// extends CertificateError) thrown at :83-85.
	ErrNonOperationalNodeID = errors.New("mattercert: NOC node id is not an operational node id")
	// ErrUnsupportedAlgorithm is returned for non-ECDSA signatures or
	// non-P-256 keys.
	ErrUnsupportedAlgorithm = errors.New("mattercert: unsupported algorithm")
	// ErrSubjectIncomplete is returned when the Subject DN does not
	// carry the required identifiers (NodeID for NOC, FabricID where
	// applicable).
	ErrSubjectIncomplete = errors.New("mattercert: subject identifiers missing")
)

// Certificate is the decoded form of a Matter operational certificate.
// All fields are populated from the TLV payload; the TBS bytes are
// retained for subsequent signature verification.
type Certificate struct {
	SerialNumber       []byte
	SignatureAlgorithm uint64
	Issuer             DistinguishedName
	NotBefore          uint64
	NotAfter           uint64
	Subject            DistinguishedName
	PublicKeyAlgorithm uint64
	EllipticCurveID    uint64
	PublicKey          []byte // 65-byte uncompressed P-256
	Extensions         CertificateExtensions
	Signature          []byte // 64-byte r||s
	Raw                []byte // full TLV bytes (for chain hashing)
	// TBSDER is the X.509 TBSCertificate as [DecodeDER] read it, nil for a
	// certificate from [Decode]; [TBSToDER] returns it unchanged, so the
	// signature of a DER certificate verifies over the bytes its issuer
	// signed (matter.js Certificate.ts:196-199 asUnsignedDer).
	TBSDER []byte
}

// CertificateExtensions captures the Matter-cert extension structure
// (Matter §6.5.1.4). All fields are optional; presence is tracked via
// the Has* booleans because zero-valued slices and IDs are legitimate
// for real-world certs (e.g. KeyUsage = 0 is technically encodable).
//
// Field layout in the TLV List at top-level tag 10:
//
//	[1] basic-constraints   STRUCT { [1] is-ca BOOL, [2] path-len UINT8? }
//	[2] key-usage           UINT16 (RFC-5280 KeyUsage bitmap)
//	[3] extended-key-usage  ARRAY of UINT8 (1=serverAuth, 2=clientAuth, …)
//	[4] subject-key-id      OCTET STRING (20 bytes, SHA-1-derived)
//	[5] authority-key-id    OCTET STRING (20 bytes, SHA-1-derived)
//	[6] future-extension    OCTET STRING (raw DER, 0+ instances)
//
// X.509 DER reconstruction (used by signature verification per
// matter.js Certificate.verifyChain) requires every extension to ride
// in the rebuilt TBS in the SAME order they appeared in the source TLV;
// Order carries that sequence.
type CertificateExtensions struct {
	HasBasicConstraints        bool
	BasicConstraintsIsCA       bool
	BasicConstraintsHasPathLen bool
	BasicConstraintsPathLen    uint8
	HasKeyUsage                bool
	KeyUsage                   uint16
	HasExtendedKeyUsage        bool
	ExtendedKeyUsage           []uint8
	HasSubjectKeyID            bool
	SubjectKeyID               []byte // 20 bytes
	HasAuthorityKeyID          bool
	AuthorityKeyID             []byte // 20 bytes
	FutureExtensions           [][]byte
	// Order records the extension tags in their original TLV arrival
	// sequence, one entry per extension (tag 6 repeats once per future
	// extension). TBSToDER emits the X.509 extension list in this
	// order: matter.js's extensionsToAsn1 (Certificate.ts:208) iterates
	// Object.entries(extensions), whose insertion order is the wire
	// order TlvObject.decodeTlvInternalValue assigned the fields in.
	// Amazon's and SmartThings' commissioner certificates carry
	// basic-constraints, subject-key-id, key-usage, authority-key-id —
	// not tag order — and a fixed order fails their signatures.
	Order []uint8
}

// DistinguishedName captures the Matter-specific DN attributes that
// the bridge needs from Issuer + Subject. Other DN entries are
// preserved on Other for diagnostic logging but otherwise unused.
type DistinguishedName struct {
	// MatterRCACID is the 64-bit Root CA ID. Set on Root + ICAC
	// certificates' Subject and on every certificate's Issuer.
	MatterRCACID uint64
	// MatterICACID is the 64-bit Intermediate CA ID. Set on ICAC
	// Subject; absent when the cert is signed directly by the root.
	MatterICACID uint64
	// MatterNodeID is the 64-bit operational Node ID. Set on NOC
	// Subject only.
	MatterNodeID uint64
	// MatterFabricID is the 64-bit Fabric ID. Set on NOC + ICAC
	// Subject.
	MatterFabricID uint64
	// CASEAuthTags collects the (multi-valued) CASE Authenticated
	// Tags from a NOC subject.
	CASEAuthTags []uint32
	// Attributes are the standard X.520 attributes (commonName,
	// organizationName, countryName, …) in their TLV arrival order; a
	// real controller's root carries them (Google's: C, O, CN), and the
	// TBS DER must carry them back or the signature does not verify.
	Attributes []DNAttribute
	// HasNodeID / HasFabricID / HasRCACID / HasICACID convey
	// presence; the spec uses zero-as-sentinel which conflicts with
	// real fabric IDs that may legitimately be 0.
	HasNodeID   bool
	HasFabricID bool
	HasRCACID   bool
	HasICACID   bool
	// MatterVVSID is the 64-bit Vendor ID Verification Signer ID
	// (matter-vvs-id, DN tag 23): the subject of a VVSC carries exactly
	// one, a NOC, ICAC or RCAC none. Mirrors matter.js
	// packages/protocol/src/certificate/kinds/definitions/operational.ts:64
	// at db021da0 (the TLV field) and definitions/asn.ts:113-114 (the OID
	// 1.3.6.1.4.1.37244.1.7).
	MatterVVSID uint64
	HasVVSID    bool
	// MatterFirmwareSigningID is the 64-bit matter-firmware-signing-id (DN
	// tag 18, OID 1.3.6.1.4.1.37244.1.2 — matter.js definitions/asn.ts:98-99
	// at db021da0), an optional attribute of an operational certificate
	// that counts as an RDN like the others.
	MatterFirmwareSigningID uint64
	HasFirmwareSigningID    bool
	// Order records the DN attribute tags in their original TLV
	// arrival sequence. The X.509 DER reconstruction (TBSToDER)
	// MUST emit the RDNs in this order — matter.js's Object.entries
	// iteration preserves insertion order, and signature verification
	// relies on the byte-exact reproduction of the issuer's DN. A
	// reordered DN produces a different SHA-256 over the TBS and
	// fails ECDSA.Verify even though every value is correct.
	Order []uint8
}

// DNAttribute is one standard X.520 attribute of a [DistinguishedName] as
// the TLV carried it: Tag is the context tag (1-16 a UTF8String, 129-143 a
// PrintableString), Value the string. Mirrors matter.js
// packages/protocol/src/certificate/kinds/definitions/operational.ts:71-111
// at db021da0.
type DNAttribute struct {
	Tag   uint8
	Value string
}

// IsRoot reports whether the cert's Subject identifies a Root CA.
// Root CAs carry a matter-rcac-id and never a matter-node-id; per
// Matter §6.5.6.1 the matter-fabric-id attribute is OPTIONAL on a
// root (chip-tool and Apple Home both ship roots with a fabric-id
// pre-bound). The earlier `!HasFabricID` clause rejected every
// real-world commissioner's trust root and aborted AddNOC with
// "cert is not a root CA".
func (c *Certificate) IsRoot() bool {
	return c.Subject.HasRCACID && !c.Subject.HasNodeID
}

// IsICA reports whether the cert's Subject identifies an Intermediate
// CA (ICAC). ICACs carry a matter-icac-id and never a matter-node-id;
// per Matter §6.5.6.1 the matter-fabric-id attribute is OPTIONAL on
// an ICAC (mirroring the same allowance on RCAC). chip-tool ships
// ICACs without a fabric-id; the earlier `HasFabricID` requirement
// rejected them with "cert is not an ICAC" mid-Sigma3.
func (c *Certificate) IsICA() bool {
	return c.Subject.HasICACID && !c.Subject.HasNodeID
}

// IsNOC reports whether the cert's Subject identifies a Node
// Operational Certificate (NOC).
func (c *Certificate) IsNOC() bool {
	return c.Subject.HasNodeID && c.Subject.HasFabricID
}

// PublicKeyECDSA returns the parsed public key. Returns
// [ErrUnsupportedAlgorithm] when the curve is not P-256 or
// [ErrMalformed] when the point is off-curve.
func (c *Certificate) PublicKeyECDSA() (*ecdsa.PublicKey, error) {
	if c.PublicKeyAlgorithm != PubKeyAlgoEC {
		return nil, fmt.Errorf("%w: pub-key-algo=%d", ErrUnsupportedAlgorithm, c.PublicKeyAlgorithm)
	}
	if c.EllipticCurveID != CurvePrime256v1 {
		return nil, fmt.Errorf("%w: ec-curve-id=%d", ErrUnsupportedAlgorithm, c.EllipticCurveID)
	}
	if len(c.PublicKey) != 65 || c.PublicKey[0] != 0x04 {
		return nil, fmt.Errorf("%w: malformed P-256 point", ErrMalformed)
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), c.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("%w: point not on curve: %w", ErrMalformed, err)
	}
	return pub, nil
}

// Decode parses raw TLV bytes into a [Certificate]. The supplied
// buffer is retained on Certificate.Raw for downstream signature
// verification — callers MUST NOT mutate it after Decode returns.
func Decode(raw []byte) (*Certificate, error) {
	// Mirrors matter.js packages/protocol/src/certificate/kinds/OperationalBase.ts:19-28
	// (assertTlvSize, called first by Noc/Rcac/Icac/Vvsc.fromTlv): §6.1.3
	// caps a Matter-TLV operational certificate at 400 bytes.
	if len(raw) > MaxOperationalCertTLVBytes {
		return nil, fmt.Errorf("%w: Matter-TLV operational certificate of %d bytes exceeds the %d byte limit",
			ErrMalformed, len(raw), MaxOperationalCertTLVBytes)
	}
	cert := &Certificate{Raw: append([]byte(nil), raw...)}

	d := tlv.NewDecoder(raw)
	top, err := d.Next()
	if err != nil {
		return nil, fmt.Errorf("%w: top: %w", ErrTruncated, err)
	}
	if top.Type != tlv.TypeStructure {
		return nil, fmt.Errorf("%w: top element must be Structure (got %d)", ErrMalformed, top.Type)
	}

	for {
		el, err := d.Next()
		if err != nil {
			return nil, fmt.Errorf("%w: cert body: %w", ErrTruncated, err)
		}
		if el.IsEndContainer {
			break
		}
		if el.Tag.Kind != tlv.TagKindContext {
			return nil, fmt.Errorf("%w: cert field with non-context tag %v", ErrMalformed, el.Tag)
		}
		if err := assignField(cert, el, d); err != nil {
			return nil, err
		}
	}

	if err := validateMandatory(cert); err != nil {
		return nil, err
	}
	return cert, nil
}

// assignField dispatches on the context tag and reads the field value
// from d. For container fields (Issuer/Subject/Extensions) the parser
// is invoked recursively.
func assignField(cert *Certificate, el tlv.Element, d *tlv.Decoder) error {
	tag := uint8(el.Tag.Number & 0xFF)
	switch tag {
	case tagSerialNumber:
		if el.Type < tlv.TypeOctetStr1 || el.Type > tlv.TypeOctetStr8 {
			return fmt.Errorf("%w: SerialNumber must be octet string", ErrMalformed)
		}
		cert.SerialNumber = append([]byte(nil), el.Octets...)
	case tagSignatureAlgorithm, tagPublicKeyAlgorithm, tagEllipticCurveID:
		return assignAlgorithm(cert, el, tag)
	case tagIssuer:
		if !el.IsContainer || el.Type != tlv.TypeList {
			// A DN is a TLV list; a structure (TC-JFADMIN-1.2 step 16
			// re-encodes the Subject so) is malformed. Mirrors chip
			// CHIPCert.cpp:782 ChipDN::DecodeFromTLV (kTLVType_List,
			// CHIP_ERROR_WRONG_TLV_TYPE) at 6170af84 and matter.js
			// DistinguishedName.ts:636-638 at db021da0 (the DN schema is
			// TlvDistinguishedName, operational.ts:111, a
			// DistinguishedNameSchema with its own codec,
			// DistinguishedName.ts:613-666; another type is
			// UnexpectedDataError).
			return fmt.Errorf("%w: issuer is not a TLV list (type 0x%02X)", ErrMalformed, el.Type)
		}
		dn, err := decodeDN(d)
		if err != nil {
			return fmt.Errorf("issuer: %w", err)
		}
		cert.Issuer = dn
	case tagNotBefore, tagNotAfter:
		// notBefore/notAfter are TlvUInt32 (operational.ts:138-139 at
		// db021da0), and matter.js's Schema.decode validates the bounds.
		v, err := uintField(el, 32, "validity time")
		if err != nil {
			return err
		}
		if tag == tagNotBefore {
			cert.NotBefore = v
		} else {
			cert.NotAfter = v
		}
	case tagSubject:
		if !el.IsContainer || el.Type != tlv.TypeList {
			// A DN is a TLV list; a structure (TC-JFADMIN-1.2 step 16
			// re-encodes the Subject so) is malformed. Mirrors chip
			// CHIPCert.cpp:782 ChipDN::DecodeFromTLV (kTLVType_List,
			// CHIP_ERROR_WRONG_TLV_TYPE) at 6170af84 and matter.js
			// DistinguishedName.ts:636-638 at db021da0 (the DN schema is
			// TlvDistinguishedName, operational.ts:111, a
			// DistinguishedNameSchema with its own codec,
			// DistinguishedName.ts:613-666; another type is
			// UnexpectedDataError).
			return fmt.Errorf("%w: subject is not a TLV list (type 0x%02X)", ErrMalformed, el.Type)
		}
		dn, err := decodeDN(d)
		if err != nil {
			return fmt.Errorf("subject: %w", err)
		}
		cert.Subject = dn
	case tagEllipticCurvePub:
		v, err := octetsField(el, "ec-pub-key")
		if err != nil {
			return err
		}
		cert.PublicKey = v
	case tagExtensions:
		// The extensions are a TLV list, as the encoder writes them
		// (encode.go); a structure, an array or a scalar is malformed,
		// not descended into as if it were one (R2-P6-wire-0103). Mirrors
		// matter.js: a TlvTaggedListPreservingOrder (operational.ts:37,
		// :150 at db021da0) is of type List (TlvObject.ts:392-393) and
		// refuses another type (:205-207).
		if el.Type != tlv.TypeList {
			return fmt.Errorf("%w: extensions are not a TLV list (type 0x%02X)", ErrMalformed, el.Type)
		}
		ext, err := decodeExtensions(d)
		if err != nil {
			return fmt.Errorf("extensions: %w", err)
		}
		cert.Extensions = ext
	case tagSignature:
		v, err := octetsField(el, "signature")
		if err != nil {
			return err
		}
		cert.Signature = v
	default:
		// Unknown future field — skip if container, else accept.
		if el.IsContainer {
			if err := skipContainer(d); err != nil {
				return fmt.Errorf("unknown container tag %d: %w", tag, err)
			}
		}
	}
	return nil
}

// assignAlgorithm reads one of the three 8-bit algorithm members — the
// signature algorithm, the public-key algorithm, the elliptic curve —
// into cert.
func assignAlgorithm(cert *Certificate, el tlv.Element, tag uint8) error {
	names := map[uint8]string{tagSignatureAlgorithm: "signature algorithm", tagPublicKeyAlgorithm: "public-key algorithm", tagEllipticCurveID: "elliptic curve"}
	v, err := uintField(el, 8, names[tag])
	if err != nil {
		return err
	}
	switch tag {
	case tagSignatureAlgorithm:
		cert.SignatureAlgorithm = v
	case tagPublicKeyAlgorithm:
		cert.PublicKeyAlgorithm = v
	case tagEllipticCurveID:
		cert.EllipticCurveID = v
	}
	return nil
}

// decodeDN reads a Matter Distinguished Name list. The List container
// header has already been consumed by [tlv.Decoder.Next] — but in our
// caller's flow the *element* representing the List was just returned;
// that element is itself only the container marker, so we read its
// children until EndContainer.
func decodeDN(d *tlv.Decoder) (DistinguishedName, error) {
	var dn DistinguishedName
	// A single-valued Matter attribute occurs at most once in a DN —
	// matter.js's DN decoder refuses a Matter attribute that repeats
	// ("Distinguished name contains … more than once",
	// DistinguishedName.ts:261-283 at db021da0; the rule :242-246), where
	// the decoder used to keep the last value.
	seen := make(map[uint8]bool, 6)
	once := func(tag uint8) error {
		if seen[tag] {
			return fmt.Errorf("%w: dn attribute %d repeated", ErrMalformed, tag)
		}
		seen[tag] = true
		return nil
	}
	for {
		el, err := d.Next()
		if err != nil {
			return dn, fmt.Errorf("%w: dn child: %w", ErrTruncated, err)
		}
		if el.IsEndContainer {
			break
		}
		if el.Tag.Kind != tlv.TagKindContext {
			return dn, fmt.Errorf("%w: dn entry with non-context tag", ErrMalformed)
		}
		tag := uint8(el.Tag.Number & 0xFF)
		switch tag {
		case dnTagMatterNodeID, dnTagMatterICACID, dnTagMatterRCACID, dnTagMatterFabricID, dnTagMatterVVSID, dnTagMatterFirmwareSignID:
			if err := once(tag); err != nil {
				return dn, err
			}
		}
		switch tag {
		case dnTagMatterNodeID, dnTagMatterICACID, dnTagMatterRCACID, dnTagMatterFabricID, dnTagMatterVVSID, dnTagMatterFirmwareSignID:
			v, err := uintField(el, 64, fmt.Sprintf("dn attribute %d", tag))
			if err != nil {
				return dn, err
			}
			dn.setMatterID(tag, v)
			dn.Order = append(dn.Order, tag)
		case dnTagMatterCASEAuth:
			v, err := uintField(el, 32, "dn case-authenticated tag")
			if err != nil {
				return dn, err
			}
			dn.CASEAuthTags = append(dn.CASEAuthTags, uint32(v)) //nolint:gosec // G115: uintField bounded v to 32 bits
			dn.Order = append(dn.Order, tag)
		default:
			if isStandardDNTag(tag) {
				// A standard X.520 attribute, kept with its place in the
				// order for the TBS DER. Mirrors matter.js
				// packages/protocol/src/certificate/kinds/definitions/operational.ts:71-111
				// at db021da0 (TlvString fields, :74-109, of a
				// TlvDistinguishedName, :111); they used to be skipped, so
				// a chain whose DNs carry them failed to verify.
				if el.Type < tlv.TypeUTF8Str1 || el.Type > tlv.TypeUTF8Str8 {
					return dn, fmt.Errorf("%w: dn attribute %d is not a UTF-8 string", ErrMalformed, tag)
				}
				dn.Attributes = append(dn.Attributes, DNAttribute{Tag: tag, Value: el.String})
				dn.Order = append(dn.Order, tag)
				continue
			}
			// Skip — DN attributes the bridge does not consume.
			if el.IsContainer {
				if err := skipContainer(d); err != nil {
					return dn, err
				}
			}
		}
	}
	return dn, nil
}

// setMatterID stores the value of a single-valued Matter DN attribute
// (node, ICAC, RCAC, fabric, VVS or firmware-signing id) under its tag.
func (dn *DistinguishedName) setMatterID(tag uint8, v uint64) {
	switch tag {
	case dnTagMatterNodeID:
		dn.MatterNodeID, dn.HasNodeID = v, true
	case dnTagMatterICACID:
		dn.MatterICACID, dn.HasICACID = v, true
	case dnTagMatterRCACID:
		dn.MatterRCACID, dn.HasRCACID = v, true
	case dnTagMatterFabricID:
		dn.MatterFabricID, dn.HasFabricID = v, true
	case dnTagMatterVVSID:
		dn.MatterVVSID, dn.HasVVSID = v, true
	case dnTagMatterFirmwareSignID:
		dn.MatterFirmwareSigningID, dn.HasFirmwareSigningID = v, true
	}
}

// isStandardDNTag reports whether tag is a standard X.520 DN attribute
// tag (1-16, or 129-143 for the PrintableString forms).
func isStandardDNTag(tag uint8) bool {
	return (tag >= dnTagStandardFirst && tag <= dnTagStandardLast) || (tag >= dnTagPrintableFirst && tag <= dnTagPrintableLast)
}

// uintField is an unsigned-integer member's value: an element of another
// TLV type, or a value wider than the member's bits, is refused — not
// masked to the width or read as 0 (R2-P6-wire-0103). The encoding's own
// width is free; only the value is bounded. Mirrors matter.js's number
// schemas, which refuse another type (packages/types/src/tlv/TlvNumber.ts:124-131
// at db021da0) and validate the bounds (:151-155) of the certificate's
// integer members (packages/protocol/src/certificate/kinds/definitions/operational.ts:129-151;
// the DN's Matter ids :58-64).
func uintField(el tlv.Element, bits uint, name string) (uint64, error) {
	switch el.Type {
	case tlv.TypeUnsignedInt1, tlv.TypeUnsignedInt2, tlv.TypeUnsignedInt4, tlv.TypeUnsignedInt8:
	default:
		return 0, fmt.Errorf("%w: %s: TLV type 0x%02X, want an unsigned integer", ErrMalformed, name, uint8(el.Type))
	}
	if bits < 64 && el.Uint>>bits != 0 {
		return 0, fmt.Errorf("%w: %s: value %d exceeds %d bits", ErrMalformed, name, el.Uint, bits)
	}
	return el.Uint, nil
}

// octetsField is an octet-string member's value; an element of another
// TLV type is refused rather than read as empty (R2-P6-wire-0103), as
// matter.js refuses it (packages/types/src/tlv/TlvString.ts:58 at
// db021da0).
func octetsField(el tlv.Element, name string) ([]byte, error) {
	if el.Type < tlv.TypeOctetStr1 || el.Type > tlv.TypeOctetStr8 {
		return nil, fmt.Errorf("%w: %s: TLV type 0x%02X, want an octet string", ErrMalformed, name, uint8(el.Type))
	}
	return append([]byte(nil), el.Octets...), nil
}

// skipContainer consumes elements (recursively if nested) until the
// matching End-of-Container marker.
func skipContainer(d *tlv.Decoder) error {
	depth := 1
	for depth > 0 {
		el, err := d.Next()
		if err != nil {
			return fmt.Errorf("%w: skip: %w", ErrTruncated, err)
		}
		if el.IsContainer {
			depth++
		}
		if el.IsEndContainer {
			depth--
		}
	}
	return nil
}

// validateMandatory checks the spec-required fields are present and
// well-formed (Matter §6.5.1).
func validateMandatory(c *Certificate) error {
	// Matter §6.5 caps the serial number at 20 octets, but matter.js
	// tolerates 21 (observed in the wild, e.g. some LG TVs) and only
	// throws above that. Mirrors OperationalBase.ts:44-57 generalVerify:
	// it warns (does not reject) at 21 octets and rejects only >21.
	if len(c.SerialNumber) == 0 || len(c.SerialNumber) > 21 {
		return fmt.Errorf("%w: serial number length=%d (want 1..21)", ErrMalformed, len(c.SerialNumber))
	}
	if c.SignatureAlgorithm != SigAlgoECDSAWithSHA256 {
		return fmt.Errorf("%w: signature algorithm %d", ErrUnsupportedAlgorithm, c.SignatureAlgorithm)
	}
	if c.PublicKeyAlgorithm != PubKeyAlgoEC {
		return fmt.Errorf("%w: public-key algorithm %d", ErrUnsupportedAlgorithm, c.PublicKeyAlgorithm)
	}
	if c.EllipticCurveID != CurvePrime256v1 {
		return fmt.Errorf("%w: elliptic curve %d", ErrUnsupportedAlgorithm, c.EllipticCurveID)
	}
	// The prefix is only reportable once the length check has passed —
	// tag 9 may be absent or empty in a peer-supplied cert, and Go
	// evaluates the whole argument list before the fmt call.
	if len(c.PublicKey) != 65 {
		return fmt.Errorf("%w: ec-pub-key length=%d (want 65)", ErrMalformed, len(c.PublicKey))
	}
	if c.PublicKey[0] != 0x04 {
		return fmt.Errorf("%w: ec-pub-key prefix=%#x (want 0x04)", ErrMalformed, c.PublicKey[0])
	}
	if len(c.Signature) != 64 {
		return fmt.Errorf("%w: signature length=%d (want 64)", ErrMalformed, len(c.Signature))
	}
	// The extensions member and four of its extensions are mandatory in
	// every certificate kind: a certificate without basic constraints,
	// key usage or either key identifier is malformed, whatever the
	// verifier of its kind would check later. Mirrors matter.js's schema
	// (packages/protocol/src/certificate/kinds/definitions/operational.ts:150
	// `extensions: TlvField(10, …)`, :38, :45, :47, :48 TlvField at
	// db021da0), which refuses a missing mandatory field at decode
	// (packages/types/src/tlv/TlvObject.ts:257-261).
	if ext := c.Extensions; !ext.HasBasicConstraints || !ext.HasKeyUsage || !ext.HasSubjectKeyID || !ext.HasAuthorityKeyID {
		return fmt.Errorf("%w: a mandatory extension is missing (basicConstraints=%t, keyUsage=%t, subjectKeyId=%t, authorityKeyId=%t)",
			ErrMalformed, ext.HasBasicConstraints, ext.HasKeyUsage, ext.HasSubjectKeyID, ext.HasAuthorityKeyID)
	}
	if c.NotAfter != 0 && c.NotAfter <= c.NotBefore {
		return fmt.Errorf("%w: NotAfter (%d) <= NotBefore (%d)", ErrMalformed, c.NotAfter, c.NotBefore)
	}
	if c.IsNOC() {
		if !c.Subject.HasNodeID || !c.Subject.HasFabricID {
			return fmt.Errorf("%w: NOC subject must carry NodeID + FabricID", ErrSubjectIncomplete)
		}
	}
	return nil
}

// keyIDSize is the length of the subject and authority key identifiers:
// 160 bits, as matter.js's certificate schema bounds them
// (packages/protocol/src/certificate/kinds/definitions/operational.ts:47-48
// at db021da0).
const keyIDSize = 20

// Extension TLV tags inside the [tagExtensions] List per Matter
// §6.5.1.4. Tag 6 (future-extension) is repeatable; the others are
// each at most once.
const (
	extTagBasicConstraints uint8 = 1
	extTagKeyUsage         uint8 = 2
	extTagExtendedKeyUsage uint8 = 3
	extTagSubjectKeyID     uint8 = 4
	extTagAuthorityKeyID   uint8 = 5
	extTagFutureExtension  uint8 = 6
	extBCInnerTagIsCa      uint8 = 1
	extBCInnerTagPathLen   uint8 = 2
)

// decodeExtensions reads the children of the top-level Extensions
// container (the container marker has already been consumed by the
// outer [Decode] loop). Each member is read at its own type — basic
// constraints a structure, extended key usage an array of 8-bit values,
// key usage a 16-bit value, the key ids and a future extension octet
// strings — and every extension but a future one occurs at most once
// (R2-P6-wire-0103). Mirrors matter.js's extensions schema
// (packages/protocol/src/certificate/kinds/definitions/operational.ts:37-50
// at db021da0): basic constraints a TlvObject (:38-44), isCA a TlvBoolean
// (:41; packages/types/src/tlv/TlvBoolean.ts:22-23), the extended key
// usage a TlvArray(TlvUInt8) (:46; TlvArray.ts:73-74), a member given
// twice refused in an order-preserving list (TlvObject.ts:230-232), the
// future extension a repeated field (:49).
func decodeExtensions(d *tlv.Decoder) (CertificateExtensions, error) {
	var ext CertificateExtensions
	var seen uint8 // bit n: extension n (1..5) already read
	for {
		el, err := d.Next()
		if err != nil {
			return ext, fmt.Errorf("%w: extension child: %w", ErrTruncated, err)
		}
		if el.IsEndContainer {
			break
		}
		if el.Tag.Kind != tlv.TagKindContext {
			return ext, fmt.Errorf("%w: extension entry with non-context tag", ErrMalformed)
		}
		tag := uint8(el.Tag.Number & 0xFF)
		if tag >= extTagBasicConstraints && tag < extTagFutureExtension {
			if seen&(1<<tag) != 0 {
				return ext, fmt.Errorf("%w: extension %d repeated", ErrMalformed, tag)
			}
			seen |= 1 << tag
		}
		if tag >= extTagBasicConstraints && tag <= extTagFutureExtension {
			ext.Order = append(ext.Order, tag)
		}
		if err := decodeExtension(d, el, tag, &ext); err != nil {
			return ext, err
		}
	}
	return ext, nil
}

// decodeExtension reads one member of the extensions list, el with its
// context tag, into ext.
func decodeExtension(d *tlv.Decoder, el tlv.Element, tag uint8, ext *CertificateExtensions) error {
	switch tag {
	case extTagBasicConstraints:
		if el.Type != tlv.TypeStructure {
			return fmt.Errorf("%w: basic-constraints not a structure (type 0x%02X)", ErrMalformed, el.Type)
		}
		if err := decodeBasicConstraints(d, ext); err != nil {
			return fmt.Errorf("basic-constraints: %w", err)
		}
	case extTagKeyUsage:
		v, err := uintField(el, 16, "key-usage")
		if err != nil {
			return err
		}
		ext.KeyUsage = uint16(v) //nolint:gosec // G115: uintField bounded v to 16 bits
		ext.HasKeyUsage = true
	case extTagExtendedKeyUsage:
		if el.Type != tlv.TypeArray {
			return fmt.Errorf("%w: extended-key-usage not an array (type 0x%02X)", ErrMalformed, el.Type)
		}
		eku, err := decodeUint8Array(d)
		if err != nil {
			return fmt.Errorf("extended-key-usage: %w", err)
		}
		ext.ExtendedKeyUsage = eku
		ext.HasExtendedKeyUsage = true
	case extTagSubjectKeyID:
		v, err := octetsField(el, "subject-key-id")
		if err != nil {
			return err
		}
		if len(v) != keyIDSize {
			return fmt.Errorf("%w: subject-key-id of %d bytes, want %d", ErrMalformed, len(v), keyIDSize)
		}
		ext.SubjectKeyID = v
		ext.HasSubjectKeyID = true
	case extTagAuthorityKeyID:
		v, err := octetsField(el, "authority-key-id")
		if err != nil {
			return err
		}
		if len(v) != keyIDSize {
			return fmt.Errorf("%w: authority-key-id of %d bytes, want %d", ErrMalformed, len(v), keyIDSize)
		}
		ext.AuthorityKeyID = v
		ext.HasAuthorityKeyID = true
	case extTagFutureExtension:
		v, err := octetsField(el, "future-extension")
		if err != nil {
			return err
		}
		ext.FutureExtensions = append(ext.FutureExtensions, v)
	default:
		if el.IsContainer {
			if err := skipContainer(d); err != nil {
				return fmt.Errorf("unknown extension container tag %d: %w", tag, err)
			}
		}
	}

	return nil
}

// decodeBasicConstraints reads the inner struct (consumed marker) of
// the basic-constraints extension.
func decodeBasicConstraints(d *tlv.Decoder, ext *CertificateExtensions) error {
	ext.HasBasicConstraints = true
	for {
		el, err := d.Next()
		if err != nil {
			return fmt.Errorf("%w: bc child: %w", ErrTruncated, err)
		}
		if el.IsEndContainer {
			return nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			return fmt.Errorf("%w: bc entry with non-context tag", ErrMalformed)
		}
		tag := uint8(el.Tag.Number & 0xFF)
		switch tag {
		case extBCInnerTagIsCa:
			if el.Type != tlv.TypeBoolTrue && el.Type != tlv.TypeBoolFalse {
				return fmt.Errorf("%w: basic-constraints: isCA: TLV type 0x%02X, want a boolean", ErrMalformed, uint8(el.Type))
			}
			ext.BasicConstraintsIsCA = el.Type == tlv.TypeBoolTrue
		case extBCInnerTagPathLen:
			// pathLen is TlvUInt8 (operational.ts:42 at db021da0): a wider
			// value is refused, not cut to its low byte.
			v, err := uintField(el, 8, "basic-constraints: pathLen")
			if err != nil {
				return err
			}
			ext.BasicConstraintsPathLen = uint8(v) //nolint:gosec // G115: uintField bounded v to 8 bits
			ext.BasicConstraintsHasPathLen = true
		default:
			if el.IsContainer {
				if err := skipContainer(d); err != nil {
					return fmt.Errorf("unknown bc tag %d: %w", tag, err)
				}
			}
		}
	}
}

// decodeUint8Array reads a TLV array of UInt elements, returning each
// as a uint8 (matter spec ExtendedKeyUsage uses 1..6 enum values); an
// entry of another type or above 8 bits is refused.
func decodeUint8Array(d *tlv.Decoder) ([]uint8, error) {
	var out []uint8
	for {
		el, err := d.Next()
		if err != nil {
			return nil, fmt.Errorf("%w: array child: %w", ErrTruncated, err)
		}
		if el.IsEndContainer {
			return out, nil
		}
		// An array's entries are anonymous; one with a tag is refused, as
		// matter.js refuses it ("Array element tags should be anonymous",
		// packages/types/src/tlv/TlvArray.ts:78 at db021da0).
		if el.Tag.Kind != tlv.TagKindAnonymous {
			return nil, fmt.Errorf("%w: extended-key-usage entry with a tag", ErrMalformed)
		}
		v, err := uintField(el, 8, "extended-key-usage entry")
		if err != nil {
			return nil, err
		}
		out = append(out, uint8(v)) //nolint:gosec // G115: uintField bounded v to 8 bits
	}
}

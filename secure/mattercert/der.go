// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"bytes"
	"encoding/asn1"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"time"
)

// MaxOperationalCertDERBytes caps a Matter operational certificate in its
// X.509 DER form — §6.1.3; matter.js MAX_DER_CERTIFICATE_SIZE
// (packages/protocol/src/certificate/kinds/common.ts:13 at 862ad75f),
// checked before a DER certificate is parsed
// (Certificate.ts:617-622 parseAsn1Certificate).
const MaxOperationalCertDERBytes = 600

// DecodeDER parses an X.509 DER operational certificate (RCAC, ICAC or
// NOC, as a PKI outside Matter issues them) into the [Certificate] its
// Matter TLV form carries — Rcac/Icac/Noc.fromAsn1 at 862ad75f
// (Certificate.ts:612-770 parseAsn1Certificate): the DER is refused over
// 600 bytes before parsing, the signature algorithm must be the same
// inside and outside the TBSCertificate (RFC 5280 §4.1.1.2), the DN
// attributes come back in their arrival order with the Matter ids parsed
// from their hex strings, a recognised extension (basicConstraints,
// keyUsage, extendedKeyUsage, subjectKeyIdentifier,
// authorityKeyIdentifier) without its value OCTET STRING is refused and an
// extension of any other OID is not read — whatever its value holds.
// The TBSCertificate bytes are kept on Certificate.TBSDER, which
// [TBSToDER] then returns unchanged (asUnsignedDer, Certificate.ts:196-199),
// and Certificate.Raw is the TLV form ([Encode]).
func DecodeDER(der []byte) (*Certificate, error) {
	if len(der) > MaxOperationalCertDERBytes {
		return nil, fmt.Errorf("%w: DER certificate of %d bytes exceeds the %d byte limit", ErrMalformed, len(der), MaxOperationalCertDERBytes)
	}
	var outer asn1.RawValue
	rest, err := asn1.Unmarshal(der, &outer)
	if err != nil || len(rest) != 0 || !outer.IsCompound || outer.Tag != asn1.TagSequence {
		return nil, fmt.Errorf("%w: not a DER certificate", ErrMalformed)
	}
	parts, err := derChildren(outer.Bytes)
	if err != nil || len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected 3 root elements, got %d", ErrMalformed, len(parts))
	}
	tbsNode, outerAlg, sigNode := parts[0], parts[1], parts[2]
	c := &Certificate{TBSDER: append([]byte(nil), tbsNode.FullBytes...)}

	fields, err := derChildren(tbsNode.Bytes)
	if err != nil || len(fields) < 7 {
		return nil, fmt.Errorf("%w: invalid TBSCertificate structure", ErrMalformed)
	}
	idx := 0
	if fields[idx].Class == asn1.ClassContextSpecific && fields[idx].Tag == 0 {
		idx++ // version [0]
	}
	c.SerialNumber = append([]byte(nil), fields[idx].Bytes...)
	idx++
	innerAlg := fields[idx]
	idx++
	if !bytes.Equal(innerAlg.FullBytes, outerAlg.FullBytes) {
		return nil, fmt.Errorf("%w: signature algorithm differs between the certificate and its TBSCertificate", ErrMalformed)
	}
	algOID, err := derFirstOID(innerAlg)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid signature algorithm structure", ErrMalformed)
	}
	if !algOID.Equal(oidEcdsaWithSHA256) {
		return nil, fmt.Errorf("%w: signature algorithm %v", ErrUnsupportedAlgorithm, algOID)
	}
	c.SignatureAlgorithm = SigAlgoECDSAWithSHA256
	if c.Issuer, err = derDN(fields[idx]); err != nil {
		return nil, fmt.Errorf("issuer: %w", err)
	}
	idx++
	validity, err := derChildren(fields[idx].Bytes)
	if err != nil || len(validity) != 2 {
		return nil, fmt.Errorf("%w: invalid validity structure", ErrMalformed)
	}
	if c.NotBefore, err = derDate(validity[0]); err != nil {
		return nil, err
	}
	if c.NotAfter, err = derDate(validity[1]); err != nil {
		return nil, err
	}
	idx++
	if c.Subject, err = derDN(fields[idx]); err != nil {
		return nil, fmt.Errorf("subject: %w", err)
	}
	idx++
	if c.PublicKey, err = derP256Key(fields[idx]); err != nil {
		return nil, err
	}
	c.PublicKeyAlgorithm, c.EllipticCurveID = PubKeyAlgoEC, CurvePrime256v1
	idx++
	if idx >= len(fields) || fields[idx].Class != asn1.ClassContextSpecific || fields[idx].Tag != 3 {
		return nil, fmt.Errorf("%w: missing required extensions in certificate", ErrMalformed)
	}
	if c.Extensions, err = derExtensions(fields[idx]); err != nil {
		return nil, err
	}
	if c.Signature, err = derECDSASignature(sigNode); err != nil {
		return nil, err
	}
	if err := validateMandatory(c); err != nil {
		return nil, err
	}
	if c.Raw, err = Encode(c); err != nil {
		return nil, err
	}
	return c, nil
}

// EncodeDER is c as a signed X.509 DER certificate — asSignedDer
// (Certificate.ts:183-191 at 862ad75f): the TBSCertificate rebuilt from
// the fields ([TBSToDER] without a kept TBSDER), the ecdsa-with-SHA256
// algorithm and the signature as a DER ECDSA-Sig-Value; refused over 600
// bytes.
func EncodeDER(c *Certificate) ([]byte, error) {
	tbs, err := buildTBS(c)
	if err != nil {
		return nil, err
	}
	tbsDER, err := asn1.Marshal(tbs)
	if err != nil {
		return nil, fmt.Errorf("matter cert: TBS asn1.Marshal: %w", err)
	}
	if len(c.Signature) != 64 {
		return nil, fmt.Errorf("%w: signature length=%d (want 64)", ErrMalformed, len(c.Signature))
	}
	sig, err := asn1.Marshal(ecdsaSignature{R: new(big.Int).SetBytes(c.Signature[:32]), S: new(big.Int).SetBytes(c.Signature[32:])})
	if err != nil {
		return nil, fmt.Errorf("matter cert: signature asn1.Marshal: %w", err)
	}
	out, err := asn1.Marshal(signedCertificate{
		TBS:                asn1.RawValue{FullBytes: tbsDER},
		SignatureAlgorithm: algorithmIdentifier{Algorithm: oidEcdsaWithSHA256},
		Signature:          asn1.BitString{Bytes: sig, BitLength: len(sig) * 8},
	})
	if err != nil {
		return nil, fmt.Errorf("matter cert: certificate asn1.Marshal: %w", err)
	}
	if len(out) > MaxOperationalCertDERBytes {
		return nil, fmt.Errorf("%w: DER certificate of %d bytes exceeds the %d byte limit", ErrMalformed, len(out), MaxOperationalCertDERBytes)
	}
	return out, nil
}

type signedCertificate struct {
	TBS                asn1.RawValue
	SignatureAlgorithm algorithmIdentifier
	Signature          asn1.BitString
}

type ecdsaSignature struct{ R, S *big.Int }

// derChildren splits a constructed value's content into its elements.
func derChildren(content []byte) ([]asn1.RawValue, error) {
	var out []asn1.RawValue
	for rest := content; len(rest) > 0; {
		var v asn1.RawValue
		var err error
		if rest, err = asn1.Unmarshal(rest, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// derFirstOID is the OBJECT IDENTIFIER that opens an AlgorithmIdentifier.
func derFirstOID(alg asn1.RawValue) (asn1.ObjectIdentifier, error) {
	parts, err := derChildren(alg.Bytes)
	if err != nil || len(parts) == 0 || parts[0].Tag != asn1.TagOID {
		return nil, fmt.Errorf("%w: no algorithm OID", ErrMalformed)
	}
	var oid asn1.ObjectIdentifier
	if _, err := asn1.Unmarshal(parts[0].FullBytes, &oid); err != nil {
		return nil, fmt.Errorf("%w: algorithm OID: %w", ErrMalformed, err)
	}
	return oid, nil
}

// derDN reads an X.509 Name into the Matter DN — parseSubjectOrIssuer
// (Certificate.ts:296-394): each AttributeTypeAndValue of each RDN SET in
// order; the Matter ids as hex strings (16 digits for the 64-bit ids, 8
// for a CAT), the standard X.520 attributes under their TLV tags
// (PrintableString as the tag + 128), an OID this module does not know
// skipped.
func derDN(name asn1.RawValue) (DistinguishedName, error) {
	var dn DistinguishedName
	rdns, err := derChildren(name.Bytes)
	if err != nil {
		return dn, fmt.Errorf("%w: invalid subject/issuer structure", ErrMalformed)
	}
	var seen []asn1.ObjectIdentifier
	once := func(oid asn1.ObjectIdentifier) error {
		for _, k := range seen {
			if k.Equal(oid) {
				return fmt.Errorf("%w: dn attribute %s repeated", ErrMalformed, oid)
			}
		}
		seen = append(seen, oid)
		return nil
	}
	for _, set := range rdns {
		attrs, err := derChildren(set.Bytes)
		if err != nil {
			continue
		}
		for _, attr := range attrs {
			kv, err := derChildren(attr.Bytes)
			if err != nil || len(kv) != 2 || kv[0].Tag != asn1.TagOID {
				continue
			}
			var oid asn1.ObjectIdentifier
			if _, err := asn1.Unmarshal(kv[0].FullBytes, &oid); err != nil {
				continue
			}
			value := string(kv[1].Bytes)
			if handled, err := dn.derMatterAttribute(oid, value, once); err != nil {
				return dn, err
			} else if handled {
				continue
			}
			tag, ok := standardDNTag(oid, kv[1].Tag)
			if !ok {
				continue
			}
			dn.Attributes = append(dn.Attributes, DNAttribute{Tag: tag, Value: value})
			dn.Order = append(dn.Order, tag)
		}
	}
	return dn, nil
}

// derMatterAttribute takes a Matter-specific DN attribute of a DER name
// into dn: the 64-bit ids and a CAT as hex strings, each single-valued id
// refused when it repeats (once). It reports false for an OID that is not
// a Matter attribute.
func (dn *DistinguishedName) derMatterAttribute(oid asn1.ObjectIdentifier, value string, once func(asn1.ObjectIdentifier) error) (bool, error) {
	id := func(bits int) (uint64, error) {
		v, err := strconv.ParseUint(value, 16, bits)
		if err != nil {
			return 0, fmt.Errorf("%w: matter id %q is not hex", ErrMalformed, value)
		}
		return v, nil
	}
	var (
		field *uint64
		has   *bool
		tag   uint8
	)
	switch {
	case oid.Equal(oidMatterVVSID):
		field, has, tag = &dn.MatterVVSID, &dn.HasVVSID, dnTagMatterVVSID
	case oid.Equal(oidMatterFirmwareSigningID):
		field, has, tag = &dn.MatterFirmwareSigningID, &dn.HasFirmwareSigningID, dnTagMatterFirmwareSignID
	case oid.Equal(oidMatterNodeID):
		field, has, tag = &dn.MatterNodeID, &dn.HasNodeID, dnTagMatterNodeID
	case oid.Equal(oidMatterICACID):
		field, has, tag = &dn.MatterICACID, &dn.HasICACID, dnTagMatterICACID
	case oid.Equal(oidMatterRCACID):
		field, has, tag = &dn.MatterRCACID, &dn.HasRCACID, dnTagMatterRCACID
	case oid.Equal(oidMatterFabricID):
		field, has, tag = &dn.MatterFabricID, &dn.HasFabricID, dnTagMatterFabricID
	case oid.Equal(oidMatterCASEAuthTag):
		v, err := id(32)
		if err != nil {
			return true, err
		}
		if v > math.MaxUint32 {
			return true, errors.New("mattercert: CASE authenticated tag exceeds 32 bits")
		}
		dn.CASEAuthTags = append(dn.CASEAuthTags, uint32(v))
		dn.Order = append(dn.Order, dnTagMatterCASEAuth)
		return true, nil
	default:
		return false, nil
	}
	if err := once(oid); err != nil {
		return true, err
	}
	v, err := id(64)
	if err != nil {
		return true, err
	}
	*field, *has = v, true
	dn.Order = append(dn.Order, tag)
	return true, nil
}

// standardDNTag is the DN TLV tag of a standard X.520 attribute OID with
// the ASN.1 string tag its value carries — the inverse of
// [standardDNAttribute].
func standardDNTag(oid asn1.ObjectIdentifier, asn1Tag int) (uint8, bool) {
	if oid.Equal(oidDomainComponent) {
		return dnTagStandardLast, true
	}
	if len(oid) != 4 || oid[0] != 2 || oid[1] != 5 || oid[2] != 4 {
		return 0, false
	}
	for i, n := range x520AttributeTypes {
		if oid[3] != n {
			continue
		}
		tag := dnTagStandardFirst + uint8(i)
		if asn1Tag == asn1.TagPrintableString {
			return tag + (dnTagPrintableFirst - dnTagStandardFirst), true
		}
		return tag, true
	}
	return 0, false
}

// derDate reads a UTCTime or GeneralizedTime as Matter epoch seconds —
// parseDate (Certificate.ts:559-596): the non-well-defined date
// 99991231235959Z is 0.
func derDate(v asn1.RawValue) (uint64, error) {
	var t time.Time
	if _, err := asn1.Unmarshal(v.FullBytes, &t); err != nil {
		return 0, fmt.Errorf("%w: unsupported date: %w", ErrMalformed, err)
	}
	t = t.UTC()
	if t.Equal(time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)) {
		return 0, nil
	}
	secs := t.Unix() - matterEpochUTCSeconds
	if secs < 0 {
		return 0, fmt.Errorf("%w: date %v before the Matter epoch", ErrMalformed, t)
	}
	return uint64(secs), nil
}

// derP256Key reads a SubjectPublicKeyInfo of an id-ecPublicKey on
// prime256v1 into the 65-byte uncompressed point (Certificate.ts:685-726).
func derP256Key(spki asn1.RawValue) ([]byte, error) {
	parts, err := derChildren(spki.Bytes)
	if err != nil || len(parts) != 2 {
		return nil, fmt.Errorf("%w: invalid public key structure", ErrMalformed)
	}
	alg, err := derChildren(parts[0].Bytes)
	if err != nil || len(alg) != 2 || alg[0].Tag != asn1.TagOID || alg[1].Tag != asn1.TagOID {
		return nil, fmt.Errorf("%w: invalid public key algorithm structure", ErrMalformed)
	}
	var keyOID, curveOID asn1.ObjectIdentifier
	if _, err := asn1.Unmarshal(alg[0].FullBytes, &keyOID); err != nil {
		return nil, fmt.Errorf("%w: public key algorithm: %w", ErrMalformed, err)
	}
	if _, err := asn1.Unmarshal(alg[1].FullBytes, &curveOID); err != nil {
		return nil, fmt.Errorf("%w: public key curve: %w", ErrMalformed, err)
	}
	if !keyOID.Equal(oidEcPublicKey) || !curveOID.Equal(oidPrime256v1) {
		return nil, fmt.Errorf("%w: public key %v on %v", ErrUnsupportedAlgorithm, keyOID, curveOID)
	}
	var key asn1.BitString
	if _, err := asn1.Unmarshal(parts[1].FullBytes, &key); err != nil || key.BitLength%8 != 0 {
		return nil, fmt.Errorf("%w: public key must be a BIT STRING without unused bits", ErrMalformed)
	}
	return append([]byte(nil), key.Bytes...), nil
}

// derExtensions reads the [3] Extensions into the Matter extension set —
// parseExtensions (Certificate.ts:406-547): the five Matter extensions by
// OID, in arrival order; one of them without its value OCTET STRING is
// refused; any other OID is not read.
func derExtensions(ctx asn1.RawValue) (CertificateExtensions, error) {
	var ext CertificateExtensions
	seqs, err := derChildren(ctx.Bytes)
	if err != nil || len(seqs) != 1 {
		return ext, fmt.Errorf("%w: invalid extensions structure", ErrMalformed)
	}
	list, err := derChildren(seqs[0].Bytes)
	if err != nil {
		return ext, fmt.Errorf("%w: invalid extensions structure", ErrMalformed)
	}
	for _, e := range list {
		parts, err := derChildren(e.Bytes)
		if err != nil || len(parts) < 2 || parts[0].Tag != asn1.TagOID {
			continue
		}
		var oid asn1.ObjectIdentifier
		if _, err := asn1.Unmarshal(parts[0].FullBytes, &oid); err != nil {
			continue
		}
		var tag uint8
		switch {
		case oid.Equal(oidExtBasicConstraints):
			tag = extTagBasicConstraints
		case oid.Equal(oidExtKeyUsage):
			tag = extTagKeyUsage
		case oid.Equal(oidExtExtendedKeyUsage):
			tag = extTagExtendedKeyUsage
		case oid.Equal(oidExtSubjectKeyID):
			tag = extTagSubjectKeyID
		case oid.Equal(oidExtAuthorityKeyID):
			tag = extTagAuthorityKeyID
		default:
			continue
		}
		valueIdx := 1
		if len(parts) > 2 && parts[1].Tag == asn1.TagBoolean {
			valueIdx = 2 // critical
		}
		value := parts[valueIdx]
		if value.Tag != asn1.TagOctetString || value.Class != asn1.ClassUniversal {
			return ext, fmt.Errorf("%w: extension %v is missing its value octet string", ErrMalformed, oid)
		}
		if err := derExtension(&ext, tag, value.Bytes); err != nil {
			return ext, err
		}
		ext.Order = append(ext.Order, tag)
	}
	// REQUIRED_EXTENSIONS (Certificate.ts:397-401): basicConstraints,
	// keyUsage, subjectKeyIdentifier and authorityKeyIdentifier, for every
	// operational kind (Rcac/Icac/Noc.fromAsn1 pass the default).
	if !ext.HasBasicConstraints || !ext.HasKeyUsage || !ext.HasSubjectKeyID || !ext.HasAuthorityKeyID {
		return ext, fmt.Errorf("%w: missing required extensions in certificate", ErrMalformed)
	}
	return ext, nil
}

func derExtension(ext *CertificateExtensions, tag uint8, value []byte) error {
	switch tag {
	case extTagBasicConstraints:
		var bc struct {
			IsCA    bool `asn1:"optional"`
			PathLen int  `asn1:"optional,default:-1"`
		}
		if _, err := asn1.Unmarshal(value, &bc); err != nil {
			return fmt.Errorf("%w: basic-constraints: %w", ErrMalformed, err)
		}
		ext.HasBasicConstraints, ext.BasicConstraintsIsCA = true, bc.IsCA
		if bc.PathLen >= 0 {
			ext.BasicConstraintsHasPathLen, ext.BasicConstraintsPathLen = true, uint8(bc.PathLen) //nolint:gosec // Matter caps the path length at 1
		}
	case extTagKeyUsage:
		var bits asn1.BitString
		if _, err := asn1.Unmarshal(value, &bits); err != nil {
			return fmt.Errorf("%w: key-usage: %w", ErrMalformed, err)
		}
		var usage uint16
		for i := range 9 {
			if bits.At(i) == 1 {
				usage |= 1 << i
			}
		}
		ext.HasKeyUsage, ext.KeyUsage = true, usage
	case extTagExtendedKeyUsage:
		var oids []asn1.ObjectIdentifier
		if _, err := asn1.Unmarshal(value, &oids); err != nil {
			return fmt.Errorf("%w: extended-key-usage: %w", ErrMalformed, err)
		}
		var eku []uint8
		for _, o := range oids {
			switch {
			case o.Equal(oidEKUServerAuth):
				eku = append(eku, 1)
			case o.Equal(oidEKUClientAuth):
				eku = append(eku, 2)
			case o.Equal(oidEKUCodeSigning):
				eku = append(eku, 3)
			case o.Equal(oidEKUEmailProtection):
				eku = append(eku, 4)
			case o.Equal(oidEKUTimeStamping):
				eku = append(eku, 5)
			case o.Equal(oidEKUOCSPSigning):
				eku = append(eku, 6)
			}
		}
		if len(eku) > 0 {
			ext.HasExtendedKeyUsage, ext.ExtendedKeyUsage = true, eku
		}
	case extTagSubjectKeyID:
		var id []byte
		if _, err := asn1.Unmarshal(value, &id); err != nil {
			return fmt.Errorf("%w: subject-key-id: %w", ErrMalformed, err)
		}
		ext.HasSubjectKeyID, ext.SubjectKeyID = true, id
	case extTagAuthorityKeyID:
		var aki struct {
			KeyID []byte `asn1:"optional,tag:0"`
		}
		if _, err := asn1.Unmarshal(value, &aki); err != nil {
			return fmt.Errorf("%w: authority-key-id: %w", ErrMalformed, err)
		}
		if len(aki.KeyID) > 0 {
			ext.HasAuthorityKeyID, ext.AuthorityKeyID = true, aki.KeyID
		}
	}
	return nil
}

// derECDSASignature reads the certificate's signature BIT STRING, a DER
// ECDSA-Sig-Value, as the 64-byte r‖s the TLV carries.
func derECDSASignature(v asn1.RawValue) ([]byte, error) {
	var bits asn1.BitString
	if _, err := asn1.Unmarshal(v.FullBytes, &bits); err != nil {
		return nil, fmt.Errorf("%w: signature: %w", ErrMalformed, err)
	}
	var sig ecdsaSignature
	if _, err := asn1.Unmarshal(bits.Bytes, &sig); err != nil {
		return nil, fmt.Errorf("%w: signature is not an ECDSA-Sig-Value: %w", ErrMalformed, err)
	}
	out := make([]byte, 64)
	sig.R.FillBytes(out[:32])
	sig.S.FillBytes(out[32:])
	return out, nil
}

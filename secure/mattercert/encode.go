// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"fmt"

	"github.com/SukramJ/go-fabric/tlv"
)

// Encode writes c as a Matter TLV operational certificate — the bytes
// [Decode] reads, with the signature — in the schema's field order, the
// DN attributes and the extensions in the order c records
// ([DistinguishedName].Order, [CertificateExtensions].Order; the canonical
// order when a caller built c without one). A certificate that came
// through Decode encodes to the bytes it was decoded from. Mirrors
// matter.js packages/protocol/src/certificate/kinds/Noc.ts:52-54
// asSignedTlv (Rcac.ts, Icac.ts alike) over the schema of
// packages/protocol/src/certificate/kinds/definitions/operational.ts:123-153
// at 862ad75f, whose DN lists and extension list are
// TlvTaggedListPreservingOrder (:36, :110).
func Encode(c *Certificate) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("%w: nil certificate", ErrMalformed)
	}
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutOctets(tlv.ContextTag(tagSerialNumber), c.SerialNumber)
	e.PutUint(tlv.ContextTag(tagSignatureAlgorithm), c.SignatureAlgorithm)
	if err := encodeDN(e, tagIssuer, &c.Issuer); err != nil {
		return nil, fmt.Errorf("issuer: %w", err)
	}
	e.PutUint(tlv.ContextTag(tagNotBefore), c.NotBefore)
	e.PutUint(tlv.ContextTag(tagNotAfter), c.NotAfter)
	if err := encodeDN(e, tagSubject, &c.Subject); err != nil {
		return nil, fmt.Errorf("subject: %w", err)
	}
	e.PutUint(tlv.ContextTag(tagPublicKeyAlgorithm), c.PublicKeyAlgorithm)
	e.PutUint(tlv.ContextTag(tagEllipticCurveID), c.EllipticCurveID)
	e.PutOctets(tlv.ContextTag(tagEllipticCurvePub), c.PublicKey)
	if err := encodeExtensions(e, &c.Extensions); err != nil {
		return nil, fmt.Errorf("extensions: %w", err)
	}
	e.PutOctets(tlv.ContextTag(tagSignature), c.Signature)
	if err := e.EndContainer(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	return e.Bytes()
}

// encodeDN writes dn as the TLV list of tag, every attribute in dn's
// arrival order — the Matter ids as the 64-bit (CATs 32-bit) unsigned
// integers the DN TLV carries (operational.ts:69-110), the standard X.520
// attributes as UTF-8 strings under their tags (1-16, 129-143).
func encodeDN(e *tlv.Encoder, tag uint8, dn *DistinguishedName) error {
	order := dn.Order
	if len(order) == 0 {
		order = canonicalDNOrder(dn)
	}
	e.StartList(tlv.ContextTag(tag))
	catIdx, attrIdx := 0, 0
	for _, t := range order {
		switch t {
		case dnTagMatterNodeID:
			e.PutUint(tlv.ContextTag(t), dn.MatterNodeID)
		case dnTagMatterICACID:
			e.PutUint(tlv.ContextTag(t), dn.MatterICACID)
		case dnTagMatterRCACID:
			e.PutUint(tlv.ContextTag(t), dn.MatterRCACID)
		case dnTagMatterFabricID:
			e.PutUint(tlv.ContextTag(t), dn.MatterFabricID)
		case dnTagMatterCASEAuth:
			if catIdx >= len(dn.CASEAuthTags) {
				return fmt.Errorf("%w: CAT order entry without value", ErrMalformed)
			}
			e.PutUint(tlv.ContextTag(t), uint64(dn.CASEAuthTags[catIdx]))
			catIdx++
		default:
			if !isStandardDNTag(t) {
				return fmt.Errorf("%w: unknown DN tag %d in order", ErrMalformed, t)
			}
			if attrIdx >= len(dn.Attributes) {
				return fmt.Errorf("%w: DN attribute order entry without value", ErrMalformed)
			}
			e.PutUTF8(tlv.ContextTag(t), dn.Attributes[attrIdx].Value)
			attrIdx++
		}
	}
	return e.EndContainer()
}

// canonicalDNOrder lists a DN's present attributes in the spec's tag order
// for a DistinguishedName built without Order (the Matter ids, then the
// CATs, then the standard attributes).
func canonicalDNOrder(dn *DistinguishedName) []uint8 {
	var order []uint8
	if dn.HasRCACID {
		order = append(order, dnTagMatterRCACID)
	}
	if dn.HasICACID {
		order = append(order, dnTagMatterICACID)
	}
	if dn.HasNodeID {
		order = append(order, dnTagMatterNodeID)
	}
	if dn.HasFabricID {
		order = append(order, dnTagMatterFabricID)
	}
	for range dn.CASEAuthTags {
		order = append(order, dnTagMatterCASEAuth)
	}
	for _, a := range dn.Attributes {
		order = append(order, a.Tag)
	}
	return order
}

// encodeExtensions writes the extensions list (top-level tag 10) in ext's
// arrival order: basic-constraints as the struct {is-ca, path-len?},
// key-usage as the 16-bit bitmap, extended-key-usage as the array of
// key-purpose ids, the key identifiers as octet strings and every future
// extension as its raw DER (operational.ts:112-121).
func encodeExtensions(e *tlv.Encoder, ext *CertificateExtensions) error {
	order := ext.Order
	if len(order) == 0 {
		order = canonicalExtensionOrder(ext)
	}
	e.StartList(tlv.ContextTag(tagExtensions))
	futureIdx := 0
	for _, t := range order {
		switch t {
		case extTagBasicConstraints:
			e.StartStruct(tlv.ContextTag(t))
			e.PutBool(tlv.ContextTag(extBCInnerTagIsCa), ext.BasicConstraintsIsCA)
			if ext.BasicConstraintsHasPathLen {
				e.PutUint(tlv.ContextTag(extBCInnerTagPathLen), uint64(ext.BasicConstraintsPathLen))
			}
			if err := e.EndContainer(); err != nil {
				return err
			}
		case extTagKeyUsage:
			e.PutUint(tlv.ContextTag(t), uint64(ext.KeyUsage))
		case extTagExtendedKeyUsage:
			e.StartArray(tlv.ContextTag(t))
			for _, v := range ext.ExtendedKeyUsage {
				e.PutUint(tlv.AnonymousTag(), uint64(v))
			}
			if err := e.EndContainer(); err != nil {
				return err
			}
		case extTagSubjectKeyID:
			e.PutOctets(tlv.ContextTag(t), ext.SubjectKeyID)
		case extTagAuthorityKeyID:
			e.PutOctets(tlv.ContextTag(t), ext.AuthorityKeyID)
		case extTagFutureExtension:
			if futureIdx >= len(ext.FutureExtensions) {
				return fmt.Errorf("%w: future-extension order entry without value", ErrMalformed)
			}
			e.PutOctets(tlv.ContextTag(t), ext.FutureExtensions[futureIdx])
			futureIdx++
		default:
			return fmt.Errorf("%w: unknown extension tag %d in order", ErrMalformed, t)
		}
	}
	return e.EndContainer()
}

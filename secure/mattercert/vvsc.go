// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"errors"
	"fmt"
)

// ErrInvalidVVSC is a Vendor ID Verification Signer Certificate that
// fails its profile — matter.js's CertificateError of
// packages/protocol/src/certificate/kinds/Vvsc.ts at db021da0.
var ErrInvalidVVSC = errors.New("mattercert: VVSC profile violated")

// DecodeVVSC decodes a VVSC from its Matter TLV form and checks its
// profile ([ValidateVVSC]). Decoding refuses what [Decode] refuses, a
// repeated Matter DN attribute among it. Mirrors matter.js Vvsc.fromTlv
// with the profile check of Vvsc.verify (Vvsc.ts:19-22, :45-80 at
// db021da0).
func DecodeVVSC(raw []byte) (*Certificate, error) {
	c, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	if err := ValidateVVSC(c); err != nil {
		return nil, err
	}
	return c, nil
}

// ValidateVVSC checks the profile of a Vendor ID Verification Signer
// Certificate (Matter Core §6.5.12): at most five RDNs in its subject and
// in its issuer (generalVerify, OperationalBase.ts:61-107, the RDN rule
// :91-97), basic
// constraints with is-ca false (Vvsc.ts:25-30 validateFields: "The basic
// constraints extension SHALL be encoded with is-ca set to false"), a key
// usage of exactly digitalSignature, an unnamed bit counted as set
// (:52-58), a matter-vvs-id (:61-64) and no matter-node-id (:66-69),
// matter-fabric-id (:71-74) or CAT (:76-79) in the subject, and the
// 160-bit key identifiers the certificate schema requires
// (definitions/operational.ts:47-48). It checks the fields as they stand,
// so a decoded certificate changed afterwards is checked as changed, as
// Vvsc.verify reads its cert. The chain of a VVSC is not verified, as
// matter.js does not ("TODO Verify the VVSC chain", Vvsc.ts:43). Mirrors
// matter.js Vvsc.ts at db021da0 (matter.js #4880).
func ValidateVVSC(c *Certificate) error {
	if c == nil {
		return fmt.Errorf("%w: nil certificate", ErrInvalidVVSC)
	}
	if n := len(c.Subject.Order); n > maxDNRDNs {
		return fmt.Errorf("%w: subject carries %d RDNs, at most %d allowed", ErrInvalidVVSC, n, maxDNRDNs)
	}
	if n := len(c.Issuer.Order); n > maxDNRDNs {
		return fmt.Errorf("%w: issuer carries %d RDNs, at most %d allowed", ErrInvalidVVSC, n, maxDNRDNs)
	}
	ext := c.Extensions
	if !ext.HasBasicConstraints {
		return fmt.Errorf("%w: basic constraints missing", ErrInvalidVVSC)
	}
	if ext.BasicConstraintsIsCA {
		return fmt.Errorf("%w: a VVSC must not be a CA", ErrInvalidVVSC)
	}
	if !ext.HasKeyUsage || ext.KeyUsage != keyUsageDigitalSignature {
		return fmt.Errorf("%w: keyUsage must have only digitalSignature set: %#04x", ErrInvalidVVSC, ext.KeyUsage)
	}
	if !c.Subject.HasVVSID {
		return fmt.Errorf("%w: subject must carry a matter-vvs-id", ErrInvalidVVSC)
	}
	if c.Subject.HasNodeID {
		return fmt.Errorf("%w: subject must not carry a matter-node-id", ErrInvalidVVSC)
	}
	if c.Subject.HasFabricID {
		return fmt.Errorf("%w: subject must not carry a matter-fabric-id", ErrInvalidVVSC)
	}
	if len(c.Subject.CASEAuthTags) > 0 {
		return fmt.Errorf("%w: subject must not carry CATs", ErrInvalidVVSC)
	}
	if !ext.HasSubjectKeyID || len(ext.SubjectKeyID) != 20 {
		return fmt.Errorf("%w: subjectKeyIdentifier must be 160 bit", ErrInvalidVVSC)
	}
	if !ext.HasAuthorityKeyID || len(ext.AuthorityKeyID) != 20 {
		return fmt.Errorf("%w: authorityKeyIdentifier must be 160 bit", ErrInvalidVVSC)
	}
	return nil
}

// VerifyVVSCIssuedBy checks that issuerPublicKey (65-byte uncompressed
// P-256) signed the VVSC c — the one step of the VVSC path the verifier of
// a VID Verification Statement takes against a trust anchor of the vendor
// (Matter Core §6.4.10.1, the text matter.js quotes at
// VendorIdVerification.ts:233-240 at db021da0, where the path check is
// still a TODO). The signature covers the X.509 DER TBSCertificate, as
// for every operational certificate (matter.js Certificate.verify).
func VerifyVVSCIssuedBy(c *Certificate, issuerPublicKey []byte) error {
	if c == nil {
		return fmt.Errorf("%w: nil certificate", ErrInvalidVVSC)
	}
	issuer := &Certificate{PublicKey: issuerPublicKey, PublicKeyAlgorithm: PubKeyAlgoEC, EllipticCurveID: CurvePrime256v1}
	key, err := issuer.PublicKeyECDSA()
	if err != nil {
		return fmt.Errorf("%w: issuer key: %w", ErrInvalidVVSC, err)
	}
	return verifySignature(c, key)
}

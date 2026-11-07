// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert_test

// The VVSC rows of matter.js
// packages/protocol/test/certificate/OperationalCertificateVerifyTest.ts at
// db021da0 ("conformant certificates" :469-471, "VVSC extensions"
// :787-826, "VVSC subject DN" :828-862): the profile of a Vendor ID
// Verification Signer Certificate (Vvsc.ts, matter.js #4880). matter.js
// checks is-ca at construction and the rest in Vvsc.verify; here
// ValidateVVSC checks both, on the decoded certificate (DecodeVVSC) and on
// a certificate changed after decoding. matter.js's "refuses a second
// matter-vvs-id" is the decoder's repeated-attribute rule
// (TestDecodeDNRulesPinDB021da0).

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// vvscCert is matter.js's vvscCert (:160-178): issuer rcac-id 1, subject
// vvs-id 3, not a CA, digitalSignature, 160-bit key identifiers. Its
// signature is not checked: matter.js verifies no VVSC chain (Vvsc.ts:43).
func vvscCert(mutate func(c *mattercert.Certificate)) *mattercert.Certificate {
	pub := make([]byte, 65)
	pub[0] = 0x04
	c := &mattercert.Certificate{
		SerialNumber: []byte{0x01}, SignatureAlgorithm: mattercert.SigAlgoECDSAWithSHA256,
		Issuer:    mattercert.DistinguishedName{MatterRCACID: 1, HasRCACID: true},
		Subject:   mattercert.DistinguishedName{MatterVVSID: 3, HasVVSID: true},
		NotBefore: 1, NotAfter: 0x7FFF_FFFF,
		PublicKeyAlgorithm: mattercert.PubKeyAlgoEC, EllipticCurveID: mattercert.CurvePrime256v1, PublicKey: pub,
		Extensions: mattercert.CertificateExtensions{
			HasBasicConstraints: true,
			HasKeyUsage:         true, KeyUsage: 0x0001,
			HasSubjectKeyID: true, SubjectKeyID: make([]byte, 20),
			HasAuthorityKeyID: true, AuthorityKeyID: make([]byte, 20),
		},
		Signature: make([]byte, 64),
	}
	if mutate != nil {
		mutate(c)
	}
	return c
}

// verifyVVSC encodes c and decodes it as a VVSC (:378-380).
func verifyVVSC(t *testing.T, c *mattercert.Certificate) error {
	t.Helper()
	raw, err := mattercert.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mattercert.DecodeVVSC(raw)
	return err
}

func TestVVSCProfileParity(t *testing.T) {
	t.Parallel()
	t.Run("verifies a VVSC", func(t *testing.T) {
		t.Parallel()
		if err := verifyVVSC(t, vvscCert(nil)); err != nil {
			t.Fatalf("DecodeVVSC: %v", err)
		}
	})
	refused := []struct {
		name   string
		mutate func(c *mattercert.Certificate)
	}{
		{"refuses a VVSC that is a CA", func(c *mattercert.Certificate) { c.Extensions.BasicConstraintsIsCA = true }},
		{"refuses a VVSC with a key usage flag besides digitalSignature", func(c *mattercert.Certificate) { c.Extensions.KeyUsage = 0x0001 | 0x0004 }},
		{"refuses a VVSC key usage bitmap with a bit that has no name", func(c *mattercert.Certificate) { c.Extensions.KeyUsage = 0x0001 | 0x0200 }},
		{"refuses a VVSC with an empty key usage", func(c *mattercert.Certificate) { c.Extensions.KeyUsage = 0 }},
		{"refuses a VVSC without digitalSignature", func(c *mattercert.Certificate) { c.Extensions.KeyUsage = 0x0020 }},
		{"refuses a VVSC without a matter-vvs-id", func(c *mattercert.Certificate) { c.Subject.HasVVSID, c.Subject.MatterVVSID = false, 0 }},
		{"refuses a matter-node-id", func(c *mattercert.Certificate) { c.Subject.HasNodeID, c.Subject.MatterNodeID = true, 0x1234 }},
		{"refuses a matter-fabric-id", func(c *mattercert.Certificate) { c.Subject.HasFabricID, c.Subject.MatterFabricID = true, 1 }},
		{"refuses CATs (Vvsc.ts:76-79)", func(c *mattercert.Certificate) { c.Subject.CASEAuthTags = []uint32{0x0001_0001} }},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := vvscCert(tc.mutate)
			if err := mattercert.ValidateVVSC(c); !errors.Is(err, mattercert.ErrInvalidVVSC) {
				t.Fatalf("ValidateVVSC: %v, want ErrInvalidVVSC", err)
			}
			if err := verifyVVSC(t, c); err == nil {
				t.Fatal("DecodeVVSC accepted it")
			}
		})
	}
	t.Run("refuses a decoded VVSC whose key usage flags were changed afterwards", func(t *testing.T) {
		t.Parallel()
		raw, err := mattercert.Encode(vvscCert(nil))
		if err != nil {
			t.Fatal(err)
		}
		c, err := mattercert.DecodeVVSC(raw)
		if err != nil {
			t.Fatal(err)
		}
		c.Extensions.KeyUsage |= 0x0004 // keyEncipherment
		if err := mattercert.ValidateVVSC(c); !errors.Is(err, mattercert.ErrInvalidVVSC) {
			t.Fatalf("ValidateVVSC: %v, want ErrInvalidVVSC", err)
		}
	})
}

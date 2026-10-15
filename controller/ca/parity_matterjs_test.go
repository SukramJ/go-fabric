// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// TestParityConstants: the certificate ids and validity of matter.js
// packages/protocol/src/certificate/CertificateAuthority.ts at 0ceb202d.
func TestParityConstants(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name      string
		got, want int64
		line      string
	}{
		{"#rootCertId", int64(ca.RootCertID), 0, ":52"},
		{"#nextCertificateId", int64(ca.FirstCertificateID), 1, ":56"},
		{"notBefore years", ca.NotBeforeYears, -1, ":233, :263, :304"},
		{"notAfter years", ca.NotAfterYears, 10, ":234, :264, :305"},
	}
	for _, r := range rows {
		if r.got != r.want {
			t.Errorf("%s (CertificateAuthority.ts%s) = %d, want %d", r.name, r.line, r.got, r.want)
		}
	}
}

// TestParityCertificateFields: the extensions of the RCAC (:237-245), the
// ICAC (:267-275) and the NOC (:308-316) as mattercert decodes them.
func TestParityCertificateFields(t *testing.T) {
	t.Parallel()
	c, err := ca.New(ca.Config{Now: fixedNow, IntermediateCA: true})
	if err != nil {
		t.Fatal(err)
	}
	noc, err := c.GenerateNOC(newKey(t), 1, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		name     string
		raw      []byte
		isCA     bool
		keyUsage uint16
		eku      []uint8
	}{
		{"RCAC", c.RootCert(), true, mattercert.KeyUsageKeyCertSign | mattercert.KeyUsageCRLSign, nil},
		{"ICAC", c.ICACCert(), true, mattercert.KeyUsageKeyCertSign | mattercert.KeyUsageCRLSign, nil},
		{"NOC", noc, false, 0x0001 /* digitalSignature, base.ts:11 */, []uint8{2, 1}},
	}
	for _, r := range rows {
		cert := decode(t, r.raw)
		ext := cert.Extensions
		switch {
		case cert.SignatureAlgorithm != 1 || cert.PublicKeyAlgorithm != 1 || cert.EllipticCurveID != 1:
			t.Errorf("%s: algorithms %d/%d/%d, want 1/1/1", r.name, cert.SignatureAlgorithm, cert.PublicKeyAlgorithm, cert.EllipticCurveID)
		case ext.BasicConstraintsIsCA != r.isCA || ext.BasicConstraintsHasPathLen:
			t.Errorf("%s: basicConstraints isCa %v pathLen %v", r.name, ext.BasicConstraintsIsCA, ext.BasicConstraintsHasPathLen)
		case ext.KeyUsage != r.keyUsage:
			t.Errorf("%s: keyUsage 0x%04X, want 0x%04X", r.name, ext.KeyUsage, r.keyUsage)
		case !slices.Equal(ext.ExtendedKeyUsage, r.eku) || ext.HasExtendedKeyUsage != (r.eku != nil):
			t.Errorf("%s: extendedKeyUsage %v, want %v", r.name, ext.ExtendedKeyUsage, r.eku)
		case !bytes.Equal(ext.SubjectKeyID, ca.KeyIdentifier(cert.PublicKey)):
			t.Errorf("%s: subjectKeyIdentifier is not SHA-1 of the key", r.name)
		}
	}
}

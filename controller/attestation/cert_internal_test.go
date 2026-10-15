// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

// TestFallbackIDs: parseMatterFallbackVidPid (Certificate.ts:249-258) and
// the OID values' parseInt(_, 16) (:345-352).
func TestFallbackIDs(t *testing.T) {
	t.Parallel()
	id, err := fallbackID("Matter Mvid:FFF1 Mpid:8000", "Mvid:")
	if err != nil || id == nil || *id != 0xFFF1 {
		t.Fatalf("Mvid: %v %v", id, err)
	}
	if id, err = fallbackID("no prefix", "Mpid:"); err != nil || id != nil {
		t.Fatalf("absent: %v %v", id, err)
	}
	if _, err = fallbackID("Mpid:80", "Mpid:"); err == nil {
		t.Fatal("a malformed Mpid: was accepted")
	}
	if _, err = fallbackID("Mvid:fff1", "Mvid:"); err == nil {
		t.Fatal("lowercase hex was accepted")
	}
	for in, want := range map[string]int{"FFF1": 0xFFF1, "8000xyz": 0x8000, "zz": -1, "": -1, "fff1": 0xFFF1} {
		got := parseHexID(in)
		if (want < 0) != (got == nil) || (got != nil && int(*got) != want) {
			t.Errorf("parseHexID(%q) = %v, want %d", in, got, want)
		}
	}
}

func selfSigned(t *testing.T, tmpl *x509.Certificate, curve elliptic.Curve) *x509.Certificate {
	t.Helper()
	k, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl.SerialNumber = big.NewInt(1)
	tmpl.NotBefore = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tmpl.NotAfter = nonWellDefined
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestCertificateRules: the required extensions (Certificate.ts:397-401,
// :549-551), the CN fallback when no OID is present, the non-well-defined
// notAfter as 0, and the P-256 key rule.
func TestCertificateRules(t *testing.T) {
	t.Parallel()
	full := &x509.Certificate{
		Subject:               pkix.Name{CommonName: "Mvid:FFF1 Mpid:8001"},
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature,
		SubjectKeyId: make([]byte, 20), AuthorityKeyId: make([]byte, 20),
	}
	c := selfSigned(t, full, elliptic.P256())
	if err := checkRequiredExtensions(c, true); err != nil {
		t.Fatal(err)
	}
	a, err := parseAttCert(c.Raw, true)
	if err != nil || a.vendorID == nil || *a.vendorID != 0xFFF1 || a.productID == nil || *a.productID != 0x8001 || a.notAfter != 0 {
		t.Fatalf("parsed %+v (%v)", a, err)
	}
	if _, err := p256Key(c); err != nil {
		t.Fatal(err)
	}
	if _, err := p256Key(selfSigned(t, &x509.Certificate{}, elliptic.P384())); err == nil {
		t.Fatal("a P-384 key was accepted")
	}
	if verifySignedBy(selfSigned(t, &x509.Certificate{SignatureAlgorithm: x509.ECDSAWithSHA384}, elliptic.P384()), &ecdsa.PublicKey{}) == nil {
		t.Fatal("ecdsa-with-SHA384 was accepted")
	}
	for name, tmpl := range map[string]*x509.Certificate{
		"basicConstraints": {KeyUsage: x509.KeyUsageDigitalSignature, SubjectKeyId: make([]byte, 20), AuthorityKeyId: make([]byte, 20)},
		"keyUsage":         {BasicConstraintsValid: true, SubjectKeyId: make([]byte, 20), AuthorityKeyId: make([]byte, 20)},
		"authorityKeyId":   {BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, SubjectKeyId: make([]byte, 20)},
	} {
		if err := checkRequiredExtensions(selfSigned(t, tmpl, elliptic.P256()), true); err == nil {
			t.Errorf("missing %s accepted", name)
		}
	}
	bad := selfSigned(t, &x509.Certificate{Subject: pkix.Name{CommonName: "Mvid:12"}, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, SubjectKeyId: make([]byte, 20), AuthorityKeyId: make([]byte, 20)}, elliptic.P256())
	if _, err := parseAttCert(bad.Raw, true); err == nil {
		t.Fatal("a malformed Mvid: fallback parsed")
	}
	if serialBytes(big.NewInt(0x80))[0] != 0 || len(serialBytes(big.NewInt(0))) != 1 {
		t.Fatal("serialBytes is not the DER INTEGER content")
	}
}

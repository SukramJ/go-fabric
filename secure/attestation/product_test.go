// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestValidateProductRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	if err := ValidateProduct(nil, 0xfff1, 0x8000); err == nil || !strings.Contains(err.Error(), "nil chain") {
		t.Fatalf("ValidateProduct(nil) = %v, want the nil-chain refusal", err)
	}
	chain, err := BuildTestChain(0xfff1, 0x8000)
	if err != nil {
		t.Fatalf("BuildTestChain: %v", err)
	}
	if err := ValidateProduct(&Chain{DAC: []byte{1, 2, 3}, PAI: chain.PAI}, 0xfff1, 0x8000); err == nil || !strings.Contains(err.Error(), "DAC") {
		t.Fatalf("ValidateProduct(unparseable DAC) = %v, want the DAC parse refusal", err)
	}
	if err := ValidateProduct(&Chain{DAC: chain.DAC, PAI: []byte{1, 2, 3}}, 0xfff1, 0x8000); err == nil || !strings.Contains(err.Error(), "PAI") {
		t.Fatalf("ValidateProduct(unparseable PAI) = %v, want the PAI parse refusal", err)
	}
	if err := ValidateProduct(chain, 0xfff1, 0x8000); err != nil {
		t.Fatalf("ValidateProduct(matching) = %v", err)
	}
}

// productIDsOf reads the Matter OIDs first; the `Mvid:`/`Mpid:` fallback
// of the commonName applies only when the DN carries neither
// (Certificate.ts:341-351, :377-381 at db021da0).
func TestProductIDsOfReadsTheOIDsElseTheCommonName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		dn       pkix.Name
		vid, pid *uint16
	}{
		{"both OIDs", dnOf(attr(oidMatterVendorID, "FFF1"), attr(oidMatterProductID, "8000")), id(0xfff1), id(0x8000)},
		{"OID wins over the commonName", dnOf(attr(oidCommonName, "Mvid:0001 Mpid:0002"), attr(oidMatterVendorID, "FFF1")), id(0xfff1), nil},
		{"a non-string value is not an id", dnOf(attr(oidMatterVendorID, 5), attr(oidMatterProductID, "8000")), nil, id(0x8000)},
		{"a non-hex value is not an id", dnOf(attr(oidMatterVendorID, "zz"), attr(oidMatterProductID, "FFFFF")), nil, nil},
		{"commonName fallback", dnOf(attr(oidCommonName, "Mvid:FFF1 Mpid:8000")), id(0xfff1), id(0x8000)},
		{"commonName with the vendor only", dnOf(attr(oidCommonName, "ACME Mvid:FFF1")), id(0xfff1), nil},
		{"nothing", dnOf(attr(oidCommonName, "ACME")), nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vid, pid, err := productIDsOf(tc.dn)
			if err != nil || !sameID(vid, tc.vid) || !sameID(pid, tc.pid) {
				t.Fatalf("productIDsOf = (%s, %s, %v), want (%s, %s)", fmtID(vid), fmtID(pid), err, fmtID(tc.vid), fmtID(tc.pid))
			}
		})
	}
}

// TestProductIDsAcceptMalformed: the cases of matter.js
// AttestationCertificateProfileTest.ts describe "VendorID and ProductID in
// the distinguished name" (:427-644 at db021da0) that parse with
// acceptMalformedVidPid, the option the device's read of its own DAC and
// PAI sets (DeviceCertification.ts:116-120): a malformed or repeated
// VendorID or ProductID reads as absent, still turns the commonName
// fallback off, and the fallback's own rule is unchanged. The DNs are the
// struct_dac_cert_version_v3 vector's as crypto/x509 parses them: the DAC
// subject commonName "Matter Test DAC", VendorID FFF1, ProductID 8000
// (:490); the PAI subject a commonName and VendorID FFF1.
func TestProductIDsAcceptMalformed(t *testing.T) {
	t.Parallel()
	cn, vid, pid := attr(oidCommonName, "Matter Test DAC"), attr(oidMatterVendorID, "FFF1"), attr(oidMatterProductID, "8000")
	cases := []struct {
		name         string
		dn           pkix.Name
		wantV, wantP *uint16
	}{
		// "parses a DAC with a ${name} when accepting a malformed VendorID
		// or ProductID, without the value" (:442-446).
		{"lowercase VendorID", dnOf(cn, attr(oidMatterVendorID, "fff1"), pid), nil, id(0x8000)},
		{"VendorID of the wrong length", dnOf(cn, attr(oidMatterVendorID, "FFF"), pid), nil, id(0x8000)},
		{"non-hexadecimal ProductID", dnOf(cn, vid, attr(oidMatterProductID, "80G0")), id(0xfff1), nil},
		{"repeated VendorID", dnOf(cn, vid, pid, vid), nil, id(0x8000)},
		{"repeated ProductID", dnOf(cn, vid, pid, pid), id(0xfff1), nil},
		// "keeps the valid one of the two values when only the other is
		// malformed" (:454-459).
		{"keeps the valid one of the two values", dnOf(cn, attr(oidMatterVendorID, "fff1"), pid), nil, id(0x8000)},
		// "reads a VendorID as absent when the first of two VendorIDs is
		// malformed and a malformed VendorID is accepted" (:461-467).
		{"the first of two VendorIDs malformed", dnOf(cn, attr(oidMatterVendorID, "fff1"), pid, vid), nil, id(0x8000)},
		// "reports a malformed VendorID in the issuer of a DAC like one in
		// the subject" (:469-473) and "reports a PAI with a malformed
		// VendorID as a profile violation" (:475-479), their accepting half.
		{"a malformed VendorID of a PAI or an issuer", dnOf(attr(oidCommonName, "Matter Test PAI"), attr(oidMatterVendorID, "fff1")), nil, nil},
		// "does not read the common name as VendorID when the OID value is
		// malformed" (:481-487).
		{"no commonName fallback over a malformed VendorID", dnOf(attr(oidCommonName, "Mvid:FFF2"), attr(oidMatterVendorID, "fff1")), nil, nil},
		// The fallback's own rule (DistinguishedName.ts:345-374).
		{"the first commonName with a value", dnOf(attr(oidCommonName, "Mvid:FFF1"), attr(oidCommonName, "Mvid:FFF2 Mpid:8000")), id(0xfff1), nil},
		{"the first 64 bytes of a commonName", dnOf(attr(oidCommonName, strings.Repeat("x", 60)+"Mvid:FFF1")), nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v, p, err := productIDsOf(tc.dn)
			if err != nil || !sameID(v, tc.wantV) || !sameID(p, tc.wantP) {
				t.Fatalf("productIDsOf = (%s, %s, %v), want (%s, %s)", fmtID(v), fmtID(p), err, fmtID(tc.wantV), fmtID(tc.wantP))
			}
		})
	}
	// A malformed prefix is a CertificateError, which acceptMalformedVidPid
	// does not cover (DistinguishedName.ts:68-70).
	for _, dn := range []pkix.Name{
		dnOf(attr(oidCommonName, "Matter Mpid:"), attr(oidCommonName, "Mvid:FFF1 Mpid:8000")),
		dnOf(attr(oidCommonName, "Mvid:fff1")),
		dnOf(attr(oidCommonName, strings.Repeat("x", 56)+"Mvid:FFF1")),
	} {
		if v, p, err := productIDsOf(dn); err == nil || !strings.Contains(err.Error(), "not followed by 4 uppercase hex digits") {
			t.Errorf("productIDsOf(%q) = (%s, %s, %v), want the malformed-prefix refusal", dn.Names[0].Value, fmtID(v), fmtID(p), err)
		}
	}
}

// TestValidateProductOverAMalformedVendorID: "starts a device whose own
// DAC has a malformed VendorID" (AttestationCertificateProfileTest.ts
// :610-624 at db021da0) — the malformed VendorID is not judged, the
// ProductID still is; a malformed fallback prefix in the DAC's issuer
// refuses the chain, as Dac.fromAsn1 reads the issuer too
// (Certificate.ts:729 at db021da0).
func TestValidateProductOverAMalformedVendorID(t *testing.T) {
	t.Parallel()
	chain, err := BuildTestChain(0xfff1, 0x8000)
	if err != nil {
		t.Fatalf("BuildTestChain: %v", err)
	}
	cn := attr(oidCommonName, "Matter Test DAC")
	malformed := &Chain{DAC: selfSignedWith(t, dnOf(cn, attr(oidMatterVendorID, "fff1"), attr(oidMatterProductID, "8000"))), PAI: chain.PAI}
	if err := ValidateProduct(malformed, 0xfff1, 0x8000); err != nil {
		t.Fatalf("ValidateProduct(DAC with VendorID \"fff1\") = %v", err)
	}
	if err := ValidateProduct(malformed, 0xfff1, 0x8001); !errors.Is(err, ErrProductMismatch) {
		t.Fatalf("ValidateProduct(other ProductID) = %v, want ErrProductMismatch", err)
	}
	badIssuer := &Chain{DAC: selfSignedWith(t, dnOf(attr(oidCommonName, "Mvid:")), attr(oidMatterVendorID, "FFF1")), PAI: chain.PAI}
	if err := ValidateProduct(badIssuer, 0xfff1, 0x8000); err == nil || !strings.Contains(err.Error(), "Mvid: not followed by 4 uppercase hex digits") {
		t.Fatalf("ValidateProduct(issuer commonName \"Mvid:\") = %v, want the malformed-prefix refusal", err)
	}
}

// selfSignedWith is a self-signed certificate whose issuer and subject
// are dn, with extra appended to the subject only when given.
func selfSignedWith(t *testing.T, dn pkix.Name, extra ...pkix.AttributeTypeAndValue) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	subject := pkix.Name{ExtraNames: append(slices.Clone(dn.Names), extra...)}
	issuer := pkix.Name{ExtraNames: dn.Names}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: subject, NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1<<31, 0)}
	parent := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: issuer, NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1<<31, 0)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func attr(oid asn1.ObjectIdentifier, v any) pkix.AttributeTypeAndValue {
	return pkix.AttributeTypeAndValue{Type: oid, Value: v}
}

// dnOf is the DN crypto/x509 parses from these attributes, one RDN each,
// in order.
func dnOf(attrs ...pkix.AttributeTypeAndValue) pkix.Name {
	return pkix.Name{Names: attrs}
}

func id(v uint16) *uint16 { return &v }

func sameID(a, b *uint16) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func fmtID(v *uint16) string {
	if v == nil {
		return "nil"
	}
	return strings.ToUpper(strings.TrimPrefix(asHex(*v), "0x"))
}

func asHex(v uint16) string {
	const digits = "0123456789abcdef"
	return "0x" + string([]byte{digits[v>>12&0xf], digits[v>>8&0xf], digits[v>>4&0xf], digits[v&0xf]})
}

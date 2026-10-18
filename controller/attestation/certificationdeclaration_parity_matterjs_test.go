// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// A port of matter.js packages/protocol/test/certificate/CertificationDeclarationTest.ts at
// 862ad75f: CertificationDeclaration.generate (secure/attestation's
// BuildTestCertificationDeclaration) parsed back by CertificationDeclaration.parse
// (parseDeclaration here).

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"

	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// The Appendix F test values of CertificationDeclarationTest.ts:16-17.
const (
	certificationDeclarationSignerPrivateKey = "AEF3484116E9481EC57BE0472DF41BF499064E5024AD869ECA5E889802D48075"
	certificationDeclarationSignerSKID       = "62FA823359ACFAA9963E1CFA140ADDF504F37160"
)

func certificationDeclarationParsed(t *testing.T, vid, pid uint16) *declaration {
	t.Helper()
	cd, err := secattestation.BuildTestCertificationDeclaration(vid, pid)
	if err != nil {
		t.Fatalf("BuildTestCertificationDeclaration: %v", err)
	}
	d, err := parseDeclaration(cd)
	if err != nil {
		t.Fatalf("parseDeclaration: %v", err)
	}
	return d
}

// TestCertificationDeclarationParse is the describe "parse"
// (CertificationDeclarationTest.ts:19).
func TestCertificationDeclarationParse(t *testing.T) {
	t.Run("round-trips: generate then parse recovers all fields", func(t *testing.T) {
		c := certificationDeclarationParsed(t, 0xfff1, 0x8000).content
		switch {
		case c.formatVersion != 1:
			t.Errorf("formatVersion = %d", c.formatVersion)
		case c.vendorID != 0xfff1:
			t.Errorf("vendorId = %#04x", c.vendorID)
		case !slices.Equal(c.productIDs, []uint16{0x8000}):
			t.Errorf("produceIdArray = %v", c.productIDs)
		case c.deviceTypeID != 22:
			t.Errorf("deviceTypeId = %d", c.deviceTypeID)
		case c.certificateID != "CSA00000SWC00000-00":
			t.Errorf("certificateId = %q", c.certificateID)
		case c.securityLevel != 0 || c.securityInformation != 0:
			t.Errorf("security = %d/%d", c.securityLevel, c.securityInformation)
		case c.versionNumber != 1:
			t.Errorf("versionNumber = %d", c.versionNumber)
		case c.certificationType != CertificationTest:
			t.Errorf("certificationType = %d, want 0 (test)", c.certificationType)
		}
	})
	t.Run("extracts correct signer subject key identifier", func(t *testing.T) {
		d := certificationDeclarationParsed(t, 0xfff1, 0x8000)
		if got := hex.EncodeToString(d.signerSubjectKeyID); !bytes.EqualFold([]byte(got), []byte(certificationDeclarationSignerSKID)) {
			t.Fatalf("signerSubjectKeyId = %s, want %s", got, certificationDeclarationSignerSKID)
		}
	})
	t.Run("extracts a valid ECDSA signature that verifies against the test signer public key", func(t *testing.T) {
		// matter.js converts the SignerInfo signature to 64-byte IEEE P1363;
		// parseDeclaration keeps the DER ECDSA-Sig-Value, so the check here
		// is the verification itself.
		d := certificationDeclarationParsed(t, 0xfff1, 0x8000)
		scalar, err := hex.DecodeString(certificationDeclarationSignerPrivateKey)
		if err != nil {
			t.Fatalf("hex: %v", err)
		}
		key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar)
		if err != nil {
			t.Fatalf("private key: %v", err)
		}
		digest := sha256.Sum256(d.signedData)
		if !ecdsa.VerifyASN1(&key.PublicKey, digest[:], d.signature) {
			t.Fatal("the CD signature does not verify over signedData")
		}
	})
	t.Run("extracts signedData that can be decoded by TlvDc", func(t *testing.T) {
		d := certificationDeclarationParsed(t, 0xfff2, 0x1234)
		c, err := decodeCDContent(d.signedData)
		if err != nil {
			t.Fatalf("decodeCDContent: %v", err)
		}
		if c.vendorID != 0xfff2 || !slices.Equal(c.productIDs, []uint16{0x1234}) {
			t.Fatalf("vendorId/produceIdArray = %#04x/%v", c.vendorID, c.productIDs)
		}
	})
	t.Run("rejects invalid input gracefully", func(t *testing.T) {
		for _, in := range [][]byte{{}, {0xde, 0xad, 0xbe, 0xef}} {
			if _, err := parseDeclaration(in); err == nil {
				t.Errorf("parseDeclaration(%x) succeeded", in)
			}
		}
	})
}

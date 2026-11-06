// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// A port of matter.js packages/protocol/test/certificate/CdSignersTest.ts
// at db021da0 on the CSA CD certificates connectedhomeip ships in
// credentials/development/cd-certs at 6170af84 (matter.js copies them into
// CsaCdCertificates.ts).

import (
	"encoding/hex"
	"regexp"
	"strings"
	"testing"

	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// csaSigningKeyCerts are the chip cd-certs of the CSA signing keys 001 to
// 005.
var csaSigningKeyCerts = []string{
	"CSA_Matter_CD_Signing_Key_001", "CSA_Matter_CD_Signing_Key_002", "CSA_Matter_CD_Signing_Key_003",
	"CSA_Matter_CD_Signing_Key_004", "CSA_Matter_CD_Signing_Key_005",
}

// TestCdSignersCSAKeys: describe "CSA signing keys 001 to 005" (:16-28 at
// db021da0).
func TestCdSignersCSAKeys(t *testing.T) {
	t.Parallel()
	for _, name := range csaSigningKeyCerts {
		t.Run(name+" is built in and verifies against the CSA CD root", func(t *testing.T) {
			t.Parallel()
			der := chipCDCert(t, name)
			c, err := parseAttCert(der, kindCDSigner, false)
			if err != nil {
				t.Fatal(err)
			}
			skid := c.x509.SubjectKeyId
			known, ok := KnownCDSigner(skid)
			if !ok || known.IsTestSigner {
				t.Fatalf("known = %+v %v", known, ok)
			}
			verified, err := VerifyCDSignerCertificate(der, skid)
			if err != nil {
				t.Fatal(err)
			}
			if !verified.Equal(known.PublicKey) {
				t.Fatal("the built-in key is not the certificate's")
			}
		})
	}
}

// TestCdSignersVerifyCertificate: describe "verifyCertificate" (:30-90 at
// db021da0).
func TestCdSignersVerifyCertificate(t *testing.T) {
	t.Parallel()
	key001 := chipCDCert(t, "CSA_Matter_CD_Signing_Key_001")
	skid001 := unhex(t, "FE343F959947763B61EE4539131338494FE67D8E")
	refused := func(t *testing.T, der, skid []byte, pattern string) {
		t.Helper()
		_, err := VerifyCDSignerCertificate(der, skid)
		if err == nil || !regexp.MustCompile(pattern).MatchString(err.Error()) {
			t.Fatalf("VerifyCDSignerCertificate = %v, want /%s/", err, pattern)
		}
	}
	t.Run("refuses a certificate whose subject key identifier is not the requested one", func(t *testing.T) {
		t.Parallel()
		refused(t, key001, unhex(t, "DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF"), `has subject key identifier fe343f`)
	})
	t.Run("refuses the CSA CD root itself", func(t *testing.T) {
		t.Parallel()
		refused(t, chipCDCert(t, "CSA_Matter_Certificate_and_Testing_Root"), unhex(t, "97E469D0C50414C26FC701F77E947739098DF6A5"),
			`root certificate is not itself a CD signer`)
	})
	t.Run("refuses a certificate another authority issued", func(t *testing.T) {
		t.Parallel()
		refused(t, chipCDCert(t, "Chip-Test-CD-Cert"), secattestation.TestCMSSignerSKID, `not the CSA CD root`)
		refused(t, secattestation.TestPAAFFF1Cert, secattestation.TestPAAFFF1SKID, `not the CSA CD root`)
	})
	t.Run("refuses a certificate that became valid before the CSA CD root did", func(t *testing.T) {
		t.Parallel()
		// notBefore 221003192855Z moved to 220103192855Z; the root is valid
		// from 2022-07-07.
		h := hex.EncodeToString(key001)
		earlier := strings.Replace(h, "170d323231303033", "170d323230313033", 1)
		if earlier == h {
			t.Fatal("notBefore not found")
		}
		refused(t, unhex(t, earlier), skid001, `outside the validity period of the CSA CD root`)
	})
	t.Run("refuses a certificate that became valid after the CSA CD root expired", func(t *testing.T) {
		t.Parallel()
		// notBefore UTCTime 221003192855Z replaced by GeneralizedTime
		// 21231003192855Z, two bytes longer.
		h := hex.EncodeToString(key001)
		later := strings.Replace(h, "30820207308201ad", "30820209308201af", 1)
		later = strings.Replace(later, "3020170d3232313030333139323835355a", "3022180f32313233313030333139323835355a", 1)
		if later == h {
			t.Fatal("validity not found")
		}
		refused(t, unhex(t, later), skid001, `outside the validity period of the CSA CD root`)
	})
	t.Run("refuses a certificate whose signature does not verify against the CSA CD root", func(t *testing.T) {
		t.Parallel()
		tampered := append([]byte(nil), key001...)
		tampered[len(tampered)-10] ^= 0x01
		refused(t, tampered, skid001, `does not verify`)
	})
}

// TestCdSignersKnown: "hands out a copy of a built-in key" and "recognizes
// the Matter test CD signer" (:92-108 at db021da0).
func TestCdSignersKnown(t *testing.T) {
	t.Parallel()
	skid001 := unhex(t, "FE343F959947763B61EE4539131338494FE67D8E")
	for _, id := range [][]byte{skid001, secattestation.TestCMSSignerSKID} {
		first, _ := KnownCDSigner(id)
		again, _ := KnownCDSigner(id)
		if first.PublicKey == again.PublicKey || !first.PublicKey.Equal(again.PublicKey) {
			t.Fatal("the built-in key is shared, not copied")
		}
	}

	if !IsTestCDSigner(secattestation.TestCMSSignerSKID) || IsTestCDSigner(skid001) {
		t.Fatal("isTestSigner")
	}
	if s, ok := KnownCDSigner(secattestation.TestCMSSignerSKID); !ok || !s.IsTestSigner {
		t.Fatal("the test CD signer is not known as such")
	}
	if _, ok := KnownCDSigner(unhex(t, "DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF")); ok {
		t.Fatal("an unknown key identifier is known")
	}
}

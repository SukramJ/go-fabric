// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// A port of matter.js packages/protocol/test/certificate/
// DeviceAttestationValidatorChipCdTest.ts at db021da0: the validator
// driven with connectedhomeip's commissioner DUT vectors, each an
// attestation whose CD breaks or keeps one rule. matter.js's offline
// DclCertificateService seeded with the CHIP test PAAs is TestStore (the
// two CHIP test PAAs as test roots, test certificates accepted, no CD
// signer beyond the built-in ones).

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"regexp"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// chipCDValidate is validate (:81-103 at db021da0): the vector's CD in
// attestation elements with a fresh nonce, signed with the vector's DAC
// key over elements ‖ challenge.
func chipCDValidate(t *testing.T, v chipVector, store PAAStore) (Result, error) {
	t.Helper()
	challenge, nonce := make([]byte, 16), make([]byte, 32)
	_, _ = rand.Read(challenge)
	_, _ = rand.Read(nonce)
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutOctets(tlv.ContextTag(1), unhex(t, v.CertificationDeclaration))
	e.PutOctets(tlv.ContextTag(2), nonce)
	e.PutUint32(tlv.ContextTag(3), 0)
	_ = e.EndContainer()
	elements, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), unhex(t, v.DACPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(append(append([]byte(nil), elements...), challenge...))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return Validate(Context{Challenge: challenge, Store: store}, Data{
		DAC: unhex(t, v.DACCert), PAI: unhex(t, v.PAICert), Elements: elements, Signature: sig, Nonce: nonce,
		VendorID: 0xFFF1, ProductID: v.BasicInfoPID,
	})
}

// chipCDRefusal is expectRefusal (:105-116 at db021da0).
func chipCDRefusal(t *testing.T, name string, check Check, pattern string) {
	t.Helper()
	_, err := chipCDValidate(t, chipVectorNamed(t, name), TestStore())
	var ae *Error
	if !errors.As(err, &ae) {
		t.Fatalf("Validate = %v, want a DeviceAttestationError %s", err, check)
	}
	if ae.Check != check || !regexp.MustCompile(pattern).MatchString(ae.Message) {
		t.Fatalf("Validate = %s %q, want %s /%s/", ae.Check, ae.Message, check, pattern)
	}
}

// noFindingsAt fails on a finding of one of the levels.
func noFindingsAt(t *testing.T, res Result, levels ...Level) {
	t.Helper()
	for _, f := range res.Findings {
		for _, l := range levels {
			if f.Level == l {
				t.Fatalf("finding %+v", f)
			}
		}
	}
}

// TestChipCDAcceptsValidCD: describe "accepts every valid CD" (:118-126 at
// db021da0).
func TestChipCDAcceptsValidCD(t *testing.T) {
	t.Parallel()
	for _, name := range chipVectors(t).Groups["acceptedCD"] {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res, err := chipCDValidate(t, chipVectorNamed(t, name), TestStore())
			if err != nil {
				t.Fatal(err)
			}
			noFindingsAt(t, res, LevelError)
		})
	}
}

// TestChipCDEnvelopeRules: describe "refuses a CD that breaks a CMS
// envelope rule" (:128-146) and "refuses a CD whose content breaks a
// format rule" (:148-164 at db021da0).
func TestChipCDEnvelopeRules(t *testing.T) {
	t.Parallel()
	for name, pattern := range map[string]string{
		"struct_cd_cms_v2":                       `CMS version must be 3, found 0x02`,
		"struct_cd_cms_digest_algo_sha1":         `CMS digestAlgorithm must be sha256`,
		"struct_cd_cms_econtent_type_msac":       `CMS eContentType must be pkcs7-data`,
		"struct_cd_signer_info_v2":               `CMS SignerInfo version must be 3, found 0x02`,
		"struct_cd_signer_info_digest_algo_sha1": `CMS SignerInfo digestAlgorithm must be sha256`,
		"struct_cd_cms_sig_algo_ecdsa_with_sha1": `CMS SignerInfo signatureAlgorithm must be ecdsa-with-SHA256`,
		"struct_cd_format_version_2":             `format_version 2 is not 1`,
		"struct_cd_cert_type_wrong":              `certification_type 255 is a reserved value`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			chipCDRefusal(t, name, CertificationDeclarationFormatInvalid, pattern)
		})
	}
}

// TestChipCDSigner: describe "CD signer" (:166-192 at db021da0).
func TestChipCDSigner(t *testing.T) {
	t.Parallel()
	t.Run("refuses struct_cd_signer_info_skid_invalid", func(t *testing.T) {
		t.Parallel()
		chipCDRefusal(t, "struct_cd_signer_info_skid_invalid", CdSignerUnknown,
			`CD signer SKID 62fa823359acfa56963e1cfa140addf504f37160 is not known to be a CD signer`)
	})
	t.Run("refuses struct_cd_cms_signature once the signer is known", func(t *testing.T) {
		t.Parallel()
		chipCDRefusal(t, "struct_cd_cms_signature", CertificationDeclarationSignatureInvalid, `signature verification failed`)
	})
	t.Run("refuses struct_cd_signer_info_skid_invalid also without any DCL service", func(t *testing.T) {
		t.Parallel()
		_, err := chipCDValidate(t, chipVectorNamed(t, "struct_cd_signer_info_skid_invalid"), nil)
		var ae *Error
		if !errors.As(err, &ae) || ae.Check != CdSignerUnknown {
			t.Fatalf("Validate = %v", err)
		}
	})
}

// TestChipCDSignerTrust: describe "CD signer trust" (:194-233 at
// db021da0).
func TestChipCDSignerTrust(t *testing.T) {
	t.Parallel()
	csaSigned := chipVectorNamed(t, "TestCredentials-FFF1-8001")
	t.Run("refuses struct_cd_official_cd, an official CD signed by the test signer", func(t *testing.T) {
		t.Parallel()
		chipCDRefusal(t, "struct_cd_official_cd", OfficialCdSignedByTestSigner,
			`certification_type=2 \(official\) is signed by the Matter test CD signer`)
	})
	t.Run("verifies CDs with the built-in keys without a DCL service", func(t *testing.T) {
		t.Parallel()
		for _, v := range []chipVector{csaSigned, chipVectorNamed(t, "struct_cd_cms_v3")} {
			res, err := chipCDValidate(t, v, nil)
			if err != nil {
				t.Fatal(err)
			}
			noFindingsAt(t, res, LevelError)
		}
		_, err := chipCDValidate(t, chipVectorNamed(t, "struct_cd_cms_signature"), nil)
		var ae *Error
		if !errors.As(err, &ae) || ae.Check != CertificationDeclarationSignatureInvalid {
			t.Fatalf("forged CD: %v", err)
		}
	})
	t.Run("verifies a CD signed by CSA signing key 001 without any CD signer source", func(t *testing.T) {
		t.Parallel()
		res, err := chipCDValidate(t, csaSigned, TestStore())
		if err != nil {
			t.Fatal(err)
		}
		noFindingsAt(t, res, LevelError, LevelWarning)
	})
	t.Run("verifies a CSA-signed CD and still reports a test PAA under a production-only policy", func(t *testing.T) {
		t.Parallel()
		st := TestStore()
		st.AcceptTest = false
		res, err := chipCDValidate(t, csaSigned, st)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, f := range res.Findings {
			found = found || (f.Level == LevelError && f.Check == TrustedAsTestCertificate)
		}
		if !found {
			t.Fatalf("findings %+v", res.Findings)
		}
	})
}

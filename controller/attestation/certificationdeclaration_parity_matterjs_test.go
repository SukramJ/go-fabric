// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// A port of matter.js packages/protocol/test/certificate/CertificationDeclarationTest.ts at
// db021da0: CertificationDeclaration.generate (secure/attestation's
// BuildTestCertificationDeclaration) parsed back by CertificationDeclaration.parse
// (parseDeclaration here).

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"strings"
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

// cdEnvelope is signedData (CertificationDeclarationTest.ts:158-196 at
// db021da0): a CMS SignedData for a valid CD content with the stated parts
// replaced; nil keeps the valid part.
type cdEnvelope struct {
	digestAlgorithms      [][]byte
	signerDigestAlgorithm []byte
	eContent              []byte // the [0] wrapper's content
	signature             []byte
	skid                  []byte
}

func (e cdEnvelope) encode(t *testing.T) []byte {
	t.Helper()
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	sha := derTLV(derSequenceTag, derTLV(derOIDTag, oidSHA256...)...)
	content := certificationDeclarationParsed(t, 0xFFF1, 0x8000).signedData
	if e.digestAlgorithms == nil {
		e.digestAlgorithms = [][]byte{sha}
	}
	if e.signerDigestAlgorithm == nil {
		e.signerDigestAlgorithm = sha
	}
	if e.eContent == nil {
		e.eContent = derTLV(derOctetStringTag, content...)
	}
	if e.signature == nil {
		e.signature = derTLV(derOctetStringTag, unhex(t, "3006020101020101")...)
	}
	if e.skid == nil {
		e.skid = unhex(t, certificationDeclarationSignerSKID)
	}
	signerInfo := derTLV(derSequenceTag, cat(
		derTLV(derIntegerTag, 3), derTLV(0x80, e.skid...), e.signerDigestAlgorithm,
		derTLV(derSequenceTag, derTLV(derOIDTag, oidECDSAWithSHA256...)...), e.signature)...)
	signedData := derTLV(derSequenceTag, cat(
		derTLV(derIntegerTag, 3), derTLV(derSetTag, cat(e.digestAlgorithms...)...),
		derTLV(derSequenceTag, cat(derTLV(derOIDTag, oidPKCS7Data...), derTLV(0xa0, e.eContent...))...),
		derTLV(derSetTag, signerInfo...))...)
	return derTLV(derSequenceTag, cat(derTLV(derOIDTag, oidPKCS7SignedData...), derTLV(0xa0, signedData...))...)
}

// cdFormatRefused requires a cdFormatError with the message.
func cdFormatRefused(t *testing.T, cd []byte, want string) {
	t.Helper()
	_, err := parseDeclaration(cd)
	var fe *cdFormatError
	if !errors.As(err, &fe) || fe.msg != want {
		t.Fatalf("parseDeclaration = %v, want the format error %q", err, want)
	}
}

// TestCertificationDeclarationEnvelope: the CMS envelope rules of
// CertificationDeclarationTest.ts:116-231 at db021da0.
func TestCertificationDeclarationEnvelope(t *testing.T) {
	t.Parallel()
	generated, err := secattestation.BuildTestCertificationDeclaration(0xFFF1, 0x8000)
	if err != nil {
		t.Fatal(err)
	}
	cdHex := hex.EncodeToString(generated)
	t.Run("rejects a ContentInfo whose contentType is not pkcs7-signedData", func(t *testing.T) {
		t.Parallel()
		const signedDataOID, dataOID = "06092a864886f70d010702", "06092a864886f70d010701"
		if !strings.Contains(cdHex, signedDataOID) {
			t.Fatal("no signedData OID")
		}
		cdFormatRefused(t, unhex(t, strings.Replace(cdHex, signedDataOID, dataOID, 1)),
			"CMS contentType must be pkcs7-signedData, found OID 2a864886f70d010701")
	})
	t.Run("rejects a CMS version that is not an INTEGER", func(t *testing.T) {
		t.Parallel()
		// The version INTEGER 3, then the digestAlgorithms SET.
		re := regexp.MustCompile(`020103(31)`)
		if !re.MatchString(cdHex) {
			t.Fatal("no CMS version")
		}
		cdFormatRefused(t, unhex(t, re.ReplaceAllString(cdHex, "040103$1")), "CMS version must be 3, found none")
	})
	t.Run("rejects a signer subject key identifier that is not 20 bytes", func(t *testing.T) {
		t.Parallel()
		cdFormatRefused(t, cdEnvelope{skid: unhex(t, certificationDeclarationSignerSKID)[:19]}.encode(t),
			"CMS SignerInfo subjectKeyIdentifier must be 20 bytes, found 19")
	})
	t.Run("rejects a CMS SignedData with an empty digestAlgorithms SET", func(t *testing.T) {
		t.Parallel()
		cdFormatRefused(t, cdEnvelope{digestAlgorithms: [][]byte{}}.encode(t), "CMS digestAlgorithms must hold sha256, found none")
	})
	t.Run("rejects a SignerInfo digestAlgorithm that is a bare OID instead of an AlgorithmIdentifier", func(t *testing.T) {
		t.Parallel()
		cdFormatRefused(t, cdEnvelope{signerDigestAlgorithm: derTLV(derOIDTag, oidSHA256...)}.encode(t),
			"CMS SignerInfo digestAlgorithm must be sha256, found OID none")
	})
	t.Run("parses the CMS SignedData these tests vary", func(t *testing.T) {
		t.Parallel()
		d, err := parseDeclaration(cdEnvelope{}.encode(t))
		if err != nil || d.content.vendorID != 0xFFF1 {
			t.Fatalf("parseDeclaration = %+v, %v", d, err)
		}
	})
	t.Run("rejects an eContent that is not an OCTET STRING", func(t *testing.T) {
		t.Parallel()
		content := certificationDeclarationParsed(t, 0xFFF1, 0x8000).signedData
		_, err := parseDeclaration(cdEnvelope{eContent: derTLV(0x80, content...)}.encode(t))
		var fe *cdFormatError
		if err == nil || errors.As(err, &fe) || !strings.Contains(err.Error(), "expected OCTET STRING eContent in encapContentInfo") {
			t.Fatalf("parseDeclaration = %v", err)
		}
	})
	t.Run("rejects a SignerInfo signature that is not an OCTET STRING", func(t *testing.T) {
		t.Parallel()
		_, err := parseDeclaration(cdEnvelope{signature: derTLV(0x84, unhex(t, "3006020101020101")...)}.encode(t))
		var fe *cdFormatError
		if err == nil || errors.As(err, &fe) || !strings.Contains(err.Error(), "expected OCTET STRING signature in SignerInfo") {
			t.Fatalf("parseDeclaration = %v", err)
		}
	})
}

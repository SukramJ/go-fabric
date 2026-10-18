// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation_test

// A port of matter.js packages/protocol/test/certificate/DeviceAttestationValidatorTest.ts at
// 862ad75f: DeviceAttestationValidator.validate, check for check, on a PAI/DAC/CD fixture.
//
// The Go surface differs in three places, the behaviour does not: the
// chain is minted by this package's buildChain under the CSA test PAA
// FFF1 (matter.js: AttestationCertificateManager under TestCert_PAA_NoVID);
// matter.js's DclCertificateService is an attestation.StaticStore (its
// isProduction flag is PAA.Production, acceptsTestCertificates is
// AcceptTest, fetchedAt is FetchedAt); and the signed DCL CRLs of the
// revocation cases are the Revocations interface the validator queries,
// answered by davRevocations with the (issuer SKID, serial, issuer DN)
// triples the matter.js test puts in its CRLs.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/attestation"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// davFixture is the before() fixture: a valid chain, CD, nonce and signature.
func davFixture(t *testing.T) fixture {
	t.Helper()
	return newFixture(t, defaultSpec(), defaultCD())
}

// davReject validates and requires an *attestation.Error of check whose
// message matches pattern (the matter.js rejectedWith regex).
func davReject(t *testing.T, f fixture, check attestation.Check, pattern string) {
	t.Helper()
	_, err := attestation.Validate(f.ctx, f.data)
	var ae *attestation.Error
	if !errors.As(err, &ae) {
		t.Fatalf("Validate = %v, want a DeviceAttestationError %s", err, check)
	}
	if ae.Check != check || !regexp.MustCompile(pattern).MatchString(ae.Message) {
		t.Fatalf("Validate = %s %q, want %s /%s/", ae.Check, ae.Message, check, pattern)
	}
}

func davPass(t *testing.T, f fixture) attestation.Result {
	t.Helper()
	res, err := attestation.Validate(f.ctx, f.data)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return res
}

func davFinding(res attestation.Result, check attestation.Check) *attestation.Finding {
	for i := range res.Findings {
		if res.Findings[i].Check == check {
			return &res.Findings[i]
		}
	}
	return nil
}

// davWithCD re-signs the attestation elements over cd (buildAttestationWithCd, :159-167).
func davWithCD(t *testing.T, f fixture, dacKey *ecdsa.PrivateKey, cd []byte) fixture {
	t.Helper()
	f.data.Elements = elements(t, cd, f.data.Nonce)
	f.data.Signature = sign(t, dacKey, f.data.Elements, f.ctx.Challenge)
	return f
}

// davFixtureWithKey is davFixture that also returns the DAC key.
func davFixtureWithKey(t *testing.T) (fixture, *ecdsa.PrivateKey) {
	t.Helper()
	dac, pai, key := buildChain(t, defaultSpec())
	challenge, nonce := make([]byte, 16), make([]byte, 32)
	_, _ = rand.Read(challenge)
	_, _ = rand.Read(nonce)
	el := elements(t, buildCD(t, defaultCD()), nonce)
	return fixture{
		ctx: attestation.Context{Challenge: challenge, Store: attestation.TestStore()},
		data: attestation.Data{
			DAC: dac, PAI: pai, Elements: el, Signature: sign(t, key, el, challenge), Nonce: nonce,
			VendorID: 0xFFF1, ProductID: 0x8000,
		},
	}, key
}

func davParse(t *testing.T, der []byte) *x509.Certificate {
	t.Helper()
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// davSerial is the DER INTEGER content of the certificate's serial, the
// bytes matter.js reads as cert.serialNumber.
func davSerial(t *testing.T, c *x509.Certificate) []byte {
	t.Helper()
	b, err := asn1.Marshal(c.SerialNumber)
	if err != nil {
		t.Fatal(err)
	}
	return b[2:]
}

// davRevocations revokes (issuer SKID, serial, issuer DN) triples — the
// entries of the CRLs the matter.js test signs (buildSignedTestCrl).
type davRevocations struct {
	*attestation.StaticStore
	revoked map[string]bool
}

func davKey(akid, serial []byte, issuerDN string) string {
	return strings.ToUpper(hex.EncodeToString(akid)) + "|" + strings.ToUpper(hex.EncodeToString(serial)) + "|" + issuerDN
}

func (d davRevocations) IsRevoked(akid, serial []byte, issuerDN string) (bool, error) {
	return d.revoked[davKey(akid, serial, issuerDN)], nil
}

func davIssuerDN(c *x509.Certificate) string { return strings.ToUpper(hex.EncodeToString(c.RawIssuer)) }

// davResign replaces the 65-byte subject key of der by its copy with the
// point marker 0x05 and re-signs the TBS with key (the brokenPai, :240-259).
func davResign(t *testing.T, der []byte, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	var cert struct {
		TBS asn1.RawValue
		Alg asn1.RawValue
		Sig asn1.BitString
	}
	if _, err := asn1.Unmarshal(der, &cert); err != nil {
		t.Fatal(err)
	}
	tbs := bytes.Clone(cert.TBS.FullBytes)
	i := bytes.Index(tbs, []byte{0x03, 0x42, 0x00, 0x04})
	if i < 0 {
		t.Fatal("no subject public key")
	}
	tbs[i+3] = 0x05
	digest := sha256.Sum256(tbs)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	cert.TBS = asn1.RawValue{FullBytes: tbs}
	cert.Sig = asn1.BitString{Bytes: sig, BitLength: 8 * len(sig)}
	out, err := asn1.Marshal(cert)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestDeviceAttestationValidatorMalformedPeerCertificates: describe
// "malformed peer certificates" (:207).
func TestDeviceAttestationValidatorMalformedPeerCertificates(t *testing.T) {
	t.Parallel()
	t.Run("throws CertificateUnparseable when the device returns an empty PAI", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.data.PAI = []byte{}
		davReject(t, f, attestation.CertificateUnparseable, `Device returned an empty PAI certificate`)
	})
	t.Run("throws CertificateUnparseable when the device returns an empty DAC", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.data.DAC = []byte{}
		davReject(t, f, attestation.CertificateUnparseable, `Device returned an empty DAC certificate`)
	})
	t.Run("throws CertificateUnparseable when the DAC's public key is malformed", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		// Flip the uncompressed-point marker, every DER length intact (:226-230).
		broken := bytes.Clone(f.data.DAC)
		broken[bytes.Index(broken, []byte{0x03, 0x42, 0x00, 0x04})+3] = 0x05
		f.data.DAC = broken
		davReject(t, f, attestation.CertificateUnparseable, `DAC certificate that cannot be parsed`)
	})
	t.Run("throws CertificateUnparseable when the PAI is signed but its public key is unreadable", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.data.PAI = davResign(t, f.data.PAI, secattestation.TestPAAFFF1PrivateKey)
		// matter.js reads the PAI key in the chain step and expects "PAI
		// whose public key cannot be read" (:272-273); crypto/x509 reads the
		// key while parsing, so the Go validator reports the same check one
		// step earlier with the step-1 message. The check, which the test
		// says must survive (:271), is the same.
		davReject(t, f, attestation.CertificateUnparseable, `PAI certificate that cannot be parsed`)
	})
	t.Run("throws CertificateUnparseable when the device returns a truncated PAI", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.data.PAI = f.data.PAI[:20]
		davReject(t, f, attestation.CertificateUnparseable, `PAI certificate that cannot be parsed`)
	})
}

// TestDeviceAttestationValidatorMalformedElements: describe "malformed
// attestation elements" (:294).
func TestDeviceAttestationValidatorMalformedElements(t *testing.T) {
	t.Parallel()
	t.Run("throws AttestationElementsUnparseable when the elements are not valid TLV", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.data.Elements, _ = hex.DecodeString("15300102") // :301
		davReject(t, f, attestation.AttestationElementsUnparseable, `attestation elements that cannot be decoded`)
	})
	t.Run("throws CertificationDeclarationUnparseable when the CD field holds something else", func(t *testing.T) {
		t.Parallel()
		f, key := davFixtureWithKey(t)
		leaked, _ := hex.DecodeString("6c6f63616c686f73743a3131333131") // :310
		f = davWithCD(t, f, key, leaked)
		davReject(t, f, attestation.CertificationDeclarationUnparseable, `Certification Declaration that cannot be parsed`)
	})
}

// TestDeviceAttestationValidatorPAATrustStore: describe "PAA trust store" (:318).
func TestDeviceAttestationValidatorPAATrustStore(t *testing.T) {
	t.Parallel()
	t.Run("throws PaaNotTrusted when PAA is not in trust store", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.ctx.Store = &attestation.StaticStore{}
		davReject(t, f, attestation.PaaNotTrusted, `PAA not found in trust store`)
	})
}

// TestDeviceAttestationValidatorChain: describe "certificate chain verification" (:336).
func TestDeviceAttestationValidatorChain(t *testing.T) {
	t.Parallel()
	t.Run("throws CertificateChainInvalid for tampered PAI", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.data.PAI = bytes.Clone(f.data.PAI)
		f.data.PAI[len(f.data.PAI)-5] ^= 0xff // :342
		davReject(t, f, attestation.CertificateChainInvalid, `PAI signature verification failed`)
	})
	t.Run("throws CertificateChainInvalid for tampered DAC", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.data.DAC = bytes.Clone(f.data.DAC)
		f.data.DAC[len(f.data.DAC)-5] ^= 0xff // :354
		davReject(t, f, attestation.CertificateChainInvalid, `DAC signature verification failed`)
	})
	t.Run("throws CertificateChainInvalid when DAC is signed by wrong PAI", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		_, otherPAI, _ := buildChain(t, defaultSpec()) // same PAA, other PAI key
		f.data.PAI = otherPAI
		davReject(t, f, attestation.CertificateChainInvalid, `DAC signature verification failed`)
	})
}

// TestDeviceAttestationValidatorValidityPeriod: describe "certificate
// validity period (Step 3b)" (:376).
func TestDeviceAttestationValidatorValidityPeriod(t *testing.T) {
	t.Parallel()
	t.Run("passes when DAC notBefore is within PAI and PAA validity window", func(t *testing.T) {
		t.Parallel()
		davPass(t, davFixture(t))
	})
	t.Run("validates that test chain has expected overlapping dates", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		dac, pai, paa := davParse(t, f.data.DAC), davParse(t, f.data.PAI), davParse(t, secattestation.TestPAAFFF1Cert)
		switch {
		case dac.NotBefore.Before(pai.NotBefore):
			t.Fatal("DAC notBefore before PAI notBefore")
		case dac.NotBefore.Before(paa.NotBefore):
			t.Fatal("DAC notBefore before PAA notBefore")
		case !pai.NotAfter.After(dac.NotBefore):
			t.Fatal("PAI notAfter not after DAC notBefore")
		case !paa.NotAfter.After(dac.NotBefore):
			t.Fatal("PAA notAfter not after DAC notBefore")
		}
	})
}

// TestDeviceAttestationValidatorVendorID: describe "VendorID matching" (:407).
func TestDeviceAttestationValidatorVendorID(t *testing.T) {
	t.Parallel()
	t.Run("throws VendorIdMismatch when DAC vendorId does not match PAI vendorId", func(t *testing.T) {
		t.Parallel()
		s := defaultSpec()
		s.dacVID = 0xFFF2 // :74
		f := newFixture(t, s, defaultCD())
		davReject(t, f, attestation.VendorIDMismatch, `DAC vendorId 0x[0-9A-F]{4} does not match PAI vendorId 0x[0-9A-F]{4}`)
	})
}

// TestDeviceAttestationValidatorNonce: describe "attestation nonce verification" (:422).
func TestDeviceAttestationValidatorNonce(t *testing.T) {
	t.Parallel()
	t.Run("passes when attestation nonce matches", func(t *testing.T) {
		t.Parallel()
		davPass(t, davFixture(t))
	})
	t.Run("throws AttestationNonceMismatch when nonce does not match", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.data.Nonce = make([]byte, 32)
		_, _ = rand.Read(f.data.Nonce)
		davReject(t, f, attestation.AttestationNonceMismatch, `AttestationNonce in response does not match`)
	})
}

// TestDeviceAttestationValidatorSignature: describe "attestation signature verification" (:445).
func TestDeviceAttestationValidatorSignature(t *testing.T) {
	t.Parallel()
	t.Run("passes with valid signature", func(t *testing.T) {
		t.Parallel()
		davPass(t, davFixture(t))
	})
	t.Run("throws AttestationSignatureInvalid for tampered signature", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.data.Signature[10] ^= 0xff // :458
		davReject(t, f, attestation.AttestationSignatureInvalid, `Attestation signature verification failed`)
	})
	t.Run("throws AttestationSignatureInvalid when signed with wrong attestation challenge", func(t *testing.T) {
		t.Parallel()
		f, key := davFixtureWithKey(t)
		wrong := make([]byte, 16)
		_, _ = rand.Read(wrong)
		f.data.Signature = sign(t, key, f.data.Elements, wrong)
		davReject(t, f, attestation.AttestationSignatureInvalid, `Attestation signature verification failed`)
	})
	t.Run("throws AttestationSignatureInvalid when signed with wrong key", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		f.data.Signature = sign(t, other, f.data.Elements, f.ctx.Challenge)
		davReject(t, f, attestation.AttestationSignatureInvalid, `Attestation signature verification failed`)
	})
}

// TestDeviceAttestationValidatorCDSignature: describe "Certification
// Declaration signature verification" (:500).
func TestDeviceAttestationValidatorCDSignature(t *testing.T) {
	t.Parallel()
	t.Run("passes with valid CD signature when signer key is provided", func(t *testing.T) {
		t.Parallel()
		davPass(t, davFixture(t))
	})
	t.Run("skips CD signature verification when no CD signer is available", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		st := attestation.TestStore()
		st.CDSigners = nil // injectTestCdSigner: false (:509-511)
		f.ctx.Store = st
		davPass(t, f)
	})
	t.Run("throws CertificationDeclarationSignatureInvalid for CD signed with wrong key", func(t *testing.T) {
		t.Parallel()
		f, key := davFixtureWithKey(t)
		wrongKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		cd := defaultCD()
		cd.signer = wrongKey // the test signer's SKID, another key (:519-537)
		f = davWithCD(t, f, key, buildCD(t, cd))
		davReject(t, f, attestation.CertificationDeclarationSignatureInvalid, `Certification Declaration signature verification failed`)
	})
}

// TestDeviceAttestationValidatorCDFields: describe "Certification
// Declaration field validation" (:550).
func TestDeviceAttestationValidatorCDFields(t *testing.T) {
	t.Parallel()
	t.Run("passes with matching vendorId and productId", func(t *testing.T) {
		t.Parallel()
		davPass(t, davFixture(t))
	})
	t.Run("throws CertificationDeclarationFieldMismatch when CD vendorId does not match BasicInformation", func(t *testing.T) {
		t.Parallel()
		f, key := davFixtureWithKey(t)
		cd := defaultCD()
		cd.vid = 0xFFF2 // :562
		f = davWithCD(t, f, key, buildCD(t, cd))
		davReject(t, f, attestation.CertificationDeclarationFieldMismatch, `CD vendor_id.*does not match BasicInformation VendorID`)
	})
	t.Run("throws CertificationDeclarationFieldMismatch when productId not in CD product_id_array", func(t *testing.T) {
		t.Parallel()
		f, key := davFixtureWithKey(t)
		cd := defaultCD()
		cd.pids = []uint16{0x9999} // :577
		f = davWithCD(t, f, key, buildCD(t, cd))
		davReject(t, f, attestation.CertificationDeclarationFieldMismatch, `CD product_id_array.*does not contain BasicInformation ProductID`)
	})
	t.Run("throws CertificationDeclarationFieldMismatch when CD vendorId does not match DAC vendorId", func(t *testing.T) {
		t.Parallel()
		f, key := davFixtureWithKey(t)
		cd := defaultCD()
		cd.vid = 0xFFF2 // :598-599
		f = davWithCD(t, f, key, buildCD(t, cd))
		f.data.VendorID = 0xFFF2
		davReject(t, f, attestation.CertificationDeclarationFieldMismatch, `DAC vendorId.*does not match CD vendor_id`)
	})
}

// TestDeviceAttestationValidatorRevocation: describe "certificate revocation checks" (:612).
func TestDeviceAttestationValidatorRevocation(t *testing.T) {
	t.Parallel()
	t.Run("throws CertificateRevoked when DAC serial is in revocation list", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		dac := davParse(t, f.data.DAC)
		f.ctx.Store = davRevocations{attestation.TestStore(), map[string]bool{
			davKey(dac.AuthorityKeyId, davSerial(t, dac), davIssuerDN(dac)): true,
		}}
		davReject(t, f, attestation.CertificateRevoked, `Device Attestation Certificate has been revoked`)
	})
	t.Run("throws CertificateRevoked when PAI serial is in revocation list", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		pai := davParse(t, f.data.PAI)
		f.ctx.Store = davRevocations{attestation.TestStore(), map[string]bool{
			davKey(pai.AuthorityKeyId, davSerial(t, pai), davIssuerDN(pai)): true,
		}}
		davReject(t, f, attestation.CertificateRevoked, `Product Attestation Intermediate certificate has been revoked`)
	})
	t.Run("passes when revocation data exists but does not contain DAC or PAI serials", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		dac := davParse(t, f.data.DAC)
		f.ctx.Store = davRevocations{attestation.TestStore(), map[string]bool{
			davKey(dac.AuthorityKeyId, []byte{0xDE, 0xAD, 0xBE, 0xEF}, davIssuerDN(dac)): true, // :660
		}}
		davPass(t, f)
	})
	t.Run("passes when no revocation data is available", func(t *testing.T) {
		t.Parallel()
		davPass(t, davFixture(t))
	})
}

// TestDeviceAttestationValidatorFindings: describe "findings model" (:676).
func TestDeviceAttestationValidatorFindings(t *testing.T) {
	t.Parallel()
	t.Run("returns findings alongside dacPublicKey on successful validation", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		res := davPass(t, f)
		if res.DACPublicKey == nil || res.Findings == nil {
			t.Fatalf("result %+v", res)
		}
		if !res.DACPublicKey.Equal(davParse(t, f.data.DAC).PublicKey) {
			t.Fatal("dacPublicKey is not the DAC's key")
		}
	})
	t.Run("returns CdSignerVerificationSkipped warning when no CD signer is available", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		st := attestation.TestStore()
		st.CDSigners = nil
		f.ctx.Store = st
		if fd := davFinding(davPass(t, f), attestation.CdSignerVerificationSkipped); fd == nil || fd.Level != attestation.LevelWarning {
			t.Fatalf("finding %+v, want a warning", fd)
		}
	})
	t.Run("still throws DeviceAttestationError for hard failures", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.data.Nonce = make([]byte, 32)
		_, _ = rand.Read(f.data.Nonce)
		var ae *attestation.Error
		if _, err := attestation.Validate(f.ctx, f.data); !errors.As(err, &ae) {
			t.Fatalf("Validate = %v, want a DeviceAttestationError", err)
		}
	})
}

// TestDeviceAttestationValidatorCertificationType: describe
// "certification_type findings" (:711).
func TestDeviceAttestationValidatorCertificationType(t *testing.T) {
	t.Parallel()
	t.Run("returns CertificationTypeProvisional warning for provisional CD (type=1)", func(t *testing.T) {
		t.Parallel()
		f, key := davFixtureWithKey(t)
		cd := defaultCD()
		cd.certType = 1 // generate(..., true), :716
		f = davWithCD(t, f, key, buildCD(t, cd))
		fd := davFinding(davPass(t, f), attestation.CertificationTypeProvisional)
		if fd == nil || fd.Level != attestation.LevelWarning || !strings.Contains(fd.Message, "provisional") {
			t.Fatalf("finding %+v", fd)
		}
	})
	t.Run("returns CertificationTypeTest info for test CD (type=0)", func(t *testing.T) {
		t.Parallel()
		fd := davFinding(davPass(t, davFixture(t)), attestation.CertificationTypeTest)
		if fd == nil || fd.Level != attestation.LevelInfo || !strings.Contains(fd.Message, "test") {
			t.Fatalf("finding %+v", fd)
		}
	})
}

// davFetchedAt is the TestStore with every PAA's fetchedAt set to at.
func davFetchedAt(at time.Time) *attestation.StaticStore {
	st := attestation.TestStore()
	for i := range st.PAAs {
		st.PAAs[i].FetchedAt = at
	}
	return st
}

// TestDeviceAttestationValidatorPAAFetchTime: describe "time-based PAA
// trust store check" (:743).
func TestDeviceAttestationValidatorPAAFetchTime(t *testing.T) {
	t.Parallel()
	t.Run("returns PaaTrustStoreTimeMismatch info when PAA was fetched after DAC notBefore", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.ctx.Store = davFetchedAt(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)) // :754
		fd := davFinding(davPass(t, f), attestation.PaaTrustStoreTimeMismatch)
		if fd == nil || fd.Level != attestation.LevelInfo || !strings.Contains(fd.Message, "PAA was fetched at") {
			t.Fatalf("finding %+v", fd)
		}
	})
	t.Run("does not report PaaTrustStoreTimeMismatch when PAA was fetched before DAC notBefore", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		f.ctx.Store = davFetchedAt(time.Unix(0, 0)) // fetchedAt = 0, :772
		if fd := davFinding(davPass(t, f), attestation.PaaTrustStoreTimeMismatch); fd != nil {
			t.Fatalf("finding %+v", fd)
		}
	})
}

// davTestOnly is a store whose PAA FFF1 has production false (addCertificate
// isProduction: false) with the test CD signer; acceptTest is the
// service's fetchTestCertificates.
func davTestOnly(production, acceptTest bool) *attestation.StaticStore {
	st := attestation.TestStore()
	st.PAAs = []attestation.PAA{{DER: secattestation.TestPAAFFF1Cert, Production: production}}
	st.AcceptTest = acceptTest
	return st
}

// TestDeviceAttestationValidatorTestCertificatePAA: describe
// "test-certificate PAA detection" (:781).
func TestDeviceAttestationValidatorTestCertificatePAA(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name                   string
		production, acceptTest bool
		want                   bool
	}{
		{"emits TrustedAsTestCertificate when PAA is test-only and service disallows test certs", false, false, true},
		{"does NOT emit TrustedAsTestCertificate when service acceptsTestCertificates is true", false, true, false},
		// setupDclService: a PAA fetched from the production DCL, test certificates not fetched.
		{"does NOT emit TrustedAsTestCertificate when the PAA is a production cert", true, false, false},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			f := davFixture(t)
			f.ctx.Store = davTestOnly(r.production, r.acceptTest)
			fd := davFinding(davPass(t, f), attestation.TrustedAsTestCertificate)
			if (fd != nil) != r.want || (fd != nil && fd.Level != attestation.LevelError) {
				t.Fatalf("finding %+v, want present=%v at level error", fd, r.want)
			}
		})
	}
	t.Run("runs revocation against a test-only PAA: a revoked DAC still throws CertificateRevoked", func(t *testing.T) {
		t.Parallel()
		f := davFixture(t)
		dac := davParse(t, f.data.DAC)
		f.ctx.Store = davRevocations{davTestOnly(false, false), map[string]bool{
			davKey(dac.AuthorityKeyId, davSerial(t, dac), davIssuerDN(dac)): true,
		}}
		davReject(t, f, attestation.CertificateRevoked, `Device Attestation Certificate has been revoked`)
	})
}

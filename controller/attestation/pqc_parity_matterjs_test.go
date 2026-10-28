// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation_test

// PQC Phase 1, by meaning, after matter.js at 862ad75f:
// packages/general/test/crypto/MlDsaTest.ts (the ML-DSA keys, signatures
// and RFC 9881 encodings the module reads through crypto/mldsa and
// crypto/x509) and the PQC cases of
// packages/protocol/test/certificate/PqcAttestationCertificatesTest.ts
// (the Annex H certificates of PqcAnnexHCertificates.ts, copied verbatim
// into testdata/pqc-annex-h, and chains under ML-DSA roots).
//
// PQC Phase 1 is a forward feature, off unless internal/forward switches
// it on. The tests that switch it on run sequentially and switch it off
// again before the package's parallel tests resume.

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // the subject key identifier is SHA-1 by definition (RFC 5280 §4.2.1.2)
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/internal/forward"
)

// annexH reads one Annex H certificate of testdata/pqc-annex-h.
func annexH(t *testing.T, name string) *x509.Certificate {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "pqc-annex-h", name+".pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		t.Fatalf("%s: no PEM", name)
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return c
}

// The Annex H certificates (PqcAnnexHCertificates.ts:13-814): kind, key
// algorithm, signature algorithm, issuer.
var annexHCerts = []struct {
	name, kind string
	key        attestation.KeyAlgorithm
	sig        x509.SignatureAlgorithm
	issuer     string
}{
	{"paaMlDsa65", "PAA", attestation.KeyMLDSA65, x509.MLDSA65, "paaMlDsa65"},
	{"paaMlDsa44", "PAA", attestation.KeyMLDSA44, x509.MLDSA44, "paaMlDsa44"},
	{"paiMlDsa65ByPaaMlDsa65", "PAI", attestation.KeyMLDSA65, x509.MLDSA65, "paaMlDsa65"},
	{"paiMlDsa44ByPaaMlDsa65", "PAI", attestation.KeyMLDSA44, x509.MLDSA65, "paaMlDsa65"},
	{"paiEcdsaByPaaMlDsa65", "PAI", attestation.KeyECDSAP256, x509.MLDSA65, "paaMlDsa65"},
	{"paiMlDsa44ByPaaMlDsa44", "PAI", attestation.KeyMLDSA44, x509.MLDSA44, "paaMlDsa44"},
	{"paiEcdsaByPaaMlDsa44", "PAI", attestation.KeyECDSAP256, x509.MLDSA44, "paaMlDsa44"},
	{"dacByPaiMlDsa65", "DAC", attestation.KeyECDSAP256, x509.MLDSA65, "paiMlDsa65ByPaaMlDsa65"},
	{"dacByPaiMlDsa44", "DAC", attestation.KeyECDSAP256, x509.MLDSA44, "paiMlDsa44ByPaaMlDsa65"},
}

// TestMlDsa covers MlDsaTest.ts's cases about keys, signatures and
// encodings.
func TestMlDsa(t *testing.T) {
	defer forward.Enable(forward.PQCPhase1)()
	t.Run("resolves the RFC 9881 algorithm OIDs and names the signature algorithm of every Annex H certificate", func(t *testing.T) {
		// :116-128.
		for _, a := range annexHCerts {
			c := annexH(t, a.name)
			if alg, err := attestation.KeyAlgorithmOf(c); err != nil || alg != a.key {
				t.Fatalf("%s: key %v (%v), want %v", a.name, alg, err, a.key)
			}
			if c.SignatureAlgorithm != a.sig {
				t.Fatalf("%s: signature %v, want %v", a.name, c.SignatureAlgorithm, a.sig)
			}
		}
	})
	for _, name := range []string{"paaMlDsa44", "paaMlDsa65"} {
		t.Run("decodes and re-encodes the "+name+" SubjectPublicKeyInfo byte for byte", func(t *testing.T) {
			// :130-137.
			c := annexH(t, name)
			der, err := x509.MarshalPKIXPublicKey(c.PublicKey)
			if err != nil || !bytes.Equal(der, c.RawSubjectPublicKeyInfo) {
				t.Fatalf("re-encoding differs (%v)", err)
			}
		})
	}
	t.Run("rejects a SubjectPublicKeyInfo", func(t *testing.T) {
		// :139-188: trailing data, a truncated key, parameters.
		spki := annexH(t, "paaMlDsa44").RawSubjectPublicKeyInfo
		for name, bad := range map[string][]byte{
			"with trailing data": append(bytes.Clone(spki), 0),
			"truncated":          spki[:len(spki)-1],
		} {
			if _, err := x509.ParsePKIXPublicKey(bad); err == nil {
				t.Fatalf("%s: accepted", name)
			}
		}
	})
	t.Run("refuses to encode a public key of the wrong length", func(t *testing.T) {
		// :190-194 and "rejects a public key of the wrong length"
		// (:268-274).
		if _, err := mldsa.NewPublicKey(mldsa.MLDSA44(), make([]byte, 1311)); err == nil {
			t.Fatal("a 1311-byte ML-DSA-44 key was accepted")
		}
	})
	for _, a := range annexHCerts {
		t.Run("verifies the Annex H "+a.name+" signature with the "+a.issuer+" key", func(t *testing.T) {
			// :196-211.
			if err := attestation.VerifyIssuedBy(annexH(t, a.name), annexH(t, a.issuer)); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("rejects a signature with one bit flipped", func(t *testing.T) {
		// :213-220.
		c := annexH(t, "paiMlDsa65ByPaaMlDsa65")
		bad := *c
		bad.Signature = bytes.Clone(c.Signature)
		bad.Signature[10] ^= 1
		if err := attestation.VerifyIssuedBy(&bad, annexH(t, "paaMlDsa65")); err == nil {
			t.Fatal("a flipped signature verified")
		}
	})
	t.Run("rejects a tampered certificate body", func(t *testing.T) {
		// :222-229.
		c := annexH(t, "paiMlDsa65ByPaaMlDsa65")
		bad := *c
		bad.RawTBSCertificate = bytes.Clone(c.RawTBSCertificate)
		bad.RawTBSCertificate[len(bad.RawTBSCertificate)-1] ^= 1
		if err := attestation.VerifyIssuedBy(&bad, annexH(t, "paaMlDsa65")); err == nil {
			t.Fatal("a tampered body verified")
		}
	})
	t.Run("rejects a signature checked against another key of the same parameter set", func(t *testing.T) {
		// :231-238: the ML-DSA-65 PAI is no signer of the PAA.
		if err := attestation.VerifyIssuedBy(annexH(t, "paiMlDsa65ByPaaMlDsa65"), annexH(t, "paiMlDsa65ByPaaMlDsa65")); err == nil {
			t.Fatal("verified with another ML-DSA-65 key")
		}
	})
	t.Run("rejects a signature of the other parameter set's length", func(t *testing.T) {
		// :240-247: an ML-DSA-65 signature checked by an ML-DSA-44 key.
		if err := attestation.VerifyIssuedBy(annexH(t, "paiMlDsa65ByPaaMlDsa65"), annexH(t, "paaMlDsa44")); err == nil {
			t.Fatal("an ML-DSA-65 signature verified with an ML-DSA-44 key")
		}
	})
	for _, params := range []mldsa.Parameters{mldsa.MLDSA44(), mldsa.MLDSA65()} {
		t.Run("signs and verifies with a generated "+params.String()+" key", func(t *testing.T) {
			// :248-259: a certificate the generated key signs verifies.
			key, err := mldsa.GenerateKey(params)
			if err != nil {
				t.Fatal(err)
			}
			root := pqcCert(t, "PAA", nil, key)
			if err := attestation.VerifyIssuedBy(root, root); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("rejects a private key seed of the wrong length", func(t *testing.T) {
		// :285-293.
		if _, err := mldsa.NewPrivateKey(mldsa.MLDSA44(), make([]byte, 31)); err == nil {
			t.Fatal("a 31-byte seed was accepted")
		}
	})
}

// pqcKey is a key of algorithm alg.
func pqcKey(t *testing.T, alg attestation.KeyAlgorithm) crypto.Signer {
	t.Helper()
	switch alg {
	case attestation.KeyMLDSA44, attestation.KeyMLDSA65:
		params := mldsa.MLDSA44()
		if alg == attestation.KeyMLDSA65 {
			params = mldsa.MLDSA65()
		}
		k, err := mldsa.GenerateKey(params)
		if err != nil {
			t.Fatal(err)
		}
		return k
	case attestation.KeyECDSAP256:
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func pqcSKID(t *testing.T, pub crypto.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	s := sha1.Sum(der) //nolint:gosec // see the import
	return s[:]
}

var pqcSerial int64 = 0x100

// pqcCert mints an attestation certificate of kind for key, issued by
// parent with parentKey (self-signed without a parent).
func pqcCert(t *testing.T, kind string, parent *x509.Certificate, key crypto.Signer, parentKey ...crypto.Signer) *x509.Certificate {
	t.Helper()
	pqcSerial++
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(pqcSerial),
		NotAfter:              time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		BasicConstraintsValid: true,
		SubjectKeyId:          pqcSKID(t, key.Public()),
	}
	switch kind {
	case "PAA":
		tmpl.Subject = name("PQC PAA", 0xFFF1, true, 0, false)
		tmpl.NotBefore = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		tmpl.KeyUsage, tmpl.IsCA = x509.KeyUsageCertSign|x509.KeyUsageCRLSign, true
	case "PAI":
		tmpl.Subject = name("PQC PAI", 0xFFF1, true, 0, false)
		tmpl.NotBefore = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		tmpl.KeyUsage, tmpl.IsCA, tmpl.MaxPathLenZero = x509.KeyUsageCertSign|x509.KeyUsageCRLSign, true, true
	default:
		tmpl.Subject = name("PQC DAC", 0xFFF1, true, 0x8000, true)
		tmpl.NotBefore = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		tmpl.KeyUsage = x509.KeyUsageDigitalSignature
	}
	signer := key
	if parent == nil {
		parent = tmpl
	} else {
		signer = parentKey[0]
		tmpl.AuthorityKeyId = parent.SubjectKeyId
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, key.Public(), signer)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// pqcFixture is an attestation exchange whose chain is a PAA of paaAlg, a
// PAI of paiAlg and a P-256 DAC, with the PAA as the one trusted root; a
// non-nil signer signs the PAI in place of the PAA.
func pqcFixture(t *testing.T, paaAlg, paiAlg attestation.KeyAlgorithm, paiSigner crypto.Signer) fixture {
	t.Helper()
	paaKey, paiKey := pqcKey(t, paaAlg), pqcKey(t, paiAlg)
	dacKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	paa := pqcCert(t, "PAA", nil, paaKey)
	if paiSigner == nil {
		paiSigner = paaKey
	}
	paaParent := *paa
	paaParent.PublicKey = paiSigner.Public()
	pai := pqcCert(t, "PAI", &paaParent, paiKey, paiSigner)
	dac := pqcCert(t, "DAC", pai, dacKey, paiKey)
	challenge, nonce := make([]byte, 16), make([]byte, 32)
	_, _ = rand.Read(challenge)
	_, _ = rand.Read(nonce)
	el := elements(t, buildCD(t, defaultCD()), nonce)
	store := attestation.TestStore()
	store.PAAs = []attestation.PAA{{DER: paa.Raw}}
	return fixture{
		ctx: attestation.Context{Challenge: challenge, Store: store},
		data: attestation.Data{
			DAC: dac.Raw, PAI: pai.Raw, Elements: el, Signature: sign(t, dacKey, el, challenge), Nonce: nonce,
			VendorID: 0xFFF1, ProductID: 0x8000,
		},
	}
}

// TestPqcAttestationChains covers PqcAttestationCertificatesTest.ts's
// "Annex H examples" and "device attestation" PQC cases.
func TestPqcAttestationChains(t *testing.T) {
	defer forward.Enable(forward.PQCPhase1)()
	t.Run("verifies a PAA's self-signature", func(t *testing.T) {
		// :116-119.
		for _, n := range []string{"paaMlDsa44", "paaMlDsa65"} {
			if err := attestation.VerifyIssuedBy(annexH(t, n), annexH(t, n)); err != nil {
				t.Fatalf("%s: %v", n, err)
			}
		}
	})
	t.Run("rejects an ML-DSA issuer key for an ECDSA signature", func(t *testing.T) {
		// :136-144: an ECDSA-signed certificate checked with an ML-DSA key.
		ecPAA := pqcCert(t, "PAA", nil, pqcKey(t, attestation.KeyECDSAP256))
		if err := attestation.VerifyIssuedBy(ecPAA, annexH(t, "paaMlDsa65")); err == nil {
			t.Fatal("an ECDSA signature verified with an ML-DSA key")
		}
	})
	t.Run("rejects an issuer key of another algorithm", func(t *testing.T) {
		// :121-134: an ML-DSA signature checked with an EC key.
		if err := attestation.VerifyIssuedBy(annexH(t, "paiEcdsaByPaaMlDsa65"), annexH(t, "paiEcdsaByPaaMlDsa65")); err == nil {
			t.Fatal("an ML-DSA signature verified with an EC key")
		}
	})
	t.Run("rejects an ML-DSA key in a DAC", func(t *testing.T) {
		// :154-156: a DAC with an ML-DSA key is unparseable.
		f := pqcFixture(t, attestation.KeyMLDSA65, attestation.KeyMLDSA65, nil)
		f.data.DAC = f.data.PAI
		davReject(t, f, attestation.CertificateUnparseable, "EC P-256")
	})
	t.Run("accepts a chain under an ML-DSA PAA and PAI", func(t *testing.T) {
		// :583-597.
		for _, l := range []struct{ paa, pai attestation.KeyAlgorithm }{
			{attestation.KeyMLDSA65, attestation.KeyMLDSA65},
			{attestation.KeyMLDSA65, attestation.KeyMLDSA44},
			{attestation.KeyMLDSA65, attestation.KeyECDSAP256},
			{attestation.KeyMLDSA44, attestation.KeyMLDSA44},
			{attestation.KeyMLDSA44, attestation.KeyECDSAP256},
		} {
			davPass(t, pqcFixture(t, l.paa, l.pai, nil))
		}
	})
	for _, l := range []struct{ paa, pai attestation.KeyAlgorithm }{
		{attestation.KeyMLDSA44, attestation.KeyMLDSA65},
		{attestation.KeyECDSAP256, attestation.KeyMLDSA44},
	} {
		t.Run("rejects a "+string(l.pai)+" PAI under a "+string(l.paa)+" PAA", func(t *testing.T) {
			// :599-605: the PAI stronger than its PAA (mayIssue).
			davReject(t, pqcFixture(t, l.paa, l.pai, nil), attestation.CertificateChainInvalid, "stronger than")
		})
	}
	t.Run("rejects a PAI the trusted ML-DSA PAA did not sign", func(t *testing.T) {
		// :607-617.
		davReject(t, pqcFixture(t, attestation.KeyMLDSA65, attestation.KeyMLDSA65, pqcKey(t, attestation.KeyMLDSA65)),
			attestation.CertificateChainInvalid, "PAI signature verification failed")
	})
	t.Run("keeps a traditional certificate bound to 600 bytes", func(t *testing.T) {
		// "rejects a traditional attestation certificate larger than 600
		// bytes" (:343-361), PQC Phase 1 on: an ECDSA PAI above 600 bytes.
		f := pqcFixture(t, attestation.KeyECDSAP256, attestation.KeyECDSAP256, nil)
		key := pqcKey(t, attestation.KeyECDSAP256)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(1), Subject: name("Big PAI", 0xFFF1, true, 0, false),
			NotBefore: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC),
			KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true, SubjectKeyId: pqcSKID(t, key.Public()),
			AuthorityKeyId:  pqcSKID(t, key.Public()),
			ExtraExtensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 3, 4}, Value: make([]byte, 700)}},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
		if err != nil {
			t.Fatal(err)
		}
		f.data.PAI = der
		davReject(t, f, attestation.CertificateUnparseable, "600 byte limit")
	})
}

// TestPqcAttestationFeatureOff covers the describe "PQC Phase 1
// attestation certificates while forward Matter features are off"
// (:619-680): every Annex H certificate and a chain under ML-DSA roots are
// refused.
func TestPqcAttestationFeatureOff(t *testing.T) {
	if forward.Enabled(forward.PQCPhase1) {
		t.Fatal("PQC Phase 1 is on outside the tests that enable it")
	}
	for _, a := range annexHCerts {
		t.Run("rejects "+a.name, func(t *testing.T) {
			c := annexH(t, a.name)
			if err := attestation.VerifyIssuedBy(c, annexH(t, a.issuer)); !errors.Is(err, attestation.ErrMLDSAAttestationDisabled) {
				t.Fatalf("err %v, want ErrMLDSAAttestationDisabled", err)
			}
		})
	}
	restore := forward.Enable(forward.PQCPhase1)
	f := pqcFixture(t, attestation.KeyMLDSA65, attestation.KeyMLDSA44, nil)
	restore()
	t.Run("does not validate a device chain of an ML-DSA-44 PAI under an ML-DSA-65 PAA", func(t *testing.T) {
		davReject(t, f, attestation.CertificateUnparseable, "PQC Phase 1 is not enabled")
	})
}

// TestCertificatesPQCSizeLimit: "rejects a DER attestation certificate
// larger than the PQC limit before parsing" (CertificatesTest.ts:698): a
// DAC above MAX_PQC_DER_CERTIFICATE_SIZE (10240 bytes, kinds/common.ts:19)
// is refused by its size, before it is parsed.
func TestCertificatesPQCSizeLimit(t *testing.T) {
	t.Parallel()
	f := davFixture(t)
	f.data.DAC = make([]byte, 10241)
	davReject(t, f, attestation.CertificateUnparseable, "10240 byte limit")
}

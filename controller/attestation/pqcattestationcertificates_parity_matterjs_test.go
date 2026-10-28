// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// A port of matter.js packages/protocol/test/certificate/PqcAttestationCertificatesTest.ts at
// 862ad75f: the traditional-certificate refusals of its "Annex H examples"
// and "parsing" groups — parseAttCert (Paa/Pai/Dac.fromAsn1 and
// Certificate.parseAsn1Certificate), P256Key (publicKey) and VerifySignedBy
// (verifySignature). The PQC Phase 1 cases are
// pqc_parity_matterjs_test.go's.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"
	"testing"

	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// pqcTraditionalPAI is buildChain({ paa: "ECDSA-P256", pai: "ECDSA-P256" })
// (PqcAttestationCertificatesTest.ts:899-961): a P-256 PAI and the key of
// the PAA that signed it.
func pqcTraditionalPAI(t *testing.T) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	chain, err := secattestation.BuildTestChain(0xfff1, 0x8000)
	if err != nil {
		t.Fatalf("BuildTestChain: %v", err)
	}
	return chain.PAI, secattestation.TestPAANoVIDPrivateKey
}

// pqcRewrite is the test's hex surgery: from must occur exactly count
// times, every occurrence becomes to.
func pqcRewrite(t *testing.T, der []byte, from, to string, count int) []byte {
	t.Helper()
	h := hex.EncodeToString(der)
	if n := strings.Count(h, from); n != count {
		t.Fatalf("%s occurs %d times, want %d", from, n, count)
	}
	out, err := hex.DecodeString(strings.ReplaceAll(h, from, to))
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return out
}

// pqcResign is ecSignedPai (PqcAttestationCertificatesTest.ts:262-270): the
// PAI's TBSCertificate with alter applied to its elements, re-signed with
// ECDSA SHA-256 by the PAA key as X509.sign does. elements are the TBS
// children: version, serial, signature, issuer, validity, subject,
// subjectPublicKeyInfo, extensions.
func pqcResign(t *testing.T, pai []byte, key *ecdsa.PrivateKey, alter func(elements [][]byte)) []byte {
	t.Helper()
	c, err := x509.ParseCertificate(pai)
	if err != nil {
		t.Fatalf("parse PAI: %v", err)
	}
	var tbs asn1.RawValue
	if _, err := asn1.Unmarshal(c.RawTBSCertificate, &tbs); err != nil {
		t.Fatalf("tbs: %v", err)
	}
	var elements [][]byte
	for rest := tbs.Bytes; len(rest) > 0; {
		var e asn1.RawValue
		if rest, err = asn1.Unmarshal(rest, &e); err != nil {
			t.Fatalf("tbs element: %v", err)
		}
		elements = append(elements, e.FullBytes)
	}
	alter(elements)
	seq := func(parts ...[]byte) []byte {
		out, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: bytes.Join(parts, nil)})
		if err != nil {
			t.Fatalf("sequence: %v", err)
		}
		return out
	}
	newTBS := seq(elements...)
	digest := sha256.Sum256(newTBS)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	bits, err := asn1.Marshal(asn1.BitString{Bytes: sig, BitLength: 8 * len(sig)})
	if err != nil {
		t.Fatalf("signature: %v", err)
	}
	return seq(newTBS, elements[2], bits)
}

func pqcHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return b
}

// pqcSPKI is { type: { algorithm, curve }, bytes: DerBitString(key) }.
func pqcSPKI(t *testing.T, algorithm, curve string, key []byte) []byte {
	t.Helper()
	algo, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: pqcHex(t, algorithm+curve)})
	if err != nil {
		t.Fatalf("algorithm: %v", err)
	}
	bits, err := asn1.Marshal(asn1.BitString{Bytes: key, BitLength: 8 * len(key)})
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	out, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: append(algo, bits...)})
	if err != nil {
		t.Fatalf("spki: %v", err)
	}
	return out
}

func pqcECKey(t *testing.T, pai []byte) []byte {
	t.Helper()
	c, err := x509.ParseCertificate(pai)
	if err != nil {
		t.Fatalf("parse PAI: %v", err)
	}
	k, err := P256Key(c)
	if err != nil {
		t.Fatalf("PAI key: %v", err)
	}
	b, err := k.Bytes()
	if err != nil {
		t.Fatalf("PAI key bytes: %v", err)
	}
	return b
}

// The OBJECT IDENTIFIER encodings the test splices in (with tag and length).
const (
	pqcOIDUnknown     = "06032a0304"           // 1.2.3.4, ObjectId("2a0304")
	pqcOIDEcPublicKey = "06072a8648ce3d0201"   // X962.PublicKeyAlgorithmEcPublicKey
	pqcOIDPrime256v1  = "06082a8648ce3d030107" // X962.PublicKeyAlgorithmEcPublicKeyP256 curve
	pqcOIDSecp384r1   = "06052b81040022"       // ObjectId("2b81040022")
)

// TestPqcAttestationCertificatesAnnexHExamples is the traditional part of
// the describe "Annex H examples" (PqcAttestationCertificatesTest.ts:69).
// matter.js runs it with PQC Phase 1 enabled; Go parses attestation
// certificates as traditional ones only, which these cases need.
func TestPqcAttestationCertificatesAnnexHExamples(t *testing.T) {
	t.Run("rejects a DAC key of an unsupported algorithm", func(t *testing.T) {
		pai, paaKey := pqcTraditionalPAI(t)
		key := pqcECKey(t, pai)
		der := pqcResign(t, pai, paaKey, func(e [][]byte) {
			e[6] = pqcSPKI(t, pqcOIDUnknown, pqcOIDPrime256v1, key)
		})
		// Dac.fromAsn1 refuses the key when it constructs the DAC
		// (AttestationCertificates.ts:66-71); Go parses the certificate and
		// refuses the key where it reads it, P256Key.
		dac, err := parseAttCert(der, true)
		if err != nil {
			return
		}
		if _, err := P256Key(dac.x509); err == nil {
			t.Fatal("a DAC with a key of algorithm 1.2.3.4 was accepted")
		}
	})
	t.Run("rejects ML-DSA certificates where only traditional certificates are allowed", func(t *testing.T) {
		// Noc.fromAsn1 has no Go counterpart (Go parses no operational
		// certificate from DER), and the traditional
		// Certificate.parseAsn1Certificate is the ledger's
		// (controller/dcl's 600-byte parse). With PQC Phase 1 off — this
		// test's state — the attestation parser refuses ML-DSA as well.
		block, _ := pem.Decode([]byte(pqcPaiMlDsa44ByPaaMlDsa44))
		if block == nil {
			t.Fatal("Annex H PEM does not decode")
		}
		_, err := parseAttCert(block.Bytes, true)
		if !errors.Is(err, ErrMLDSAAttestationDisabled) {
			t.Fatalf("parseAttCert = %v, want ErrMLDSAAttestationDisabled", err)
		}
	})
}

// TestPqcAttestationCertificatesParsing is the traditional part of the
// describe "parsing" (PqcAttestationCertificatesTest.ts:186).
func TestPqcAttestationCertificatesParsing(t *testing.T) {
	t.Run("refuses a public key of an unsupported algorithm", func(t *testing.T) {
		pai, paaKey := pqcTraditionalPAI(t)
		key := pqcECKey(t, pai)
		der := pqcResign(t, pai, paaKey, func(e [][]byte) {
			e[6] = pqcSPKI(t, pqcOIDUnknown, pqcOIDPrime256v1, key)
		})
		c := certificatesParse(t, der, true)
		if _, err := P256Key(c.x509); err == nil {
			t.Fatal("P256Key accepted a key of algorithm 1.2.3.4")
		}
	})
	t.Run("refuses an EC key on an unsupported curve", func(t *testing.T) {
		pai, paaKey := pqcTraditionalPAI(t)
		key := pqcECKey(t, pai)
		der := pqcResign(t, pai, paaKey, func(e [][]byte) {
			e[6] = pqcSPKI(t, pqcOIDEcPublicKey, pqcOIDSecp384r1, key)
		})
		// matter.js parses the certificate and refuses the key on access;
		// crypto/x509 already cannot place the P-256 point on secp384r1.
		c, err := parseAttCert(der, true)
		if err != nil {
			return
		}
		if _, err := P256Key(c.x509); err == nil {
			t.Fatal("P256Key accepted a key on secp384r1")
		}
	})
	t.Run("refuses to verify a signature of an unsupported algorithm", func(t *testing.T) {
		// ecdsa-with-SHA384 declared, while the signature is SHA-256 (:296).
		pai, paaKey := pqcTraditionalPAI(t)
		sha384, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: pqcHex(t, "06082a8648ce3d040303")})
		if err != nil {
			t.Fatalf("algorithm: %v", err)
		}
		der := pqcResign(t, pai, paaKey, func(e [][]byte) { e[2] = sha384 })
		c := certificatesParse(t, der, true)
		if err := VerifySignedBy(c.x509, &paaKey.PublicKey); err == nil || !strings.Contains(err.Error(), "ecdsa-with-SHA256") {
			t.Fatalf("VerifySignedBy = %v, want the unsupported signature algorithm", err)
		}
	})
	t.Run("rejects a signature algorithm that is not an OBJECT IDENTIFIER", func(t *testing.T) {
		pai, _ := pqcTraditionalPAI(t)
		altered := pqcRewrite(t, pai, "300a06082a8648ce3d040302", "300a02082a8648ce3d040302", 2)
		if _, err := parseAttCert(altered, true); err == nil {
			t.Fatal("parseAttCert accepted a signature algorithm tagged INTEGER")
		}
	})
	// rejects ${description} (:316-341).
	for _, c := range []struct{ description, from, to string }{
		{"a public key algorithm that is not an OBJECT IDENTIFIER", "06072a8648ce3d0201", "02072a8648ce3d0201"},
		{"a curve that is not an OBJECT IDENTIFIER", "06082a8648ce3d030107", "02082a8648ce3d030107"},
		{"an EC key BIT STRING with unused bits", "0342000", "0342010"},
	} {
		t.Run("rejects "+c.description, func(t *testing.T) {
			pai, _ := pqcTraditionalPAI(t)
			if _, err := parseAttCert(pqcRewrite(t, pai, c.from, c.to, 1), true); err == nil {
				t.Fatalf("parseAttCert accepted %s", c.description)
			}
		})
	}
	t.Run("rejects a traditional attestation certificate larger than 600 bytes", func(t *testing.T) {
		pai, paaKey := pqcTraditionalPAI(t)
		// futureExtension: DerObject("2a0304", { value: new Uint8Array(200) }).
		ext, err := asn1.Marshal(struct {
			ID    asn1.ObjectIdentifier
			Value []byte
		}{asn1.ObjectIdentifier{1, 2, 3, 4}, make([]byte, 200)})
		if err != nil {
			t.Fatalf("extension: %v", err)
		}
		bulky := pqcResign(t, pai, paaKey, func(e [][]byte) {
			var outer, list asn1.RawValue
			if _, err := asn1.Unmarshal(e[7], &outer); err != nil {
				t.Fatalf("extensions: %v", err)
			}
			if _, err := asn1.Unmarshal(outer.Bytes, &list); err != nil {
				t.Fatalf("extension list: %v", err)
			}
			list = asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: append(list.Bytes, ext...)}
			inner, err := asn1.Marshal(list)
			if err != nil {
				t.Fatalf("extension list: %v", err)
			}
			if e[7], err = asn1.Marshal(asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 3, IsCompound: true, Bytes: inner}); err != nil {
				t.Fatalf("extensions: %v", err)
			}
		})
		if len(bulky) <= 600 {
			t.Fatalf("bulky PAI is %d bytes, want more than 600", len(bulky))
		}
		if _, err := parseAttCert(bulky, true); err == nil || !strings.Contains(err.Error(), "600 byte limit") {
			t.Fatalf("parseAttCert = %v, want the 600 byte limit", err)
		}
	})
}

// PqcAnnexHCertificates.ts:526-611 at 862ad75f, verbatim (Matter 1.6 Annex
// H: an ML-DSA-44 PAI signed by the ML-DSA-44 PAA).
const pqcPaiMlDsa44ByPaaMlDsa44 = `-----BEGIN CERTIFICATE-----
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
AAAAAAAAAAAAAAAAAAAAAAAAAAARHy42
-----END CERTIFICATE-----
`

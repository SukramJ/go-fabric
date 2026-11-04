// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation_test

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/internal/forward"
)

// pqcParsingHex decodes a hex string.
func pqcParsingHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// pqcParsingRewrite is the test's hex surgery (`rewrite`): from must occur
// exactly once in der, and becomes to.
func pqcParsingRewrite(t *testing.T, der []byte, from, to string) []byte {
	t.Helper()
	h := hex.EncodeToString(der)
	if n := strings.Count(h, from); n != 1 {
		t.Fatalf("%s occurs %d times, want 1", from, n)
	}
	return pqcParsingHex(t, strings.Replace(h, from, to, 1))
}

// pqcReplaceSignature is X509.certificateToDer({...x509Of(cert), signature})
// for a parsed certificate: the TBSCertificate and the algorithm kept,
// the signature BIT STRING replaced, the lengths re-encoded.
func pqcReplaceSignature(t *testing.T, c *x509.Certificate, sig []byte) []byte {
	t.Helper()
	var outer, tbs, alg, old asn1.RawValue
	if _, err := asn1.Unmarshal(c.Raw, &outer); err != nil {
		t.Fatal(err)
	}
	rest, err := asn1.Unmarshal(outer.Bytes, &tbs)
	if err != nil {
		t.Fatal(err)
	}
	if rest, err = asn1.Unmarshal(rest, &alg); err != nil {
		t.Fatal(err)
	}
	if _, err = asn1.Unmarshal(rest, &old); err != nil {
		t.Fatal(err)
	}
	bits, err := asn1.Marshal(asn1.BitString{Bytes: sig, BitLength: len(sig) * 8})
	if err != nil {
		t.Fatal(err)
	}
	out, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: bytes.Join([][]byte{tbs.FullBytes, alg.FullBytes, bits}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// pqcRefused reports where this module refuses der as a PAI of paa: at
// parse (crypto/x509, Pai.fromAsn1's twin) or — where crypto/x509 parses
// what matter.js refuses on the DER structure — at the issuer check
// (VerifyIssuedBy); "" when it passes both.
func pqcRefused(t *testing.T, der []byte, paa *x509.Certificate) string {
	t.Helper()
	c, err := x509.ParseCertificate(der)
	if err != nil {
		return "parse: " + err.Error()
	}
	if err := attestation.VerifyIssuedBy(c, paa); err != nil {
		return "verify: " + err.Error()
	}
	return ""
}

// TestPqcAttestationCertificatesParsingParityMatterJS ports the six
// PqcAttestationCertificatesTest.ts parsing cases on
// paiMlDsa44ByPaaMlDsa44 (:194-258 at 862ad75f) that matter.js's
// parseAsn1Certificate refuses on the DER: differing inner and outer
// signature algorithms (:194-204), an ML-DSA signature of the wrong
// length (:206-214), an ML-DSA signature algorithm with parameters
// (:216-232), a signature BIT STRING with unused bits (:234-239), a
// signature that is not a BIT STRING (:241-251) and a public key BIT
// STRING with unused bits (:253-258). Go's parser is crypto/x509; what it
// does not refuse on the structure fails the issuer's verification, so no
// such certificate is ever accepted.
func TestPqcAttestationCertificatesParsingParityMatterJS(t *testing.T) {
	defer forward.Enable(forward.PQCPhase1)()
	pai, paa := annexH(t, "paiMlDsa44ByPaaMlDsa44"), annexH(t, "paaMlDsa44")
	if where := pqcRefused(t, pai.Raw, paa); where != "" {
		t.Fatalf("the Annex H PAI itself is refused: %s", where)
	}
	t.Run("rejects differing inner and outer signature algorithms", func(t *testing.T) {
		h := hex.EncodeToString(pai.Raw)
		oid := "0609608648016503040311"
		outer := strings.LastIndex(h, oid)
		altered := pqcParsingHex(t, h[:outer]+"0609608648016503040312"+h[outer+len(oid):])
		if where := pqcRefused(t, altered, paa); where == "" {
			t.Fatal("accepted a certificate whose outer signature algorithm differs from the TBSCertificate's")
		}
	})
	t.Run("rejects an ML-DSA signature of the wrong length", func(t *testing.T) {
		if where := pqcRefused(t, pqcReplaceSignature(t, pai, make([]byte, 2419)), paa); where == "" {
			t.Fatal("accepted a 2419-byte ML-DSA-44 signature")
		}
	})
	t.Run("rejects an ML-DSA signature algorithm with parameters", func(t *testing.T) {
		// A freshly minted ML-DSA-44 PAI whose AlgorithmIdentifier carries
		// a NULL parameter inside the TBSCertificate and outside, re-signed
		// by its PAA's key.
		paaKey := pqcKey(t, attestation.KeyMLDSA44)
		paaCert := pqcCert(t, "PAA", nil, paaKey)
		paiCert := pqcCert(t, "PAI", paaCert, pqcKey(t, attestation.KeyMLDSA44), paaKey)
		withParams := pqcParsingHex(t, "300d06096086480165030403110500")
		var outer, tbs asn1.RawValue
		if _, err := asn1.Unmarshal(paiCert.Raw, &outer); err != nil {
			t.Fatal(err)
		}
		if _, err := asn1.Unmarshal(outer.Bytes, &tbs); err != nil {
			t.Fatal(err)
		}
		var elements [][]byte
		for rest := tbs.Bytes; len(rest) > 0; {
			var e asn1.RawValue
			var err error
			if rest, err = asn1.Unmarshal(rest, &e); err != nil {
				t.Fatal(err)
			}
			elements = append(elements, e.FullBytes)
		}
		elements[2] = withParams
		tbsDER, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: bytes.Join(elements, nil)})
		if err != nil {
			t.Fatal(err)
		}
		sig, err := paaKey.Sign(rand.Reader, tbsDER, crypto.Hash(0))
		if err != nil {
			t.Fatal(err)
		}
		bits, err := asn1.Marshal(asn1.BitString{Bytes: sig, BitLength: len(sig) * 8})
		if err != nil {
			t.Fatal(err)
		}
		signed, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: bytes.Join([][]byte{tbsDER, withParams, bits}, nil)})
		if err != nil {
			t.Fatal(err)
		}
		if where := pqcRefused(t, signed, paaCert); where == "" {
			t.Fatal("accepted an ML-DSA signature algorithm carrying parameters")
		}
	})
	t.Run("rejects an ML-DSA signature BIT STRING with unused bits", func(t *testing.T) {
		if where := pqcRefused(t, pqcParsingRewrite(t, pai.Raw, "0382097500", "0382097501"), paa); where == "" {
			t.Fatal("accepted a signature BIT STRING with an unused bit")
		}
	})
	t.Run("rejects an ML-DSA signature that is not a BIT STRING", func(t *testing.T) {
		h := hex.EncodeToString(pai.Raw)
		if !strings.HasPrefix(h, "30820f") {
			t.Fatalf("PAI DER starts %s, want a 3082 SEQUENCE of 0x0fxx bytes", h[:6])
		}
		n, err := strconv.ParseUint(h[4:8], 16, 16)
		if err != nil {
			t.Fatal(err)
		}
		altered := pqcParsingHex(t, fmt.Sprintf("3082%04x", n-1)+strings.Replace(h[8:], "0382097500", "04820974", 1))
		if where := pqcRefused(t, altered, paa); where == "" {
			t.Fatal("accepted a signature that is an OCTET STRING")
		}
	})
	t.Run("rejects an ML-DSA public key that is not canonical RFC 9881", func(t *testing.T) {
		if where := pqcRefused(t, pqcParsingRewrite(t, pai.Raw, "0382052100", "0382052101"), paa); where == "" {
			t.Fatal("accepted a public key BIT STRING with an unused bit")
		}
	})
}

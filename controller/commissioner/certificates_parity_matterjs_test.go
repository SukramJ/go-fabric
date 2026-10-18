// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// A port of matter.js packages/protocol/test/certificate/CertificatesTest.ts at
// 862ad75f: the describe "getPublicKeyFromCsr" — Certificate.getPublicKeyFromCsr
// is publicKeyFromCSR here. The other cases are in
// secure/mattercert/certificates_parity_matterjs_test.go and
// controller/attestation/certificates_parity_matterjs_test.go.

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
	"strings"
	"testing"
)

// TestCertificates.ts:203-209 at 862ad75f: TEST_PRIVATE_KEY,
// TEST_PUBLIC_KEY and TEST_CSR_REQUEST_ASN1, the CertificationRequestInfo
// Certificate.createCertificateSigningRequest produces for that key.
const (
	certificatesTestPrivateKey = "727f1005cba47ed7822a9d930943621617cfd3b79d9af528b801ecf9f1992204"
	certificatesTestPublicKey  = "0462e2b6e1baff8d74a6fd8216c4cb67a3363a31e691492792e61aee610261481396725ef95e142686ba98f339b0ff65bc338bec7b9e8be0bdf3b2774982476220"
	certificatesCSRRequestASN1 = "3070020100300e310c300a060355040a0c034353523059301306072a8648ce3d020106082a8648ce3d0301070342000462e2b6e1baff8d74a6fd8216c4cb67a3363a31e691492792e61aee610261481396725ef95e142686ba98f339b0ff65bc338bec7b9e8be0bdf3b2774982476220a000"
)

func certificatesMustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	return b
}

func certificatesTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), certificatesMustHex(t, certificatesTestPrivateKey))
	if err != nil {
		t.Fatalf("private key: %v", err)
	}
	return key
}

// certificatesSignCSR wraps request as the CSR the test builds with
// DerCodec.encode({ request, signAlgorithm: X962.EcdsaWithSHA256,
// signature: DerBitString(sig) }) (CertificatesTest.ts:714-718).
func certificatesSignCSR(t *testing.T, key *ecdsa.PrivateKey, request []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(request)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	algo, err := asn1.Marshal(struct{ Algorithm asn1.ObjectIdentifier }{asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}})
	if err != nil {
		t.Fatalf("algorithm: %v", err)
	}
	bits, err := asn1.Marshal(asn1.BitString{Bytes: sig, BitLength: 8 * len(sig)})
	if err != nil {
		t.Fatalf("signature: %v", err)
	}
	csr, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: bytes.Join([][]byte{request, algo, bits}, nil)})
	if err != nil {
		t.Fatalf("csr: %v", err)
	}
	return csr
}

// certificatesRequest is the request object of CertificatesTest.ts:708-713
// with subject as the raw subject element: version 0, the key as
// X962.PublicKeyEcPrime256v1 and endSignedBytes ContextTagged(0).
func certificatesRequest(t *testing.T, key *ecdsa.PrivateKey, subject []byte) []byte {
	t.Helper()
	spki, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("spki: %v", err)
	}
	body := bytes.Join([][]byte{{0x02, 0x01, 0x00}, subject, spki, {0xa0, 0x00}}, nil)
	req, err := asn1.Marshal(asn1.RawValue{Tag: asn1.TagSequence, IsCompound: true, Bytes: body})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	return req
}

// TestCertificatesGetPublicKeyFromCsr is the describe "getPublicKeyFromCsr"
// (CertificatesTest.ts:695). Go's device-side CSR builder differs from
// createCertificateSigningRequest (its subject), so the first case signs
// matter.js's own TEST_CSR_REQUEST_ASN1 instead of building one.
func TestCertificatesGetPublicKeyFromCsr(t *testing.T) {
	key := certificatesTestKey(t)
	want := certificatesMustHex(t, certificatesTestPublicKey)
	t.Run("get the public key from the CSR", func(t *testing.T) {
		got, err := publicKeyFromCSR(certificatesSignCSR(t, key, certificatesMustHex(t, certificatesCSRRequestASN1)))
		if err != nil {
			t.Fatalf("publicKeyFromCSR: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("public key = %x, want %x", got, want)
		}
	})
	t.Run("get the public key from a CSR with an empty subject", func(t *testing.T) {
		got, err := publicKeyFromCSR(certificatesSignCSR(t, key, certificatesRequest(t, key, []byte{0x30, 0x00})))
		if err != nil {
			t.Fatalf("publicKeyFromCSR: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("public key = %x, want %x", got, want)
		}
	})
	t.Run("declines a CSR with a non-SEQUENCE subject", func(t *testing.T) {
		// A constructed SET where the RDNSequence belongs (CertificatesTest.ts:739).
		_, err := publicKeyFromCSR(certificatesSignCSR(t, key, certificatesRequest(t, key, []byte{0x31, 0x00})))
		// The refusal is the parse of the subject, not the signature check.
		if !errors.Is(err, errUnexpectedResponse) || strings.Contains(err.Error(), "signature") {
			t.Fatalf("publicKeyFromCSR = %v, want the CSR refused for its subject", err)
		}
	})
}

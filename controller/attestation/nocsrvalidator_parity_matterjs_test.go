// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation_test

// A port of matter.js packages/protocol/test/certificate/NocsrValidatorTest.ts
// at b25ed960: NocsrValidator.validate is attestation.ValidateNOCSR, its
// NocsrError with the failed NocsrCheck an *attestation.NOCSRError. The
// CSR is Go's x509.CreateCertificateRequest for the operational key, not
// matter.js's Certificate.createCertificateSigningRequest; the checks only
// read its version, key, algorithm and signature.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/tlv"
)

// nocsrCSRPublicKeyPrefix is CSR_PUBLIC_KEY_PREFIX (:14-15): the BIT
// STRING of 66 bytes, no unused bits, an uncompressed point.
var nocsrCSRPublicKeyPrefix = []byte{0x03, 0x42, 0x00, 0x04}

// nocsrScenario is the scenario() of :18-61.
type nocsrScenario struct {
	ctx            attestation.NOCSRContext
	dacKey         *ecdsa.PrivateKey
	operationalKey *ecdsa.PrivateKey
	csr            []byte
}

func newNOCSRScenario(t *testing.T) nocsrScenario {
	t.Helper()
	dacKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	operationalKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	challenge, nonce := make([]byte, 16), make([]byte, 32)
	_, _ = rand.Read(challenge)
	_, _ = rand.Read(nonce)
	return nocsrScenario{
		ctx:            attestation.NOCSRContext{Challenge: challenge, CSRNonce: nonce, DACPublicKey: &dacKey.PublicKey},
		dacKey:         dacKey,
		operationalKey: operationalKey,
		csr:            nocsrCSR(t, operationalKey),
	}
}

// nocsrCSR is a PKCS #10 CSR for key.
func nocsrCSR(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{Organization: []string{"CSR"}}}, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

// nocsrElements encodes TlvCertSigningRequest {[1] csr, [2] nonce, [3]
// vendorReserved1 when not nil}.
func nocsrElements(t *testing.T, csr, nonce, reserved1 []byte) []byte {
	t.Helper()
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutOctets(tlv.ContextTag(1), csr)
	e.PutOctets(tlv.ContextTag(2), nonce)
	if reserved1 != nil {
		e.PutOctets(tlv.ContextTag(3), reserved1)
	}
	if err := e.EndContainer(); err != nil {
		t.Fatal(err)
	}
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// respondRaw is the CSRResponse for elements, signed with the DAC key
// over elements ‖ challenge (:26-31).
func (s nocsrScenario) respondRaw(t *testing.T, elements []byte) attestation.NOCSRResponse {
	t.Helper()
	return attestation.NOCSRResponse{Elements: elements, Signature: sign(t, s.dacKey, elements, s.ctx.Challenge)}
}

// respond is the CSRResponse of a device with the given NOCSR Information
// fields replaced (:34-36); nil keeps the scenario's.
func (s nocsrScenario) respond(t *testing.T, csr, nonce []byte) attestation.NOCSRResponse {
	t.Helper()
	if csr == nil {
		csr = s.csr
	}
	if nonce == nil {
		nonce = s.ctx.CSRNonce
	}
	return s.respondRaw(t, nocsrElements(t, csr, nonce, nil))
}

// respondWithLength is a valid CSRResponse whose NOCSRElements are length
// bytes long, filled up with vendorReserved1 (:39-51).
func (s nocsrScenario) respondWithLength(t *testing.T, length int) attestation.NOCSRResponse {
	t.Helper()
	encode := func(fill int) []byte { return nocsrElements(t, s.csr, s.ctx.CSRNonce, make([]byte, fill)) }
	fill := length - len(encode(0))
	for fill >= 0 && len(encode(fill)) > length {
		fill--
	}
	if fill < 0 || len(encode(fill)) != length {
		t.Fatalf("no fill gives NOCSRElements of %d bytes", length)
	}
	return s.respondRaw(t, encode(fill))
}

// nocsrExpectFailure is expectFailure (:63-65).
func nocsrExpectFailure(t *testing.T, err error, check attestation.NOCSRCheck) {
	t.Helper()
	var ne *attestation.NOCSRError
	if !errors.As(err, &ne) || ne.Check != check {
		t.Fatalf("ValidateNOCSR = %v, want NocsrError %s", err, check)
	}
}

func nocsrMustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestNocsrValidatorParity is the describe "NocsrValidator" (:17).
func TestNocsrValidatorParity(t *testing.T) {
	t.Parallel()
	t.Run("returns the operational public key of a valid response", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		key, err := attestation.ValidateNOCSR(s.ctx, s.respond(t, nil, nil))
		if err != nil {
			t.Fatal(err)
		}
		want, _ := s.operationalKey.PublicKey.Bytes()
		if !bytes.Equal(key, want) {
			t.Fatalf("key %x, want %x", key, want)
		}
	})
	t.Run("refuses an empty response", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		r := s.respond(t, nil, nil)
		empty := r
		empty.Elements = []byte{}
		_, err := attestation.ValidateNOCSR(s.ctx, empty)
		nocsrExpectFailure(t, err, attestation.NOCSRResponseEmpty)
		empty = r
		empty.Signature = []byte{}
		_, err = attestation.ValidateNOCSR(s.ctx, empty)
		nocsrExpectFailure(t, err, attestation.NOCSRResponseEmpty)
	})
	t.Run("refuses a response without NOCSRElements or AttestationSignature", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		r := s.respond(t, nil, nil)
		_, err := attestation.ValidateNOCSR(s.ctx, attestation.NOCSRResponse{Signature: r.Signature, ElementsAbsent: true})
		nocsrExpectFailure(t, err, attestation.NOCSRFieldMissing)
		_, err = attestation.ValidateNOCSR(s.ctx, attestation.NOCSRResponse{Elements: r.Elements, SignatureAbsent: true})
		nocsrExpectFailure(t, err, attestation.NOCSRFieldMissing)
	})
	t.Run("accepts NOCSRElements of 900 bytes and refuses 901 bytes", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		if _, err := attestation.ValidateNOCSR(s.ctx, s.respondWithLength(t, 900)); err != nil {
			t.Fatalf("900 bytes: %v", err)
		}
		_, err := attestation.ValidateNOCSR(s.ctx, s.respondWithLength(t, 901))
		nocsrExpectFailure(t, err, attestation.NOCSRElementsTooLong)
	})
	t.Run("refuses an attestation signature of another key", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		ctx := s.ctx
		ctx.DACPublicKey = &other.PublicKey
		_, err := attestation.ValidateNOCSR(ctx, s.respond(t, nil, nil))
		nocsrExpectFailure(t, err, attestation.NOCSRAttestationSignatureInvalid)
	})
	t.Run("refuses an attestation signature over another challenge", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		ctx := s.ctx
		ctx.Challenge = make([]byte, 16)
		_, _ = rand.Read(ctx.Challenge)
		_, err := attestation.ValidateNOCSR(ctx, s.respond(t, nil, nil))
		nocsrExpectFailure(t, err, attestation.NOCSRAttestationSignatureInvalid)
	})
	t.Run("refuses an attestation signature that is not 64 bytes", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		r := s.respond(t, nil, nil)
		r.Signature = []byte{0x01, 0x02}
		_, err := attestation.ValidateNOCSR(s.ctx, r)
		nocsrExpectFailure(t, err, attestation.NOCSRAttestationSignatureInvalid)
	})
	t.Run("refuses NOCSR elements that are not a NOCSR Information structure", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		_, err := attestation.ValidateNOCSR(s.ctx, s.respondRaw(t, []byte{0x01, 0x02}))
		nocsrExpectFailure(t, err, attestation.NOCSRElementsUnparseable)
	})
	t.Run("refuses another CSRNonce", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		other := make([]byte, 32)
		_, _ = rand.Read(other)
		_, err := attestation.ValidateNOCSR(s.ctx, s.respond(t, nil, other))
		nocsrExpectFailure(t, err, attestation.NOCSRCSRNonceMismatch)
	})
	t.Run("refuses a CSR with an invalid structure", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		_, err := attestation.ValidateNOCSR(s.ctx, s.respond(t, nocsrMustHex(t, "3000"), nil))
		nocsrExpectFailure(t, err, attestation.NOCSRCSRInvalid)
	})
	t.Run("refuses a CSR that is not DER", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		_, err := attestation.ValidateNOCSR(s.ctx, s.respond(t, nocsrMustHex(t, "30ff"), nil))
		nocsrExpectFailure(t, err, attestation.NOCSRCSRInvalid)
	})
	t.Run("refuses a CSR whose own signature does not verify", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		// Another key's CSR with the scenario's key put in, its signature
		// left the other key's (:169-183).
		otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		otherCSR := nocsrCSR(t, otherKey)
		own, _ := s.operationalKey.PublicKey.Bytes()
		otherPub, _ := otherKey.PublicKey.Bytes()
		forged := bytes.Replace(otherCSR, otherPub[1:], own[1:], 1)
		if bytes.Equal(forged, otherCSR) {
			t.Fatal("the other CSR carries no key to replace")
		}
		_, err := attestation.ValidateNOCSR(s.ctx, s.respond(t, forged, nil))
		nocsrExpectFailure(t, err, attestation.NOCSRCSRInvalid)
	})
	t.Run("refuses a CSR whose public key is not an uncompressed point", func(t *testing.T) {
		t.Parallel()
		s := newNOCSRScenario(t)
		csr := bytes.Replace(s.csr, nocsrCSRPublicKeyPrefix, []byte{0x03, 0x42, 0x00, 0x05}, 1)
		if bytes.Equal(csr, s.csr) {
			t.Fatal("the CSR carries no key prefix")
		}
		_, err := attestation.ValidateNOCSR(s.ctx, s.respond(t, csr, nil))
		nocsrExpectFailure(t, err, attestation.NOCSRCSRInvalid)
	})
}

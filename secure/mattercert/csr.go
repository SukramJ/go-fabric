// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"fmt"
	"io"
)

// CreateCSR builds the PKCS#10 certificate signing request a node sends
// in its NOCSR elements for key: version 0, the subject `O=CSR`, the
// P-256 public key, no attributes, signed ecdsa-with-SHA256 — matter.js
// Certificate.createCertificateSigningRequest
// (packages/protocol/src/certificate/kinds/Certificate.ts:264-277 at
// 862ad75f). The commissioner's CA ignores the subject and issues the
// operational subject DN of its own. rnd is the signature's randomness;
// nil is crypto/rand.
func CreateCSR(key *ecdsa.PrivateKey, rnd io.Reader) ([]byte, error) {
	if key == nil {
		return nil, fmt.Errorf("%w: nil key", ErrMalformed)
	}
	if rnd == nil {
		rnd = rand.Reader
	}
	pub, err := key.PublicKey.ECDH()
	if err != nil {
		return nil, fmt.Errorf("matter cert: CSR key: %w", err)
	}
	org, err := asn1.Marshal(rdnAttribute{Type: asn1.ObjectIdentifier{2, 5, 4, 10}, Value: asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagUTF8String, Bytes: []byte("CSR")}})
	if err != nil {
		return nil, fmt.Errorf("matter cert: CSR subject: %w", err)
	}
	pkParams, err := asn1.Marshal(oidPrime256v1)
	if err != nil {
		return nil, fmt.Errorf("matter cert: CSR key params: %w", err)
	}
	request, err := asn1.Marshal(csrRequest{
		Version: 0,
		Subject: []asn1.RawValue{{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: org}},
		PublicKey: subjectPublicKeyInfo{
			Algorithm:        algorithmIdentifier{Algorithm: oidEcPublicKey, Parameters: asn1.RawValue{FullBytes: pkParams}},
			SubjectPublicKey: asn1.BitString{Bytes: pub.Bytes(), BitLength: len(pub.Bytes()) * 8},
		},
		Attributes: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true},
	})
	if err != nil {
		return nil, fmt.Errorf("matter cert: CSR request: %w", err)
	}
	digest := sha256.Sum256(request)
	sig, err := ecdsa.SignASN1(rnd, key, digest[:])
	if err != nil {
		return nil, fmt.Errorf("matter cert: CSR sign: %w", err)
	}
	out, err := asn1.Marshal(csrSigned{
		Request:            asn1.RawValue{FullBytes: request},
		SignatureAlgorithm: algorithmIdentifier{Algorithm: oidEcdsaWithSHA256},
		Signature:          asn1.BitString{Bytes: sig, BitLength: len(sig) * 8},
	})
	if err != nil {
		return nil, fmt.Errorf("matter cert: CSR: %w", err)
	}
	return out, nil
}

// csrRequest is the CertificationRequestInfo (RFC 2986 §4.1).
type csrRequest struct {
	Version    int
	Subject    []asn1.RawValue
	PublicKey  subjectPublicKeyInfo
	Attributes asn1.RawValue
}

// csrSigned is the CertificationRequest (RFC 2986 §4.2).
type csrSigned struct {
	Request            asn1.RawValue
	SignatureAlgorithm algorithmIdentifier
	Signature          asn1.BitString
}

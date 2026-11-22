// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/secure/mattercert"
)

// TestVerifyVVSCIssuedBy holds R2-P6-test-0117: VerifyVVSCIssuedBy accepts
// the VVSC its issuer's key signed, and refuses it under another key, with
// a changed field after signing, and for a malformed issuer key. Before,
// no test named it.
func TestVerifyVVSCIssuedBy(t *testing.T) {
	t.Parallel()
	issuer, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubOf := func(k *ecdsa.PrivateKey) []byte {
		ek, err := k.PublicKey.ECDH()
		if err != nil {
			t.Fatal(err)
		}
		return ek.Bytes()
	}

	subjectKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed := vvscCert(func(c *mattercert.Certificate) { c.PublicKey = pubOf(subjectKey) })
	tbs, err := mattercert.TBSToDER(signed)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(tbs)
	r, s, err := ecdsa.Sign(rand.Reader, issuer, h[:])
	if err != nil {
		t.Fatal(err)
	}
	signed.Signature = make([]byte, 64)
	r.FillBytes(signed.Signature[:32])
	s.FillBytes(signed.Signature[32:])

	if err := mattercert.VerifyVVSCIssuedBy(signed, pubOf(issuer)); err != nil {
		t.Fatalf("the issuer's key: %v, want accepted", err)
	}
	if err := mattercert.VerifyVVSCIssuedBy(signed, pubOf(other)); !errors.Is(err, mattercert.ErrSignatureInvalid) {
		t.Errorf("another key: %v, want ErrSignatureInvalid", err)
	}
	changed := *signed
	changed.Subject.MatterVVSID++
	if err := mattercert.VerifyVVSCIssuedBy(&changed, pubOf(issuer)); !errors.Is(err, mattercert.ErrSignatureInvalid) {
		t.Errorf("a field changed after signing: %v, want ErrSignatureInvalid", err)
	}
	if err := mattercert.VerifyVVSCIssuedBy(signed, make([]byte, 65)); !errors.Is(err, mattercert.ErrInvalidVVSC) {
		t.Errorf("a malformed issuer key: %v, want ErrInvalidVVSC", err)
	}
	if err := mattercert.VerifyVVSCIssuedBy(nil, pubOf(issuer)); !errors.Is(err, mattercert.ErrInvalidVVSC) {
		t.Errorf("a nil certificate: %v, want ErrInvalidVVSC", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"testing"
)

// TestEcdsaSignatureP1363ParityMatterJS mirrors
// packages/general/test/crypto/EcdsaSignatureTest.ts:10-25 at 862ad75f
// ("accepts a 64-byte IEEE P1363 signature", "rejects an IEEE P1363
// signature of the wrong length"): the Sigma signature is r ‖ s, 32 bytes
// each (unpackECDSASig / packECDSASig), and any other length is refused.
func TestEcdsaSignatureP1363ParityMatterJS(t *testing.T) {
	t.Parallel()
	ieee := make([]byte, 64)
	for i := range ieee {
		ieee[i] = byte(i + 1)
	}
	r, s, err := unpackECDSASig(ieee)
	if err != nil {
		t.Fatal(err)
	}
	if got := packECDSASig(r, s); !bytes.Equal(got, ieee) {
		t.Errorf("round trip %x, want %x", got, ieee)
	}
	if _, _, err := unpackECDSASig(make([]byte, 32)); err == nil {
		t.Error("a 32-byte signature was accepted")
	}
}

// TestDhSharedSecretParityMatterJS mirrors
// packages/general/test/crypto/StandardCryptoTest.ts:107-116 and
// packages/nodejs/test/crypto/NodeJsCryptoTest.ts:186-195 at 862ad75f
// ("computes correct DH shared secret"): two fresh P-256 key pairs derive
// the same 32-byte secret (ecdhSharedSecret).
func TestDhSharedSecretParityMatterJS(t *testing.T) {
	t.Parallel()
	k1, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s1, err := ecdhSharedSecret(k1, k2.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	s2, err := ecdhSharedSecret(k2, k1.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s1, s2) || len(s1) != 32 {
		t.Errorf("secrets %x and %x, want equal 32-byte values", s1, s2)
	}
}

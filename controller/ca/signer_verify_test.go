// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/controller/ca"
)

// strayingSigner states key as its public key and, once wrong is set,
// signs with other — a key file, HSM or KMS that does not hold the key it
// names.
type strayingSigner struct {
	key, other *ecdsa.PrivateKey
	wrong      atomic.Bool
}

func (s *strayingSigner) Public() crypto.PublicKey { return &s.key.PublicKey }

func (s *strayingSigner) Sign(r io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if s.wrong.Load() {
		return s.other.Sign(r, digest, opts)
	}
	return s.key.Sign(r, digest, opts)
}

// TestSignerSignatureIsVerified: a certificate signed through a host's
// crypto.Signer is verified under the key it names before it leaves the
// CA, so a signer that signs with another key fails New and GenerateNOC,
// and nothing is saved (review II, R2-P6-err-1855).
func TestSignerSignatureIsVerified(t *testing.T) {
	t.Parallel()
	newSigner := func(t *testing.T) *strayingSigner {
		t.Helper()
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		o, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return &strayingSigner{key: k, other: o}
	}
	subject := func(t *testing.T) []byte {
		t.Helper()
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		b, _ := k.PublicKey.Bytes()
		return b
	}
	t.Run("New", func(t *testing.T) {
		t.Parallel()
		s := newSigner(t)
		s.wrong.Store(true)
		st := &ca.MemoryStore{}
		if _, err := ca.New(ca.Config{Store: st, RootSigner: s}); !signatureRefused(err) {
			t.Fatalf("New = %v, want the signature refused", err)
		}
		if st.Saves() != 0 {
			t.Fatalf("%d saves", st.Saves())
		}
	})
	t.Run("GenerateNOC", func(t *testing.T) {
		t.Parallel()
		s := newSigner(t)
		st := &ca.MemoryStore{}
		c, err := ca.New(ca.Config{Store: st, RootSigner: s})
		if err != nil {
			t.Fatal(err)
		}
		saves := st.Saves()
		s.wrong.Store(true)
		if _, err := c.GenerateNOC(subject(t), 1, 2, nil); !signatureRefused(err) {
			t.Fatalf("GenerateNOC = %v, want the signature refused", err)
		}
		if st.Saves() != saves {
			t.Fatal("the counter was saved")
		}
		s.wrong.Store(false)
		if _, err := c.GenerateNOC(subject(t), 1, 2, nil); err != nil {
			t.Fatalf("control: %v", err)
		}
	})
}

func signatureRefused(err error) bool {
	return err != nil && strings.Contains(err.Error(), "the signer's signature does not verify")
}

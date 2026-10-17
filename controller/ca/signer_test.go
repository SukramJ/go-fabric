// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca_test

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/controller/ca"
)

// refSigner is a crypto.Signer outside the CA, counting its signatures,
// naming its key.
type refSigner struct {
	key   *ecdsa.PrivateKey
	ref   string
	signs atomic.Int32
}

func newRefSigner(t *testing.T, ref string) *refSigner {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &refSigner{key: k, ref: ref}
}

func (s *refSigner) Public() crypto.PublicKey { return &s.key.PublicKey }

func (s *refSigner) Sign(r io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	s.signs.Add(1)
	return s.key.Sign(r, digest, opts)
}

func (s *refSigner) KeyRef() string { return s.ref }

// bareSigner is a crypto.Signer that names no key.
type bareSigner struct{ crypto.Signer }

// TestRootSigner: a CA over a RootSigner issues its RCAC for the signer's
// key and signs through it; the stored record holds the certificates and
// the KeyRef, no private key; it opens again with the signer, not
// without it or with another key.
func TestRootSigner(t *testing.T) {
	settings := &mapSettings{}
	st := ca.SettingStore{Settings: settings}
	root := newRefSigner(t, "pkcs11:token=ctl;object=root")
	c, err := ca.New(ca.Config{Store: st, RootSigner: root, Now: fixedNow})
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := root.key.PublicKey.Bytes()
	if !bytes.Equal(c.RootPublicKey(), pub) || root.signs.Load() != 1 {
		t.Fatalf("root key %x, %d signatures", c.RootPublicKey(), root.signs.Load())
	}
	noc, err := c.GenerateNOC(newKey(t), 1, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Verifier().VerifyAndExtractPubKey(noc, nil); err != nil || root.signs.Load() != 2 {
		t.Fatalf("NOC: %v, %d signatures", err, root.signs.Load())
	}
	js := settings.m[ca.DefaultSettingKey]
	if strings.Contains(js, "privateKey") || strings.Contains(js, "rootKeyPair") || !strings.Contains(js, `"rootKeyRef":"pkcs11:token=ctl;object=root"`) {
		t.Fatalf("stored %s", js)
	}

	again, err := ca.New(ca.Config{Store: st, RootSigner: root, Now: fixedNow})
	if err != nil || !bytes.Equal(again.RootCert(), c.RootCert()) {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := ca.New(ca.Config{Store: st, Now: fixedNow}); !errors.Is(err, ca.ErrConfig) || !strings.Contains(err.Error(), "pkcs11:") {
		t.Fatalf("without the signer: %v", err)
	}
	if _, err := ca.New(ca.Config{Store: st, RootSigner: newRefSigner(t, "other"), Now: fixedNow}); !errors.Is(err, ca.ErrConfig) {
		t.Fatalf("another key: %v", err)
	}
	if ca.KeyRefOf(bareSigner{root.key}) != ca.ExternalKeyRef {
		t.Fatal("a signer without KeyRef is not recorded as external")
	}
}

// TestRootSignerTakesOverStoredKey: a CA stored with its private key and
// opened with a RootSigner over the same key is rewritten without it.
func TestRootSignerTakesOverStoredKey(t *testing.T) {
	settings := &mapSettings{}
	st := ca.SettingStore{Settings: settings}
	if _, err := ca.New(ca.Config{Store: st, Now: fixedNow}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(settings.m[ca.DefaultSettingKey], "privateKey") {
		t.Fatal("the SQLite path no longer stores the key")
	}
	stored, _, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), stored.RootKeyPair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	signer := &refSigner{key: k, ref: "file:/k"}
	if _, err := ca.New(ca.Config{Store: st, RootSigner: signer, Now: fixedNow}); err != nil {
		t.Fatal(err)
	}
	if js := settings.m[ca.DefaultSettingKey]; strings.Contains(js, "privateKey") || !strings.Contains(js, `"rootKeyRef":"file:/k"`) {
		t.Fatalf("stored %s", js)
	}
}

// TestICACSigner: with IntermediateCA, both keys behind signers; the
// ICAC signs the NOCs, the record holds the ICAC's public key and KeyRef.
func TestICACSigner(t *testing.T) {
	settings := &mapSettings{}
	st := ca.SettingStore{Settings: settings}
	root, icac := newRefSigner(t, "root"), newRefSigner(t, "icac")
	cfg := ca.Config{Store: st, IntermediateCA: true, RootSigner: root, ICACSigner: icac, Now: fixedNow}
	c, err := ca.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	noc, err := c.GenerateNOC(newKey(t), 1, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Verifier().VerifyAndExtractPubKey(noc, c.ICACCert()); err != nil {
		t.Fatal(err)
	}
	if root.signs.Load() != 2 || icac.signs.Load() != 1 {
		t.Fatalf("root %d, icac %d signatures", root.signs.Load(), icac.signs.Load())
	}
	if js := settings.m[ca.DefaultSettingKey]; strings.Contains(js, "privateKey") || !strings.Contains(js, `"icacKeyRef":"icac"`) {
		t.Fatalf("stored %s", js)
	}
	if _, err := ca.New(cfg); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	// The root may stay offline: the ICAC signer alone opens it.
	if _, err := ca.New(ca.Config{Store: st, IntermediateCA: true, ICACSigner: icac, Now: fixedNow}); err != nil {
		t.Fatalf("offline root: %v", err)
	}
	if !strings.Contains(settings.m[ca.DefaultSettingKey], `"rootKeyRef":"root"`) {
		t.Fatal("the offline root's KeyRef was dropped")
	}
	if _, err := ca.New(ca.Config{Store: st, IntermediateCA: true, Now: fixedNow}); !errors.Is(err, ca.ErrConfig) {
		t.Fatalf("without the ICAC signer: %v", err)
	}
	if _, err := ca.New(ca.Config{ICACSigner: icac}); !errors.Is(err, ca.ErrConfig) {
		t.Fatalf("ICACSigner without IntermediateCA: %v", err)
	}
}

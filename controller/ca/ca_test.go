// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

var issuedAt = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

// matterSeconds is t in Matter epoch seconds.
func matterSeconds(t time.Time) uint64 {
	return uint64(t.Unix() - 946684800)
}

func newKey(t *testing.T) []byte {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, err := k.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decode(t *testing.T, raw []byte) *mattercert.Certificate {
	t.Helper()
	c, err := mattercert.Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return c
}

// TestRootAndNOCRoundTrip: the RCAC passes ValidateRCAC, a NOC verifies
// against the root and carries the fields of CertificateAuthority.ts:287-320.
func TestRootAndNOCRoundTrip(t *testing.T) {
	t.Parallel()
	c, err := ca.New(ca.Config{Clock: clocktest.New(issuedAt)})
	if err != nil {
		t.Fatal(err)
	}
	root := decode(t, c.RootCert())
	if err := mattercert.ValidateRCAC(root); err != nil {
		t.Fatalf("ValidateRCAC: %v", err)
	}
	if !bytes.Equal(root.PublicKey, c.RootPublicKey()) || len(c.RootPublicKey()) != 65 {
		t.Fatal("root public key differs from the RCAC's")
	}
	if !bytes.Equal(root.SerialNumber, []byte{0}) || root.Subject.MatterRCACID != 0 || root.Issuer.MatterRCACID != 0 {
		t.Fatalf("RCAC serial %x subject %+v", root.SerialNumber, root.Subject)
	}
	wantNB, wantNA := matterSeconds(issuedAt)-365*86400, matterSeconds(issuedAt)+10*365*86400
	if root.NotBefore != wantNB || root.NotAfter != wantNA {
		t.Fatalf("RCAC validity %d..%d, want %d..%d", root.NotBefore, root.NotAfter, wantNB, wantNA)
	}
	if c.ICACCert() != nil {
		t.Fatal("ICAC without IntermediateCA")
	}

	pub := newKey(t)
	nocBytes, err := c.GenerateNOC(pub, 0x0F0B, 0x1234, []uint32{0x0001_0001})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Verifier().VerifyAndExtractPubKey(nocBytes, nil)
	if err != nil {
		t.Fatalf("VerifyAndExtractPubKey: %v", err)
	}
	gotBytes, _ := got.Bytes()
	if !bytes.Equal(gotBytes, pub) {
		t.Fatal("NOC public key differs")
	}
	noc := decode(t, nocBytes)
	switch {
	case !bytes.Equal(noc.SerialNumber, []byte{1}):
		t.Fatalf("NOC serial %x, want 01", noc.SerialNumber)
	case noc.Subject.MatterFabricID != 0x0F0B || noc.Subject.MatterNodeID != 0x1234:
		t.Fatalf("NOC subject %+v", noc.Subject)
	case len(noc.Subject.CASEAuthTags) != 1 || noc.Subject.CASEAuthTags[0] != 0x0001_0001:
		t.Fatalf("NOC CATs %v", noc.Subject.CASEAuthTags)
	case noc.Extensions.BasicConstraintsIsCA:
		t.Fatal("NOC is a CA")
	case !bytes.Equal(noc.Extensions.AuthorityKeyID, root.Extensions.SubjectKeyID):
		t.Fatal("NOC authority key id is not the root's")
	case !bytes.Equal(noc.Extensions.SubjectKeyID, ca.KeyIdentifier(pub)):
		t.Fatal("NOC subject key id is not SHA-1 of its key")
	case noc.NotBefore != wantNB || noc.NotAfter != wantNA:
		t.Fatalf("NOC validity %d..%d", noc.NotBefore, noc.NotAfter)
	}
	// The DN order of the object literal (fabricId, nodeId, CATs).
	if want := []uint8{21, 17, 22}; !bytes.Equal(noc.Subject.Order, want) {
		t.Fatalf("NOC subject order %v, want %v", noc.Subject.Order, want)
	}
}

// TestICACChain: with IntermediateCA the ICAC is cert id 1, issued by the
// root; NOCs are issued and signed by the ICAC (#getSigningParameters).
func TestICACChain(t *testing.T) {
	t.Parallel()
	c, err := ca.New(ca.Config{Clock: clocktest.New(issuedAt), IntermediateCA: true})
	if err != nil {
		t.Fatal(err)
	}
	icac := decode(t, c.ICACCert())
	if !icac.Subject.HasICACID || icac.Subject.MatterICACID != 1 || icac.Issuer.MatterRCACID != 0 || !icac.Extensions.BasicConstraintsIsCA {
		t.Fatalf("ICAC subject %+v issuer %+v", icac.Subject, icac.Issuer)
	}
	pub := newKey(t)
	nocBytes, err := c.GenerateNOC(pub, 7, 0x42, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Verifier().VerifyAndExtractPubKey(nocBytes, c.ICACCert()); err != nil {
		t.Fatalf("VerifyAndExtractPubKey: %v", err)
	}
	noc := decode(t, nocBytes)
	if !bytes.Equal(noc.SerialNumber, []byte{2}) || noc.Issuer.MatterICACID != 1 || !bytes.Equal(noc.Extensions.AuthorityKeyID, icac.Extensions.SubjectKeyID) {
		t.Fatalf("NOC serial %x issuer %+v", noc.SerialNumber, noc.Issuer)
	}
	if len(noc.Subject.CASEAuthTags) != 0 {
		t.Fatal("NOC carries CATs it was not given")
	}
}

// TestStoreSaveAndLoad: a new CA saves its root; New over the same store
// loads it; the counter is saved after every NOC; a store whose ICAC
// presence disagrees with the configuration is refused.
func TestStoreSaveAndLoad(t *testing.T) {
	t.Parallel()
	store := &ca.MemoryStore{}
	first, err := ca.New(ca.Config{Store: store, Clock: clocktest.New(issuedAt)})
	if err != nil {
		t.Fatal(err)
	}
	st, ok, _ := store.Load()
	if !ok || st.RootCertID != 0 || st.NextCertificateID != 1 || st.RootKeyPair == nil || st.ICAC != nil ||
		!bytes.Equal(st.RootCertBytes, first.RootCert()) || len(st.RootKeyIdentifier) != 20 {
		t.Fatalf("stored %+v (%v)", st, ok)
	}
	if _, err := first.GenerateNOC(newKey(t), 1, 1, nil); err != nil {
		t.Fatal(err)
	}
	if st, _, _ = store.Load(); st.NextCertificateID != 2 || store.Saves() != 2 {
		t.Fatalf("counter %d after one NOC, %d saves", st.NextCertificateID, store.Saves())
	}

	second, err := ca.New(ca.Config{Store: store, Clock: clocktest.New(issuedAt)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(second.RootCert(), first.RootCert()) || !bytes.Equal(second.RootPublicKey(), first.RootPublicKey()) {
		t.Fatal("the second CA did not load the stored root")
	}
	noc, err := second.GenerateNOC(newKey(t), 1, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decode(t, noc).SerialNumber, []byte{2}) {
		t.Fatal("the loaded counter was not continued")
	}
	if _, err := first.Verifier().VerifyAndExtractPubKey(noc, nil); err != nil {
		t.Fatalf("a NOC of the loaded CA does not verify against the first: %v", err)
	}

	if _, err := ca.New(ca.Config{Store: store, IntermediateCA: true}); !errors.Is(err, ca.ErrConfig) {
		t.Fatalf("ICAC mismatch: %v", err)
	}
	icacStore := &ca.MemoryStore{}
	withICAC, err := ca.New(ca.Config{Store: icacStore, IntermediateCA: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ca.New(ca.Config{Store: icacStore}); !errors.Is(err, ca.ErrConfig) {
		t.Fatalf("ICAC present, not configured: %v", err)
	}
	reloaded, err := ca.New(ca.Config{Store: icacStore, IntermediateCA: true})
	if err != nil || !bytes.Equal(reloaded.ICACCert(), withICAC.ICACCert()) {
		t.Fatalf("ICAC reload: %v", err)
	}
	// An offline root: the stored ICAC alone signs.
	st, _, _ = icacStore.Load()
	st.RootKeyPair = nil
	offline := &ca.MemoryStore{}
	_ = offline.Save(st)
	ica, err := ca.New(ca.Config{Store: offline, IntermediateCA: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ica.GenerateNOC(newKey(t), 1, 1, nil); err != nil {
		t.Fatalf("NOC from an offline root: %v", err)
	}
	// Without an ICAC the root key is required.
	st, _, _ = store.Load()
	st.RootKeyPair = nil
	broken := &ca.MemoryStore{}
	_ = broken.Save(st)
	if _, err := ca.New(ca.Config{Store: broken}); !errors.Is(err, ca.ErrConfig) {
		t.Fatalf("no root key, no ICAC: %v", err)
	}
}

// TestCompressedFabricID: the derivation matches bridgetest's (and so the
// device's at AddNOC) for the same root.
func TestCompressedFabricID(t *testing.T) {
	t.Parallel()
	const fabricID = 0x0F0B
	c := bridgetest.Dial(t, "[::1]:5540", bridgetest.Options{Passcode: 20202021, FabricID: fabricID, NodeID: 1, Seed: 3})
	got, err := ca.CompressedFabricID(c.RootPublicKey(), fabricID)
	if err != nil {
		t.Fatal(err)
	}
	if got != c.CompressedFabricID() {
		t.Fatalf("compressed fabric id %X, bridgetest %X", got, c.CompressedFabricID())
	}
	if _, err := ca.CompressedFabricID([]byte{4, 1}, 1); err == nil {
		t.Fatal("a short key was accepted")
	}
}

// TestRejects: a bad subject key and a time outside the Matter range.
func TestRejects(t *testing.T) {
	t.Parallel()
	c, err := ca.New(ca.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.GenerateNOC([]byte{4, 1, 2}, 1, 1, nil); err == nil {
		t.Fatal("a malformed subject key was accepted")
	}
	if _, err := ca.New(ca.Config{Clock: clocktest.New(time.Date(2000, 6, 1, 0, 0, 0, 0, time.UTC))}); err == nil {
		t.Fatal("a notBefore before the Matter epoch was accepted")
	}
}

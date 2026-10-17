// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// opSigner is an operational key outside the fabric, counting signatures.
type opSigner struct {
	key   *ecdsa.PrivateKey
	signs atomic.Int32
}

func newOpSigner(t *testing.T) *opSigner {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &opSigner{key: k}
}

func (s *opSigner) Public() crypto.PublicKey { return &s.key.PublicKey }

func (s *opSigner) Sign(r io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	s.signs.Add(1)
	return s.key.Sign(r, digest, opts)
}

func (s *opSigner) KeyRef() string { return "kms:op" }

// TestOperationalSignerRecord: with an OperationalSigner the NOC is for
// the signer's key, the identity has no PrivateKey, the record holds the
// NOC, the public key and the KeyRef; it loads again with the signer;
// without it the record is ErrFabricMismatch; another signer gets a new
// NOC; a stored private key leaves the record when a signer takes over.
func TestOperationalSignerRecord(t *testing.T) {
	t.Parallel()
	mem := &ca.MemoryStore{}
	authority, err := ca.New(ca.Config{Store: mem})
	if err != nil {
		t.Fatal(err)
	}
	s := newOpSigner(t)
	cfg := FabricConfig{FabricID: testFabricID, RootNodeID: controllerNodeID, VendorID: 0xFFF1, Store: mem, OperationalSigner: s}
	f, err := NewFabric(authority, cfg)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := s.key.PublicKey.Bytes()
	noc, err := mattercert.Decode(f.Identity.NOC)
	if err != nil || !bytes.Equal(noc.PublicKey, pub) || f.Identity.PrivateKey != nil || f.Identity.Signer != s {
		t.Fatalf("identity: %v", err)
	}
	rec, _, _ := mem.LoadFabric()
	if rec.KeyRef != "kms:op" || rec.KeyPair == nil || len(rec.KeyPair.PrivateKey) != 0 || !bytes.Equal(rec.KeyPair.PublicKey, pub) {
		t.Fatalf("record %+v", rec)
	}
	again, err := NewFabric(authority, FabricConfig{Store: mem, OperationalSigner: s})
	if err != nil || !bytes.Equal(again.Identity.NOC, f.Identity.NOC) {
		t.Fatalf("reload: %v", err)
	}
	if _, err := NewFabric(authority, FabricConfig{Store: mem}); !errors.Is(err, ErrFabricMismatch) {
		t.Fatalf("without the signer: %v", err)
	}
	other := newOpSigner(t)
	moved, err := NewFabric(authority, FabricConfig{Store: mem, OperationalSigner: other})
	if err != nil || bytes.Equal(moved.Identity.NOC, f.Identity.NOC) || moved.IPK == nil || !bytes.Equal(moved.IPK, f.IPK) {
		t.Fatalf("another signer: %v", err)
	}

	// The SQLite path, then a signer over the same key.
	mem2 := &ca.MemoryStore{}
	a2, err := ca.New(ca.Config{Store: mem2})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := NewFabric(a2, FabricConfig{FabricID: testFabricID, RootNodeID: controllerNodeID, Store: mem2})
	if err != nil {
		t.Fatal(err)
	}
	taken := &opSigner{key: plain.Identity.PrivateKey}
	moved, err = NewFabric(a2, FabricConfig{Store: mem2, OperationalSigner: taken})
	if err != nil || !bytes.Equal(moved.Identity.NOC, plain.Identity.NOC) {
		t.Fatalf("take over: %v", err)
	}
	if rec, _, _ := mem2.LoadFabric(); len(rec.KeyPair.PrivateKey) != 0 || rec.KeyRef != "kms:op" {
		t.Fatalf("record after take-over %+v", rec)
	}
}

// TestCommissionWithOperationalSigner: a controller whose operational key
// is a signer commissions the node; CASE's Sigma3 is signed by it.
func TestCommissionWithOperationalSigner(t *testing.T) {
	t.Parallel()
	_, addr := startNode(t, nil, "127.0.0.1:0")
	conn := newFakeMDNS()
	authority, err := ca.New(ca.Config{Store: &ca.MemoryStore{}})
	if err != nil {
		t.Fatal(err)
	}
	s := newOpSigner(t)
	c, _ := newControllerWith(t, discovery.NewNames(conn, discard()), "127.0.0.1:0", authority,
		FabricConfig{FabricID: testFabricID, RootNodeID: controllerNodeID, VendorID: 0xFFF1, Label: fabricLabel, OperationalSigner: s})
	c.announceNode(t, conn, addr.Port, commissionedNode)
	dev := c.find(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := c.com.Commission(ctx, Options{Fabric: c.fabric, Passcode: nodePasscode, NodeID: commissionedNode, Addresses: udpAddrs(dev), DiscoveryData: &dev}); err != nil {
		t.Fatalf("Commission: %v", err)
	}
	if s.signs.Load() < 1 {
		t.Fatal("CASE did not sign through the operational signer")
	}
}

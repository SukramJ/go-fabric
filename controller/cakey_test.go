// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/store"
)

// TestNewCAKey: with Config.CAKey the fabric's root of trust is the
// signer's key, the database holds no root private key, and a second run
// under the same signer loads the same fabric; Peers is the peer set.
func TestNewCAKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openDB(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var fabricID uint64
	for run := range 2 {
		cfg := config(db)
		cfg.CAKey = key
		c, err := controller.New(ctx, cfg)
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		f := c.Fabric()
		if !bytes.Equal(f.RootPublicKey, pub) {
			t.Errorf("run %d: root public key %X, want the signer's %X", run, f.RootPublicKey, pub)
		}
		if run == 0 {
			fabricID = f.FabricID
		} else if f.FabricID != fabricID {
			t.Errorf("run %d: fabric %x, want %x", run, f.FabricID, fabricID)
		}
		if c.Peers() == nil {
			t.Errorf("run %d: Peers() is nil", run)
		}
		if err := c.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
	stored, ok, err := ca.SettingStore{Settings: store.New(db)}.Load()
	if err != nil || !ok {
		t.Fatalf("stored CA: %v, %v", ok, err)
	}
	if stored.RootKeyPair != nil && len(stored.RootKeyPair.PrivateKey) > 0 {
		t.Error("the database holds the root private key")
	}
}

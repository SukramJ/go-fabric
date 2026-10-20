// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/store"
)

// TestCaseIdentitiesResolveEveryIPKOfKeySet0: a Sigma1 is resolved under
// every key of key set 0 and answered with the key that matched (matter.js
// Fabric.destinationIdsFor, Fabric.ts:399-403; chip CASESession.cpp:973-990);
// the identity an initiator uses keys on the second-newest of three
// (Fabric.currentDestinationIdFor, KeySets.ts:113-121); a KeySetWrite of key
// set 0 rotates both through the group manager's reload.
func TestCaseIdentitiesResolveEveryIPKOfKeySet0(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := testStore(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub, err := priv.PublicKey.Bytes() // the uncompressed point the store keeps
	if err != nil {
		t.Fatal(err)
	}
	rawPriv, err := priv.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	compressed := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	idx, err := st.AddFabric(ctx, store.FabricRecord{FabricIndex: 1, FabricID: 0xFAB, NodeID: 0x1234, RootPublicKey: rootPub, CompressedID: compressed})
	if err != nil {
		t.Fatal(err)
	}
	rawIPK := make([]byte, 16)
	rawIPK[0] = 0xA0
	if err := st.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: idx, NOC: []byte{0x15, 0x18}, PrivateKey: rawPriv, IPK: rawIPK}); err != nil {
		t.Fatal(err)
	}
	epoch := func(b byte) []byte { k := make([]byte, 16); k[15] = b; return k }
	// Key set 0 with three epoch keys: the AddNOC key, a newer and the newest.
	if err := st.UpsertGroupKeySet(ctx, store.GroupKeySet{
		FabricIndex: idx, GroupKeySetID: 0, SecurityPolicy: store.SecurityPolicyTrustFirst,
		EpochKey0: rawIPK, EpochStart0: 0, EpochKey1: epoch(1), EpochStart1: 1_000, EpochKey2: epoch(2), EpochStart2: 2_000,
	}); err != nil {
		t.Fatal(err)
	}
	gm, err := groups.NewManager(st, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	c := newCaseIdentities(slog.Default(), gm)
	gm.OnGroupcastChanged(c.refreshIPK)
	if err := c.load(ctx, st, idx); err != nil {
		t.Fatalf("load: %v", err)
	}
	op := func(ek []byte) [16]byte {
		k, err := sigma.DeriveOperationalIPK(ek, compressed)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	var random [sigma.RandomSize]byte
	random[0] = 7
	resolve := func(ipk [16]byte) (*sigma.Identity, bool) {
		dest := sigma.ComputeDestinationID(ipk, random, rootPub, 0xFAB, 0x1234)
		id, _, ok := c.ResolveSigma1Destination(dest, random)
		return id, ok
	}
	for i, ek := range [][]byte{rawIPK, epoch(1), epoch(2)} {
		id, ok := resolve(op(ek))
		if !ok || id.IPK != op(ek) {
			t.Fatalf("key %d of key set 0: resolved %v, identity keyed on the matched key %v", i, ok, ok && id.IPK == op(ek))
		}
	}
	if _, ok := resolve(op(epoch(9))); ok {
		t.Fatal("a key outside key set 0 resolved")
	}
	if f, ok := c.forFabric(idx); !ok || f.identity.IPK != op(epoch(1)) {
		t.Fatalf("initiator key: want the second-newest of three (slot 1)")
	}

	// KeySetWrite of key set 0 with one new key: the reload rotates the
	// identity; the AddNOC key stays a candidate, the dropped one does not.
	if err := st.UpsertGroupKeySet(ctx, store.GroupKeySet{
		FabricIndex: idx, GroupKeySetID: 0, SecurityPolicy: store.SecurityPolicyTrustFirst, EpochKey0: epoch(5), EpochStart0: 5_000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := gm.Reload(ctx, idx); err != nil {
		t.Fatal(err)
	}
	if id, ok := resolve(op(epoch(5))); !ok || id.IPK != op(epoch(5)) {
		t.Fatal("the rotated key does not resolve")
	}
	if _, ok := resolve(op(epoch(1))); ok {
		t.Fatal("a key the KeySetWrite dropped still resolves")
	}
	if id, ok := resolve(op(rawIPK)); !ok || id.IPK != op(rawIPK) {
		t.Fatal("the AddNOC key no longer resolves")
	}
	if f, ok := c.forFabric(idx); !ok || f.identity.IPK != op(epoch(5)) {
		t.Fatal("initiator key: want the only key of the rotated key set 0")
	}
}

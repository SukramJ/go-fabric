// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/groups"
	"thingfabric.dev/go-fabric/secure/sigma"
	"thingfabric.dev/go-fabric/store"
)

// refreshParkedOnLock reports whether a goroutine waits on a sync.Mutex
// inside refreshIPK (its refreshMu), from the runtime's stack dump.
func refreshParkedOnLock() bool {
	buf := make([]byte, 1<<20)
	for _, g := range strings.Split(string(buf[:runtime.Stack(buf, true)]), "\n\n") {
		header, _, _ := strings.Cut(g, "\n")
		if strings.Contains(header, "[sync.Mutex.Lock") && strings.Contains(g, "node.(*caseIdentities).refreshIPK(") {
			return true
		}
	}
	return false
}

// TestRefreshIPKOverlapInstallsTheNewerKeySet (review II, R3-conc-3750):
// two refreshes of one fabric's key set 0 overlap — the first read the key
// set before a KeySetWrite, the second after it. Whichever installs first,
// the identity ends on the newer key set; the older read never shadows it.
func TestRefreshIPKOverlapInstallsTheNewerKeySet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := testStore(t)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPub, err := priv.PublicKey.Bytes()
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
	keySet := func(ek []byte) {
		t.Helper()
		if err := st.UpsertGroupKeySet(ctx, store.GroupKeySet{
			FabricIndex: idx, GroupKeySetID: 0, SecurityPolicy: store.SecurityPolicyTrustFirst, EpochKey0: ek, EpochStart0: 1_000,
		}); err != nil {
			t.Fatal(err)
		}
	}
	keySet(epoch(1))
	gm, err := groups.NewManager(st, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	c := newCaseIdentities(slog.Default(), gm)
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

	// The seam parks each refresh between its read of the key set and its
	// install: the first until the test lets it go, the second until the
	// first installed.
	var calls atomic.Int32
	firstRead, secondRead := make(chan struct{}, 1), make(chan struct{}, 1)
	releaseFirst, firstDone := make(chan struct{}), make(chan struct{})
	c.testAfterCandidates = func() {
		switch calls.Add(1) {
		case 1:
			firstRead <- struct{}{}
			<-releaseFirst
		case 2:
			secondRead <- struct{}{}
			<-firstDone
		}
	}
	// A first KeySetWrite lands; its refresh reads that set and parks.
	keySet(epoch(3))
	if err := gm.Reload(ctx, idx); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		c.refreshIPK(ctx, idx)
		close(firstDone)
	})
	<-firstRead
	// A second KeySetWrite lands; its refresh reads the newer set.
	keySet(epoch(2))
	if err := gm.Reload(ctx, idx); err != nil {
		t.Fatal(err)
	}
	wg.Go(func() { c.refreshIPK(ctx, idx) })
	// Without serialisation the second refresh reaches its seam now; with
	// it, it waits for the first on refreshMu. Either way the first
	// installs next. No wall-clock wait: the second is at its seam or
	// parked on the lock, read from the runtime's stack dump.
	for waiting := true; waiting; {
		select {
		case <-secondRead:
			waiting = false
		default:
			if waiting = !refreshParkedOnLock(); waiting {
				runtime.Gosched()
			}
		}
	}
	close(releaseFirst)
	wg.Wait()
	if f, ok := c.forFabric(idx); !ok || f.identity.IPK != op(epoch(2)) {
		t.Fatalf("after two overlapping refreshes the identity keys on %x, want the newer key set's %x", f.identity.IPK, op(epoch(2)))
	}
}

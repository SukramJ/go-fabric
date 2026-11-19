// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync/atomic"
	"testing"
)

// fillChunkWaitClaims takes n claims for fabric on sessions numbered from
// base, maxChunkWaitsPerSession per session.
func fillChunkWaitClaims(t *testing.T, c *chunkWaitClaims, base uint16, fabric uint8, n int) {
	t.Helper()
	for i := range n {
		if !c.claim(base+uint16(i/maxChunkWaitsPerSession), fabric, 0) {
			t.Fatalf("fabric %d: claim %d refused while filling", fabric, i)
		}
	}
}

// TestChunkWaitShareIsStrict (review II, R2-P7-req-05, decision 275): at
// the threshold a fabric holds at most its share — share-1 claims held,
// the share-th is admitted, the next is Busy.
func TestChunkWaitShareIsStrict(t *testing.T) {
	t.Parallel()
	const share = chunkWaitFabricThreshold / 2
	var c chunkWaitClaims
	fillChunkWaitClaims(t, &c, 0x100, 2, chunkWaitFabricThreshold)
	fillChunkWaitClaims(t, &c, 0x1000, 1, share-1)
	if !c.claim(0x2000, 1, share) {
		t.Fatalf("the fabric's %d-th claim, its share, was refused", share)
	}
	if c.claim(0x2001, 1, share) {
		t.Fatalf("a fabric holding its share of %d took one more", share)
	}
}

// TestChunkWaitShareExcludesFabricZero (review II, R2-P7-req-05, decision
// 275): claims of sessions without a fabric (fabric 0: PASE, unresolved)
// are held to no share at the threshold, only to the per-session bound.
func TestChunkWaitShareExcludesFabricZero(t *testing.T) {
	t.Parallel()
	const share = chunkWaitFabricThreshold / 2
	var c chunkWaitClaims
	fillChunkWaitClaims(t, &c, 0x100, 0, chunkWaitFabricThreshold)
	for i := range share + 1 {
		if !c.claim(0x2000+uint16(i/maxChunkWaitsPerSession), 0, share) {
			t.Fatalf("fabric 0's claim %d beyond the threshold was refused by a share", i)
		}
	}
	const full = 0x3000
	for range maxChunkWaitsPerSession {
		if !c.claim(full, 0, share) {
			t.Fatal("a fresh session without a fabric was refused")
		}
	}
	if c.claim(full, 0, share) {
		t.Fatal("a session without a fabric took more than the per-session bound")
	}
}

// TestClaimChunkWaitAsksNoCountForFabricZero (review II, R2-P7-req-05,
// decision 275): at the threshold a session without a fabric does not
// ask the host's fabric count; one with a fabric does.
func TestClaimChunkWaitAsksNoCountForFabricZero(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, manyTempSensorsSnapshotterForTest())
	var asked atomic.Int32
	b.SetFabricCounter(func(context.Context) (int, error) { asked.Add(1); return 2, nil })
	lookup := fabricPeersLookup{fabric: map[uint16]uint8{0x10: 1}}
	b.AttachSessionLookup(lookup)
	fillChunkWaitClaims(t, &b.routing.chunkWaits, 0x100, 2, chunkWaitFabricThreshold)
	release, ok := b.claimChunkWait(context.Background(), 0x20, true) // unresolved: fabric 0
	if !ok {
		t.Fatal("a session without a fabric was refused at the threshold")
	}
	release()
	if n := asked.Load(); n != 0 {
		t.Fatalf("a session without a fabric asked the host's fabric count %d times, want 0", n)
	}
	release, ok = b.claimChunkWait(context.Background(), 0x10, true)
	if !ok {
		t.Fatal("fabric 1, under its share, was refused")
	}
	release()
	if n := asked.Load(); n != 1 {
		t.Fatalf("a session of fabric 1 asked the fabric count %d times, want 1", n)
	}
}

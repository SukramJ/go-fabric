// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"log/slog"
	"testing"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// TestAccessControlFilteredReadSettlesOnceW2B4 (#621, P8-PR1-opus-L2): a
// filtered read of a non-list attribute while a synchronisation is owed and
// the source keeps failing queries the source once, not once in
// MatterReadFiltered and again in MatterRead.
func TestAccessControlFilteredReadSettlesOnceW2B4(t *testing.T) {
	t.Parallel()
	a, src, _ := newAuxACLW2B4(t, &dropACLStore{acl: map[uint8][]store.ACLEntry{}})
	a.mu.Lock()
	a.auxOwed = true
	a.mu.Unlock()
	src.failing.Store(true)
	rctx := im.WithFabricFilter(context.Background(), true, 1)
	for _, attr := range []uint32{
		cluster.AttrGlobalClusterRevision,
		cluster.AttrGlobalFeatureMap,
		accessControlAttrSubjectsPerAccessControl,
	} {
		src.reads.Store(0)
		if _, ok := a.MatterReadFiltered(rctx, attr); !ok {
			t.Fatalf("attribute %#x: read failed", attr)
		}
		if n := src.reads.Load(); n != 1 {
			t.Errorf("attribute %#x: one filtered read queried the source %d times, want 1", attr, n)
		}
	}
}

// TestAccessControlFailingSourceWarnsOncePerDebtW2B4 (#621): while the
// source keeps failing, the read that finds the debt failing again logs at
// Debug; only a debt that starts failing logs a Warn. A source that
// recovers settles the debt, and the next failure is a new debt: one Warn
// again.
func TestAccessControlFailingSourceWarnsOncePerDebtW2B4(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a, src, h := newAuxACLW2B4(t, &dropACLStore{acl: map[uint8][]store.ACLEntry{}})
	rctx := im.WithFabricFilter(ctx, true, 1)

	src.failing.Store(true)
	a.syncAuxiliary(ctx, true, nil) // a change whose synchronisation fails
	for range 5 {
		for _, attr := range []uint32{accessControlAttrACL, cluster.AttrGlobalClusterRevision, accessControlAttrAuxiliaryACL} {
			a.MatterReadFiltered(rctx, attr)
		}
	}
	if n := h.count(slog.LevelWarn); n != 1 {
		t.Errorf("one failing debt logged %d Warns, want 1", n)
	}
	if n := h.count(slog.LevelDebug); n == 0 {
		t.Error("the repeated failures of the owed debt logged nothing at Debug")
	}

	src.failing.Store(false)
	a.MatterReadFiltered(rctx, cluster.AttrGlobalClusterRevision) // settles
	a.mu.RLock()
	owed := a.auxOwed
	a.mu.RUnlock()
	if owed {
		t.Fatal("the recovered source left the debt owed")
	}
	src.failing.Store(true)
	a.syncAuxiliary(ctx, true, nil)
	a.MatterReadFiltered(rctx, cluster.AttrGlobalClusterRevision)
	if n := h.count(slog.LevelWarn); n != 2 {
		t.Errorf("a new debt after recovery: %d Warns in all, want 2", n)
	}
}

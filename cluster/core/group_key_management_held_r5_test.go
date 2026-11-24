// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// gkmStoreNoLister is a memstore seen only through GroupStoreFacade: a
// store that cannot enumerate its fabrics, so construction marks nothing.
type gkmStoreNoLister struct{ GroupStoreFacade }

// TestGroupKeyManagementHeldMarkFromReadAndWrite (R5-test-5001): the
// GroupKeyMap mark a fabric's removal is reported by is set and cleared by
// a controller's read and write and by the Groupcast binding, not only at
// construction — a fabric without rows at construction that writes
// GroupKeyMap, or is bound by setGroupKeyMapping, and is removed is
// reported; the same fabric after it wrote an empty list is not, and is
// again once it writes rows; a fabric seen only by a read (a store that
// cannot list fabrics) is reported.
func TestGroupKeyManagementHeldMarkFromReadAndWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const (
		f        uint8  = 2
		keySet   uint16 = 0x42
		groupID  uint16 = 0x0101
		attrGKM         = groupKeyMgmtAttrGroupKeyMap
		otherFab uint8  = 1
	)
	rows := []GroupKeyMapStruct{{GroupID: groupID, GroupKeySetID: keySet, FabricIndex: f}}
	wctx := im.WithFabricFilter(ctx, true, f)
	newStore := func(t *testing.T) *memstore.Store {
		t.Helper()
		st := memstore.New()
		for _, fi := range []uint8{otherFab, f} {
			if _, err := st.AddFabric(ctx, store.FabricRecord{FabricIndex: fi, FabricID: uint64(0x0FA0 + int(fi)), CompressedID: [8]byte{fi, 2, 3, 4, 5, 6, 7, 8}}); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: f, GroupKeySetID: keySet, EpochKey0: make([]byte, 16), EpochStart0: 1}); err != nil {
			t.Fatal(err)
		}
		return st
	}
	newServer := func(t *testing.T, s GroupStoreFacade) (*GroupKeyManagement, *heardAttrs) {
		t.Helper()
		g, err := NewGroupKeyManagement(s, GroupKeyMgmtConfig{MaxGroupsPerFabric: 4, MaxGroupKeysPerFabric: 4})
		if err != nil {
			t.Fatal(err)
		}
		return g, threadChangeListener(t, g)
	}
	dropReports := func(t *testing.T, g *GroupKeyManagement, heard *heardAttrs, want []uint32) {
		t.Helper()
		heard.take()
		v0 := g.MatterDataVersion()
		if err := g.MatterDropFabric(ctx, f); err != nil {
			t.Fatal(err)
		}
		if got := heard.take(); !slices.Equal(got, want) {
			t.Errorf("the drop reported %v, want %v", got, want)
		}
		if moved := g.MatterDataVersion() != v0; moved != (len(want) > 0) {
			t.Errorf("the drop moved the DataVersion %t, want %t", moved, len(want) > 0)
		}
	}
	remove := func(t *testing.T, st *memstore.Store) {
		t.Helper()
		if err := st.RemoveFabric(ctx, f); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("written", func(t *testing.T) {
		t.Parallel()
		st := newStore(t)
		g, heard := newServer(t, st)
		if err := g.MatterWrite(wctx, attrGKM, rows); err != nil {
			t.Fatal(err)
		}
		remove(t, st)
		dropReports(t, g, heard, []uint32{attrGKM})
	})

	t.Run("emptied, then written again", func(t *testing.T) {
		t.Parallel()
		st := newStore(t)
		g, heard := newServer(t, st)
		if err := g.MatterWrite(wctx, attrGKM, rows); err != nil {
			t.Fatal(err)
		}
		if err := g.MatterWrite(wctx, attrGKM, []GroupKeyMapStruct{}); err != nil {
			t.Fatal(err)
		}
		// The empty list cleared the mark: nothing shrinks at a drop.
		dropReports(t, g, heard, nil)
		if err := g.MatterWrite(wctx, attrGKM, rows); err != nil {
			t.Fatal(err)
		}
		remove(t, st)
		dropReports(t, g, heard, []uint32{attrGKM})
	})

	t.Run("bound by setGroupKeyMapping", func(t *testing.T) {
		t.Parallel()
		st := newStore(t)
		g, heard := newServer(t, st)
		if err := g.setGroupKeyMapping(ctx, f, groupID, keySet); err != nil {
			t.Fatal(err)
		}
		remove(t, st)
		dropReports(t, g, heard, []uint32{attrGKM})
	})

	t.Run("read only", func(t *testing.T) {
		t.Parallel()
		st := newStore(t)
		if err := st.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: groupID, GroupKeySetID: keySet}); err != nil {
			t.Fatal(err)
		}
		g, heard := newServer(t, gkmStoreNoLister{st})
		if _, ok := g.MatterReadFiltered(wctx, attrGKM); !ok {
			t.Fatal("the GroupKeyMap read failed")
		}
		remove(t, st)
		dropReports(t, g, heard, []uint32{attrGKM})
	})

	t.Run("read only, unread", func(t *testing.T) {
		t.Parallel()
		// The control: without the read nothing marked the fabric.
		st := newStore(t)
		if err := st.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: groupID, GroupKeySetID: keySet}); err != nil {
			t.Fatal(err)
		}
		g, heard := newServer(t, gkmStoreNoLister{st})
		remove(t, st)
		dropReports(t, g, heard, nil)
	})
}

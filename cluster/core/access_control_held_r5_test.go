// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// aclStoreNoLister is a dropACLStore seen without ListFabrics: a store
// that cannot enumerate its fabrics, so construction marks nothing.
type aclStoreNoLister struct{ s *dropACLStore }

func (n aclStoreNoLister) ListACL(ctx context.Context, f uint8) ([]store.ACLEntry, error) {
	return n.s.ListACL(ctx, f)
}

func (n aclStoreNoLister) ReplaceACL(ctx context.Context, f uint8, e []store.ACLEntry) error {
	return n.s.ReplaceACL(ctx, f, e)
}

// TestAccessControlHeldMarkFromReadAndWrite (R5-test-5001): the Acl mark a
// fabric's removal is reported by is set and cleared by a controller's
// read and write too, not only at construction — a fabric without rows at
// construction that writes Acl and is removed is reported; the same fabric
// after it wrote an empty list is not, and is again once it writes rows;
// a fabric seen only by a read (a store that cannot list fabrics) is
// reported.
func TestAccessControlHeldMarkFromReadAndWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const f uint8 = 3
	rows := []AccessControlEntryStruct{{Privilege: 5, AuthMode: 2, Subjects: []uint64{0x1003}, FabricIndex: f}}
	wctx := im.WithFabricFilter(ctx, true, f)
	setup := func(t *testing.T) (*AccessControl, *dropACLStore, *heardAttrs) {
		t.Helper()
		// Fabric 3 is listed but holds no rows at construction: unmarked.
		st := &dropACLStore{acl: map[uint8][]store.ACLEntry{1: {dropAdmin(1)}}, fabrics: []uint8{1, f}}
		a, err := NewAccessControl(st)
		if err != nil {
			t.Fatal(err)
		}
		return a, st, threadChangeListener(t, a)
	}
	dropReports := func(t *testing.T, a *AccessControl, heard *heardAttrs, want []uint32) {
		t.Helper()
		heard.take()
		v0 := a.MatterDataVersion()
		if err := a.MatterDropFabric(ctx, f); err != nil {
			t.Fatal(err)
		}
		got := heard.take()
		if !slices.Equal(got, want) {
			t.Errorf("the drop reported %v, want %v", got, want)
		}
		if moved := a.MatterDataVersion() != v0; moved != (len(want) > 0) {
			t.Errorf("the drop moved the DataVersion %t, want %t", moved, len(want) > 0)
		}
	}

	t.Run("written", func(t *testing.T) {
		t.Parallel()
		a, st, heard := setup(t)
		if err := a.MatterWrite(wctx, accessControlAttrACL, rows); err != nil {
			t.Fatal(err)
		}
		st.removeFabric(f)
		dropReports(t, a, heard, []uint32{accessControlAttrACL})
	})

	t.Run("emptied, then written again", func(t *testing.T) {
		t.Parallel()
		a, st, heard := setup(t)
		if err := a.MatterWrite(wctx, accessControlAttrACL, rows); err != nil {
			t.Fatal(err)
		}
		if err := a.MatterWrite(wctx, accessControlAttrACL, []AccessControlEntryStruct{}); err != nil {
			t.Fatal(err)
		}
		// The empty list cleared the mark: nothing shrinks at a drop.
		dropReports(t, a, heard, nil)
		if err := a.MatterWrite(wctx, accessControlAttrACL, rows); err != nil {
			t.Fatal(err)
		}
		st.removeFabric(f)
		dropReports(t, a, heard, []uint32{accessControlAttrACL})
	})

	t.Run("read only", func(t *testing.T) {
		t.Parallel()
		inner := &dropACLStore{acl: map[uint8][]store.ACLEntry{f: {dropAdmin(f)}}}
		a, err := NewAccessControl(aclStoreNoLister{inner})
		if err != nil {
			t.Fatal(err)
		}
		heard := threadChangeListener(t, a)
		if _, ok := a.MatterReadFiltered(wctx, accessControlAttrACL); !ok {
			t.Fatal("the Acl read failed")
		}
		inner.removeFabric(f)
		dropReports(t, a, heard, []uint32{accessControlAttrACL})
	})

	t.Run("read only, unread", func(t *testing.T) {
		t.Parallel()
		// The control: without the read nothing marked the fabric.
		inner := &dropACLStore{acl: map[uint8][]store.ACLEntry{f: {dropAdmin(f)}}}
		a, err := NewAccessControl(aclStoreNoLister{inner})
		if err != nil {
			t.Fatal(err)
		}
		heard := threadChangeListener(t, a)
		inner.removeFabric(f)
		dropReports(t, a, heard, nil)
	})
}

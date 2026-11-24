// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/store"
)

// dropACLStore holds ACL rows per fabric and a fabric table; removeFabric
// drops a fabric's row and its ACL rows, as the store's fabric cascade
// does (the fake states the cascade, it does not measure it).
type dropACLStore struct {
	acl     map[uint8][]store.ACLEntry
	fabrics []uint8
}

func (s *dropACLStore) ListACL(_ context.Context, f uint8) ([]store.ACLEntry, error) {
	return slices.Clone(s.acl[f]), nil
}

func (s *dropACLStore) ReplaceACL(_ context.Context, f uint8, e []store.ACLEntry) error {
	s.acl[f] = slices.Clone(e)
	return nil
}

func (s *dropACLStore) ListFabrics(context.Context) ([]store.FabricRecord, error) {
	out := make([]store.FabricRecord, 0, len(s.fabrics))
	for _, f := range s.fabrics {
		out = append(out, store.FabricRecord{FabricIndex: f})
	}
	return out, nil
}

func (s *dropACLStore) removeFabric(f uint8) {
	delete(s.acl, f)
	s.fabrics = slices.DeleteFunc(s.fabrics, func(x uint8) bool { return x == f })
}

func dropAdmin(f uint8) store.ACLEntry {
	return store.ACLEntry{FabricIndex: f, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{0x1000 + uint64(f)}}
}

// TestAccessControlFabricDropReportsWhatShrank (R4-par-4051): a fabric's
// removal that takes its Acl rows (the store's cascade) or its Extension
// entry out of the attributes advances the DataVersion once and reports
// the attributes that shrank; a drop that removes nothing — a second drop,
// the drop at a new fabric's index whose rows the store holds, an index
// never held — moves and reports nothing.
func TestAccessControlFabricDropReportsWhatShrank(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	setup := func(t *testing.T) (*AccessControl, *dropACLStore, *heardAttrs) {
		t.Helper()
		st := &dropACLStore{acl: map[uint8][]store.ACLEntry{1: {dropAdmin(1)}, 2: {dropAdmin(2)}}, fabrics: []uint8{1, 2}}
		a, err := NewAccessControl(st)
		if err != nil {
			t.Fatal(err)
		}
		return a, st, threadChangeListener(t, a)
	}

	t.Run("Acl rows only", func(t *testing.T) {
		t.Parallel()
		a, st, heard := setup(t)
		v0 := a.MatterDataVersion()
		st.removeFabric(2)
		if err := a.MatterDropFabric(ctx, 2); err != nil {
			t.Fatal(err)
		}
		if got := a.MatterDataVersion(); got != v0+1 {
			t.Errorf("DataVersion %d → %d, want one step", v0, got)
		}
		if got := heard.take(); !slices.Equal(got, []uint32{accessControlAttrACL}) {
			t.Errorf("the removal reported %v, want [Acl]", got)
		}
		v1 := a.MatterDataVersion()
		if err := a.MatterDropFabric(ctx, 2); err != nil {
			t.Fatal(err)
		}
		if a.MatterDataVersion() != v1 || len(heard.take()) != 0 {
			t.Error("a second drop of the removed fabric moved the version or reported")
		}
	})

	t.Run("Acl rows and Extension", func(t *testing.T) {
		t.Parallel()
		a, st, heard := setup(t)
		a.mu.Lock()
		a.extensions = map[uint8][]AccessControlExtensionEntry{2: {{Data: []byte{0x15, 0x18}, FabricIndex: 2}}}
		a.mu.Unlock()
		v0 := a.MatterDataVersion()
		st.removeFabric(2)
		if err := a.MatterDropFabric(ctx, 2); err != nil {
			t.Fatal(err)
		}
		if got := a.MatterDataVersion(); got != v0+1 {
			t.Errorf("DataVersion %d → %d, want one step", v0, got)
		}
		got := heard.take()
		slices.Sort(got)
		if !slices.Equal(got, []uint32{accessControlAttrACL, accessControlAttrExtension}) {
			t.Errorf("the removal reported %v, want [Acl Extension]", got)
		}
	})

	t.Run("drop at a held fabric's index", func(t *testing.T) {
		t.Parallel()
		a, _, heard := setup(t)
		v0 := a.MatterDataVersion()
		// The node drops a new fabric's index before it is announced; the
		// store holds the fabric's default admin entry.
		if err := a.MatterDropFabric(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if a.MatterDataVersion() != v0 || len(heard.take()) != 0 {
			t.Error("a drop that removed no entry moved the version or reported")
		}
	})

	t.Run("new fabric at a reused index", func(t *testing.T) {
		t.Parallel()
		a, st, heard := setup(t)
		st.removeFabric(2)
		if err := a.MatterDropFabric(ctx, 2); err != nil {
			t.Fatal(err)
		}
		heard.take()
		// A new fabric at index 2: its admin entry, then the node's drop.
		st.fabrics = append(st.fabrics, 2)
		st.acl[2] = []store.ACLEntry{dropAdmin(2)}
		a.NotifyAdminEntryInstalled(dropAdmin(2))
		if err := a.MatterDropFabric(ctx, 2); err != nil {
			t.Fatal(err)
		}
		heard.take()
		// Its removal is reported in turn.
		v0 := a.MatterDataVersion()
		st.removeFabric(2)
		if err := a.MatterDropFabric(ctx, 2); err != nil {
			t.Fatal(err)
		}
		if got := heard.take(); a.MatterDataVersion() != v0+1 || !slices.Equal(got, []uint32{accessControlAttrACL}) {
			t.Errorf("the reused index's removal: version moved %t, reported %v; want one step and [Acl]", a.MatterDataVersion() != v0, got)
		}
	})

	t.Run("index never held", func(t *testing.T) {
		t.Parallel()
		a, _, heard := setup(t)
		v0 := a.MatterDataVersion()
		if err := a.MatterDropFabric(ctx, 7); err != nil {
			t.Fatal(err)
		}
		if a.MatterDataVersion() != v0 || len(heard.take()) != 0 {
			t.Error("a drop of an index never held moved the version or reported")
		}
	})
}

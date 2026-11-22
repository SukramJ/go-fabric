// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	mstore "thingfabric.dev/go-fabric/store"
)

// An Acl or Extension write whose fabric was removed while the write was
// on its way is refused and stores nothing (review II, R2-P6-store-1010,
// N4-notesE-1).
func TestACLWriteOfARemovedFabricStoresNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := newFakeStore()
	if _, err := fs.AddFabric(ctx, mstore.FabricRecord{FabricID: 1}); err != nil {
		t.Fatal(err)
	}
	ac, err := core.NewAccessControl(fs)
	if err != nil {
		t.Fatal(err)
	}
	gone := im.WithFabricFilter(ctx, true, 2) // a fabric the store does not hold
	ext := []core.AccessControlExtensionEntry{{Data: encodeExtensionTLV(t), FabricIndex: 2}}
	if err := ac.MatterWrite(gone, 0x0001, ext); statusOf(err) != im.StatusUnsupportedAccess {
		t.Errorf("Extension write of a removed fabric: %v, want UnsupportedAccess", err)
	}
	if v, ok, _ := fs.GetSetting(ctx, "access_control.extension.2"); ok && v != "" {
		t.Errorf("Extension of the removed fabric stored: %q", v)
	}
	acl := []core.AccessControlEntryStruct{{Privilege: 5, AuthMode: 2, Subjects: []uint64{112233}, FabricIndex: 2}}
	if err := ac.MatterWrite(gone, 0x0000, acl); statusOf(err) != im.StatusUnsupportedAccess {
		t.Errorf("Acl write of a removed fabric: %v, want UnsupportedAccess", err)
	}
	if list, _ := fs.ListACL(ctx, 2); len(list) != 0 {
		t.Errorf("Acl of the removed fabric stored: %d entries", len(list))
	}
	if err := ac.MatterWrite(im.WithFabricFilter(ctx, true, 1), 0x0001, []core.AccessControlExtensionEntry{{Data: encodeExtensionTLV(t), FabricIndex: 1}}); err != nil {
		t.Errorf("Extension write of a held fabric: %v", err)
	}
}

// extLoadFailStore fails the read of fabric 1's stored Extension while
// fail is set.
type extLoadFailStore struct {
	*fakeStore
	fail atomic.Bool
}

func (s *extLoadFailStore) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	if s.fail.Load() && strings.HasSuffix(key, ".extension.1") {
		return "", false, errors.New("injected settings read failure")
	}
	return s.fakeStore.GetSetting(ctx, key)
}

// A fabric whose stored Extension could not be read refuses its Extension
// write while the value still cannot be read, so the stored value is never
// overwritten unseen; the write reads it again first, so a store that
// recovered takes it; another fabric writes as before (review II,
// R2-P6-err-1016).
func TestACLExtensionWriteRefusedWhileItsLoadFailed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := newFakeStore()
	for _, id := range []uint64{1, 2} {
		if _, err := fs.AddFabric(ctx, mstore.FabricRecord{FabricID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.SetSetting(ctx, "access_control.extension.1", "1718"); err != nil {
		t.Fatal(err)
	}
	st := &extLoadFailStore{fakeStore: fs}
	st.fail.Store(true)
	ac, err := core.NewAccessControl(st)
	if err != nil {
		t.Fatal(err)
	}
	ext := func(f uint8) []core.AccessControlExtensionEntry {
		return []core.AccessControlExtensionEntry{{Data: encodeExtensionTLV(t), FabricIndex: f}}
	}
	if err := ac.MatterWrite(im.WithFabricFilter(ctx, true, 1), 0x0001, ext(1)); statusOf(err) != im.StatusFailure {
		t.Errorf("Extension write over an unread stored value: %v, want FAILURE", err)
	}
	if v, _, _ := fs.GetSetting(ctx, "access_control.extension.1"); v != "1718" {
		t.Errorf("stored Extension of fabric 1 = %q, want the unread 1718 kept", v)
	}
	if err := ac.MatterWrite(im.WithFabricFilter(ctx, true, 2), 0x0001, ext(2)); err != nil {
		t.Errorf("Extension write of fabric 2: %v", err)
	}
	// The store recovers: the next write reads the stored value again and
	// is taken, without a restart.
	st.fail.Store(false)
	if err := ac.MatterWrite(im.WithFabricFilter(ctx, true, 1), 0x0001, ext(1)); err != nil {
		t.Errorf("Extension write of fabric 1 once its value reads again: %v", err)
	}
	v, _ := ac.MatterReadFiltered(im.WithFabricFilter(ctx, true, 1), 0x0001)
	if got, _ := v.([]core.AccessControlExtensionEntry); len(got) != 1 {
		t.Errorf("fabric 1's Extension after the recovery = %+v, want the stored entry", v)
	}
}

// With a store that cannot list fabrics, an unfiltered read of Extension
// still covers every fabric the server holds an entry of (review II,
// N4-w3core-1).
func TestACLUnfilteredExtensionReadWithoutAFabricListing(t *testing.T) {
	t.Parallel()
	ac, err := core.NewAccessControl(&fakeACLStore{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, f := range []uint8{1, 2} {
		if err := ac.MatterWrite(im.WithFabricFilter(ctx, true, f), 0x0001, []core.AccessControlExtensionEntry{{Data: encodeExtensionTLV(t), FabricIndex: f}}); err != nil {
			t.Fatal(err)
		}
	}
	v, ok := ac.MatterReadFiltered(im.WithFabricFilter(ctx, false, 1), 0x0001)
	got, _ := v.([]core.AccessControlExtensionEntry)
	if !ok || len(got) != 2 {
		t.Errorf("unfiltered Extension read from fabric 1 = %+v, want both fabrics' entries", v)
	}
}

// A fabric whose row is still pending — after AddNOC, before
// CommissioningComplete — is held: the commissioner's Acl and Extension
// writes inside the fail-safe are stored (review II, R2-P6-store-1010).
func TestACLWriteOfAPendingFabricIsStored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := newFakeStore()
	idx, err := fs.AddFabric(ctx, mstore.FabricRecord{FabricID: 1, Pending: mstore.FabricPendingAdd})
	if err != nil {
		t.Fatal(err)
	}
	ac, err := core.NewAccessControl(fs)
	if err != nil {
		t.Fatal(err)
	}
	pending := im.WithFabricFilter(ctx, true, idx)
	acl := []core.AccessControlEntryStruct{{Privilege: 5, AuthMode: 2, Subjects: []uint64{112233}, FabricIndex: idx}}
	if err := ac.MatterWrite(pending, 0x0000, acl); err != nil {
		t.Fatalf("Acl write of a pending fabric: %v", err)
	}
	if list, _ := fs.ListACL(ctx, idx); len(list) != 1 {
		t.Errorf("Acl of the pending fabric: %d entries, want 1", len(list))
	}
	if err := ac.MatterWrite(pending, 0x0001, []core.AccessControlExtensionEntry{{Data: encodeExtensionTLV(t), FabricIndex: idx}}); err != nil {
		t.Fatalf("Extension write of a pending fabric: %v", err)
	}
	if v, ok, _ := fs.GetSetting(ctx, "access_control.extension.1"); !ok || v == "" {
		t.Error("Extension of the pending fabric not stored")
	}
}

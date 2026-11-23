// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
	mstore "thingfabric.dev/go-fabric/store"
)

// heldExtensionStore is a p7ExtStore holding fabric 2's Extension, read at
// construction, and an AccessControl over it.
func heldExtensionStore(t *testing.T) (*p7ExtStore, *core.AccessControl) {
	t.Helper()
	key := "access_control.extension.2"
	st := &p7ExtStore{
		settingsACLStore: settingsACLStore{settings: map[string]string{key: hex.EncodeToString(encodeExtensionTLV(t))}},
		fabrics:          []mstore.FabricRecord{{FabricIndex: 2}},
	}
	ac, err := core.NewAccessControl(st)
	if err != nil {
		t.Fatal(err)
	}
	if held := ac.MatterHeldFabrics(); len(held) != 1 || held[0] != 2 {
		t.Fatalf("held %v, want [2]", held)
	}
	return st, ac
}

// failedDrop drops fabric 2 with the store refusing the clear once.
func failedDrop(t *testing.T, st *p7ExtStore, ac *core.AccessControl) {
	t.Helper()
	st.setErr = errors.New("disk")
	if err := ac.MatterDropFabric(context.Background(), 2); err == nil {
		t.Fatal("MatterDropFabric(2) reported no error for a failed clear")
	}
	st.setErr = nil
}

// TestDropFabricOwesAFailedClearOfAHeldExtension (R2-C2-store-1004): a
// drop of a fabric whose Extension memory held, whose clear of the stored
// setting fails, keeps the owed clear — the next drop, a new fabric's
// default admin entry at the index, or that fabric's Extension write
// clears it — so a fabric commissioned onto the reused index does not
// load the removed fabric's Extension as its own at the next start.
func TestDropFabricOwesAFailedClearOfAHeldExtension(t *testing.T) {
	t.Parallel()
	const key = "access_control.extension.2"
	afterRestart := func(t *testing.T, st *p7ExtStore) {
		t.Helper()
		if v := st.settings[key]; v != "" {
			t.Errorf("the setting still holds %q", v)
		}
		again, err := core.NewAccessControl(st)
		if err != nil {
			t.Fatal(err)
		}
		if held := again.MatterHeldFabrics(); len(held) != 0 {
			t.Errorf("held after restart: %v, want none", held)
		}
	}
	t.Run("the next drop", func(t *testing.T) {
		t.Parallel()
		st, ac := heldExtensionStore(t)
		failedDrop(t, st, ac)
		if err := ac.MatterDropFabric(context.Background(), 2); err != nil {
			t.Fatal(err)
		}
		afterRestart(t, st)
	})
	t.Run("a new fabric's admin entry at the index", func(t *testing.T) {
		t.Parallel()
		st, ac := heldExtensionStore(t)
		failedDrop(t, st, ac)
		ac.NotifyAdminEntryInstalled(mstore.ACLEntry{FabricIndex: 2, Privilege: mstore.PrivilegeAdminister, AuthMode: mstore.AuthModeCASE, Subjects: []uint64{1}})
		afterRestart(t, st)
	})
	t.Run("the new fabric's empty Extension write", func(t *testing.T) {
		t.Parallel()
		st, ac := heldExtensionStore(t)
		failedDrop(t, st, ac)
		ctx := im.WithFabricFilter(context.Background(), true, 2)
		if err := ac.MatterWrite(ctx, 0x0001, []core.AccessControlExtensionEntry{}); err != nil {
			t.Fatal(err)
		}
		afterRestart(t, st)
	})
	t.Run("another index's admin entry leaves the owed clear", func(t *testing.T) {
		t.Parallel()
		st, ac := heldExtensionStore(t)
		failedDrop(t, st, ac)
		ac.NotifyAdminEntryInstalled(mstore.ACLEntry{FabricIndex: 3, Privilege: mstore.PrivilegeAdminister, AuthMode: mstore.AuthModeCASE, Subjects: []uint64{1}})
		if v := st.settings[key]; v == "" {
			t.Error("an admin entry of fabric 3 cleared fabric 2's setting")
		}
	})
}

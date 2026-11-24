// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"encoding/hex"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	mstore "thingfabric.dev/go-fabric/store"
)

// TestAdminEntryClearsAnExtensionThatFailedToLoadA1 (R3-store-3001): a
// fabric's stored Extension the store could not read at construction is
// cleared when a new fabric's default admin entry is installed at that
// index, so the next construction does not load it as the new fabric's
// own.
func TestAdminEntryClearsAnExtensionThatFailedToLoadA1(t *testing.T) {
	t.Parallel()
	const f uint8 = 2
	key := "access_control.extension.2"
	st := &p7ExtStore{settingsACLStore: settingsACLStore{settings: map[string]string{key: hex.EncodeToString(encodeExtensionTLV(t))}}}
	st.getErr = errors.New("busy")
	ac, err := core.NewAccessControl(st)
	if err != nil {
		t.Fatal(err)
	}
	st.getErr = nil
	ac.NotifyAdminEntryInstalled(mstore.ACLEntry{FabricIndex: f, Privilege: mstore.PrivilegeAdminister, AuthMode: mstore.AuthModeCASE, Subjects: []uint64{0x1002}})
	if v := st.settings[key]; v != "" {
		t.Errorf("after the new fabric's admin entry the setting still holds %q", v)
	}
	again, err := core.NewAccessControl(st)
	if err != nil {
		t.Fatal(err)
	}
	if held := again.MatterHeldFabrics(); len(held) != 0 {
		t.Errorf("held after restart: %v, want none — the new fabric %d loaded the old one's Extension", held, f)
	}
}

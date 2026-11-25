// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// TestGroupKeyMappingRemovalClearsTheMarkA1 (R5-par-5005): Groupcast's
// removal of a fabric's last GroupKeyMap row clears the fabric's mark, so
// the fabric's later removal reports no GroupKeyMap shrinking and moves no
// DataVersion for it.
func TestGroupKeyMappingRemovalClearsTheMarkA1(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const f uint8 = 2
	st := memstore.New()
	if _, err := st.AddFabric(ctx, store.FabricRecord{FabricIndex: f, FabricID: 0x0FA2, CompressedID: [8]byte{f, 2, 3, 4, 5, 6, 7, 8}}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: f, GroupKeySetID: 0x42, EpochKey0: make([]byte, 16), EpochStart0: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: 0x101, GroupKeySetID: 0x42}); err != nil {
		t.Fatal(err)
	}
	g, err := NewGroupKeyManagement(st, GroupKeyMgmtConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if held := g.MatterHeldFabrics(); !slices.Contains(held, f) {
		t.Fatalf("MatterHeldFabrics = %v, want %d", held, f)
	}
	heard := threadChangeListener(t, g)
	if err := g.removeGroupKeyMapping(ctx, f, 0x101); err != nil {
		t.Fatal(err)
	}
	if held := g.MatterHeldFabrics(); slices.Contains(held, f) {
		t.Errorf("MatterHeldFabrics = %v after the last row's removal, want %d gone", held, f)
	}
	if err := st.RemoveFabric(ctx, f); err != nil {
		t.Fatal(err)
	}
	v0 := g.MatterDataVersion()
	if err := g.MatterDropFabric(ctx, f); err != nil {
		t.Fatal(err)
	}
	if got := heard.take(); len(got) != 0 {
		t.Errorf("the drop of a fabric whose GroupKeyMap was already empty reported %v", got)
	}
	if v := g.MatterDataVersion(); v != v0 {
		t.Errorf("the drop moved the DataVersion %d → %d", v0, v)
	}
}

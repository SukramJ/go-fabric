// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/groups"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// gkmHeard collects the attributes a server's change listener hears.
type gkmHeard struct {
	mu    sync.Mutex
	attrs []uint32
}

func (h *gkmHeard) take() []uint32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.attrs
	h.attrs = nil
	return out
}

// TestGroupKeyManagementFabricDropReportsWhatShrank (R4-par-4051a,
// decision 614): a fabric's removal whose cascade takes its GroupKeyMap
// rows or its GroupTable entries out of the attributes advances the
// DataVersion once and reports the attributes that shrank; the fabric is
// listed as held before; a drop that removes nothing — a second drop, the
// drop at an index whose rows the store still holds, a fabric that never
// held rows — moves and reports nothing.
func TestGroupKeyManagementFabricDropReportsWhatShrank(t *testing.T) {
	ctx := context.Background()
	const (
		gkmAttrGroupKeyMap uint32 = 0x0000
		gkmAttrGroupTable  uint32 = 0x0001
	)
	setup := func(t *testing.T) (*core.GroupKeyManagement, *memstore.Store, *groups.Manager, *gkmHeard) {
		t.Helper()
		st := memstore.New()
		for _, f := range []uint8{1, 2, 3} {
			if _, err := st.AddFabric(ctx, store.FabricRecord{FabricIndex: f, FabricID: uint64(0x0FA0 + int(f)), CompressedID: [8]byte{f, 2, 3, 4, 5, 6, 7, 8}}); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: 2, GroupKeySetID: 0x42, EpochKey0: make([]byte, 16), EpochStart0: 1}); err != nil {
			t.Fatal(err)
		}
		if err := st.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: 2, GroupID: 0x101, GroupKeySetID: 0x42}); err != nil {
			t.Fatal(err)
		}
		mgr, err := groups.NewManager(st, nil)
		if err != nil {
			t.Fatal(err)
		}
		g, err := core.NewGroupKeyManagement(st, core.GroupKeyMgmtConfig{Groups: mgr, MaxGroupsPerFabric: 4, MaxGroupKeysPerFabric: 4})
		if err != nil {
			t.Fatal(err)
		}
		notifier, ok := any(g).(contract.AttributeChangeNotifier)
		if !ok {
			t.Fatalf("%T implements no contract.AttributeChangeNotifier: a fabric's removal reaches no subscriber", g)
		}
		h := &gkmHeard{}
		t.Cleanup(notifier.OnAttributeChange(func(c contract.Change) {
			h.mu.Lock()
			h.attrs = append(h.attrs, c.Attributes...)
			h.mu.Unlock()
		}))
		return g, st, mgr, h
	}
	dropper := func(t *testing.T, g *core.GroupKeyManagement) contract.FabricScopedState {
		t.Helper()
		d, ok := any(g).(contract.FabricScopedState)
		if !ok {
			t.Fatalf("%T implements no contract.FabricScopedState: a fabric's removal is never told to it", g)
		}
		return d
	}

	t.Run("GroupKeyMap rows", func(t *testing.T) {
		g, st, _, heard := setup(t)
		d := dropper(t, g)
		if held := d.MatterHeldFabrics(); !slices.Contains(held, 2) {
			t.Errorf("MatterHeldFabrics = %v, want 2 listed", held)
		}
		v0 := g.MatterDataVersion()
		if err := st.RemoveFabric(ctx, 2); err != nil {
			t.Fatal(err)
		}
		if err := d.MatterDropFabric(ctx, 2); err != nil {
			t.Fatal(err)
		}
		if got := g.MatterDataVersion(); got != v0+1 {
			t.Errorf("DataVersion %d → %d, want one step", v0, got)
		}
		if got := heard.take(); !slices.Equal(got, []uint32{gkmAttrGroupKeyMap}) {
			t.Errorf("the removal reported %v, want [GroupKeyMap]", got)
		}
		if held := d.MatterHeldFabrics(); slices.Contains(held, 2) {
			t.Errorf("MatterHeldFabrics = %v after the drop, want 2 gone", held)
		}
		if err := d.MatterDropFabric(ctx, 2); err != nil {
			t.Fatal(err)
		}
		if got := g.MatterDataVersion(); got != v0+1 {
			t.Errorf("a second drop moved the DataVersion to %d", got)
		}
		if got := heard.take(); len(got) != 0 {
			t.Errorf("a second drop reported %v", got)
		}
	})

	t.Run("GroupTable entries", func(t *testing.T) {
		g, st, mgr, heard := setup(t)
		d := dropper(t, g)
		if err := mgr.AddEndpointForGroup(ctx, 3, 0x202, 1, "g"); err != nil {
			t.Fatal(err)
		}
		heard.take()
		v0 := g.MatterDataVersion()
		if err := st.RemoveFabric(ctx, 3); err != nil {
			t.Fatal(err)
		}
		mgr.ForgetFabric(3)
		if err := d.MatterDropFabric(ctx, 3); err != nil {
			t.Fatal(err)
		}
		if got := g.MatterDataVersion(); got != v0+1 {
			t.Errorf("DataVersion %d → %d, want one step", v0, got)
		}
		if got := heard.take(); !slices.Equal(got, []uint32{gkmAttrGroupTable}) {
			t.Errorf("the removal reported %v, want [GroupTable]", got)
		}
	})

	t.Run("drop at a held fabric's index", func(t *testing.T) {
		g, _, _, heard := setup(t)
		d := dropper(t, g)
		v0 := g.MatterDataVersion()
		if err := d.MatterDropFabric(ctx, 2); err != nil {
			t.Fatal(err)
		}
		if got := g.MatterDataVersion(); got != v0 {
			t.Errorf("DataVersion moved %d → %d for rows the store still holds", v0, got)
		}
		if got := heard.take(); len(got) != 0 {
			t.Errorf("reported %v for rows the store still holds", got)
		}
	})

	t.Run("fabric that held nothing", func(t *testing.T) {
		g, st, _, heard := setup(t)
		d := dropper(t, g)
		v0 := g.MatterDataVersion()
		if err := st.RemoveFabric(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if err := d.MatterDropFabric(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if got := g.MatterDataVersion(); got != v0 {
			t.Errorf("DataVersion moved %d → %d for a fabric without rows", v0, got)
		}
		if got := heard.take(); len(got) != 0 {
			t.Errorf("reported %v for a fabric without rows", got)
		}
	})
}

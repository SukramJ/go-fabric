// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// keyStoreOnly hides every method of the fake store but the
// GroupStoreFacade ones, so the cluster cannot enumerate fabrics through it.
type keyStoreOnly struct{ core.GroupStoreFacade }

func gkmFabricsOf(ctx context.Context, t *testing.T, gkm *core.GroupKeyManagement, attr uint32) []uint8 {
	t.Helper()
	v, ok := gkm.MatterReadFiltered(ctx, attr)
	if !ok {
		t.Fatal("read answered ok=false")
	}
	var out []uint8
	switch x := v.(type) {
	case []core.GroupKeyMapStruct:
		for _, e := range x {
			out = append(out, e.FabricIndex)
		}
	case []core.GroupInfoMapStruct:
		for _, e := range x {
			out = append(out, e.FabricIndex)
		}
	default:
		t.Fatalf("unexpected value %T", v)
	}
	return out
}

// TestGKMUnfilteredReadsParityMatterJS pins matter.js ListManager
// createProxy for the two fabric-scoped lists: filtered for a
// fabric-filtered read, whole for an unfiltered one — whether the store or
// the group state enumerates the fabrics.
func TestGKMUnfilteredReadsParityMatterJS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fs := newFakeStore()
	for _, id := range []uint64{0xA, 0xB} {
		if _, err := fs.AddFabric(ctx, mstore.FabricRecord{FabricID: id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []uint8{1, 2} {
		if err := fs.SetGroupKeyMapping(ctx, mstore.GroupKeyMapping{FabricIndex: f, GroupID: uint16(f), GroupKeySetID: 7}); err != nil {
			t.Fatal(err)
		}
	}
	mgr, err := groups.NewManager(fs, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []uint8{1, 2} {
		if err := mgr.AddEndpointForGroup(ctx, f, uint16(f), 3, ""); err != nil {
			t.Fatal(err)
		}
	}
	for name, facade := range map[string]core.GroupStoreFacade{"store lists fabrics": fs, "group state lists fabrics": keyStoreOnly{fs}} {
		gkm, err := core.NewGroupKeyManagement(facade, core.GroupKeyMgmtConfig{Groups: mgr})
		if err != nil {
			t.Fatal(err)
		}
		for _, attr := range []uint32{0x0000, 0x0001} {
			if got := gkmFabricsOf(im.WithFabricFilter(ctx, true, 2), t, gkm, attr); !slices.Equal(got, []uint8{2}) {
				t.Errorf("%s: attribute %d fabric-filtered = %v, want [2]", name, attr, got)
			}
			if got := gkmFabricsOf(im.WithFabricFilter(ctx, false, 2), t, gkm, attr); !slices.Equal(got, []uint8{1, 2}) {
				t.Errorf("%s: attribute %d unfiltered = %v, want [1 2]", name, attr, got)
			}
			if got := gkmFabricsOf(im.WithFabricFilter(ctx, true, 0), t, gkm, attr); len(got) != 0 {
				t.Errorf("%s: attribute %d fabric-filtered without an accessing fabric = %v, want none", name, attr, got)
			}
		}
	}
	// Neither the store nor a group state can enumerate: the accessing
	// fabric is all an unfiltered read can cover.
	bare, err := core.NewGroupKeyManagement(keyStoreOnly{fs}, core.GroupKeyMgmtConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got := gkmFabricsOf(im.WithFabricFilter(ctx, false, 1), t, bare, 0x0000); !slices.Equal(got, []uint8{1}) {
		t.Errorf("unfiltered without an enumerator = %v, want [1]", got)
	}
	if got := gkmFabricsOf(im.WithFabricFilter(ctx, false, 0), t, bare, 0x0000); len(got) != 0 {
		t.Errorf("unfiltered without an enumerator or a fabric = %v, want none", got)
	}
}

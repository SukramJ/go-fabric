// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// TestBindingDropFabricReportsTheSaveError: the removal contract drops a
// fabric's Binding entries and answers the persisted list's save error,
// which RemoveFabric discards (review II, R2-P2-fabric-06).
func TestBindingDropFabricReportsTheSaveError(t *testing.T) {
	t.Parallel()
	b := core.NewBinding()
	stored := []core.TargetStruct{{Node: 2, FabricIndex: 1}, {Node: 3, FabricIndex: 2}}
	refused := errors.New("disk full")
	if err := b.Persist(core.BindingConfig{
		Load: func() ([]core.TargetStruct, error) { return stored, nil },
		Save: func([]core.TargetStruct) error { return refused },
	}); err != nil {
		t.Fatal(err)
	}
	if got := b.MatterHeldFabrics(); !slices.Equal(got, []uint8{1, 2}) {
		t.Fatalf("held %v, want [1 2]", got)
	}
	if err := b.MatterDropFabric(context.Background(), 1); !errors.Is(err, refused) {
		t.Errorf("MatterDropFabric(1) = %v, want the save error", err)
	}
	if got := b.MatterHeldFabrics(); !slices.Equal(got, []uint8{2}) {
		t.Errorf("held after the drop %v, want [2]", got)
	}
	if err := b.MatterDropFabric(context.Background(), 1); err != nil {
		t.Errorf("a second MatterDropFabric(1) = %v, want nil: nothing held", err)
	}
}

// TestScenesDropFabricForgetsTheSceneInfo: the removal contract drops a
// fabric's scenes and its scene info entry, so a fabric reusing the index
// starts without the old current scene.
func TestScenesDropFabricForgetsTheSceneInfo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	state := core.NewScenesState(nil)
	srv, err := core.NewScenesManagement(core.ScenesConfig{
		State:   state,
		Fabrics: func(context.Context) []uint8 { return []uint8{1, 2} },
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.StoreGlobalScene(ctx, 1)
	// A read creates both fabrics' scene info entries.
	if _, ok := srv.MatterReadFiltered(im.WithFabricFilter(ctx, false, 2), 0x0002); !ok {
		t.Fatal("FabricSceneInfo not read")
	}
	if got := srv.MatterHeldFabrics(); !slices.Equal(got, []uint8{1, 2}) {
		t.Fatalf("held %v, want [1 2]", got)
	}
	if err := srv.MatterDropFabric(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got := srv.MatterHeldFabrics(); !slices.Equal(got, []uint8{2}) {
		t.Errorf("held after dropping fabric 1 %v, want [2]", got)
	}
}

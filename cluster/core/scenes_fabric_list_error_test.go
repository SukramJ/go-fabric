// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
)

// TestFabricSceneInfoFailsOnAFabricListError (R2-C2-err-0455, decision
// 527): a fabric list that cannot be read is not a node without fabrics.
// FabricSceneInfo answers FAILURE ([contract.FabricScopedReader]'s (nil,
// false)), as it does on a scene table the store cannot load — not Success
// with an empty list, nor with the accessing fabric's entry alone. A list
// that reads is served as before.
func TestFabricSceneInfoFailsOnAFabricListError(t *testing.T) {
	t.Parallel()
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	failing, err := core.NewScenesManagement(core.ScenesConfig{
		State:       core.NewScenesState(nil),
		ListFabrics: func(context.Context) ([]uint8, error) { return nil, errors.New("store read fault") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := failing.MatterReadFiltered(ctx, 0x0002); ok {
		t.Errorf("FabricSceneInfo on a fabric-list error = %+v, Success; want FAILURE", v)
	}
	listing, err := core.NewScenesManagement(core.ScenesConfig{
		State:       core.NewScenesState(nil),
		ListFabrics: func(context.Context) ([]uint8, error) { return []uint8{1, 2}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	v, ok := listing.MatterReadFiltered(ctx, 0x0002)
	if info, _ := v.([]core.SceneInfoStruct); !ok || len(info) != 2 || info[0].FabricIndex != 1 || info[1].FabricIndex != 2 {
		t.Errorf("FabricSceneInfo with fabrics 1 and 2 = %+v, %v; want both entries", v, ok)
	}
}

// TestDeprecatedFabricsKeepsItsBehaviour: the deprecated ScenesConfig.Fabrics
// serves as before for its window (README §The deprecation window, rule 2),
// and ListFabrics wins when both are set.
func TestDeprecatedFabricsKeepsItsBehaviour(t *testing.T) {
	t.Parallel()
	ctx := im.WithFabricFilter(context.Background(), false, 1)
	old, err := core.NewScenesManagement(core.ScenesConfig{
		State:   core.NewScenesState(nil),
		Fabrics: func(context.Context) []uint8 { return []uint8{1, 2} }, //nolint:staticcheck // the deprecated field's own test
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := old.MatterReadFiltered(ctx, 0x0002); !ok || len(v.([]core.SceneInfoStruct)) != 2 {
		t.Errorf("FabricSceneInfo through the deprecated Fabrics = %+v, %v; want both entries", v, ok)
	}
	both, err := core.NewScenesManagement(core.ScenesConfig{
		State:       core.NewScenesState(nil),
		Fabrics:     func(context.Context) []uint8 { return []uint8{1, 2} }, //nolint:staticcheck // the deprecated field's own test
		ListFabrics: func(context.Context) ([]uint8, error) { return nil, errors.New("fault") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := both.MatterReadFiltered(ctx, 0x0002); ok {
		t.Errorf("with both set, a ListFabrics error = %+v, Success; want FAILURE (ListFabrics wins)", v)
	}
}

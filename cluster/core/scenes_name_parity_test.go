// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// TestScenesAddSceneNameLengthParity: AddScene with a SceneName over 16
// answers CONSTRAINT_ERROR in its response and stores nothing, as
// connectedhomeip's ScenesManagementCluster.cpp:535-541 at 6170af84 does;
// a name of 16 is stored whole (W3-clucore-03). It used to be cut to 16
// and answered SUCCESS.
func TestScenesAddSceneNameLengthParity(t *testing.T) {
	t.Parallel()
	s, _ := newScenes(t, &sceneOnOff{}, nil)
	long := invoke(t, s, 0x00, core.AddSceneRequest{GroupID: 0x0101, SceneID: 1, SceneName: "seventeen-chars-x"}).(core.SceneStatusResponse)
	if long.Status != im.StatusConstraintError || long.SceneID != 1 || long.GroupID != 0x0101 {
		t.Fatalf("AddScene with 17 characters: %+v, want CONSTRAINT_ERROR", long)
	}
	if v := invoke(t, s, 0x01, core.SceneRef{GroupID: 0x0101, SceneID: 1}).(core.ViewSceneResponse); v.Status != im.StatusNotFound {
		t.Fatalf("ViewScene after the refused AddScene: %v, want NOT_FOUND", v.Status)
	}
	name := strings.Repeat("ä", 16) // 16 UTF-16 units, 32 bytes
	if r := invoke(t, s, 0x00, core.AddSceneRequest{GroupID: 0x0101, SceneID: 2, SceneName: name}).(core.SceneStatusResponse); r.Status != im.StatusSuccess {
		t.Fatalf("AddScene with 16 characters: %v", r.Status)
	}
	if v := invoke(t, s, 0x01, core.SceneRef{GroupID: 0x0101, SceneID: 2}).(core.ViewSceneResponse); v.SceneName != name {
		t.Fatalf("ViewScene name %q, want %q", v.SceneName, name)
	}
}

// TestScenesFabricSceneInfoWithoutFabrics: a ScenesManagement built
// without Fabrics serves the accessing fabric its own FabricSceneInfo
// entry, as matter.js's fabric-scoped state does; a read without an
// accessing fabric gets none (W3-clucore-14).
func TestScenesFabricSceneInfoWithoutFabrics(t *testing.T) {
	t.Parallel()
	srv, err := core.NewScenesManagement(core.ScenesConfig{State: core.NewScenesState(nil)})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := srv.MatterReadFiltered(im.WithFabricFilter(context.Background(), true, 3), 0x0002)
	if info := v.([]core.SceneInfoStruct); len(info) != 1 || info[0].FabricIndex != 3 {
		t.Fatalf("FabricSceneInfo for fabric 3: %+v, want its own entry", info)
	}
	v, _ = srv.MatterReadFiltered(context.Background(), 0x0002)
	if info := v.([]core.SceneInfoStruct); len(info) != 0 {
		t.Fatalf("FabricSceneInfo without an accessing fabric: %+v, want none", info)
	}
}

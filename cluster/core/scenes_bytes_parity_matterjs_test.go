// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/im"
)

// TestScenesManagementServerParityMatterJS ports two cases of
// packages/node/test/behaviors/scenes-management/ScenesManagementServerTest.ts
// at db91886c: the remaining capacity shared across fabrics (#4963, :158-199)
// and the scene name bound in UTF-8 bytes (#4995, :201-213).
func TestScenesManagementServerParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("limits a fabric's remaining capacity by the entries other fabrics use", func(t *testing.T) {
		t.Parallel()
		// Another fabric fills the table up to three free entries, far
		// fewer than this fabric's own share (63 of 128).
		other := make([]core.SceneEntry, 128-3)
		for i := range other {
			other[i] = core.SceneEntry{FabricIndex: 2, SceneID: uint8(i)} //nolint:gosec // i < 125
		}
		data, err := json.Marshal(other)
		if err != nil {
			t.Fatal(err)
		}
		st, err := core.LoadScenesState(data, nil)
		if err != nil {
			t.Fatal(err)
		}
		srv, err := core.NewScenesManagement(core.ScenesConfig{State: st, Fabrics: func(context.Context) []uint8 { return []uint8{1, 2} }})
		if err != nil {
			t.Fatal(err)
		}
		membership := invoke(t, srv, 0x06, core.SceneGroupRequest{GroupID: 0}).(core.GetSceneMembershipResponse)
		if membership.Capacity == nil || *membership.Capacity != 3 {
			t.Fatalf("GetSceneMembership capacity %v, want 3", membership.Capacity)
		}
		add := func(sceneID uint8) im.StatusCode {
			return invoke(t, srv, 0x00, core.AddSceneRequest{GroupID: 0, SceneID: sceneID}).(core.SceneStatusResponse).Status
		}
		for _, id := range []uint8{1, 2, 3} {
			if s := add(id); s != im.StatusSuccess {
				t.Fatalf("AddScene %d: %v, want SUCCESS", id, s)
			}
		}
		if s := add(4); s != im.StatusResourceExhausted {
			t.Fatalf("AddScene 4: %v, want RESOURCE_EXHAUSTED", s)
		}
	})
	t.Run("refuses a scene name over 16 bytes in UTF-8", func(t *testing.T) {
		t.Parallel()
		var stored []byte
		s, _ := newScenes(t, &sceneOnOff{}, func(b []byte) { stored = b })
		addScene := func(sceneID uint8, name string) im.StatusCode {
			return invoke(t, s, 0x00, core.AddSceneRequest{GroupID: 0, SceneID: sceneID, SceneName: name}).(core.SceneStatusResponse).Status
		}
		// 9 × "é" is 18 bytes in UTF-8 but only 9 UTF-16 code units.
		if got := addScene(1, strings.Repeat("é", 9)); got != im.StatusConstraintError {
			t.Fatalf("18 bytes: %v, want CONSTRAINT_ERROR", got)
		}
		if got := addScene(2, strings.Repeat("é", 8)); got != im.StatusSuccess {
			t.Fatalf("16 bytes: %v, want SUCCESS", got)
		}
		var table []core.SceneEntry
		if err := json.Unmarshal(stored, &table); err != nil {
			t.Fatal(err)
		}
		if len(table) != 1 || table[0].SceneID != 2 || table[0].Name != strings.Repeat("é", 8) {
			t.Fatalf("scene table %+v, want scene 2 named 8 × é only", table)
		}
	})
}

// TestScenesRestoreCutsANameOverSixteenBytes: a scene name stored while
// the bound counted UTF-16 units (9 × "é", 18 bytes) is cut at load to the
// 16 bytes it may hold, between two runes, as matter.js's datasource cuts
// a stored string over its byte limit
// (packages/node/src/behavior/state/managed/Datasource.ts:124-163 at
// db91886c, #4995).
func TestScenesRestoreCutsANameOverSixteenBytes(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal([]core.SceneEntry{
		{FabricIndex: 1, SceneID: 1, Name: strings.Repeat("é", 9)},
		{FabricIndex: 1, SceneID: 2, Name: "a" + strings.Repeat("é", 8)}, // 17 bytes: the last "é" would be split
		{FabricIndex: 1, SceneID: 3, Name: strings.Repeat("é", 8)},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := core.LoadScenesState(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := core.NewScenesManagement(core.ScenesConfig{State: st, Fabrics: func(context.Context) []uint8 { return []uint8{1} }})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[uint8]string{1: strings.Repeat("é", 8), 2: "a" + strings.Repeat("é", 7), 3: strings.Repeat("é", 8)} {
		v := invoke(t, srv, 0x01, core.SceneRef{SceneID: id}).(core.ViewSceneResponse)
		if v.Status != im.StatusSuccess || v.SceneName != want {
			t.Errorf("scene %d: %v %q, want SUCCESS %q", id, v.Status, v.SceneName, want)
		}
	}
}

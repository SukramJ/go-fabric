// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// TestScenesManagementGlobalSceneWithOnOff runs the global-scene paths of
// matter.js's OnOffServer against this package's ScenesManagement as its
// scenes port: OffWithEffect stores the fabric's global scene
// (OnOffServer.ts:159-165 → ScenesManagementServer.ts:563-582, an entry
// "Global Scene" in group 0, scene 0, transition 0, the current scene
// unchanged), OnWithRecallGlobalScene recalls it (OnOffServer.ts:175-184 →
// ScenesManagementServer.ts:552-561, applied, then current and valid), the
// recall's On leaves the recalled scene valid because OnOff does not change
// again, and the next Off invalidates it (OnOffServer.ts:119-122 →
// ScenesManagementServer.ts:966-975).
func TestScenesManagementGlobalSceneWithOnOff(t *testing.T) {
	st := core.NewScenesState(nil)
	var light *onoff.Server
	sm, err := core.NewScenesManagement(core.ScenesConfig{
		State:    st,
		Siblings: func() []contract.ClusterServer { return []contract.ClusterServer{light} },
		Fabrics:  func(context.Context) []uint8 { return []uint8{1} },
	})
	if err != nil {
		t.Fatal(err)
	}
	light, err = onoff.New(onoff.Config{Features: onoff.FeatureLT, Scenes: sm, TimeSource: clocktest.New(time.Unix(0, 0))})
	if err != nil {
		t.Fatal(err)
	}
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	invokeOnOff := func(cmd uint32, fields any) {
		t.Helper()
		if _, err := light.MatterInvoke(ctx, cmd, fields); err != nil {
			t.Fatalf("OnOff command 0x%02X: %v", cmd, err)
		}
	}
	info := func() core.SceneInfoStruct {
		t.Helper()
		v, _ := sm.MatterReadFiltered(ctx, 0x0002)
		si, _ := v.([]core.SceneInfoStruct)
		if len(si) != 1 {
			t.Fatalf("FabricSceneInfo %+v", v)
		}
		return si[0]
	}

	invokeOnOff(onoff.CmdOn, nil)
	invokeOnOff(onoff.CmdOffWithEffect, onoffdef.OffWithEffectRequest{})
	if light.OnOff() {
		t.Fatal("OffWithEffect left the light on")
	}
	view, err := sm.MatterInvoke(ctx, 0x01, core.SceneRef{GroupID: 0, SceneID: 0})
	if err != nil {
		t.Fatal(err)
	}
	v := view.(core.ViewSceneResponse) //nolint:forcetypeassert // ViewScene answers a ViewSceneResponse
	if v.Status != im.StatusSuccess || v.SceneName != "Global Scene" || v.TransitionTime != 0 ||
		len(v.ExtensionFieldSets) != 1 || v.ExtensionFieldSets[0].ClusterID != 0x0006 || v.ExtensionFieldSets[0].AttributeValueList[0].Unsigned != 1 {
		t.Fatalf("global scene %+v", v)
	}
	if i := info(); i.SceneCount != 1 || i.SceneValid {
		t.Fatalf("after OffWithEffect: %+v (stored, not current)", i)
	}

	invokeOnOff(onoff.CmdOnWithRecallGlobalScene, nil)
	if !light.OnOff() {
		t.Fatal("OnWithRecallGlobalScene left the light off")
	}
	if i := info(); !i.SceneValid || i.CurrentScene != 0 || i.CurrentGroup != 0 {
		t.Fatalf("after the recall: %+v, want the global scene current and valid", i)
	}

	invokeOnOff(onoff.CmdOff, nil)
	if i := info(); i.SceneValid {
		t.Fatalf("after Off: %+v, want the scene invalid", i)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/levelcontrol"
	"thingfabric.dev/go-fabric/cluster/wire"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// TestSceneRecallBypassesTheHostHandler: a scene recall is the server's
// logic, not a command — with a host handler on MoveToLevel the recall
// does not call it, the server moves the level, and the recalled scene is
// the valid one afterwards (the server's invalidation comes before the
// recall's activation). A MoveToLevel the host's handler then accepts
// leaves the scene valid: the server runs nothing of its own for it
// (decision 112; R2-P5-apispec-05, F-db021da0-31).
func TestSceneRecallBypassesTheHostHandler(t *testing.T) {
	lc := engineServer(newDevice(100, true), nil)
	st := core.NewScenesState(nil)
	scenes, err := core.NewScenesManagement(core.ScenesConfig{
		State:      st,
		Siblings:   func() []contract.ClusterServer { return []contract.ClusterServer{lc} },
		GroupKnown: func(context.Context, uint8, uint16) bool { return true },
		Fabrics:    func(context.Context) []uint8 { return []uint8{1} },
	})
	if err != nil {
		t.Fatal(err)
	}
	lc.SetScenes(scenes)
	calls := 0
	if err := lc.Handle(levelcontrol.CmdMoveToLevel, func(context.Context, any) (any, error) { calls++; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	run := func(cmd uint32, fields any) any {
		t.Helper()
		resp, err := scenes.MatterInvoke(ctx, cmd, fields)
		if err != nil {
			t.Fatalf("ScenesManagement command 0x%02X: %v", cmd, err)
		}
		return resp
	}
	add := run(0x00, core.AddSceneRequest{
		GroupID: 0, SceneID: 1, TransitionTime: 0, SceneName: "Dim",
		ExtensionFieldSets: []core.ExtensionFieldSet{{ClusterID: levelcontrol.ClusterID, AttributeValueList: []core.AttributeValuePair{
			{AttributeID: levelcontrol.AttrCurrentLevel, Tag: 1, Unsigned: 200, Fields: 1},
		}}},
	}).(core.SceneStatusResponse)
	if add.Status != im.StatusSuccess {
		t.Fatalf("AddScene: %v", add.Status)
	}
	valid := func() bool {
		t.Helper()
		info, _ := scenes.MatterReadFiltered(ctx, 0x0002)
		si, _ := info.([]core.SceneInfoStruct)
		return len(si) == 1 && si[0].CurrentScene == 1 && si[0].SceneValid
	}
	run(0x05, core.RecallSceneRequest{GroupID: 0, SceneID: 1})
	if calls != 0 {
		t.Errorf("the recall called the host's MoveToLevel handler %d times", calls)
	}
	if l := level(t, lc); l != 200 {
		t.Errorf("CurrentLevel %d after the recall, want 200", l)
	}
	if !valid() {
		t.Error("the recalled scene is not the valid current scene")
	}
	if _, err := lc.MatterInvoke(context.Background(), levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{Level: 50, TransitionTime: u16(0)}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !valid() {
		t.Errorf("after a replaced MoveToLevel: %d handler calls, scene valid %v; want 1, true", calls, valid())
	}
}

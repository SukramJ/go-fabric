// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/levelcontrol"
	onoffsrv "thingfabric.dev/go-fabric/cluster/onoff"
	"thingfabric.dev/go-fabric/cluster/spec"
	lvldef "thingfabric.dev/go-fabric/cluster/spec/levelcontrol"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/groups"
	"thingfabric.dev/go-fabric/im"
)

// levelOnOff is the OnOff server as the LevelControl server's On/Off.
type levelOnOff struct{ srv *onoffsrv.Server }

func (p levelOnOff) OnOff() bool { return p.srv.OnOff() }
func (p levelOnOff) SetOnOff(_ context.Context, on bool) error {
	return p.srv.SetAttributes(map[uint32]any{onoffsrv.AttrOnOff: on})
}

// TestScenesMountWiresLevelControlScenesPort pins the LevelControl
// server's scenes port end to end, through the dispatcher: mounting
// ScenesManagement hands it the endpoint's server, so a MoveToLevel the
// ExecuteIfOff gate refuses leaves the current scene valid (matter.js
// LevelControlServer.ts:269-276 at db021da0, the gate before
// #invalidateScenes), while a MoveToLevelWithOnOff ends it (:293); and a
// scene's CurrentLevel outside MinLevel..MaxLevel is not recalled
// (#applySceneValues, :746-758).
func TestScenesMountWiresLevelControlScenesPort(t *testing.T) {
	t.Parallel()
	mgr, _ := groups.NewManager(oneFabricGroupStore{}, nil)
	light, err := onoffsrv.New(onoffsrv.Config{Features: onoffsrv.FeatureLT})
	if err != nil {
		t.Fatal(err)
	}
	lvl := uint8(100)
	level, err := levelcontrol.New(levelcontrol.HeldConfig{
		Features: levelcontrol.FeatureOnOff | levelcontrol.FeatureLighting, CurrentLevel: &lvl,
		OnOff: levelOnOff{light}, Bridged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ep := &Endpoint{
		ID: 7, DeviceType: 0x0101, FriendlyName: "Lamp", SourceKey: StringKey("lamp"),
		Source: deviceTypeSource{dt: 0x0101, servers: []contract.ClusterServer{light, level}},
		groups: mgr,
	}
	d := NewTopologyDispatcher(makeTopology(ep))
	var scenes *mattercore.ScenesManagement
	for _, s := range ClusterServers(ep) {
		if sm, ok := s.(*mattercore.ScenesManagement); ok {
			scenes = sm
		}
	}
	if scenes == nil {
		t.Fatal("no stack ScenesManagement server")
	}
	if !level.MatterInvalidatesScenes() {
		t.Fatal("the LevelControl server has no scenes port after the mount")
	}
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	valid := func() bool {
		v, _ := scenes.MatterReadFiltered(ctx, 0x0002)
		si, _ := v.([]mattercore.SceneInfoStruct)
		return len(si) == 1 && si[0].SceneValid
	}
	currentLevel := func() any {
		v, _ := level.MatterRead(lvldef.AttrCurrentLevel)
		return v
	}
	invoke := func(cmd uint32) {
		t.Helper()
		res := d.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 7, HasEndpoint: true, Cluster: 0x0008, Command: cmd},
			lvldef.MoveToLevelRequest{Level: 200, TransitionTime: spec.ValueOf(uint16(0))})
		if res.Status != im.StatusSuccess {
			t.Fatalf("command 0x%02X: status 0x%02X", cmd, uint8(res.Status))
		}
	}

	// A stored CurrentLevel 0, below the Lighting MinLevel 1, is not
	// recalled; the recall makes the scene current.
	if r, _ := scenes.MatterInvoke(ctx, 0x00, mattercore.AddSceneRequest{
		SceneID: 1,
		ExtensionFieldSets: []mattercore.ExtensionFieldSet{{ClusterID: 0x0008, AttributeValueList: []mattercore.AttributeValuePair{
			{AttributeID: 0, Tag: 1, Unsigned: 0, Fields: 1},
		}}},
	}); r.(mattercore.SceneStatusResponse).Status != im.StatusSuccess {
		t.Fatalf("AddScene: %+v", r)
	}
	if _, err := scenes.MatterInvoke(ctx, 0x05, mattercore.RecallSceneRequest{SceneID: 1}); err != nil {
		t.Fatal(err)
	}
	if got := currentLevel(); got != uint8(100) {
		t.Errorf("recalling a stored level 0 moved CurrentLevel to %v, want 100", got)
	}
	if !valid() {
		t.Fatal("the recalled scene is not current")
	}

	invoke(lvldef.CmdMoveToLevel) // the light is off, no ExecuteIfOff
	if got := currentLevel(); got != uint8(100) {
		t.Fatalf("the gated MoveToLevel moved CurrentLevel to %v", got)
	}
	if !valid() {
		t.Error("a MoveToLevel the ExecuteIfOff gate refused ended the current scene")
	}
	invoke(lvldef.CmdMoveToLevelWithOnOff)
	if valid() {
		t.Error("MoveToLevelWithOnOff left the current scene valid")
	}
}

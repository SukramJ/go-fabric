// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// hookOnOff is a sceneOnOff that runs during its On/Off command — while
// the scenes server applies a recalled scene without its lock.
type hookOnOff struct {
	sceneOnOff
	during func()
}

func (s *hookOnOff) MatterInvoke(ctx context.Context, cmd uint32, f any) (any, error) {
	if fn := s.during; fn != nil {
		s.during = nil
		fn()
	}
	return s.sceneOnOff.MatterInvoke(ctx, cmd, f)
}

// TestScenesRecallOfASceneRemovedDuringTheApplyLeavesItInvalid
// (R2-C2-conc-1205): a scene removed while its recall applies the values
// does not become the current, valid scene afterwards — FabricSceneInfo
// does not name a scene the table no longer holds — for RecallScene and
// for the OnOff-driven RecallGlobalScene.
func TestScenesRecallOfASceneRemovedDuringTheApplyLeavesItInvalid(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		group  uint16
		scene  uint8
		recall func(t *testing.T, s *core.ScenesManagement)
	}{
		"RecallScene": {0x0101, 1, func(t *testing.T, s *core.ScenesManagement) {
			invoke(t, s, 0x05, core.RecallSceneRequest{GroupID: 0x0101, SceneID: 1})
		}},
		"RecallGlobalScene": {0, 0, func(_ *testing.T, s *core.ScenesManagement) { s.RecallGlobalScene(fabric1, 1) }},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			light := &hookOnOff{}
			st := core.NewScenesState(nil)
			s, err := core.NewScenesManagement(core.ScenesConfig{
				State:      st,
				Siblings:   func() []contract.ClusterServer { return []contract.ClusterServer{light} },
				GroupKnown: func(_ context.Context, _ uint8, g uint16) bool { return g == 0x0101 },
				Fabrics:    func(context.Context) []uint8 { return []uint8{1, 2} },
			})
			if err != nil {
				t.Fatal(err)
			}
			add := invoke(t, s, 0x00, core.AddSceneRequest{
				GroupID: tc.group, SceneID: tc.scene, SceneName: "Evening",
				ExtensionFieldSets: []core.ExtensionFieldSet{{ClusterID: 0x0006, AttributeValueList: []core.AttributeValuePair{
					{AttributeID: 0, Tag: 1, Unsigned: 1, Fields: 1},
				}}},
			}).(core.SceneStatusResponse)
			if add.Status != im.StatusSuccess {
				t.Fatalf("AddScene: %v", add.Status)
			}
			light.during = func() {
				if r := invoke(t, s, 0x02, core.SceneRef{GroupID: tc.group, SceneID: tc.scene}).(core.SceneStatusResponse); r.Status != im.StatusSuccess {
					t.Errorf("RemoveScene during the apply: %v", r.Status)
				}
			}
			tc.recall(t, s)
			if light.during != nil {
				t.Fatal("the recall applied nothing")
			}
			v, _ := s.MatterReadFiltered(fabric1, 0x0002)
			for _, si := range v.([]core.SceneInfoStruct) {
				if si.FabricIndex == 1 && si.SceneValid {
					t.Errorf("FabricSceneInfo %+v: valid for a scene the table no longer holds", si)
				}
			}
		})
	}
}

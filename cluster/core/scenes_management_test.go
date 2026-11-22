// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/schema"
)

// sceneOnOff is an OnOff server whose On/Off commands set its state.
type sceneOnOff struct{ on bool }

func (s *sceneOnOff) MatterClusterID() uint32 { return 0x0006 }
func (s *sceneOnOff) MatterRead(a uint32) (any, bool) {
	if a == 0 {
		return s.on, true
	}
	return nil, false
}
func (s *sceneOnOff) MatterWrite(context.Context, uint32, any) error { return nil }
func (s *sceneOnOff) MatterReportable() []uint32                     { return []uint32{0} }
func (s *sceneOnOff) MatterAttributes() []uint32                     { return []uint32{0} }
func (s *sceneOnOff) MatterInvoke(_ context.Context, cmd uint32, _ any) (any, error) {
	s.on = cmd == 0x01
	return nil, nil
}

func newScenes(t *testing.T, onoff *sceneOnOff, persist func([]byte)) (*core.ScenesManagement, *core.ScenesState) {
	t.Helper()
	st := core.NewScenesState(persist)
	srv, err := core.NewScenesManagement(core.ScenesConfig{
		State:    st,
		Siblings: func() []contract.ClusterServer { return []contract.ClusterServer{onoff} },
		GroupKnown: func(_ context.Context, _ uint8, g uint16) bool {
			return g == 0x0101
		},
		Fabrics: func(context.Context) []uint8 { return []uint8{1, 2} },
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv, st
}

var fabric1 = im.WithFabricFilter(context.Background(), true, 1)

func invoke(t *testing.T, s *core.ScenesManagement, cmd uint32, fields any) any {
	t.Helper()
	resp, err := s.MatterInvoke(fabric1, cmd, fields)
	if err != nil {
		t.Fatalf("command 0x%02X: %v", cmd, err)
	}
	return resp
}

// TestScenesAddAndRecallOnOff ports matter.js
// packages/node/test/behaviors/scenes-management/ScenesManagementServerTest.ts
// ("add and recall onoff boolean scene value"): a scene added with OnOff=true
// is viewable and recalls the light on, and becomes the current, valid scene.
func TestScenesAddAndRecallOnOff(t *testing.T) {
	t.Parallel()
	light := &sceneOnOff{}
	s, _ := newScenes(t, light, nil)
	add := invoke(t, s, 0x00, core.AddSceneRequest{
		GroupID: 0x0101, SceneID: 1, TransitionTime: 0, SceneName: "Evening",
		ExtensionFieldSets: []core.ExtensionFieldSet{{ClusterID: 0x0006, AttributeValueList: []core.AttributeValuePair{
			{AttributeID: 0, Tag: 1, Unsigned: 1, Fields: 1},
		}}},
	}).(core.SceneStatusResponse)
	if add.Status != im.StatusSuccess {
		t.Fatalf("AddScene: %v", add.Status)
	}
	view := invoke(t, s, 0x01, core.SceneRef{GroupID: 0x0101, SceneID: 1}).(core.ViewSceneResponse)
	if view.Status != im.StatusSuccess || view.SceneName != "Evening" || len(view.ExtensionFieldSets) != 1 ||
		view.ExtensionFieldSets[0].AttributeValueList[0].Unsigned != 1 {
		t.Fatalf("ViewScene: %+v", view)
	}
	invoke(t, s, 0x05, core.RecallSceneRequest{GroupID: 0x0101, SceneID: 1})
	if !light.on {
		t.Fatal("RecallScene did not switch the light on")
	}
	info, _ := s.MatterReadFiltered(fabric1, 0x0002)
	si := info.([]core.SceneInfoStruct)
	if len(si) != 1 || si[0].CurrentScene != 1 || si[0].CurrentGroup != 0x0101 || !si[0].SceneValid || si[0].SceneCount != 1 {
		t.Fatalf("FabricSceneInfo after recall: %+v", si)
	}
}

// TestScenesCommandRules pins matter.js's command checks: an unknown group
// is INVALID_COMMAND, SceneId 255 CONSTRAINT_ERROR, a missing scene
// NOT_FOUND, a pair of the wrong width INVALID_COMMAND, StoreScene captures
// the current state, GetSceneMembership reports capacity and the list,
// CopyScene copies, removing the group drops its scenes, and the capacity per
// fabric is (SceneTableSize-1)/2.
func TestScenesCommandRules(t *testing.T) {
	t.Parallel()
	light := &sceneOnOff{on: true}
	var saved []byte
	s, st := newScenes(t, light, func(b []byte) { saved = b })

	if r := invoke(t, s, 0x00, core.AddSceneRequest{GroupID: 7, SceneID: 1}).(core.SceneStatusResponse); r.Status != im.StatusInvalidCommand {
		t.Errorf("unknown group: %v, want INVALID_COMMAND", r.Status)
	}
	if r := invoke(t, s, 0x00, core.AddSceneRequest{SceneID: 255}).(core.SceneStatusResponse); r.Status != im.StatusConstraintError {
		t.Errorf("SceneId 255: %v, want CONSTRAINT_ERROR", r.Status)
	}
	if r := invoke(t, s, 0x01, core.SceneRef{SceneID: 9}).(core.ViewSceneResponse); r.Status != im.StatusNotFound {
		t.Errorf("missing scene: %v, want NOT_FOUND", r.Status)
	}
	wrongWidth := core.AddSceneRequest{SceneID: 2, ExtensionFieldSets: []core.ExtensionFieldSet{{
		ClusterID:          6,
		AttributeValueList: []core.AttributeValuePair{{AttributeID: 0, Tag: 3, Unsigned: 1, Fields: 1}},
	}}}
	if r := invoke(t, s, 0x00, wrongWidth).(core.SceneStatusResponse); r.Status != im.StatusInvalidCommand {
		t.Errorf("ValueUnsigned16 for a bool: %v, want INVALID_COMMAND", r.Status)
	}
	if r := invoke(t, s, 0x04, core.SceneRef{GroupID: 0x0101, SceneID: 3}).(core.SceneStatusResponse); r.Status != im.StatusSuccess {
		t.Fatalf("StoreScene: %v", r.Status)
	}
	view := invoke(t, s, 0x01, core.SceneRef{GroupID: 0x0101, SceneID: 3}).(core.ViewSceneResponse)
	if len(view.ExtensionFieldSets) != 1 || view.ExtensionFieldSets[0].AttributeValueList[0].Unsigned != 1 {
		t.Fatalf("StoreScene captured %+v, want OnOff=1", view.ExtensionFieldSets)
	}
	if saved == nil {
		t.Error("a stored scene was not persisted")
	}
	m := invoke(t, s, 0x06, core.SceneGroupRequest{GroupID: 0x0101}).(core.GetSceneMembershipResponse)
	if m.Status != im.StatusSuccess || len(m.SceneList) != 1 || m.Capacity == nil || *m.Capacity != 62 {
		t.Fatalf("GetSceneMembership: %+v (capacity %v), want one scene and 62 left of 63", m, m.Capacity)
	}
	if r := invoke(t, s, 0x40, core.CopySceneRequest{GroupFrom: 0x0101, SceneFrom: 3, GroupTo: 0, SceneTo: 4}).(core.CopySceneResponse); r.Status != im.StatusSuccess {
		t.Fatalf("CopyScene: %v", r.Status)
	}
	if r := invoke(t, s, 0x01, core.SceneRef{GroupID: 0, SceneID: 4}).(core.ViewSceneResponse); r.Status != im.StatusSuccess {
		t.Fatalf("copied scene: %v", r.Status)
	}
	st.RemoveScenesForGroup(1, 0x0101)
	if r := invoke(t, s, 0x01, core.SceneRef{GroupID: 0x0101, SceneID: 3}).(core.ViewSceneResponse); r.Status != im.StatusNotFound {
		t.Fatalf("scene of a removed group: %v, want NOT_FOUND", r.Status)
	}
	for i := range 63 {
		if r := invoke(t, s, 0x04, core.SceneRef{SceneID: uint8(10 + i)}).(core.SceneStatusResponse); r.Status != im.StatusSuccess && i < 62 {
			t.Fatalf("StoreScene %d: %v", i, r.Status)
		}
	}
	if r := invoke(t, s, 0x04, core.SceneRef{SceneID: 200}).(core.SceneStatusResponse); r.Status != im.StatusResourceExhausted {
		t.Fatalf("scene beyond the fabric capacity: %v, want RESOURCE_EXHAUSTED", r.Status)
	}
}

// TestScenesOutOfRangeValues pins matter.js #decodeValueFromAttributeValuePair:
// a non-nullable boolean other than 0/1 is FALSE.
func TestScenesOutOfRangeValues(t *testing.T) {
	t.Parallel()
	light := &sceneOnOff{on: true}
	s, _ := newScenes(t, light, nil)
	invoke(t, s, 0x00, core.AddSceneRequest{SceneID: 5, ExtensionFieldSets: []core.ExtensionFieldSet{{
		ClusterID:          6,
		AttributeValueList: []core.AttributeValuePair{{AttributeID: 0, Tag: 1, Unsigned: 7, Fields: 1}},
	}}})
	view := invoke(t, s, 0x01, core.SceneRef{SceneID: 5}).(core.ViewSceneResponse)
	if got := view.ExtensionFieldSets[0].AttributeValueList[0].Unsigned; got != 0 {
		t.Fatalf("OnOff 7 stored as %d, want 0 (FALSE)", got)
	}
}

// TestScenesStateRoundTrip pins persistence: a restored table serves the
// stored scenes.
func TestScenesStateRoundTrip(t *testing.T) {
	t.Parallel()
	var saved []byte
	s, _ := newScenes(t, &sceneOnOff{on: true}, func(b []byte) { saved = b })
	invoke(t, s, 0x04, core.SceneRef{SceneID: 1})
	restored, err := core.LoadScenesState(saved, nil)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := core.NewScenesManagement(core.ScenesConfig{State: restored, Fabrics: func(context.Context) []uint8 { return []uint8{1} }})
	if err != nil {
		t.Fatal(err)
	}
	if r := invoke(t, s2, 0x01, core.SceneRef{SceneID: 1}).(core.ViewSceneResponse); r.Status != im.StatusSuccess {
		t.Fatalf("restored scene: %v", r.Status)
	}
}

// TestScenesRemainingCapacityIsBoundByTheTable pins chip
// FabricTableImpl::GetRemainingCapacity, which TC-S-2.6 asserts: a
// fabric's RemainingCapacity is its quota left, bounded by the table's
// free entries the other fabrics share; once the table is full, AddScene
// is RESOURCE_EXHAUSTED even within the fabric's quota. Every change
// reaches FabricSceneInfo subscribers.
func TestScenesRemainingCapacityIsBoundByTheTable(t *testing.T) {
	t.Parallel()
	st := core.NewScenesState(nil)
	srv, err := core.NewScenesManagement(core.ScenesConfig{
		State:   st,
		Fabrics: func(context.Context) []uint8 { return []uint8{1, 2, 3} },
	})
	if err != nil {
		t.Fatal(err)
	}
	reports := 0
	unsub := srv.OnAttributeChange(func(change contract.Change) {
		ids := change.Attributes
		if len(ids) == 1 && ids[0] == 0x0002 {
			reports++
		}
	})
	defer unsub()
	add := func(fabric, scene uint8) im.StatusCode {
		ctx := im.WithFabricFilter(context.Background(), true, fabric)
		resp, err := srv.MatterInvoke(ctx, 0x00, core.AddSceneRequest{SceneID: scene})
		if err != nil {
			t.Fatal(err)
		}
		return resp.(core.SceneStatusResponse).Status
	}
	remaining := func(fabric uint8) uint8 {
		v, _ := srv.MatterReadFiltered(im.WithFabricFilter(context.Background(), true, fabric), 0x0002)
		return v.([]core.SceneInfoStruct)[0].RemainingCapacity
	}
	for f := uint8(1); f <= 2; f++ {
		for i := range uint8(63) {
			if s := add(f, i); s != im.StatusSuccess {
				t.Fatalf("fabric %d scene %d: %v", f, i, s)
			}
		}
	}
	if reports != 126 {
		t.Errorf("%d FabricSceneInfo reports for 126 added scenes", reports)
	}
	if got := remaining(3); got != 2 {
		t.Fatalf("fabric 3 RemainingCapacity %d, want 128-126 = 2", got)
	}
	add(3, 1)
	add(3, 2)
	if got := remaining(3); got != 0 {
		t.Fatalf("fabric 3 RemainingCapacity %d after filling the table, want 0", got)
	}
	if s := add(3, 3); s != im.StatusResourceExhausted {
		t.Fatalf("AddScene into a full table: %v, want RESOURCE_EXHAUSTED", s)
	}
	before := reports
	ctx := im.WithFabricFilter(context.Background(), true, 3)
	if _, err := srv.MatterInvoke(ctx, 0x01, core.SceneRef{SceneID: 1}); err != nil {
		t.Fatal(err)
	}
	if reports != before {
		t.Error("ViewScene, which changes nothing, reported FabricSceneInfo")
	}
}

// TestScenesParityWithSchema pins the served ClusterRevision against the
// matter.js schema snapshot and the attribute and command lists against
// matter.js ScenesManagementServer with the SceneNames feature.
func TestScenesParityWithSchema(t *testing.T) {
	t.Parallel()
	s, _ := newScenes(t, &sceneOnOff{}, nil)
	want, ok := schema.ClusterRevision(core.ScenesManagementClusterID)
	if !ok {
		t.Fatal("ScenesManagement missing from the schema")
	}
	if got, _ := s.MatterRead(0xFFFD); got != want {
		t.Fatalf("ClusterRevision %v, schema %d", got, want)
	}
	if got, _ := s.MatterRead(0xFFFC); got != uint32(1) {
		t.Fatalf("FeatureMap %v, want SceneNames (1)", got)
	}
	if got := s.MatterAcceptedCommands(); !slices.Equal(got, []uint32{0, 1, 2, 3, 4, 5, 6, 0x40}) {
		t.Errorf("AcceptedCommandList %v", got)
	}
	if got := s.MatterGeneratedCommands(); !slices.Equal(got, []uint32{0, 1, 2, 3, 4, 6, 0x40}) {
		t.Errorf("GeneratedCommandList %v", got)
	}
}

// TestScenesRefusedStoreRestoresSceneInfo (review II, phase 5b): a
// RemoveScene of the current, valid scene whose store the scene store
// refuses answers FAILURE and leaves everything as it was — the scene
// stored, FabricSceneInfo's current scene valid, and the scene still
// monitored, so a change of a scene-able attribute invalidates it.
// Before, the table came back but SceneValid stayed false and nothing
// was monitored.
func TestScenesRefusedStoreRestoresSceneInfo(t *testing.T) {
	t.Parallel()
	light := &sceneOnOff{on: true}
	refuse := false
	st := core.NewPersistedScenesState(
		func(context.Context) ([]byte, error) { return nil, nil },
		func(context.Context, []byte) error {
			if refuse {
				return errors.New("the store refuses")
			}
			return nil
		})
	s, err := core.NewScenesManagement(core.ScenesConfig{
		State:      st,
		Siblings:   func() []contract.ClusterServer { return []contract.ClusterServer{light} },
		GroupKnown: func(_ context.Context, _ uint8, g uint16) bool { return g == 0x0101 },
		Fabrics:    func(context.Context) []uint8 { return []uint8{1, 2} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := invoke(t, s, 0x04, core.SceneRef{GroupID: 0x0101, SceneID: 1}).(core.SceneStatusResponse); r.Status != im.StatusSuccess {
		t.Fatalf("StoreScene: %v", r.Status)
	}
	invoke(t, s, 0x05, core.RecallSceneRequest{GroupID: 0x0101, SceneID: 1})
	info := func() core.SceneInfoStruct {
		t.Helper()
		v, _ := s.MatterReadFiltered(fabric1, 0x0002)
		si, _ := v.([]core.SceneInfoStruct)
		if len(si) != 1 {
			t.Fatalf("FabricSceneInfo = %+v", v)
		}
		return si[0]
	}
	if si := info(); !si.SceneValid || si.SceneCount != 1 {
		t.Fatalf("after the recall: %+v", si)
	}
	refuse = true
	if r := invoke(t, s, 0x02, core.SceneRef{GroupID: 0x0101, SceneID: 1}).(core.SceneStatusResponse); r.Status != im.StatusFailure {
		t.Fatalf("RemoveScene with the store refusing = %v, want FAILURE", r.Status)
	}
	if si := info(); !si.SceneValid || si.SceneCount != 1 || si.CurrentScene != 1 || si.CurrentGroup != 0x0101 {
		t.Errorf("after the refused removal: %+v, want scene 1 of group 0x0101 current, valid and stored", si)
	}
	refuse = false
	st.InvalidateCurrentScene()
	if si := info(); si.SceneValid {
		t.Errorf("the current scene is no longer monitored: still valid after a change, %+v", si)
	}
}

// TestScenesRefusedFirstStoreLeavesNoSceneInfo (review II, phase 5b): a
// fabric's first scene command whose store the scene store refuses answers
// FAILURE and leaves no scene info entry behind for the fabric — the state
// holds the fabric no more than before the command. Before, the entry the
// command created stayed (ScenesState.Fabrics listed the fabric).
func TestScenesRefusedFirstStoreLeavesNoSceneInfo(t *testing.T) {
	t.Parallel()
	st := core.NewPersistedScenesState(
		func(context.Context) ([]byte, error) { return nil, nil },
		func(context.Context, []byte) error { return errors.New("the store refuses") })
	s, err := core.NewScenesManagement(core.ScenesConfig{
		State:      st,
		Siblings:   func() []contract.ClusterServer { return []contract.ClusterServer{&sceneOnOff{on: true}} },
		GroupKnown: func(_ context.Context, _ uint8, g uint16) bool { return g == 0x0101 },
		Fabrics:    func(context.Context) []uint8 { return []uint8{1} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := invoke(t, s, 0x04, core.SceneRef{GroupID: 0x0101, SceneID: 1}).(core.SceneStatusResponse); r.Status != im.StatusFailure {
		t.Fatalf("StoreScene with the store refusing = %v, want FAILURE", r.Status)
	}
	if got := st.Fabrics(); len(got) != 0 {
		t.Errorf("Fabrics after the refused first command = %v, want none", got)
	}
}

// TestScenesInfoReadFailsWhileTheTableDoesNotLoad (review II, phase 5b):
// while the scene store refuses to load the table, a FabricSceneInfo read
// fails ((nil, false), which the dispatcher answers FAILURE on a request)
// instead of serving SceneCount 0 with the whole capacity free; once the
// table loads, the read serves it. SceneTableSize needs no table and is
// served throughout. Before, the failed load was ignored and the read
// served an empty fabric.
func TestScenesInfoReadFailsWhileTheTableDoesNotLoad(t *testing.T) {
	t.Parallel()
	refuse := true
	stored := []byte(`[{"f":1,"g":257,"s":1}]`)
	st := core.NewPersistedScenesState(
		func(context.Context) ([]byte, error) {
			if refuse {
				return nil, errors.New("the store refuses")
			}
			return stored, nil
		},
		func(context.Context, []byte) error { return nil })
	s, err := core.NewScenesManagement(core.ScenesConfig{
		State:      st,
		Siblings:   func() []contract.ClusterServer { return nil },
		GroupKnown: func(context.Context, uint8, uint16) bool { return true },
		Fabrics:    func(context.Context) []uint8 { return []uint8{1} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := s.MatterReadFiltered(fabric1, 0x0002); ok || v != nil {
		t.Errorf("FabricSceneInfo with the table unloadable = %+v, %v; want (nil, false)", v, ok)
	}
	if _, ok := s.MatterRead(0x0002); ok {
		t.Error("FabricSceneInfo without a request is served with the table unloadable")
	}
	if _, ok := s.MatterReadFiltered(fabric1, 0x0001); !ok {
		t.Error("SceneTableSize is not served while the table does not load")
	}
	refuse = false
	v, ok := s.MatterReadFiltered(fabric1, 0x0002)
	si, _ := v.([]core.SceneInfoStruct)
	if !ok || len(si) != 1 || si[0].SceneCount != 1 {
		t.Errorf("FabricSceneInfo once the table loads = %+v, %v; want fabric 1 with its stored scene", v, ok)
	}
}

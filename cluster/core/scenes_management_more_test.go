// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// sceneLevel is a LevelControl server recording MoveToLevel.
type sceneLevel struct {
	level *uint8
	moved []wire.MoveToLevelRequest
}

func (s *sceneLevel) MatterClusterID() uint32 { return 0x0008 }
func (s *sceneLevel) MatterRead(a uint32) (any, bool) {
	switch a {
	case 0:
		if s.level == nil {
			return nil, true
		}
		return *s.level, true
	case 4:
		return uint16(1234), true
	}
	return nil, false
}
func (s *sceneLevel) MatterWrite(context.Context, uint32, any) error { return nil }
func (s *sceneLevel) MatterReportable() []uint32                     { return []uint32{0, 4} }
func (s *sceneLevel) MatterAttributes() []uint32                     { return []uint32{0, 4} }
func (s *sceneLevel) MatterInvoke(_ context.Context, _ uint32, f any) (any, error) {
	if r, ok := f.(wire.MoveToLevelRequest); ok {
		s.moved = append(s.moved, r)
	}
	return nil, nil
}

// sceneColor is a ColorControl server recording its color commands.
type sceneColor struct {
	mode  uint8
	cmds  []uint32
	mired uint16
}

func (s *sceneColor) MatterClusterID() uint32 { return 0x0300 }
func (s *sceneColor) MatterRead(a uint32) (any, bool) {
	switch a {
	case 0x0007:
		return s.mired, true
	case 0x4001:
		return s.mode, true
	case 0x0001:
		return uint8(100), true
	case 0x4000:
		return uint16(0x2000), true
	}
	return nil, false
}
func (s *sceneColor) MatterWrite(context.Context, uint32, any) error { return nil }
func (s *sceneColor) MatterReportable() []uint32                     { return []uint32{0x0001, 0x0007, 0x4000, 0x4001} }

func (s *sceneColor) MatterAttributes() []uint32 { return []uint32{0x0001, 0x0007, 0x4000, 0x4001} }

func (s *sceneColor) MatterInvoke(_ context.Context, cmd uint32, _ any) (any, error) {
	s.cmds = append(s.cmds, cmd)
	return nil, nil
}

func scenesWith(t *testing.T, siblings ...contract.ClusterServer) (*core.ScenesManagement, *core.ScenesState) {
	t.Helper()
	st := core.NewScenesState(nil)
	srv, err := core.NewScenesManagement(core.ScenesConfig{
		State:      st,
		Siblings:   func() []contract.ClusterServer { return siblings },
		GroupKnown: func(_ context.Context, _ uint8, g uint16) bool { return g == 0x0101 || g == 0x0202 },
		Fabrics:    func(context.Context) []uint8 { return []uint8{1, 2} },
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv, st
}

func scenesStatus(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var sc interface{ MatterStatusCode() im.StatusCode }
	if !errors.As(err, &sc) {
		t.Fatalf("error %v carries no status", err)
	}
	if err.Error() == "" {
		t.Fatal("empty error text")
	}
	return sc.MatterStatusCode()
}

// TestScenesCaptureAndRecallLevelAndColor pins the per-cluster recall of
// matter.js LevelControlServer and ColorControlServer #applySceneValues:
// a stored scene carries CurrentLevel / CurrentFrequency and the color
// attributes, and RecallScene drives MoveToLevel (ExecuteIfOff) and
// MoveToColorTemperature for a CT scene, MoveToHueAndSaturation for a
// hue/saturation one.
func TestScenesCaptureAndRecallLevelAndColor(t *testing.T) {
	t.Parallel()
	lvl := uint8(77)
	level := &sceneLevel{level: &lvl}
	color := &sceneColor{mode: 2, mired: 300}
	s, _ := scenesWith(t, &sceneOnOff{on: true}, level, color)
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	if r, _ := s.MatterInvoke(ctx, 0x04, core.SceneRef{SceneID: 1}); r.(core.SceneStatusResponse).Status != im.StatusSuccess {
		t.Fatalf("StoreScene: %+v", r)
	}
	view, _ := s.MatterInvoke(ctx, 0x01, core.SceneRef{SceneID: 1})
	if sets := view.(core.ViewSceneResponse).ExtensionFieldSets; len(sets) != 3 {
		t.Fatalf("stored %d extension sets, want OnOff, LevelControl and ColorControl", len(sets))
	}
	tt := uint32(1500)
	if _, err := s.MatterInvoke(ctx, 0x05, core.RecallSceneRequest{SceneID: 1, TransitionTime: &tt}); err != nil {
		t.Fatalf("RecallScene: %v", err)
	}
	if len(level.moved) != 1 || level.moved[0].Level != 77 || level.moved[0].OptionsMask != 1 || *level.moved[0].TransitionTime != 15 {
		t.Fatalf("MoveToLevel %+v, want level 77, ExecuteIfOff, 15 tenths", level.moved)
	}
	if len(color.cmds) != 1 || color.cmds[0] != wire.ColorCtrlCmdMoveToColorTemperature {
		t.Fatalf("color commands %v, want MoveToColorTemperature", color.cmds)
	}

	// A hue/saturation scene recalls through MoveToHueAndSaturation, and a
	// null CurrentLevel is captured as null and not recalled.
	color.mode, color.cmds = 0, nil
	level.level, level.moved = nil, nil
	s.MatterInvoke(ctx, 0x04, core.SceneRef{SceneID: 2})
	if _, err := s.MatterInvoke(ctx, 0x05, core.RecallSceneRequest{SceneID: 2}); err != nil {
		t.Fatal(err)
	}
	if len(color.cmds) != 1 || color.cmds[0] != wire.ColorCtrlCmdMoveToHueAndSaturation {
		t.Fatalf("color commands %v, want MoveToHueAndSaturation", color.cmds)
	}
	if len(level.moved) != 0 {
		t.Fatalf("a null level was recalled: %+v", level.moved)
	}
}

// TestScenesRemoveAndCopy pins RemoveScene, RemoveAllScenes and both
// CopyScene modes, the fabric removal and the invalidation of the current
// scene.
func TestScenesRemoveAndCopy(t *testing.T) {
	t.Parallel()
	s, st := scenesWith(t, &sceneOnOff{on: true})
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	call := func(cmd uint32, f any) any {
		t.Helper()
		r, err := s.MatterInvoke(ctx, cmd, f)
		if err != nil {
			t.Fatalf("command 0x%02X: %v", cmd, err)
		}
		return r
	}
	for _, id := range []uint8{1, 2, 3} {
		call(0x04, core.SceneRef{GroupID: 0x0101, SceneID: id})
	}
	// Copy all of 0x0101 to 0x0202, then one scene to a new id.
	if r := call(0x40, core.CopySceneRequest{CopyAllScenes: true, GroupFrom: 0x0101, GroupTo: 0x0202}).(core.CopySceneResponse); r.Status != im.StatusSuccess {
		t.Fatalf("CopyScene all: %+v", r)
	}
	if r := call(0x40, core.CopySceneRequest{GroupFrom: 0x0101, SceneFrom: 9, GroupTo: 0x0202, SceneTo: 7}).(core.CopySceneResponse); r.Status != im.StatusNotFound {
		t.Fatalf("CopyScene of a missing scene: %+v, want NOT_FOUND", r)
	}
	if r := call(0x40, core.CopySceneRequest{GroupFrom: 0x0999}).(core.CopySceneResponse); r.Status != im.StatusInvalidCommand {
		t.Fatalf("CopyScene from an unknown group: %+v, want INVALID_COMMAND", r)
	}
	m := call(0x06, core.SceneGroupRequest{GroupID: 0x0202}).(core.GetSceneMembershipResponse)
	if len(m.SceneList) != 3 {
		t.Fatalf("membership of 0x0202 after the copy: %v", m.SceneList)
	}
	if r := call(0x02, core.SceneRef{GroupID: 0x0101, SceneID: 3}).(core.SceneStatusResponse); r.Status != im.StatusSuccess {
		t.Fatalf("RemoveScene: %+v", r)
	}
	if r := call(0x02, core.SceneRef{GroupID: 0x0101, SceneID: 3}).(core.SceneStatusResponse); r.Status != im.StatusNotFound {
		t.Fatalf("RemoveScene twice: %+v, want NOT_FOUND", r)
	}
	if r := call(0x02, core.SceneRef{GroupID: 0x0101, SceneID: 255}).(core.SceneStatusResponse); r.Status != im.StatusConstraintError {
		t.Fatalf("RemoveScene 255: %+v", r)
	}
	if r := call(0x03, core.SceneGroupRequest{GroupID: 0x0101}).(core.RemoveAllScenesResponse); r.Status != im.StatusSuccess {
		t.Fatalf("RemoveAllScenes: %+v", r)
	}
	if r := call(0x03, core.SceneGroupRequest{GroupID: 0x0999}).(core.RemoveAllScenesResponse); r.Status != im.StatusInvalidCommand {
		t.Fatalf("RemoveAllScenes of an unknown group: %+v", r)
	}
	if m := call(0x06, core.SceneGroupRequest{GroupID: 0x0999}).(core.GetSceneMembershipResponse); m.Status != im.StatusInvalidCommand {
		t.Fatalf("membership of an unknown group: %+v", m)
	}

	// The current scene loses its validity on a state-changing command,
	// and a fabric's removal takes its scenes and info along.
	call(0x05, core.RecallSceneRequest{GroupID: 0x0202, SceneID: 1})
	info := func() []core.SceneInfoStruct {
		v, _ := s.MatterReadFiltered(ctx, 0x0002)
		return v.([]core.SceneInfoStruct)
	}
	if !info()[0].SceneValid {
		t.Fatal("the recalled scene is not valid")
	}
	st.InvalidateCurrentScene()
	if info()[0].SceneValid {
		t.Fatal("InvalidateCurrentScene left the scene valid")
	}
	st.RemoveScenesForFabric(1)
	if info()[0].SceneCount != 0 {
		t.Fatalf("scenes left after the fabric's removal: %+v", info())
	}
}

// TestScenesCommandGates pins the gates in front of every command: an
// accessing fabric, the request type, a known command, and the cluster's
// read-only attributes and privileges.
func TestScenesCommandGates(t *testing.T) {
	t.Parallel()
	s, _ := scenesWith(t)
	if _, err := s.MatterInvoke(context.Background(), 0x01, core.SceneRef{}); scenesStatus(t, err) != im.StatusUnsupportedAccess {
		t.Errorf("without a fabric: %v", err)
	}
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	for _, cmd := range []uint32{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x40} {
		if _, err := s.MatterInvoke(ctx, cmd, "wrong"); scenesStatus(t, err) != im.StatusInvalidCommand {
			t.Errorf("command 0x%02X with wrong fields: %v", cmd, err)
		}
	}
	if _, err := s.MatterInvoke(ctx, 0x41, nil); err == nil {
		t.Error("an unknown command was accepted")
	}
	if _, err := s.MatterInvoke(ctx, 0x05, core.RecallSceneRequest{SceneID: 3}); scenesStatus(t, err) != im.StatusNotFound {
		t.Errorf("RecallScene of a missing scene: %v", err)
	}
	if _, err := s.MatterInvoke(ctx, 0x05, core.RecallSceneRequest{GroupID: 7}); scenesStatus(t, err) != im.StatusInvalidCommand {
		t.Errorf("RecallScene in an unknown group: %v", err)
	}
	if err := s.MatterWrite(ctx, 0x0001, uint16(1)); err == nil {
		t.Error("SceneTableSize was writable")
	}
	if s.MatterClusterID() != 0x0062 || len(s.MatterAttributes()) == 0 || len(s.MatterReportable()) == 0 {
		t.Error("cluster identity or attribute lists missing")
	}
	if s.MinInvokePrivilege(0x00) != 4 || s.MinInvokePrivilege(0x01) != 3 {
		t.Error("AddScene needs Manage, ViewScene Operate")
	}
	if _, err := core.NewScenesManagement(core.ScenesConfig{}); err == nil {
		t.Error("a server without state was built")
	}
	if _, err := core.LoadScenesState([]byte("not json"), nil); err == nil {
		t.Error("a corrupt table was loaded")
	}
}

// sceneApplier is a ColorControl server that recalls its scene values
// itself (core.SceneValuesApplier, matter.js implementScenes).
type sceneApplier struct {
	sceneColor
	values map[uint32]uint64
	ms     uint32
}

func (s *sceneApplier) MatterApplySceneValues(_ context.Context, values map[uint32]uint64, transitionMs uint32) {
	s.values, s.ms = values, transitionMs
}

// TestScenesRecallThroughTheServersApplier: a server that implements
// SceneValuesApplier receives the scene's non-null values and the
// transition in milliseconds instead of the module's commands.
func TestScenesRecallThroughTheServersApplier(t *testing.T) {
	t.Parallel()
	color := &sceneApplier{sceneColor: sceneColor{mode: 2, mired: 300}}
	s, _ := scenesWith(t, color)
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	s.MatterInvoke(ctx, 0x04, core.SceneRef{SceneID: 1})
	tt := uint32(1500)
	if _, err := s.MatterInvoke(ctx, 0x05, core.RecallSceneRequest{SceneID: 1, TransitionTime: &tt}); err != nil {
		t.Fatal(err)
	}
	if len(color.cmds) != 0 {
		t.Fatalf("commands %v reached a server that applies its scene itself", color.cmds)
	}
	if color.ms != 1500 || color.values[0x0007] != 300 || color.values[0x4001] != 2 || color.values[0x0001] != 100 || color.values[0x4000] != 0x2000 {
		t.Fatalf("applied %v over %d ms", color.values, color.ms)
	}
}

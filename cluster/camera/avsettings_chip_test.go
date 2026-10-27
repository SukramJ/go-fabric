// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// CameraAvSettingsUserLevelManagement: a port of connectedhomeip
// src/app/clusters/camera-av-settings-user-level-management-server/tests/
// TestCameraAvSettingsUserLevelManagementCluster.cpp at 6170af84, by
// meaning. chip's fixture (:99-123) is every feature with five presets on
// a delegate that accepts everything (:55-97, mockPTZ); chip's
// HandleMPTZ… calls are the server's MatterInvoke, chip's
// OnPhysicalMovementComplete the server's PhysicalMovementComplete, and a
// Shutdown and Startup over the same storage a new server built from the
// state the host persisted (cluster/camera doc, Persistence).
//
// Not ported: the four TestMigrateCameraAvSettingsUserLevelManagementMigration
// cases (:885-1035, MigratesMPTZPositionTlv, MigratesMPTZPresetsTlv,
// MigratesDPTZStreamsTlv, OldDataDeletedAfterMigration) — they test chip's
// move from its SafeAttributePersistenceProvider to the attribute storage,
// a storage layout this module never had.
package camera_test

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	"github.com/SukramJ/go-fabric/im"
)

// mockPTZ is MockCameraAvSettingsUserLevelManagementDelegate (:55-97):
// every movement, preset and viewport change succeeds.
type mockPTZ struct{ moves []camera.MPTZ }

func (m *mockPTZ) MptzSetPosition(_ context.Context, target camera.MPTZ) error {
	m.moves = append(m.moves, target)
	return nil
}

func (m *mockPTZ) MptzRelativeMove(_ context.Context, target camera.MPTZ) error {
	m.moves = append(m.moves, target)
	return nil
}

func (m *mockPTZ) MptzMoveToPreset(_ context.Context, _ uint8, target camera.MPTZ) error {
	m.moves = append(m.moves, target)
	return nil
}

func (*mockPTZ) MptzSavePreset(context.Context, uint8) error   { return nil }
func (*mockPTZ) MptzRemovePreset(context.Context, uint8) error { return nil }

func (*mockPTZ) DptzSetViewport(context.Context, uint16, camera.Viewport) error { return nil }

func (*mockPTZ) DptzRelativeMove(context.Context, avdef.DptzRelativeMoveRequest) (camera.Viewport, error) {
	return camera.Viewport{}, nil
}

const testMaxPresets uint8 = 5 // :53

const allFeatures = camera.FeatureDigitalPTZ | camera.FeatureMechanicalPan | camera.FeatureMechanicalTilt |
	camera.FeatureMechanicalZoom | camera.FeatureMechanicalPresets

func newAvSettings(t *testing.T, edit ...func(*camera.AvSettingsConfig)) *camera.AvSettings {
	t.Helper()
	m := &mockPTZ{}
	cfg := camera.AvSettingsConfig{Features: allFeatures, MaxPresets: testMaxPresets, Mechanical: m, Digital: m}
	for _, e := range edit {
		e(&cfg)
	}
	a, err := camera.NewAvSettings(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// restart is chip's Shutdown and Startup over the same storage: a new
// server from the state the host persisted.
func restart(t *testing.T, a *camera.AvSettings) *camera.AvSettings {
	t.Helper()
	pos := a.Position()
	return newAvSettings(t, func(cfg *camera.AvSettingsConfig) {
		cfg.MptzPosition, cfg.MptzPresets, cfg.DptzStreams = &pos, a.Presets(), a.Streams()
	})
}

func invokeAv(a *camera.AvSettings, cmd uint32, req any) error {
	_, err := a.MatterInvoke(context.Background(), cmd, req)
	return err
}

func readAv[T any](t *testing.T, a *camera.AvSettings, attrID uint32) T {
	t.Helper()
	v, ok := a.MatterRead(attrID)
	if !ok {
		t.Fatalf("attribute 0x%04X not readable", attrID)
	}
	got, ok := v.(T)
	if !ok {
		t.Fatalf("attribute 0x%04X is %T", attrID, v)
	}
	return got
}

func wantMovement(t *testing.T, a *camera.AvSettings, want camera.PhysicalMovement) {
	t.Helper()
	if got := camera.PhysicalMovement(readAv[uint8](t, a, avdef.AttrMovementState)); got != want {
		t.Fatalf("MovementState %d, want %d", got, want)
	}
}

func wantPosition(t *testing.T, a *camera.AvSettings, pan, tilt int16, zoom uint8) {
	t.Helper()
	p := readAv[camera.MPTZ](t, a, avdef.AttrMptzPosition)
	if p.Pan == nil || *p.Pan != pan || p.Tilt == nil || *p.Tilt != tilt || p.Zoom == nil || *p.Zoom != zoom {
		t.Fatalf("MPTZPosition %v/%v/%v, want %d/%d/%d", deref(p.Pan), deref(p.Tilt), deref(p.Zoom), pan, tilt, zoom)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func presetsOf(t *testing.T, a *camera.AvSettings) []camera.MPTZPreset {
	t.Helper()
	return readAv[spec.List[camera.MPTZPreset]](t, a, avdef.AttrMptzPresets)
}

func streamsOf(t *testing.T, a *camera.AvSettings) []camera.DPTZStream {
	t.Helper()
	return readAv[spec.List[camera.DPTZStream]](t, a, avdef.AttrDptzStreams)
}

// The mid-points of :223-225: pan 180, tilt 180, zoom 49.
var (
	testPan  = (camera.PanMaxMaxValue - camera.PanMinMinValue) / 2
	testTilt = (camera.TiltMaxMaxValue - camera.TiltMinMinValue) / 2
	testZoom = (camera.ZoomMaxMaxValue - camera.ZoomMinValue) / 2
)

func setPosition(t *testing.T, a *camera.AvSettings) {
	t.Helper()
	pan, tilt, zoom := testPan, testTilt, testZoom
	if err := invokeAv(a, avdef.CmdMptzSetPosition, avdef.MptzSetPositionRequest{Pan: &pan, Tilt: &tilt, Zoom: &zoom}); err != nil {
		t.Fatal(err)
	}
}

func savePreset(t *testing.T, a *camera.AvSettings, id uint8, name string) {
	t.Helper()
	if err := invokeAv(a, avdef.CmdMptzSavePreset, avdef.MptzSavePresetRequest{PresetId: &id, Name: name}); err != nil {
		t.Fatal(err)
	}
}

var fullHD = camera.Viewport{X1: 0, Y1: 0, X2: 1920, Y2: 1080} // :586

func TestChipAvSettings(t *testing.T) { //nolint:gocognit,gocyclo,funlen // one subtest per chip TEST_F
	t.Run("TestAttributes", func(t *testing.T) { // :125-140
		a := newAvSettings(t)
		for _, id := range []uint32{
			avdef.AttrMptzPosition, avdef.AttrMaxPresets, avdef.AttrMptzPresets, avdef.AttrDptzStreams, avdef.AttrZoomMax,
			avdef.AttrTiltMin, avdef.AttrTiltMax, avdef.AttrPanMin, avdef.AttrPanMax, avdef.AttrMovementState,
		} {
			if !slices.Contains(a.MatterAttributes(), id) {
				t.Errorf("attribute 0x%04X not listed", id)
			}
		}
	})
	t.Run("TestCommands", func(t *testing.T) { // :142-154
		want := []uint32{
			avdef.CmdMptzSetPosition, avdef.CmdMptzRelativeMove, avdef.CmdMptzMoveToPreset, avdef.CmdMptzSavePreset,
			avdef.CmdMptzRemovePreset, avdef.CmdDptzSetViewport, avdef.CmdDptzRelativeMove,
		}
		if got := newAvSettings(t).MatterAcceptedCommands(); !slices.Equal(got, want) {
			t.Fatalf("accepted %v, want %v", got, want)
		}
	})
	t.Run("ReadAllAttributesWithClusterTesterTest", func(t *testing.T) { // :156-213
		a := newAvSettings(t)
		if got := readAv[uint8](t, a, avdef.AttrMaxPresets); got != testMaxPresets {
			t.Fatalf("MaxPresets %d", got)
		}
		zoomMax := readAv[uint8](t, a, avdef.AttrZoomMax)
		tiltMin, tiltMax := readAv[int16](t, a, avdef.AttrTiltMin), readAv[int16](t, a, avdef.AttrTiltMax)
		panMin, panMax := readAv[int16](t, a, avdef.AttrPanMin), readAv[int16](t, a, avdef.AttrPanMax)
		if zoomMax < camera.ZoomMaxMinValue || zoomMax > camera.ZoomMaxMaxValue ||
			tiltMin < camera.TiltMinMinValue || tiltMin > camera.TiltMinMaxValue ||
			tiltMax < camera.TiltMaxMinValue || tiltMax > camera.TiltMaxMaxValue ||
			panMin < camera.PanMinMinValue || panMin > camera.PanMinMaxValue ||
			panMax < camera.PanMaxMinValue || panMax > camera.PanMaxMaxValue {
			t.Fatalf("limits zoom %d tilt %d..%d pan %d..%d", zoomMax, tiltMin, tiltMax, panMin, panMax)
		}
		p := readAv[camera.MPTZ](t, a, avdef.AttrMptzPosition)
		if *p.Pan < panMin || *p.Pan > panMax || *p.Tilt < tiltMin || *p.Tilt > tiltMax || *p.Zoom < camera.ZoomMinValue || *p.Zoom > zoomMax {
			t.Fatalf("MPTZPosition %d/%d/%d outside the limits", *p.Pan, *p.Tilt, *p.Zoom)
		}
		wantMovement(t, a, camera.MovementIdle)
		if len(presetsOf(t, a)) != 0 || len(streamsOf(t, a)) != 0 {
			t.Fatal("presets or streams at start-up")
		}
	})
	t.Run("ExecuteMPTZSetPositionCommandTest", func(t *testing.T) { // :215-263
		a := newAvSettings(t)
		setPosition(t, a)
		wantMovement(t, a, camera.MovementMoving)
		a.PhysicalMovementComplete(nil)
		wantPosition(t, a, testPan, testTilt, testZoom)
		wantMovement(t, a, camera.MovementIdle)
	})
	t.Run("ExecuteMPTZRelativeMoveCommandTest", func(t *testing.T) { // :265-315
		a := newAvSettings(t)
		start := readAv[camera.MPTZ](t, a, avdef.AttrMptzPosition)
		dp, dt, dz := int16(10), int16(10), int8(10)
		if err := invokeAv(a, avdef.CmdMptzRelativeMove, avdef.MptzRelativeMoveRequest{PanDelta: &dp, TiltDelta: &dt, ZoomDelta: &dz}); err != nil {
			t.Fatal(err)
		}
		wantMovement(t, a, camera.MovementMoving)
		a.PhysicalMovementComplete(nil)
		wantPosition(t, a, *start.Pan+dp, *start.Tilt+dt, *start.Zoom+uint8(dz))
		wantMovement(t, a, camera.MovementIdle)
	})
	t.Run("ExecuteMPTZSavePresetCommandTest", func(t *testing.T) { // :317-367
		a := newAvSettings(t)
		if len(presetsOf(t, a)) != 0 {
			t.Fatal("presets at start-up")
		}
		savePreset(t, a, 2, "DefaultPreset")
		got := presetsOf(t, a)
		if len(got) != 1 || got[0].Name != "DefaultPreset" || got[0].PresetId != 2 {
			t.Fatalf("MPTZPresets %+v", got)
		}
		if id := a.NextPresetID(); id != 1 { // :365-366
			t.Fatalf("next preset id %d, want 1", id)
		}
	})
	t.Run("ExecuteMPTZMoveToPresetCommandTest", func(t *testing.T) { // :369-495
		a := newAvSettings(t)
		move := avdef.MptzMoveToPresetRequest{PresetId: 2}
		if got := statusOf(invokeAv(a, avdef.CmdMptzMoveToPreset, move)); got != im.StatusNotFound {
			t.Fatalf("MoveToPreset without presets: %s", got)
		}
		savePreset(t, a, 2, "DefaultPreset")
		preset := presetsOf(t, a)[0].Settings
		setPosition(t, a)
		a.PhysicalMovementComplete(nil)
		wantPosition(t, a, testPan, testTilt, testZoom)
		if err := invokeAv(a, avdef.CmdMptzMoveToPreset, move); err != nil {
			t.Fatal(err)
		}
		wantMovement(t, a, camera.MovementMoving)
		a.PhysicalMovementComplete(nil)
		wantMovement(t, a, camera.MovementIdle)
		wantPosition(t, a, *preset.Pan, *preset.Tilt, *preset.Zoom)
	})
	t.Run("ExecuteMPTZRemovePresetCommandTest", func(t *testing.T) { // :497-568
		a := newAvSettings(t)
		remove := avdef.MptzRemovePresetRequest{PresetId: 2}
		if got := statusOf(invokeAv(a, avdef.CmdMptzRemovePreset, remove)); got != im.StatusNotFound {
			t.Fatalf("RemovePreset without presets: %s", got)
		}
		savePreset(t, a, 2, "DefaultPreset")
		if len(presetsOf(t, a)) != 1 {
			t.Fatal("no preset saved")
		}
		if err := invokeAv(a, avdef.CmdMptzRemovePreset, remove); err != nil {
			t.Fatal(err)
		}
		if len(presetsOf(t, a)) != 0 {
			t.Fatal("preset not removed")
		}
	})
	t.Run("ExecuteDPTZSetViewportCommandTest", func(t *testing.T) { // :570-647
		a := newAvSettings(t)
		req := avdef.DptzSetViewportRequest{VideoStreamId: 1, Viewport: fullHD}
		if got := statusOf(invokeAv(a, avdef.CmdDptzSetViewport, req)); got != im.StatusNotFound {
			t.Fatalf("SetViewport without streams: %s", got)
		}
		a.AddMoveCapableVideoStream(1, fullHD)
		if got := streamsOf(t, a); len(got) != 1 || got[0] != (camera.DPTZStream{VideoStreamId: 1, Viewport: fullHD}) {
			t.Fatalf("DPTZStreams %+v", got)
		}
		hd := camera.Viewport{X1: 0, Y1: 0, X2: 1280, Y2: 720} // :620
		req.Viewport = hd
		if err := invokeAv(a, avdef.CmdDptzSetViewport, req); err != nil {
			t.Fatal(err)
		}
		if got := streamsOf(t, a); len(got) != 1 || got[0].Viewport != hd {
			t.Fatalf("DPTZStreams %+v", got)
		}
	})
	t.Run("ExecuteDPTZRelativeMoveCommandTest", func(t *testing.T) { // :649-719
		a := newAvSettings(t)
		dx, dy, dz, dzFail := int16(100), int16(100), int8(-50), int8(120)
		req := avdef.DptzRelativeMoveRequest{VideoStreamId: 1, DeltaX: &dx, DeltaY: &dy, ZoomDelta: &dz}
		if got := statusOf(invokeAv(a, avdef.CmdDptzRelativeMove, req)); got != im.StatusNotFound {
			t.Fatalf("RelativeMove without streams: %s", got)
		}
		a.AddMoveCapableVideoStream(1, fullHD)
		if err := invokeAv(a, avdef.CmdDptzRelativeMove, req); err != nil {
			t.Fatal(err)
		}
		req.ZoomDelta = &dzFail
		if got := statusOf(invokeAv(a, avdef.CmdDptzRelativeMove, req)); got != im.StatusConstraintError {
			t.Fatalf("RelativeMove with ZoomDelta 120: %s", got)
		}
	})
	t.Run("ExecuteMPTZPositionPersistenceTest", func(t *testing.T) { // :721-753
		a := newAvSettings(t)
		pan, tilt, zoom := testPan, testTilt, testZoom
		a.SetPosition(&pan, &tilt, &zoom)
		wantPosition(t, a, testPan, testTilt, testZoom)
		wantPosition(t, restart(t, a), testPan, testTilt, testZoom)
	})
	t.Run("ExecuteMPTZPresetsPersistenceTest", func(t *testing.T) { // :755-823
		a := newAvSettings(t)
		savePreset(t, a, 2, "DefaultPreset")
		check := func(got []camera.MPTZPreset) {
			t.Helper()
			if len(got) != 1 || got[0].Name != "DefaultPreset" || got[0].PresetId != 2 {
				t.Fatalf("MPTZPresets %+v", got)
			}
			s := got[0].Settings
			if *s.Pan != camera.DefaultPan || *s.Tilt != camera.DefaultTilt || *s.Zoom != camera.DefaultZoom {
				t.Fatalf("preset settings %d/%d/%d", *s.Pan, *s.Tilt, *s.Zoom)
			}
		}
		check(presetsOf(t, a))
		check(presetsOf(t, restart(t, a)))
	})
	t.Run("ExecuteDPTZStreamsPersistenceTest", func(t *testing.T) { // :825-879
		a := newAvSettings(t)
		a.AddMoveCapableVideoStream(1, fullHD)
		want := []camera.DPTZStream{{VideoStreamId: 1, Viewport: fullHD}}
		if got := streamsOf(t, a); !slices.Equal(got, want) {
			t.Fatalf("DPTZStreams %+v", got)
		}
		if got := streamsOf(t, restart(t, a)); !slices.Equal(got, want) {
			t.Fatalf("DPTZStreams after the restart %+v", got)
		}
	})
}

// TestAvSettingsRules holds chip's rules no chip test case reaches: the
// feature gates and limits of MPTZSetPosition (:733-780), BUSY while a
// movement runs (:788), the clamp of MPTZRelativeMove (:849-857), the
// preset id range and RESOURCE_EXHAUSTED (:1044, :1068), the next free
// preset id (:593-629) and its start past a restored preset
// (skipTakenPresetID), a failed movement (:1276-1283), the start-up
// feature check (:59-64) and the ports a feature needs.
func TestAvSettingsRules(t *testing.T) { //nolint:funlen // one subtest per rule
	t.Run("MPTZSetPosition", func(t *testing.T) {
		a := newAvSettings(t, func(cfg *camera.AvSettingsConfig) { cfg.Features = camera.FeatureMechanicalPan })
		tilt, pan, far := int16(1), int16(10), int16(181)
		if got := statusOf(invokeAv(a, avdef.CmdMptzSetPosition, avdef.MptzSetPositionRequest{Tilt: &tilt})); got != im.StatusInvalidCommand {
			t.Fatalf("Tilt without MTILT: %s", got)
		}
		if got := statusOf(invokeAv(a, avdef.CmdMptzSetPosition, avdef.MptzSetPositionRequest{Pan: &far})); got != im.StatusConstraintError {
			t.Fatalf("Pan 181: %s", got)
		}
		if got := statusOf(invokeAv(a, avdef.CmdMptzSetPosition, avdef.MptzSetPositionRequest{})); got != im.StatusInvalidCommand {
			t.Fatalf("no value: %s", got)
		}
		if err := invokeAv(a, avdef.CmdMptzSetPosition, avdef.MptzSetPositionRequest{Pan: &pan}); err != nil {
			t.Fatal(err)
		}
		if got := statusOf(invokeAv(a, avdef.CmdMptzSetPosition, avdef.MptzSetPositionRequest{Pan: &pan})); got != im.StatusBusy {
			t.Fatalf("while moving: %s", got)
		}
		a.PhysicalMovementComplete(spec.Errorf(im.StatusFailure, "stalled"))
		if p := a.Position(); *p.Pan != camera.DefaultPan || a.MovementState() != camera.MovementIdle {
			t.Fatalf("after a failed movement: pan %d, state %d", *p.Pan, a.MovementState())
		}
	})
	t.Run("MPTZRelativeMove clamps to the limits", func(t *testing.T) {
		a := newAvSettings(t, func(cfg *camera.AvSettingsConfig) { cfg.PanMax = ptr(int16(20)) })
		d, tooFar := int16(50), int16(201)
		if got := statusOf(invokeAv(a, avdef.CmdMptzRelativeMove, avdef.MptzRelativeMoveRequest{PanDelta: &tooFar})); got != im.StatusConstraintError {
			t.Fatalf("PanDelta beyond the range: %s", got)
		}
		if err := invokeAv(a, avdef.CmdMptzRelativeMove, avdef.MptzRelativeMoveRequest{PanDelta: &d}); err != nil {
			t.Fatal(err)
		}
		a.PhysicalMovementComplete(nil)
		if p := a.Position(); *p.Pan != 20 {
			t.Fatalf("pan %d, want the PanMax 20", *p.Pan)
		}
	})
	t.Run("presets", func(t *testing.T) {
		a := newAvSettings(t, func(cfg *camera.AvSettingsConfig) { cfg.MaxPresets = 2 })
		zero := uint8(0)
		if got := statusOf(invokeAv(a, avdef.CmdMptzSavePreset, avdef.MptzSavePresetRequest{PresetId: &zero, Name: "x"})); got != im.StatusConstraintError {
			t.Fatalf("preset 0: %s", got)
		}
		for range 2 {
			if err := invokeAv(a, avdef.CmdMptzSavePreset, avdef.MptzSavePresetRequest{Name: "auto"}); err != nil {
				t.Fatal(err)
			}
		}
		if got := presetsOf(t, a); len(got) != 2 || got[0].PresetId != 1 || got[1].PresetId != 2 {
			t.Fatalf("MPTZPresets %+v", got)
		}
		if got := statusOf(invokeAv(a, avdef.CmdMptzSavePreset, avdef.MptzSavePresetRequest{Name: "third"})); got != im.StatusResourceExhausted {
			t.Fatalf("a third preset: %s", got)
		}
		savePreset(t, a, 1, "renamed") // an update needs no free slot
		if got := presetsOf(t, a); len(got) != 2 || got[0].Name != "renamed" {
			t.Fatalf("MPTZPresets %+v", got)
		}
	})
	t.Run("a restored preset holds the next id", func(t *testing.T) {
		a := newAvSettings(t, func(cfg *camera.AvSettingsConfig) {
			cfg.MptzPresets = []camera.MPTZPreset{{PresetId: 1, Name: "door", Settings: camera.MPTZ{Pan: ptr(int16(5))}}}
		})
		if id := a.NextPresetID(); id != 2 {
			t.Fatalf("next preset id %d, want 2", id)
		}
	})
	t.Run("construction", func(t *testing.T) {
		for _, c := range []struct {
			name string
			cfg  camera.AvSettingsConfig
		}{
			{"no movement feature", camera.AvSettingsConfig{Features: camera.FeatureMechanicalPresets, Mechanical: &mockPTZ{}}},
			{"MPAN without a MechanicalPTZ", camera.AvSettingsConfig{Features: camera.FeatureMechanicalPan}},
			{"DPTZ without a DigitalPTZ", camera.AvSettingsConfig{Features: camera.FeatureDigitalPTZ}},
		} {
			if _, err := camera.NewAvSettings(c.cfg); err == nil {
				t.Errorf("%s: built", c.name)
			}
		}
	})
}

func ptr[T any](v T) *T { return &v }

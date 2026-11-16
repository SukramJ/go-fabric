// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/camera"
	"github.com/SukramJ/go-fabric/cluster/spec"
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	"github.com/SukramJ/go-fabric/im"
)

// TestAvSettingsPresetsHeldToMaxPresets: a host's MPTZPresets longer than
// MaxPresets is refused, and the server's presets and the attribute stay
// as they were. Before, SetAttributes answered nil, Presets() held the
// three presets and the attribute kept its old value — the publication's
// error was dropped (review II, R2-P6-store-1608, R2-P6-err-1611).
func TestAvSettingsPresetsHeldToMaxPresets(t *testing.T) {
	t.Parallel()
	a := newAvSettings(t, func(c *camera.AvSettingsConfig) { c.MaxPresets = 2 })
	three := []camera.MPTZPreset{{PresetId: 1, Name: "a"}, {PresetId: 2, Name: "b"}, {PresetId: 3, Name: "c"}}
	if err := a.SetAttributes(map[uint32]any{avdef.AttrMptzPresets: three}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("3 presets with MaxPresets 2: %v, want ErrInvalidValue", err)
	}
	if n := len(a.Presets()); n != 0 {
		t.Errorf("Presets() holds %d after the refusal", n)
	}
	if l := readAv[spec.List[camera.MPTZPreset]](t, a, avdef.AttrMptzPresets); len(l) != 0 {
		t.Errorf("MPTZPresets = %v after the refusal", l)
	}
	// Full is full: with MaxPresets presets held a save under a new id
	// is RESOURCE_EXHAUSTED.
	if err := a.SetAttributes(map[uint32]any{avdef.AttrMptzPresets: three[:2]}); err != nil {
		t.Fatal(err)
	}
	if err := invokeAv(a, avdef.CmdMptzSavePreset, avdef.MptzSavePresetRequest{Name: "c"}); statusOf(err) != im.StatusResourceExhausted {
		t.Errorf("save with every preset taken: %v, want RESOURCE_EXHAUSTED", err)
	}
}

// TestCameraTablesAreHandedOutAsDeepCopies is the package's guard of
// decision 142: a caller that edits through a pointer or a list of an
// entry a getter returned changes nothing the server serves. Before, the
// getters returned slices.Clone of the tables, whose entries shared their
// lists and optional fields with the server's (review II, R2-P6-err-1460's
// class).
func TestCameraTablesAreHandedOutAsDeepCopies(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t, nil)
	video := []uint16{1}
	if err := p.UpsertSession(camera.ProviderSession{Id: 1, FabricIndex: 1, VideoStreams: &video}); err != nil {
		t.Fatal(err)
	}
	video[0] = 9 // the list handed in
	(*p.Sessions()[0].VideoStreams)[0] = 8
	if got := (*p.Sessions()[0].VideoStreams)[0]; got != 1 {
		t.Errorf("Provider session stream %d, want 1", got)
	}

	push, _ := newPush(t)
	allocatePush(t, push, motionOptions())
	push.Connections()[0].TransportOptions.Url = "https://changed.example/"
	if got := push.Connections()[0].TransportOptions.Url; got == "https://changed.example/" {
		t.Error("PushTransport connection changed through Connections()")
	}

	a := newAvSettings(t)
	pan := int16(5)
	if err := a.SetAttributes(map[uint32]any{avdef.AttrMptzPresets: []camera.MPTZPreset{{PresetId: 1, Name: "a", Settings: camera.MPTZ{Pan: &pan}}}}); err != nil {
		t.Fatal(err)
	}
	pan = 6
	*a.Presets()[0].Settings.Pan = 7
	*a.Position().Pan = 7
	if got := *a.Presets()[0].Settings.Pan; got != 5 {
		t.Errorf("preset Pan %d, want 5", got)
	}
	if got := a.Position().Pan; got != nil && *got == 7 {
		t.Error("MPTZPosition changed through Position()")
	}
}

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

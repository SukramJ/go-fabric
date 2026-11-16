// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/media"
	"github.com/SukramJ/go-fabric/cluster/spec"
)

// TestConstructorsRefuseAnUndefinedSelection: every media server whose
// configuration selects features, attributes or events refuses a selection
// its definition does not define, with the definition's error, before it
// builds any state.
func TestConstructorsRefuseAnUndefinedSelection(t *testing.T) {
	t.Parallel()
	const bit = 1 << 30
	unknown := []uint32{0xFFF0}
	for name, c := range map[string]struct {
		build func() error
		want  error
	}{
		"AccountLogin": {func() error {
			_, err := media.NewAccountLogin(media.AccountLoginConfig{Port: &login{}, Events: unknown})
			return err
		}, spec.ErrUnknownElement},
		"AppBasic": {func() error {
			_, err := media.NewAppBasic(media.AppBasicConfig{Attributes: unknown})
			return err
		}, spec.ErrUnknownElement},
		"AppLauncher": {func() error {
			_, err := media.NewAppLauncher(media.AppLauncherConfig{Port: &platform{}, Features: bit})
			return err
		}, spec.ErrUnknownFeature},
		"Channels": {func() error {
			_, err := media.NewChannels(media.ChannelsConfig{Port: &basicTuner{}, Features: bit})
			return err
		}, spec.ErrUnknownFeature},
		"ContentControl": {func() error {
			_, err := media.NewContentControl(media.ContentControlConfig{Port: &parental{}, Features: bit})
			return err
		}, spec.ErrUnknownFeature},
		"ContentLauncher": {func() error {
			_, err := media.NewContentLauncher(media.ContentLauncherConfig{Port: &launcher{}, Features: bit})
			return err
		}, spec.ErrUnknownFeature},
		"Inputs": {func() error {
			_, err := media.NewInputs(media.InputsConfig{Port: &inputs{}, Features: bit})
			return err
		}, spec.ErrUnknownFeature},
		"Keypad": {func() error {
			_, err := media.NewKeypad(media.KeypadConfig{Port: &keypad{}, Features: bit})
			return err
		}, spec.ErrUnknownFeature},
		"Outputs": {func() error {
			_, err := media.NewOutputs(media.OutputsConfig{Port: &outputs{}, Features: bit})
			return err
		}, spec.ErrUnknownFeature},
		"Playback": {func() error {
			_, err := media.NewPlayback(media.PlaybackConfig{Port: &basicTransport{}, Features: bit})
			return err
		}, spec.ErrUnknownFeature},
		"Targets": {func() error {
			_, err := media.NewTargets(media.TargetsConfig{Port: targets{}, Attributes: unknown})
			return err
		}, spec.ErrUnknownElement},
		"WakeOnLan": {func() error {
			_, err := media.NewWakeOnLan(media.WakeOnLanConfig{Attributes: unknown})
			return err
		}, spec.ErrUnknownElement},
	} {
		if err := c.build(); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/spec"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/contract"
)

// fullColorControl is a ColorControl server with every feature, under
// its typed server.
func fullColorControl(t *testing.T) (*ccdef.Server, *light.ColorControlServer) {
	t.Helper()
	srv, err := light.NewColorControl(light.ColorControlServerConfig{
		Features: light.ColorFeatureHueSaturation | light.ColorFeatureEnhancedHue | light.ColorFeatureColorLoop |
			light.ColorFeatureXY | light.ColorFeatureColorTemperature,
		MinMireds: 153, MaxMireds: 500, InitialMireds: 250,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ccdef.Wrap(srv), srv
}

// TestColorControlSetStartUpMireds: StartUpColorTemperatureMireds is the
// host's to set, a number or null; setting the value it holds reports
// nothing.
func TestColorControlSetStartUpMireds(t *testing.T) {
	t.Parallel()
	s, srv := fullColorControl(t)
	var changes [][]uint32
	defer s.OnChanged(func(_ ccdef.State, change contract.Change) { ids := change.Attributes; changes = append(changes, ids) })()

	set := func(v any) error {
		return srv.SetAttributes(map[uint32]any{ccdef.AttrStartUpColorTemperatureMireds: v})
	}
	if err := set(uint16(300)); err != nil {
		t.Fatal(err)
	}
	if got := s.Get().StartUpColorTemperatureMireds; got == nil || *got != 300 {
		t.Errorf("StartUpColorTemperatureMireds %v, want 300", got)
	}
	n := len(changes)
	if err := set(uint16(300)); err != nil || len(changes) != n {
		t.Errorf("the same value: %v, changes %v", err, changes)
	}
	if err := set(nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Get().StartUpColorTemperatureMireds; got != nil {
		t.Errorf("StartUpColorTemperatureMireds %v, want null", *got)
	}
	if len(changes) != n+1 {
		t.Errorf("changes %v, want one more report for null", changes)
	}
}

// TestColorControlSetColourModes: EnhancedColorMode alone sets ColorMode
// to its plain mode, both together are stored as given, and the 16-bit
// colour attributes are the host's to set.
func TestColorControlSetColourModes(t *testing.T) {
	t.Parallel()
	s, srv := fullColorControl(t)
	if err := srv.SetAttributes(map[uint32]any{
		ccdef.AttrEnhancedColorMode:  uint8(light.ColorModeEnhancedHueSaturation),
		ccdef.AttrEnhancedCurrentHue: uint16(40000),
	}); err != nil {
		t.Fatal(err)
	}
	st := s.Get()
	if st.EnhancedColorMode != ccdef.EnhancedColorModeEnhancedCurrentHueAndCurrentSaturation ||
		st.ColorMode != ccdef.ColorModeCurrentHueAndCurrentSaturation || st.EnhancedCurrentHue != 40000 {
		t.Errorf("after EnhancedColorMode alone: %+v", st)
	}
	if err := srv.SetAttributes(map[uint32]any{
		ccdef.AttrColorMode:         uint8(light.ColorModeXY),
		ccdef.AttrEnhancedColorMode: uint8(light.ColorModeXY),
		ccdef.AttrCurrentX:          uint16(20000),
		ccdef.AttrCurrentY:          uint16(21000),
		ccdef.AttrColorLoopTime:     uint16(30),
	}); err != nil {
		t.Fatal(err)
	}
	st = s.Get()
	if st.ColorMode != ccdef.ColorModeCurrentXAndCurrentY || st.CurrentX != 20000 || st.CurrentY != 21000 || st.ColorLoopTime != 30 {
		t.Errorf("after both modes: %+v", st)
	}
}

// TestColorControlSetRefusesAModeWithoutItsFeature: a colour mode of a
// feature the server lacks is an invalid value, and nothing is stored.
func TestColorControlSetRefusesAModeWithoutItsFeature(t *testing.T) {
	t.Parallel()
	s := typedColorControl(t) // HS and CT only
	before := s.Get()
	err := s.Underlying().(*light.ColorControlServer).SetAttributes(map[uint32]any{ccdef.AttrColorMode: uint8(light.ColorModeXY)})
	if !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("ColorMode XY without XY: %v, want ErrInvalidValue", err)
	}
	if after := s.Get(); after.ColorMode != before.ColorMode {
		t.Errorf("ColorMode moved to %v", after.ColorMode)
	}
}

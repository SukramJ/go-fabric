// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/light"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// TestColorControlFieldShapes: every command takes its fields as the
// generated request (value or pointer), as the generic tag map an
// in-process caller hands over, and — for the commands cluster/wire has a
// struct for — as that struct; any other shape is INVALID_COMMAND, and a
// tag map value outside its field's type reads as 0.
func TestColorControlFieldShapes(t *testing.T) {
	t.Parallel()
	u := func(v uint64) uint64 { return v }
	cases := []struct {
		cmd    uint32
		typed  any
		ptr    any
		tagMap map[uint8]any
		legacy any
	}{
		{
			ccdef.CmdMoveToHue,
			ccdef.MoveToHueRequest{Hue: 5},
			&ccdef.MoveToHueRequest{Hue: 5},
			map[uint8]any{0: u(5), 1: u(0), 2: u(0), 3: u(0), 4: u(0)},
			wire.MoveToHueRequest{Hue: 5},
		},
		{
			ccdef.CmdMoveHue,
			ccdef.MoveHueRequest{MoveMode: 1, Rate: 1},
			&ccdef.MoveHueRequest{MoveMode: 3, Rate: 1},
			map[uint8]any{0: u(0), 1: u(0), 2: u(0), 3: u(0)},
			wire.MoveHueRequest{MoveMode: 1, Rate: 1},
		},
		{
			ccdef.CmdStepHue,
			ccdef.StepHueRequest{StepMode: 1, StepSize: 1},
			&ccdef.StepHueRequest{StepMode: 3, StepSize: 1},
			map[uint8]any{0: u(1), 1: u(1), 2: u(0), 3: u(0), 4: u(0)},
			wire.StepHueRequest{StepMode: 1, StepSize: 1},
		},
		{
			ccdef.CmdMoveToSaturation,
			ccdef.MoveToSaturationRequest{Saturation: 3},
			&ccdef.MoveToSaturationRequest{},
			map[uint8]any{0: u(3), 1: u(0), 2: u(0), 3: u(0)},
			wire.MoveToSaturationRequest{Saturation: 4},
		},
		{
			ccdef.CmdMoveSaturation,
			ccdef.MoveSaturationRequest{MoveMode: 1, Rate: 1},
			&ccdef.MoveSaturationRequest{},
			map[uint8]any{0: u(3), 1: u(5), 2: u(0), 3: u(0)},
			wire.MoveSaturationRequest{MoveMode: 3, Rate: 2},
		},
		{
			ccdef.CmdStepSaturation,
			ccdef.StepSaturationRequest{StepMode: 1, StepSize: 1},
			&ccdef.StepSaturationRequest{StepMode: 3, StepSize: 1},
			map[uint8]any{0: u(1), 1: u(1), 2: u(0), 3: u(0), 4: u(0)},
			wire.StepSaturationRequest{StepMode: 3, StepSize: 2},
		},
		{
			ccdef.CmdMoveToHueAndSaturation,
			ccdef.MoveToHueAndSaturationRequest{Hue: 1},
			&ccdef.MoveToHueAndSaturationRequest{},
			map[uint8]any{0: u(2), 1: u(2), 2: u(0), 3: u(0), 4: u(0)},
			wire.MoveToHueAndSaturationRequest{Hue: 3},
		},
		{
			ccdef.CmdMoveToColor,
			ccdef.MoveToColorRequest{ColorX: 1},
			&ccdef.MoveToColorRequest{},
			map[uint8]any{0: u(2), 1: u(2), 2: u(0), 3: u(0), 4: u(0)},
			nil,
		},
		{
			ccdef.CmdMoveColor,
			ccdef.MoveColorRequest{RateX: -1},
			&ccdef.MoveColorRequest{},
			map[uint8]any{0: int64(-5), 1: 3, 2: u(0), 3: u(0)},
			nil,
		},
		{
			ccdef.CmdStepColor,
			ccdef.StepColorRequest{StepY: -1},
			&ccdef.StepColorRequest{StepX: 1},
			map[uint8]any{0: int16(4), 1: int8(-2), 2: int32(0), 3: u(0), 4: u(0)},
			nil,
		},
		{
			ccdef.CmdMoveToColorTemperature,
			ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 200},
			&ccdef.MoveToColorTemperatureRequest{},
			map[uint8]any{0: u(300), 1: u(0), 2: u(0), 3: u(0)},
			wire.MoveToColorTemperatureRequest{ColorTemperatureMireds: 250},
		},
		{
			ccdef.CmdEnhancedMoveToHue,
			ccdef.EnhancedMoveToHueRequest{EnhancedHue: 7},
			&ccdef.EnhancedMoveToHueRequest{},
			map[uint8]any{0: u(8), 1: u(1), 2: u(0), 3: u(0), 4: u(0)},
			nil,
		},
		{
			ccdef.CmdEnhancedMoveHue,
			ccdef.EnhancedMoveHueRequest{MoveMode: 0},
			&ccdef.EnhancedMoveHueRequest{MoveMode: 1, Rate: 9},
			map[uint8]any{0: u(0), 1: u(0), 2: u(0), 3: u(0)},
			nil,
		},
		{
			ccdef.CmdEnhancedStepHue,
			ccdef.EnhancedStepHueRequest{StepMode: 1, StepSize: 9},
			&ccdef.EnhancedStepHueRequest{StepMode: 3, StepSize: 9},
			map[uint8]any{0: u(1), 1: u(9), 2: u(0), 3: u(0), 4: u(0)},
			nil,
		},
		{
			ccdef.CmdEnhancedMoveToHueAndSaturation,
			ccdef.EnhancedMoveToHueAndSaturationRequest{EnhancedHue: 9},
			&ccdef.EnhancedMoveToHueAndSaturationRequest{},
			map[uint8]any{0: u(9), 1: u(9), 2: u(0), 3: u(0), 4: u(0)},
			nil,
		},
		{
			ccdef.CmdColorLoopSet,
			ccdef.ColorLoopSetRequest{UpdateFlags: ccdef.UpdateFlagsUpdateStartHue, StartHue: 5},
			&ccdef.ColorLoopSetRequest{},
			map[uint8]any{0: u(4), 1: u(0), 2: u(1), 3: u(30), 4: u(0), 5: u(0), 6: u(0)},
			nil,
		},
		{ccdef.CmdStopMoveStep, ccdef.StopMoveStepRequest{}, &ccdef.StopMoveStepRequest{}, map[uint8]any{0: u(0), 1: u(0)}, nil},
		{
			ccdef.CmdMoveColorTemperature,
			ccdef.MoveColorTemperatureRequest{MoveMode: 1, Rate: 5},
			&ccdef.MoveColorTemperatureRequest{},
			map[uint8]any{0: u(0), 1: u(0), 2: u(0), 3: u(0), 4: u(0), 5: u(0)},
			nil,
		},
		{
			ccdef.CmdStepColorTemperature,
			ccdef.StepColorTemperatureRequest{StepMode: 1, StepSize: 5},
			&ccdef.StepColorTemperatureRequest{StepMode: 3, StepSize: 5},
			map[uint8]any{0: u(1), 1: u(5), 2: u(0), 3: u(0), 4: u(0), 5: u(0), 6: u(0)},
			nil,
		},
	}
	srv := light.NewColorControlServer(light.ColorControlServerConfig{Features: allColorFeatures})
	for _, tc := range cases {
		for _, fields := range []any{tc.typed, tc.ptr, tc.tagMap, tc.legacy} {
			if fields == nil {
				continue
			}
			if _, err := srv.MatterInvoke(context.Background(), tc.cmd, fields); err != nil {
				t.Errorf("command 0x%02X with %T: %v", tc.cmd, fields, err)
			}
		}
		if _, err := srv.MatterInvoke(context.Background(), tc.cmd, "fields"); !statusIs(err, im.StatusInvalidCommand) {
			t.Errorf("command 0x%02X with a string: %v, want INVALID_COMMAND", tc.cmd, err)
		}
	}
	// A StepHue in cluster/wire's struct whose TransitionTime does not
	// fit the generated uint8 field is refused, not truncated.
	if _, err := srv.MatterInvoke(context.Background(), ccdef.CmdStepHue, wire.StepHueRequest{StepMode: 1, StepSize: 1, TransitionTime: 300}); !statusIs(err, im.StatusInvalidCommand) {
		t.Errorf("StepHue TransitionTime 300: %v", err)
	}
	// A tag-map value wider than its field reads as 0.
	if _, err := srv.MatterInvoke(context.Background(), ccdef.CmdMoveToColor,
		map[uint8]any{0: uint64(1 << 20), 1: int64(-1), 2: uint64(0), 3: uint64(0), 4: uint64(0)}); err != nil {
		t.Fatal(err)
	}
	if c := srv.Color(); c.X != 0 || c.Y != 0 {
		t.Errorf("over-wide tag map values gave %d/%d, want 0/0", c.X, c.Y)
	}
	if _, err := srv.MatterInvoke(context.Background(), ccdef.CmdMoveColor,
		map[uint8]any{0: uint64(1 << 63), 1: int64(40000), 2: uint64(0), 3: uint64(0)}); err != nil {
		t.Fatal(err)
	}
}

// TestColorControlStopMoveStepWithoutFields: StopMoveStep without a
// payload uses no options.
func TestColorControlStopMoveStepWithoutFields(t *testing.T) {
	t.Parallel()
	srv := light.NewColorControlServer(light.DefaultColorControlServerConfig())
	if _, err := srv.MatterInvoke(context.Background(), ccdef.CmdStopMoveStep, nil); err != nil {
		t.Fatal(err)
	}
}

// TestColorControlRefusedModeSwitch: a ColorWriter that refuses the
// converted colour of a mode switch fails every command that switches,
// and the state keeps its mode.
func TestColorControlRefusedModeSwitch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		start light.ColorMode
		cmd   uint32
		req   any
	}{
		{light.ColorModeXY, ccdef.CmdMoveToHue, ccdef.MoveToHueRequest{Hue: 1}},
		{light.ColorModeXY, ccdef.CmdMoveHue, ccdef.MoveHueRequest{MoveMode: 1, Rate: 1}},
		{light.ColorModeXY, ccdef.CmdStepHue, ccdef.StepHueRequest{StepMode: 1, StepSize: 1}},
		{light.ColorModeXY, ccdef.CmdMoveToSaturation, ccdef.MoveToSaturationRequest{Saturation: 1}},
		{light.ColorModeXY, ccdef.CmdMoveSaturation, ccdef.MoveSaturationRequest{MoveMode: 1, Rate: 1}},
		{light.ColorModeXY, ccdef.CmdStepSaturation, ccdef.StepSaturationRequest{StepMode: 1, StepSize: 1}},
		{light.ColorModeXY, ccdef.CmdMoveToHueAndSaturation, ccdef.MoveToHueAndSaturationRequest{Hue: 1}},
		{light.ColorModeXY, ccdef.CmdEnhancedMoveToHue, ccdef.EnhancedMoveToHueRequest{EnhancedHue: 1}},
		{light.ColorModeXY, ccdef.CmdEnhancedMoveHue, ccdef.EnhancedMoveHueRequest{MoveMode: 1, Rate: 1}},
		{light.ColorModeXY, ccdef.CmdEnhancedStepHue, ccdef.EnhancedStepHueRequest{StepMode: 1, StepSize: 1}},
		{light.ColorModeXY, ccdef.CmdEnhancedMoveToHueAndSaturation, ccdef.EnhancedMoveToHueAndSaturationRequest{EnhancedHue: 1}},
		{light.ColorModeXY, ccdef.CmdColorLoopSet, ccdef.ColorLoopSetRequest{UpdateFlags: ccdef.UpdateFlagsUpdateAction, Action: 1}},
		{light.ColorModeXY, ccdef.CmdMoveToColorTemperature, ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 300}},
		{light.ColorModeXY, ccdef.CmdMoveColorTemperature, ccdef.MoveColorTemperatureRequest{MoveMode: 1, Rate: 1}},
		{light.ColorModeXY, ccdef.CmdStepColorTemperature, ccdef.StepColorTemperatureRequest{StepMode: 1, StepSize: 1}},
		{light.ColorModeHueSaturation, ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 1}},
		{light.ColorModeHueSaturation, ccdef.CmdMoveColor, ccdef.MoveColorRequest{RateX: 1}},
		{light.ColorModeHueSaturation, ccdef.CmdStepColor, ccdef.StepColorRequest{StepX: 1}},
	} {
		start := tc.start
		srv := light.NewColorControlServer(light.ColorControlServerConfig{
			Features: allColorFeatures, MinMireds: 153, MaxMireds: 370, InitialMireds: 250, InitialColorMode: &start,
			InitialHue: 100, InitialSaturation: 100,
		})
		srv.SetColorWriter(&colorSink{err: context.Canceled})
		if _, err := srv.MatterInvoke(context.Background(), tc.cmd, tc.req); err == nil {
			t.Errorf("command 0x%02X switched mode past a refusing writer", tc.cmd)
		}
		if srv.Color().Mode != start {
			t.Errorf("command 0x%02X left mode %d", tc.cmd, srv.Color().Mode)
		}
	}
	for _, err := range []error{
		func() error {
			_, err := light.NewColorControlServer(light.ColorControlServerConfig{Features: allColorFeatures}).
				MatterInvoke(context.Background(), ccdef.CmdMoveToHue, ccdef.MoveToHueRequest{Direction: 9})
			return err
		}(),
		func() error {
			_, err := light.NewColorControlServer(light.ColorControlServerConfig{Features: allColorFeatures}).
				MatterInvoke(context.Background(), ccdef.CmdStepColor, ccdef.StepColorRequest{})
			return err
		}(),
	} {
		if err == nil || err.Error() == "" {
			t.Errorf("error %v carries no text", err)
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/light"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
)

// TestIssue84_OneWritePerCommand: a command that moves two attributes
// (MoveToColor x and y, StepColor, MoveToHueAndSaturation) reaches the
// ColorWriter once, with both targets — matter.js starts the two
// transitions inside the command's transaction, so a hook on the
// committed state sees both at once (moveToColorLogic,
// ColorControlServer.ts:797-813 at 0ceb202d; Transitions.start
// transitionImmediately, Transitions.ts:132-148, :238-241). Without a
// transition engine (the hand-off path) the writer saw the target x with
// the old y first (#84).
func TestIssue84_OneWritePerCommand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		cmd    uint32
		fields map[uint8]any
		want   light.Color
	}{
		{
			"MoveToColor", ccdef.CmdMoveToColor,
			map[uint8]any{0: uint64(45914), 1: uint64(19615), 3: uint64(1), 4: uint64(1)},
			light.Color{Mode: light.ColorModeXY, X: 45914, Y: 19615},
		},
		{
			"StepColor", ccdef.CmdStepColor,
			map[uint8]any{0: int64(1000), 1: int64(-2000), 3: uint64(1), 4: uint64(1)},
			light.Color{Mode: light.ColorModeXY, X: 13102 + 1000, Y: 22196 - 2000},
		},
		{
			"MoveToHueAndSaturation", ccdef.CmdMoveToHueAndSaturation,
			map[uint8]any{0: uint64(40), 1: uint64(200), 3: uint64(1), 4: uint64(1)},
			light.Color{Mode: light.ColorModeHueSaturation, Hue: 40, Saturation: 200},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start := light.ColorModeHueSaturation
			srv := light.NewColorControlServer(light.ColorControlServerConfig{
				Features: light.ColorFeatureHueSaturation | light.ColorFeatureXY, InitialColorMode: &start,
			})
			// The device's colour as observed: HS 127/127, which the XY
			// mode switch converts to x 13102, y 22196.
			srv.Set(light.Color{Mode: light.ColorModeHueSaturation, Hue: 127, Saturation: 127})
			dev := &hsDevice{srv: srv}
			srv.SetColorWriter(dev)
			if _, err := srv.MatterInvoke(context.Background(), tc.cmd, tc.fields); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			writes := dev.writes()
			if len(writes) != 1 {
				t.Fatalf("the writer was driven %d times with %+v, want once with both targets", len(writes), writes)
			}
			w := writes[0]
			var ok bool
			switch tc.want.Mode {
			case light.ColorModeXY:
				ok = w.X == tc.want.X && w.Y == tc.want.Y
			case light.ColorModeHueSaturation:
				ok = w.Hue == tc.want.Hue && w.Saturation == tc.want.Saturation
			case light.ColorModeColorTemperature, light.ColorModeEnhancedHueSaturation:
				t.Fatalf("unexpected want mode %d", tc.want.Mode)
			}
			if w.Mode != tc.want.Mode || !ok {
				t.Errorf("write = %+v, want the mode and both targets of %+v", w, tc.want)
			}
		})
	}
}

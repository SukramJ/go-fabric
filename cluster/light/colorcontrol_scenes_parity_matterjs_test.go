// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/light"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
)

// countingScenes is an onoff.Scenes that counts the invalidations.
type countingScenes struct {
	mu          sync.Mutex
	invalidated int
}

func (c *countingScenes) MakeAllFabricSceneInfoEntriesInvalid() {
	c.mu.Lock()
	c.invalidated++
	c.mu.Unlock()
}

func (c *countingScenes) StoreGlobalScene(context.Context, uint8)  {}
func (c *countingScenes) RecallGlobalScene(context.Context, uint8) {}

func (c *countingScenes) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.invalidated
}

// TestParityMatterJS_ColorControlInvalidatesScenes: with the endpoint's
// scenes port the ColorControl server invalidates the current scene from
// MoveToSaturation, MoveToHueAndSaturation, MoveToColor,
// EnhancedMoveToHueAndSaturation and ColorLoopSet only, after the
// ExecuteIfOff gate, and takes the invalidation over from the endpoint
// layer — matter.js
// packages/node/src/behaviors/color-control/ColorControlServer.ts:597,
// :732, :779, :1079, :1145 at 862ad75f (W3-club1-03).
func TestParityMatterJS_ColorControlInvalidatesScenes(t *testing.T) {
	t.Parallel()
	invalidating := map[uint32]any{
		ccdef.CmdMoveToSaturation:               ccdef.MoveToSaturationRequest{Saturation: 9},
		ccdef.CmdMoveToHueAndSaturation:         ccdef.MoveToHueAndSaturationRequest{Hue: 9, Saturation: 9},
		ccdef.CmdMoveToColor:                    ccdef.MoveToColorRequest{ColorX: 9, ColorY: 9},
		ccdef.CmdEnhancedMoveToHueAndSaturation: ccdef.EnhancedMoveToHueAndSaturationRequest{EnhancedHue: 9, Saturation: 9},
		ccdef.CmdColorLoopSet:                   ccdef.ColorLoopSetRequest{UpdateFlags: ccdef.UpdateFlagsUpdateTime, Time: 5},
	}
	other := map[uint32]any{
		ccdef.CmdMoveToHue:              ccdef.MoveToHueRequest{Hue: 9},
		ccdef.CmdEnhancedMoveToHue:      ccdef.EnhancedMoveToHueRequest{EnhancedHue: 9},
		ccdef.CmdMoveToColorTemperature: ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 300},
		ccdef.CmdStopMoveStep:           ccdef.StopMoveStepRequest{},
	}
	for _, on := range []bool{true, false} {
		sc := &countingScenes{}
		srv := light.NewColorControlServer(light.ColorControlServerConfig{Features: allColorFeatures, OnOff: &onOff{on: on}})
		if srv.MatterInvalidatesScenes() {
			t.Fatal("invalidates scenes without a scenes port")
		}
		srv.SetScenes(sc)
		if !srv.MatterInvalidatesScenes() {
			t.Fatal("does not invalidate scenes with a scenes port")
		}
		for cmd, fields := range invalidating {
			before := sc.count()
			invoke(t, srv, cmd, fields)
			want := before
			if on {
				want++
			}
			if got := sc.count(); got != want {
				t.Errorf("on=%v command 0x%02X: %d invalidations, want %d", on, cmd, got-before, want-before)
			}
		}
		for cmd, fields := range other {
			before := sc.count()
			invoke(t, srv, cmd, fields)
			if got := sc.count(); got != before {
				t.Errorf("on=%v command 0x%02X invalidated the scene", on, cmd)
			}
		}
	}
}

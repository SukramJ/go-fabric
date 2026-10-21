// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"

	colorcontroldef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	levelcontroldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/device"
)

// extendedColorFeatures are the ColorControl features of the extended
// colour light: every feature matter.js's ColorControlServer implements,
// as matter.js's own CHIP test endpoint enables them
// (support/chip-testing/src/devices/ExtendedColorLightEndpoint.ts:
// Xy, ColorTemperature, HueSaturation, EnhancedHue, ColorLoop), so the
// certification cases of every ColorControl feature run against it.
const extendedColorFeatures = colorcontroldef.FeatureXy | colorcontroldef.FeatureColorTemperature |
	colorcontroldef.FeatureHueSaturation | colorcontroldef.FeatureEnhancedHue | colorcontroldef.FeatureColorLoop

// lightingLevelMin is MinLevel under the Lighting feature — the only value
// LT allows (matter.js LevelControlServer.ts:89-90, :205-220).
const lightingLevelMin uint8 = 1

// The light's colour-temperature range: the physical limits and the
// colour it starts with, as the daemon has always served them (≈ 6535 K
// to 2000 K, warm white).
const (
	lightMinMireds     uint16 = 153
	lightMaxMireds     uint16 = 500
	lightInitialMireds uint16 = 370
)

// lightingSurface is what turns a [dimmer] into a colour-temperature light:
// the LevelControl Lighting attributes and the colour temperature (the
// OnOff Lighting state is the device layer's default OnOff server). The
// device kit builds the light's LevelControl and ColorControl servers once
// per device, so they keep the colour temperature and the running
// transition over every reassembly.
type lightingSurface struct {
	// extended makes the light an ExtendedColorLight: ColorControl with
	// every feature, starting in the XY mode.
	extended bool
}

// newDemoCeilingLight returns a colour-temperature light that is off at
// half brightness.
func newDemoCeilingLight(name string) *dimmer {
	return &dimmer{
		name:     name,
		kind:     "ceiling",
		minLevel: lightingLevelMin,
		level:    127,
		lighting: &lightingSurface{},
	}
}

// newDemoColorLight returns an extended colour light that is off at half
// brightness, showing CIE x/y in the XY mode — the mode matter.js's CHIP
// test endpoint starts in (ExtendedColorLightEndpoint.ts).
func newDemoColorLight(name string) *dimmer {
	return &dimmer{
		name:     name,
		kind:     "colorlight",
		minLevel: lightingLevelMin,
		level:    127,
		lighting: &lightingSurface{extended: true},
	}
}

// lightingDeclarations declare the light's cluster surface.
//
// The lamp cannot ramp, so both servers run the module's transition engine
// (cluster/transition, matter.js behavior/Transitions.ts): LevelControl
// with the Lighting feature steps the level through the dimmer's
// [dimmer.MoveToLevel] every 100 ms and couples it to the endpoint's
// OnOff and to the colour temperature; ColorControl steps the colour
// temperature through [ceilingColorWriter]. RemainingTime is live, and the
// CHIP cases that read a level or a colour temperature part-way through a
// transition (TC-LVL-3.1 to 6.1, TC-CC-6.2, 6.3) and count its reports
// (TC-LVL-2.3, TC-CC-2.2) run against it. The speaker keeps the hand-off
// path, where the device is assumed to ramp natively.
//
// The extended colour light is the same surface with every ColorControl
// feature: hue and saturation, enhanced hue, the colour loop, x/y and the
// colour temperature all step on the engine, and [lampColorWriter] receives
// the whole colour in whichever mode — the CHIP ColorControl family runs
// against it (TC-CC-3.x to 9.x). ScenesManagement is the assembler's: the
// device type mandates it, and the assembler mounts the stack's server
// there.
func (s *dimmer) lightingDeclarations() []device.Option {
	minMireds, maxMireds, mireds := lightMinMireds, lightMaxMireds, lightInitialMireds
	color := colorcontroldef.Declare{
		Transitions:            true,
		ColorTemperatureWriter: ceilingColorWriter{name: s.name},
		Initial: colorcontroldef.Patch{
			ColorTempPhysicalMinMireds: &minMireds, ColorTempPhysicalMaxMireds: &maxMireds, ColorTemperatureMireds: &mireds,
		},
	}
	if s.lighting.extended {
		color.Features = extendedColorFeatures
		color.ColorWriter = lampColorWriter{name: s.name}
	}
	return []device.Option{levelcontroldef.Declare{LevelSource: s, Transitions: true}, color}
}

// ceilingColorWriter is where a real host would push the colour
// temperature to the lamp. Here it only logs, which is what the chip-tool
// suite reads back as proof the command reached the device. A transition
// pushes every step; those are logged at debug level, so the log shows one
// line per command.
type ceilingColorWriter struct{ name string }

// SetColorTemperatureMireds implements [colorcontroldef.ColorTemperatureWriter].
func (w ceilingColorWriter) SetColorTemperatureMireds(_ context.Context, mireds uint16) error {
	slog.Debug("ceiling.color", slog.String("device", w.name), slog.Int("mireds", int(mireds)))
	return nil
}

// lampColorWriter is where a real host would push the colour to a lamp
// that takes x/y or hue and saturation. Here it only logs, at debug level
// like [ceilingColorWriter], since a transition pushes every step.
type lampColorWriter struct{ name string }

// SetColor implements [colorcontroldef.ColorWriter].
func (w lampColorWriter) SetColor(_ context.Context, c colorcontroldef.Color) error {
	slog.Debug("colorlight.color", slog.String("device", w.name), slog.Int("mode", int(c.Mode)),
		slog.Int("x", int(c.X)), slog.Int("y", int(c.Y)), slog.Int("hue", int(c.Hue)),
		slog.Int("enhanced_hue", int(c.EnhancedHue)), slog.Int("saturation", int(c.Saturation)),
		slog.Int("mireds", int(c.ColorTemperatureMireds)))
	return nil
}

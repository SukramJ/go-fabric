// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package light

import (
	"context"

	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
)

// MatterApplySceneValues recalls a scene's ColorControl values: matter.js
// ColorControlServer #applySceneValues, which ScenesManagementServer calls
// through implementScenes. values holds the scene's non-null values by
// attribute id; transitionMs is the scene's transition in milliseconds.
//
// The colour loop, when the scene has it active, restarts through
// ColorLoopSet from the current enhanced hue; otherwise the scene's
// EnhancedColorMode selects which values move: saturation (hue and
// saturation), x and y, the colour temperature, or enhanced hue and
// saturation. Three departures from matter.js, each a defect there that
// would leave a recalled colour unapplied
// (BD-Matter-ColorControl-SceneRecall): the scene's EnhancedColorMode is
// honoured for every mode the server supports, not only with the EHUE
// feature (matter.js recalls a light without EHUE in the hue and
// saturation mode alone, so a CT or XY light never recalls its colour);
// every mode's transition is converted to tenths of a second (matter.js
// converts for saturation only and hands milliseconds to the XY, CT and
// enhanced hue logic); and the light switches to the scene's colour mode
// as the commands do, where matter.js moves the values and leaves the mode.
// The hue of a hue and saturation scene comes from its EnhancedCurrentHue,
// the one hue attribute with the "S" quality, as this module recalled it
// before.
func (s *ColorControlServer) MatterApplySceneValues(ctx context.Context, values map[uint32]uint64, transitionMs uint32) {
	s.cmd.Lock()
	defer s.cmd.Unlock()
	tenths := float64(min(transitionMs/100, maxTransitionTime))

	mode := ColorModeHueSaturation
	if v, ok := values[ccdef.AttrEnhancedColorMode]; ok && v <= uint64(ColorModeEnhancedHueSaturation) {
		mode = ColorMode(v)
	} else if !ok && s.has(ColorFeatureColorTemperature) && !s.has(ColorFeatureHueSaturation) && !s.has(ColorFeatureXY) {
		// A scene stored without its mode on a CT-only light: the colour
		// temperature is the only colour it has.
		mode = ColorModeColorTemperature
	}
	if active, ok := values[ccdef.AttrColorLoopActive]; ok && active == colorLoopActiveActive && s.has(ColorFeatureColorLoop) {
		direction := uint64(ccdef.ColorLoopDirectionDecrement)
		if v, ok := values[ccdef.AttrColorLoopDirection]; ok {
			direction = v
		}
		loopTime := uint64(defaultColorLoopTime)
		if v, ok := values[ccdef.AttrColorLoopTime]; ok {
			loopTime = v
		}
		_ = s.colorLoopSet(ctx, ccdef.ColorLoopSetRequest{
			UpdateFlags: ccdef.UpdateFlagsUpdateDirection | ccdef.UpdateFlagsUpdateTime | ccdef.UpdateFlagsUpdateAction,
			Action:      ccdef.ColorLoopActionActivateFromEnhancedCurrentHue,
			Direction:   ccdef.ColorLoopDirectionEnum(min(direction, 1)), //nolint:gosec // bounded by min
			Time:        uint16(min(loopTime, maxEnhancedHueValue)),      //nolint:gosec // bounded by min
			OptionsMask: ccdef.OptionsExecuteIfOff, OptionsOverride: ccdef.OptionsExecuteIfOff,
		})
		return
	}
	if !s.supportsColorMode(mode) {
		return
	}

	sat, hasSat := values[ccdef.AttrCurrentSaturation]
	sat = min(sat, maxSaturationValue)
	ehue, hasEHue := values[ccdef.AttrEnhancedCurrentHue]
	switch mode {
	case ColorModeHueSaturation:
		if !hasSat {
			return
		}
		if s.setEnhancedColorMode(ctx, ColorModeHueSaturation) != nil {
			return
		}
		if hasEHue {
			_ = s.moveToHueLogic(float64(min(ehue>>8, maxHueValue)), ccdef.DirectionShortest, tenths, false)
		}
		_ = s.moveToSaturationLogic(float64(sat), tenths)
	case ColorModeXY:
		x, okX := values[ccdef.AttrCurrentX]
		y, okY := values[ccdef.AttrCurrentY]
		if !okX || !okY || s.setEnhancedColorMode(ctx, ColorModeXY) != nil {
			return
		}
		_ = s.moveToColorLogic(float64(min(x, maxCIEXYValue)), float64(min(y, maxCIEXYValue)), tenths)
	case ColorModeColorTemperature:
		ct, ok := values[ccdef.AttrColorTemperatureMireds]
		if !ok || s.setEnhancedColorMode(ctx, ColorModeColorTemperature) != nil {
			return
		}
		_ = s.moveToColorTemperatureLogic(float64(min(ct, uint64(maxTemperatureMireds))), tenths)
	case ColorModeEnhancedHueSaturation:
		if !hasEHue || !hasSat || s.setEnhancedColorMode(ctx, ColorModeEnhancedHueSaturation) != nil {
			return
		}
		_ = s.moveToHueAndSaturationLogic(float64(ehue), float64(sat), tenths, true)
	}
}

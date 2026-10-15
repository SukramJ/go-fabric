// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light

// The attribute-range scalers a host needs when its device speaks degrees,
// percent or a bounded mireds window and the ColorControl attributes speak
// their Matter ranges. Each one is the arithmetic of a matter.js
// ColorControlServer accessor at the schema pin (0ceb202d,
// packages/node/src/behaviors/color-control/ColorControlServer.ts), which
// the server already uses internally ([hueAttribute],
// [saturationAttribute], cropMireds); exporting them keeps a host from
// re-deriving the rounding.

// HueDegreesToAttr maps a hue in degrees (0..360) to CurrentHue (0..254):
// Math.round(deg * 254 / 360), cropped to 0..0xFE. NaN maps to 0.
// Mirrors matter.js ColorControlServer.ts:181-183 (the hue setter) with
// MIN_HUE_VALUE / MAX_HUE_VALUE (:57-58).
func HueDegreesToAttr(deg float64) uint8 { return hueAttribute(deg) }

// AttrToHueDegrees maps CurrentHue (0..254) to degrees (0..360):
// hue * 360 / 254. Mirrors matter.js ColorControlServer.ts:174-176 (the
// hue getter).
func AttrToHueDegrees(hue uint8) float64 { return float64(hue) * 360 / 254 }

// SaturationPercentToAttr maps a saturation in percent (0..100) to
// CurrentSaturation (0..254). matter.js's setter takes 0..1 —
// Math.round(value * 254), cropped to 0..0xFE (ColorControlServer.ts:
// 205-211, MIN_/MAX_SATURATION_VALUE :61-62); the percent is divided by
// 100 first, which is the only step that is this helper's own. NaN maps to
// 0.
func SaturationPercentToAttr(percent float64) uint8 { return saturationAttribute(percent / 100) }

// AttrToSaturationPercent maps CurrentSaturation (0..254) to percent
// (0..100): matter.js's getter, saturation / 254 (ColorControlServer.ts:
// 198-200), times 100.
func AttrToSaturationPercent(saturation uint8) float64 { return float64(saturation) / 254 * 100 }

// CropMireds crops a colour temperature to the device's physical window.
// A zero bound is an unset one and falls back to matter.js's range,
// 0..0xFEFF. Mirrors matter.js ColorControlServer.ts:1790-1796
// #cropColorTemperature over minimumColorTemperatureMireds /
// maximumColorTemperatureMireds (:239-253, MIN_/MAX_TEMPERATURE_VALUE
// :54-55).
func CropMireds(mireds, physicalMin, physicalMax uint16) uint16 {
	lo, hi := physicalMin, physicalMax
	if lo == 0 {
		lo = minTemperatureMireds
	}
	if hi == 0 {
		hi = maxTemperatureMireds
	}
	return uint16(cropValue(float64(mireds), float64(lo), float64(hi)))
}

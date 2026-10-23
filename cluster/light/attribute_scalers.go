// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light

// The attribute-range scalers a host needs when its device speaks degrees,
// percent or a bounded mireds window and the ColorControl attributes speak
// their Matter ranges. Each one is the arithmetic of a matter.js
// ColorControlServer accessor at the schema pin (0ceb202d,
// packages/node/src/behaviors/color-control/ColorControlServer.ts), which
// the server already uses internally ([hueAttribute]); exporting them keeps
// a host from re-deriving the rounding. The saturation and mireds scalers
// live in cluster/spec/colorcontrol (SaturationPercentToAttr,
// AttrToSaturationPercent, CropMireds).

// HueDegreesToAttr maps a hue in degrees (0..360) to CurrentHue (0..254):
// Math.round(deg * 254 / 360), cropped to 0..0xFE. NaN maps to 0.
// Mirrors matter.js ColorControlServer.ts:181-183 (the hue setter) with
// MIN_HUE_VALUE / MAX_HUE_VALUE (:57-58).
func HueDegreesToAttr(deg float64) uint8 { return hueAttribute(deg) }

// AttrToHueDegrees maps CurrentHue (0..254) to degrees (0..360):
// hue * 360 / 254. Mirrors matter.js ColorControlServer.ts:174-176 (the
// hue getter).
func AttrToHueDegrees(hue uint8) float64 { return float64(hue) * 360 / 254 }

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol

import "math"

// Level bounds applied to Level and OnLevel, the range the
// cluster/levelcontrol server and a host converting a device level share.
//
// LevelMax is the MoveToLevel Level constraint "max 254" (element :74)
// and the MaxLevel default (:38-41). LevelMin is MinLevel without the
// Lighting feature and LightingLevelMin with it: matter.js resolves an
// unset MinLevel to `this.features.lighting ? 1 : 0`
// (LevelControlServer.ts:89-90, :115-116).
const (
	LevelMin uint8 = 0
	LevelMax uint8 = 254
	// LightingLevelMin is MinLevel under the Lighting feature — the only
	// value LT allows (matter.js LevelControlServer.ts:205-220
	// initializeLighting warns on any other).
	LightingLevelMin uint8 = 1
)

// LevelFromUnit maps a fraction 0..1 to the CurrentLevel range 0..254:
// Math.round(u * 254), cropped; NaN is 0. matter.js has no level
// counterpart: this is the attribute's range arithmetic (CurrentLevel
// "max 254", level-control.element.ts:29-30, :74), rounded as matter.js
// rounds its 0..1 saturation (ColorControlServer.ts:205-211). A light
// crops the result with cluster/levelcontrol.CropLevel to its MinLevel.
func LevelFromUnit(u float64) uint8 {
	if math.IsNaN(u) {
		return 0
	}
	return uint8(min(max(jsRound(u*float64(LevelMax)), 0), float64(LevelMax)))
}

// UnitFromLevel maps a CurrentLevel 0..254 to a fraction 0..1: level /
// 254. matter.js has no level counterpart: this is the attribute's range
// arithmetic, as [LevelFromUnit].
func UnitFromLevel(level uint8) float64 { return float64(level) / float64(LevelMax) }

// jsRound is JavaScript's Math.round: half rounds up.
func jsRound(v float64) float64 { return math.Floor(v + 0.5) }

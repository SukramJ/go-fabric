// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valve

import "github.com/SukramJ/go-fabric/surface"

// Ahead declares the ValveConfigurationAndControl server's elements that
// implement rows of the "Ahead of matter.js" register
// (notes/parity/by_design.md): a host that selects one of these features
// enables its row in surface.Policies.Extensions, or the device layer
// refuses the composition (surface.ErrExtensionNotEnabled, ADR 0030
// decision 3).
//
//   - BD-chip-Valve-RulesInServer: Level and TimeSync. The register's
//     matter.js marker: "`ValveConfigurationAndControlServer.ts:14` is the
//     empty subclass (absence: the class line, 14 lines in all)".
//
// The features' attributes (AutoCloseTime, CurrentLevel, TargetLevel) and
// the optional DefaultOpenLevel and LevelStep "[LVL]" follow from the
// feature bits and the declaration; the row adds no further optional
// element.
var Ahead = surface.AheadSet{
	"BD-chip-Valve-RulesInServer": {Features: FeatureLevel | FeatureTimeSync},
}

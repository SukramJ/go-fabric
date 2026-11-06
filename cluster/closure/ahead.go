// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure

import (
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/surface"
)

// Ahead declares the ClosureControl server's elements that implement rows
// of the "Ahead of matter.js" register (notes/parity/by_design.md): a host
// that selects one of these features enables its row in
// surface.Policies.Extensions, or the device layer refuses the composition
// (surface.ErrExtensionNotEnabled, ADR 0030 decision 3).
//
//   - BD-chip-ClosureControl-RulesInServer: MotionLatching, Instantaneous,
//     Speed and Calibration. The register's matter.js marker:
//     "`ClosureControlServer.ts:17` is the empty subclass (absence: the
//     class line, 17 lines in all)".
//
// The features' attributes (LatchControlModes), commands (Calibrate; Stop
// withdrawn by Instantaneous) and events (MovementCompleted withdrawn by
// Instantaneous) follow from the feature bits; the row adds no further
// optional element.
var Ahead = surface.AheadSet{
	"BD-chip-ClosureControl-RulesInServer": {Features: AheadFeatures},
}

// AheadFeatures are the ClosureControl features this server serves ahead
// of matter.js: MotionLatching, Instantaneous, Speed and Calibration.
const AheadFeatures = wire.ClosureControlFeatureMotionLatching |
	wire.ClosureControlFeatureInstantaneous |
	wire.ClosureControlFeatureSpeed |
	wire.ClosureControlFeatureCalibration

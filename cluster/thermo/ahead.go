// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import "github.com/SukramJ/go-fabric/surface"

// Ahead declares the Thermostat server's elements that implement rows of
// the "Ahead of matter.js" register (notes/parity/by_design.md): a host
// that selects one of these features enables its row in
// surface.Policies.Extensions, or the device layer refuses the composition
// (surface.ErrExtensionNotEnabled, ADR 0030 decision 3).
//
//   - BD-chip-Thermostat-Schedules: MatterScheduleConfiguration
//     ([ThermostatFeatureMSCH]). The register's matter.js marker:
//     "`ThermostatServer.ts:267` at `b25ed960`: implemented in matter.js
//     behind a forward gate, not released" (the `thermostat-msch` gate
//     line; owner decision 291).
//   - BD-chip-Thermostat-Events: the Events feature
//     ([ThermostatFeatureTEVT]). The register's matter.js marker:
//     "`ThermostatServer.ts:272` at `b25ed960`: implemented in matter.js
//     behind a forward gate, not released" (the `thermostat-events` gate
//     line; owner decision 291).
//
// ThermostatSuggestions ([ThermostatFeatureTSUGGEST]) is no row: matter.js
// serves it (packages/node/src/behaviors/thermostat/ThermostatServer.ts:2261-2293
// at b25ed960, #setupSuggestions), and its row
// BD-chip-Thermostat-Suggestions closed caught up (owner decision 336).
//
// The features' attributes, commands and events follow from the feature
// bits; the rows add no further optional element.
var Ahead = surface.AheadSet{
	"BD-chip-Thermostat-Schedules": {Features: ThermostatFeatureMSCH},
	"BD-chip-Thermostat-Events":    {Features: ThermostatFeatureTEVT},
}

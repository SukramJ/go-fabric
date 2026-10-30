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
//     "`ThermostatServer.ts:82`, the class doc lists the feature
//     unimplemented (presence; :126-128 warns when it is enabled)".
//   - BD-chip-Thermostat-Events: the Events feature
//     ([ThermostatFeatureTEVT]). The register's matter.js marker:
//     "`ThermostatServer.ts:525` is the only `events.` emit of the server,
//     the custom `calibratedTemperature$Changed` (absence: none of the
//     model's eight Thermostat events is emitted anywhere in the file,
//     1790 lines)".
//
// The features' attributes, commands and events follow from the feature
// bits; the rows add no further optional element.
var Ahead = surface.AheadSet{
	"BD-chip-Thermostat-Schedules": {Features: ThermostatFeatureMSCH},
	"BD-chip-Thermostat-Events":    {Features: ThermostatFeatureTEVT},
}

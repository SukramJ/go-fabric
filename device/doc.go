// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package device is the device layer: one Go type per Matter device type in
// scope, generated from parity/schema.json by script/devicegen, and the
// hand-written runtime they share — matter.js's packages/node/src/devices
// and packages/node/src/endpoints (ADR 0018).
//
// A device type's constructor ([NewDimmableLight], …) mounts every server
// cluster the type mandates, and every optional one the host opts in, with
// a default server: the module's own where it has one, else the generated
// [spec.Server] with the features the type mandates and the defaults it
// states. The host states the rest in one declaration per cluster —
// <cluster>def.Declare of the generated cluster packages: initial state,
// feature selection, the write veto, command handlers, the host port the
// kit's server runs on, or its own server (HostServer), as a matter.js host
// sets state and overrides a behavior; a sensor's measurement clusters come
// from the host's live reading ([WithMeasurement]). [Device.Spec]
// hands the device to the endpoint assembler as an [endpoint.Spec], so
// endpoint-id persistence and device-type validation are the assembler's
// (ADR 0016). The clusters the assembler mounts on every endpoint —
// Descriptor, Identify, BridgedDeviceBasicInformation, Groups and
// ScenesManagement — are never mounted here; an optional Groups or
// ScenesManagement is opted into with its declaration
// (groupsdef.Declare{}, scenesmanagementdef.Declare{}) and mounted by the assembler.
//
// The package is public API from its first commit (README.md §API
// stability). A device type matter.js renames becomes an alias with a
// deprecation, not a break.
package device

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package device is the device layer: one Go type per Matter device type in
// scope, generated from parity/schema.json by script/devicegen, and the
// hand-written runtime they share — matter.js's packages/node/src/devices
// and packages/node/src/endpoints (ADR 0018).
//
// # Declaring a device
//
// A device type's constructor ([NewDimmableLight], …) mounts every server
// cluster the type mandates, each with its default server: the module's
// own where it has one, else the generated [spec.Server] with the features
// the type mandates and the defaults it states. The host states the rest
// in one declaration per cluster, the Declare of the cluster's typed
// package (onoffdef.Declare, …): the initial state, the feature selection,
// the write veto, the command handlers, the host port the module's server
// runs on, or the host's own server (HostServer) — as a matter.js host
// sets state and overrides a behavior. Declaring an optional cluster
// mounts it. [WithServer] mounts the host's own server for a cluster
// without a typed package. [FromSpec] builds the same device from data.
//
// A device's typed accessors (DimmableLight.OnOff, …) wrap the server the
// device mounts for the cluster, whatever its type (Underlying returns it),
// and read and set its state; OnChanged sees every change with its origin
// ([contract.Change]), OnChangedAsync runs on the device's own goroutine.
// Once the device is added to a node, [Device.SetReachable],
// [Device.Rename] and [Device.EndpointNumber] are its lifecycle.
//
// [Device.Spec] hands the device to the endpoint assembler as an
// [endpoint.Spec], so endpoint-id persistence and device-type validation
// are the assembler's (ADR 0016). The clusters the assembler mounts on
// every endpoint — Descriptor, Identify, BridgedDeviceBasicInformation,
// Groups and ScenesManagement — are never mounted here; an optional Groups
// or ScenesManagement is opted into with its declaration (groupsdef.Declare{},
// scenesmanagementdef.Declare{}) and mounted by the assembler.
//
// The package is public API from its first commit (README.md §API
// stability). A device type matter.js renames becomes an alias with a
// deprecation, not a break.
//
// The lists of typed cluster packages and device types below are
// generated (doc_gen.go); each type's cluster requirements are listed in
// docs/reference/device-types.md.
package device

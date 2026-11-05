// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package coreseam carries the one-way seams through which the module's
// tests reach what a cluster/core server does without a public knob: the
// SpecificationVersion a BasicInformation server reports — a test stands
// in for a node of an older release with it, as matter.js's IcdClientTest
// sets basicInformation.specificationVersion (IcdClientTest.ts:567-582 at
// 862ad75f); a release node always reports the release the module
// implements — and a hook on an ICDManagement server's RegisterClient and
// UnregisterClient commands, which counts or refuses them as matter.js's
// CountingIcdServer and RefusingIcdServer do (IcdClientTest.ts:265-274,
// :298-312 at 862ad75f). Unset, a server serves as it always does.
//
// Direction of the edge: package cluster/core installs the hook from its
// init; package node calls it when internal/nodeseam.UseSpecificationVersion
// marked its config. A testing-only field on cluster/core's public Config
// would be a production affordance every consumer sees; the seam is
// module-internal, so it cannot become a consumer-visible surface.
package coreseam

// SetSpecificationVersion makes server (a *core.BasicInformation, threaded
// through as any because this package must not import cluster/core) report
// version as its SpecificationVersion. Call it before the server is
// served. Reports false when server is not a *core.BasicInformation or
// version is 0. Set by cluster/core's init; nil until that package is
// linked.
var SetSpecificationVersion func(server any, version uint32) bool

// ObserveICDRegistrations makes server (a *core.ICDManagement) call hook
// at the start of every RegisterClient (register true) and UnregisterClient
// (register false) command, before the command touches anything; a
// non-nil error from hook fails the command with the FAILURE status, as
// matter.js's RefusingIcdServer throws StatusResponse.FailureError. nil
// removes the hook. Reports false when server is not a
// *core.ICDManagement. Set by cluster/core's init; nil until that package
// is linked.
var ObserveICDRegistrations func(server any, hook func(register bool) error) bool

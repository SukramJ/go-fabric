// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package coreseam carries the one-way seam through which the module sets
// what a cluster/core server serves without a public knob: the
// SpecificationVersion a BasicInformation server reports. A test stands in
// for a node of an older release with it, as matter.js's IcdClientTest
// sets basicInformation.specificationVersion (IcdClientTest.ts:567-582 at
// 862ad75f); a release node always reports the release the module
// implements.
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

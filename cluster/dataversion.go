// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cluster

import (
	"thingfabric.dev/go-fabric/contract"
)

// DataVersionTracker is a per-cluster monotonic counter that cluster
// servers embed to satisfy [contract.ClusterDataVersion]. Every
// successful attribute write SHOULD call [DataVersionTracker.Bump] so
// subscribers see a fresh version number in their cached state.
//
// The concrete implementation lives in pkg/contract so that model
// packages on the host side can embed it without importing
// this package, removing the model→northbound coupling. This alias
// keeps all cluster-internal callers unchanged.
//
// Mirrors matter.js's per-cluster datasource version
// (packages/node/src/behavior/state/managed/Datasource.ts:763,
// `entropy.randomUint32`, advanced at :856-865, at b25ed960). The
// initial value is a uniformly random uint32, 0 included, chosen at first
// access — matter.js samples observed in the field carry values like
// 3408898508 / 3191265986 / 2061169561 (per-cluster initial randoms).
// Apple Home's MTRDevice
// cache appears to treat a uniform DataVersion=1 across every cluster
// as an "uninitialised" signal and refuses to persist those clusters
// to its on-disk cache (surface symptom: `Storing cluster information
// count: 3` despite 207 attribute reports landing live). Random init
// makes each (endpoint, cluster) carry a distinct DataVersion before
// the first mutation, which is what Apple looks for.
//
// 0 is a version like any other: neither the generator nor the wrap
// skips it, as matter.js's entropy.randomUint32
// (packages/general/src/util/Entropy.ts:26-28) and advanceVersion
// (Datasource.ts:862-864) do not, at b25ed960 (owner decision 146).
//
// See [contract.DataVersionTracker] for the counter itself.
//
// Usage:
//
//	type MyCluster struct {
//	    cluster.DataVersionTracker
//	    // ...
//	}
//
//	func (c *MyCluster) MatterDataVersion() uint32 { return c.DataVersionTracker.Current() }
//
//	func (c *MyCluster) MatterWrite(...) error {
//	    // do mutation; when it changed a value:
//	    c.DataVersionTracker.Bump()
//	    return nil
//	}
type DataVersionTracker = contract.DataVersionTracker

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package contract holds the port contracts between a host's
// domain model and the Matter bridge: the interfaces a data point
// implements to materialise as a bridged endpoint, the cluster-server
// surface the bridge dispatches through, and the measurement /
// eligibility classifications the host computes once and the bridge
// consumes.
//
// The package depends on nothing else in this module — its imports are
// stdlib only. That is the property worth preserving: a port contract
// that names a host type drags the host's release cadence and its
// transitive dependencies along with it, and the bridge is the one
// surface where that coupling is expensive.
//
// The symbols carry no Matter prefix — the package name already says it.
package contract

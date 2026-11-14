// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract

import "context"

// FabricScopedState is the capability of a [ClusterServer] that holds
// state keyed by fabric index — the entries of a fabric-scoped or
// fabric-sensitive attribute, or anything else that belongs to one fabric.
// It is the one removal path of such state: the node calls it on every
// server of every endpoint, the root's and the Aggregator's included,
// when a fabric is removed, and reconciles every server against the
// fabric table before it serves a topology (at start, and whenever an
// endpoint joins a running node), so a fabric removed while an endpoint
// was absent, or by a run that stopped before its fan-out, leaves nothing
// behind, and a later fabric that receives the same index inherits
// nothing.
//
// Mirrors matter.js FabricScopedDataHandler
// (packages/node/src/behavior/cluster/FabricScopedDataHandler.ts:41-73,
// :178-189 at db021da0), which strips a departed fabric's entries from
// every fabric-scoped attribute of every behaviour on deletion
// (node/server/ServerEnvironment.ts:188-196) and when an endpoint
// initialises (node/server/ServerEndpointInitializer.ts:129-136); a Go
// server keeps its state in its own fields, so it states what it holds.
type FabricScopedState interface {
	// MatterDropFabric drops everything the server holds for fabricIndex,
	// from memory and from its own persistence, and tells whatever the
	// server tells of a change it makes (its change reports, its port).
	// A fabric it holds nothing for changes nothing. An error reports
	// that the server's persistence was not updated; whatever it kept
	// then, in memory or restored by a restart, the next reconcile drops.
	MatterDropFabric(ctx context.Context, fabricIndex uint8) error
	// MatterHeldFabrics lists the fabric indices the server holds state
	// for, in memory or in its own persistence, in any order.
	MatterHeldFabrics() []uint8
}

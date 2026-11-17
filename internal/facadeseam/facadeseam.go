// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package facadeseam carries the one-way seam through which the node
// facade installs its own handlers on four hooks of the root servers and
// the commissioning window: OperationalCredentials' fabric-removed and
// fabric-updated hooks, GeneralCommissioning's commissioning-complete hook
// and the window's transition hook. Each of them has two slots: the
// facade's, set here, and the host's, set by the server's public Set…
// method. A hook runs the facade's handler first and the host's after it,
// so a host that sets the public slot through [node.Node.Root] or
// [node.Node.Bridge] adds to the facade's wiring instead of replacing it
// (review II, R2-P5-apinode-07). It also hands the node the
// AccessControl server's write lock for the entries the node adds to a
// fabric's ACL itself ([WithACLWriteLock]).
//
// Direction of the edge: cluster/core and bridge install the setters from
// their init; package node calls them. The seam is module-internal, so the
// facade's slot is not a consumer-visible surface.
package facadeseam

import "context"

// The setters report false when the value handed in is not the server or
// window they serve (each type-asserts it back). nil fn empties the
// facade's slot. Set by cluster/core's and bridge's init; nil until those
// packages are linked.
var (
	// SetOnCommissioningComplete sets the facade's slot of a
	// *core.GeneralCommissioning's commissioning-complete hook.
	SetOnCommissioningComplete func(server any, fn func(ctx context.Context, fabricIndex uint8)) bool
	// SetOnFabricRemoved sets the facade's slot of a
	// *core.OperationalCredentials' fabric-removed hook.
	SetOnFabricRemoved func(server any, fn func(ctx context.Context, fabricIndex uint8)) bool
	// SetOnFabricUpdated sets the facade's slot of a
	// *core.OperationalCredentials' fabric-updated hook.
	SetOnFabricUpdated func(server any, fn func(ctx context.Context, fabricIndex uint8)) bool
	// SetTransitionHook sets the facade's slot of a
	// *bridge.CommissioningWindow's transition hook.
	SetTransitionHook func(window any, fn func()) bool
	// OwnFabricState tells a *bridge.Bridge that the node drops the
	// state of a removed fabric from every server itself
	// (contract.FabricScopedState): the bridge's own drop, which serves a
	// host without the node, then stands aside, so each server is asked
	// once.
	OwnFabricState func(bridge any) bool
	// WithACLWriteLock runs fn while it holds the write lock of a
	// *core.AccessControl — the lock a controller's ACL write holds across
	// its read of the fabric's list, its comparison and its store — and
	// reports true; it runs nothing and reports false when server is not
	// one. The node's own read-modify-write of a fabric's ACL (the entries
	// it adds for a served requestor or OTA provider) runs inside fn
	// (review II, R2-P6-sec-2401).
	WithACLWriteLock func(server any, fn func()) bool
)

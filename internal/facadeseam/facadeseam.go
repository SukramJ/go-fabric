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
// (review II, R2-P5-apinode-07).
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
)

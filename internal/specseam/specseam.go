// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package specseam carries the one-way seam through which a cluster
// server built on a cluster/spec Server keeps its own copy of a stored
// attribute in step with the store before the change listeners are told:
// a listener, and whatever it wakes, then reads through the server's own
// accessor the state it is told about (PR 1 review M1, decision 682: the
// camera servers' session tables).
//
// Direction of the edge: package cluster/spec installs the hook from its
// init; the servers of the cluster tree call it. A store-callback on
// cluster/spec's public Server would be a production affordance every
// consumer sees; the seam is module-internal, so it cannot become a
// consumer-visible surface.
package specseam

import "thingfabric.dev/go-fabric/contract"

// SetAttributesStored is server's (a *spec.Server, threaded through as
// any because this package must not import cluster/spec)
// SetAttributesFrom(origin, values) with stored, when not nil, run once
// the values are stored and before any change listener is told; a Set
// the server refuses runs nothing. stored runs under the server's lock —
// in the hold that stores the values and queues their notification, so
// the order holds for a Set that joins an open controller write's commit
// too — and must not call the server. A server that is not a *spec.Server
// is an error. Set by cluster/spec's init; nil until that package is
// linked.
var SetAttributesStored func(server any, origin contract.Origin, values map[uint32]any, stored func()) error

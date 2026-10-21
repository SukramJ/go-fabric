// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package binding resolves the entries of the node's Binding servers into
// the peers they direct: matter.js's BindingManager
// (packages/node/src/behaviors/binding/BindingManager.ts at 0ceb202d), the
// node-scoped service its BindingServer registers every entry with.
//
// A controller writes a Binding entry on one of our endpoints — "talk to
// node 2, endpoint 1, cluster OnOff". The [Manager] hears it through the
// server's change hook (cluster/core.Binding.OnChange) and resolves it into
// a [Resolution] of one of three kinds:
//
//   - [KindClient]: the entry names another node. The manager takes the
//     peer from [Peers] (client/peer.Set), opens a CASE session to it
//     in the background and reports the resolution, with an
//     [interaction.Interaction] bound to the peer, once a session exists.
//   - [KindGroup]: the entry names a group. It resolves once the fabric
//     holds a key for the group ([Topology.HasGroupKey]), through the
//     [GroupSender] — commands to it go out group-addressed and nobody
//     answers.
//   - [KindServer]: the entry names another endpoint of this node (a
//     self-binding). It resolves locally; nothing goes on the wire.
//
// The host observes the outcome with [Manager.OnEstablished] and
// [Manager.OnRemoved]. Entries stay dormant — no peer, no CASE session —
// until something observes OnEstablished (BindingManager.ts:385-438).
//
// An entry the manager cannot use is dropped with a warning whose reason
// attribute is matter.js's message, verbatim; the write that brought it
// already succeeded. The manager never closes a CASE session when a
// binding goes: matter.js's reference counts of bound peers have no
// consumer (BindingManager.ts:656-677) and are not ported.
package binding

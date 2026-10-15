// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package groups is the node's operational view of group communication:
// for every fabric, the group key sets in derived form (operational group
// keys, privacy keys, group session ids), the GroupKeyMap that binds a
// group to a key set, the group table that names the local endpoints of
// each group, the Groupcast cluster's per-group properties (multicast
// address policy, auxiliary-ACL flag), and the replay-protection state of
// received group messages. From these it derives what matter.js's
// GroupcastServer derives: the Groupcast Membership list, the multicast
// address of each group (the shared FF05::FA for the IanaAddr policy) and
// the auxiliary access control entries.
//
// It is the Go counterpart of matter.js packages/protocol/src/groups
// (FabricGroups, Groups, KeySets, MessagingState) together with the
// receive half of packages/protocol/src/session/GroupSession.ts. The
// GroupKeyManagement and Groups cluster servers write the state, the
// bridge's receive path reads it to authenticate a group message and
// route it to the member endpoints, and the bridge joins the multicast
// address of every group that has a member endpoint.
//
// Group membership is stack state, as in matter.js: a host neither
// supplies nor observes it.
//
// The send half mirrors SessionManager.groupSessionForAddress and
// GroupSession.create / encode: [Manager.Sender] resolves a group's current
// operational key into an outbound group session, and [Sender.Send] —
// with [Sender.Invoke] and [Sender.Write] on top — frames, seals and
// privacy-obfuscates a message and sends it to the group's multicast
// address. Every message is numbered by the node-global, persisted group
// data message counter (counter.go).
package groups

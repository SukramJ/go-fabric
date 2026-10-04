// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package groups is the node's operational view of group communication:
// for every fabric, the group key sets in derived form (operational group
// keys, privacy keys, group session ids), the GroupKeyMap that binds a
// group to a key set, the group table that names the local endpoints of
// each group, and the replay-protection state of received group messages.
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
// This module has no controller role, so nothing here sends group
// messages; there is no group data message counter to keep.
package groups

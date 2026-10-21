// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

// The AccessControl attributes whose list values are never chunked.
// Mirrors matter.js packages/protocol/src/action/request/Write.ts:14-21 at
// 0ceb202d (AccessControl 0x001F, Acl 0x0000, Extension 0x0001 —
// model/src/standard/elements/access-control.element.ts:20, :31, :35), the
// connectedhomeip #38263 exemption of :151-156.
const (
	accessControlCluster   uint32 = 0x001F
	accessControlACL       uint32 = 0x0000
	accessControlExtension uint32 = 0x0001
)

// ListChunkedByDefault reports whether a client writes a list value of
// the attribute list-chunked — REPLACE-ALL with the empty list, then one
// append per element — by default: every attribute but AccessControl ACL
// and Extension (Write.ts:18-21 isAclOrExtensionPath, :149-171). Unicast
// (client/interaction) and group (groups) writes share it.
func ListChunkedByDefault(cluster, attribute uint32) bool {
	return cluster != accessControlCluster || (attribute != accessControlACL && attribute != accessControlExtension)
}

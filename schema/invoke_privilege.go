// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

// Invoke privileges, as matter.js's AccessLevel numbers them (View=1,
// ProxyView=2, Operate=3, Manage=4, Administer=5).
const (
	PrivilegeView       uint8 = 1
	PrivilegeOperate    uint8 = 3
	PrivilegeManage     uint8 = 4
	PrivilegeAdminister uint8 = 5
)

// InvokePrivilege returns the privilege a subject needs to invoke the
// command: the matter.js access level of the command, Operate when matter.js
// gives it none above the default. Mirrors matter.js
// AccessControl(command).limits.writeLevel (AccessControl.ts). The
// privilege is checked before the command is decoded, so a subject below it
// gets UNSUPPORTED_ACCESS rather than a field error.
// elevatedInvokePrivileges (invoke_access_gen.go) is generated from each
// request command's effective writePriv in parity/schema.json, every
// cluster of the snapshot.
func InvokePrivilege(clusterID, commandID uint32) uint8 {
	if p, ok := elevatedInvokePrivileges[clusterID][commandID]; ok {
		return p
	}
	return PrivilegeOperate
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package schema

// fabricScopedInvokePaths lists the commands matter.js marks fabric-scoped
// ("F" in the command's access string) among the clusters this module
// serves. A fabric-scoped command invoked on a session with no accessing
// fabric — a PASE session before AddNOC — answers UNSUPPORTED_ACCESS
// (matter.js packages/protocol/src/action/server/CommandInvokeResponse.ts:287
// `limits.fabricScoped && !this.session.fabric`).
//
// Hand-written like timedInvokePaths: the extracted snapshot does not carry
// command access. Each row cites its element file.
var fabricScopedInvokePaths = map[uint32]map[uint32]struct{}{
	// AccessControl — ReviewFabricRestrictions "F A" (access-control.element.ts:121)
	0x001F: {0x00: {}},
	// GeneralCommissioning — CommissioningComplete "F A" (general-commissioning.element.ts:94)
	0x0030: {0x04: {}},
	// OperationalCredentials — UpdateNOC, UpdateFabricLabel, SetVidVerificationStatement
	// "F A" (operational-credentials.element.ts:104,121,142)
	0x003E: {0x07: {}, 0x09: {}, 0x0C: {}},
	// GroupKeyManagement — KeySetWrite, KeySetRead, KeySetRemove,
	// KeySetReadAllIndices "F A" (group-key-management.element.ts:58,64,75,81)
	0x003F: {0x00: {}, 0x01: {}, 0x03: {}, 0x04: {}},
	// Groups — every command "F M" / "F O" (groups.element.ts:36-76)
	0x0004: {0x00: {}, 0x01: {}, 0x02: {}, 0x03: {}, 0x04: {}, 0x05: {}},
	// Groupcast — JoinGroup, LeaveGroup, UpdateGroupKey, ConfigureAuxiliaryAcl,
	// GroupcastTesting (groupcast.element.ts:63,78,98,106,115)
	0x0065: {0x00: {}, 0x01: {}, 0x03: {}, 0x04: {}, 0x05: {}},
	// ScenesManagement — every request "F M" / "F O" (scenes-management.element.ts:37-156)
	0x0062: {0x00: {}, 0x01: {}, 0x02: {}, 0x03: {}, 0x04: {}, 0x05: {}, 0x06: {}, 0x40: {}},
	// IcdManagement — RegisterClient, UnregisterClient "F M" (icd-management.element.ts:74,91)
	0x0046: {0x00: {}, 0x02: {}},
	// TimeSynchronization — SetTrustedTimeSource "F A" (time-synchronization.element.ts:103)
	0x0038: {0x01: {}},
	// OtaSoftwareUpdateRequestor — AnnounceOtaProvider "F A" (ota-software-update-requestor.element.ts:65)
	0x002A: {0x00: {}},
}

// IsFabricScopedInvoke reports whether the (cluster, command) pair carries
// matter.js's fabric-scoped access quality. Mirrors matter.js
// Access.Fabric.Scoped on a command (AccessControl.ts:524 `fabricScoped`).
func IsFabricScopedInvoke(clusterID, commandID uint32) bool {
	cmds, ok := fabricScopedInvokePaths[clusterID]
	if !ok {
		return false
	}
	_, ok = cmds[commandID]
	return ok
}

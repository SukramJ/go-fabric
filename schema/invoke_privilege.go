// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package schema

// Invoke privileges, as matter.js's AccessLevel numbers them (View=1,
// ProxyView=2, Operate=3, Manage=4, Administer=5).
const (
	PrivilegeOperate    uint8 = 3
	PrivilegeManage     uint8 = 4
	PrivilegeAdminister uint8 = 5
)

// elevatedInvokePrivileges lists, for the clusters this module serves, every
// request command matter.js gives an invoke privilege above the Operate
// default — "M" (Manage) or "A" (Administer) in the command's access string.
// The privilege is checked before the command is decoded, so a subject
// below it gets UNSUPPORTED_ACCESS rather than a field error (matter.js
// packages/protocol/src/action/server/CommandInvokeResponse.ts authorizes
// with limits.writeLevel before invoking).
//
// Hand-written like fabricScopedInvokePaths: the extracted snapshot does not
// carry command access. TestInvokePrivilegeMatchesMatterJS holds the table
// against the element files named in each row.
var elevatedInvokePrivileges = map[uint32]map[uint32]uint8{
	// Identify — Identify, TriggerEffect "M" (identify.element.ts)
	0x0003: {0x00: PrivilegeManage, 0x40: PrivilegeManage},
	// Groups — AddGroup, RemoveGroup, RemoveAllGroups, AddGroupIfIdentifying "F M" (groups.element.ts)
	0x0004: {0x00: PrivilegeManage, 0x03: PrivilegeManage, 0x04: PrivilegeManage, 0x05: PrivilegeManage},
	// AccessControl — ReviewFabricRestrictions "F A" (access-control.element.ts)
	0x001F: {0x00: PrivilegeAdminister},
	// OtaSoftwareUpdateRequestor — AnnounceOtaProvider "F A" (ota-software-update-requestor.element.ts)
	0x002A: {0x00: PrivilegeAdminister},
	// GeneralCommissioning — ArmFailSafe, SetRegulatoryConfig, CommissioningComplete,
	// SetTcAcknowledgements "A" (general-commissioning.element.ts)
	0x0030: {0x00: PrivilegeAdminister, 0x02: PrivilegeAdminister, 0x04: PrivilegeAdminister, 0x06: PrivilegeAdminister},
	// NetworkCommissioning — ScanNetworks, AddOrUpdate*, RemoveNetwork, ConnectNetwork,
	// ReorderNetwork "A" (network-commissioning.element.ts)
	0x0031: {0x00: PrivilegeAdminister, 0x02: PrivilegeAdminister, 0x03: PrivilegeAdminister, 0x04: PrivilegeAdminister, 0x06: PrivilegeAdminister, 0x08: PrivilegeAdminister},
	// GeneralDiagnostics — TestEventTrigger, PayloadTestRequest "M" (general-diagnostics.element.ts)
	0x0033: {0x00: PrivilegeManage, 0x03: PrivilegeManage},
	// SoftwareDiagnostics — ResetWatermarks "M" (software-diagnostics.element.ts)
	0x0034: {0x00: PrivilegeManage},
	// EthernetNetworkDiagnostics — ResetCounts "M" (ethernet-network-diagnostics.element.ts)
	0x0037: {0x00: PrivilegeManage},
	// TimeSynchronization — SetUtcTime, SetTrustedTimeSource, SetDefaultNtp "A";
	// SetTimeZone, SetDstOffset "M" (time-synchronization.element.ts)
	0x0038: {0x00: PrivilegeAdminister, 0x01: PrivilegeAdminister, 0x02: PrivilegeManage, 0x04: PrivilegeManage, 0x05: PrivilegeAdminister},
	// AdministratorCommissioning — OpenCommissioningWindow, OpenBasicCommissioningWindow,
	// RevokeCommissioning "A T" (administrator-commissioning.element.ts)
	0x003C: {0x00: PrivilegeAdminister, 0x01: PrivilegeAdminister, 0x02: PrivilegeAdminister},
	// OperationalCredentials — every request but the responses "A" / "F A"
	// (operational-credentials.element.ts)
	0x003E: {
		0x00: PrivilegeAdminister, 0x02: PrivilegeAdminister, 0x04: PrivilegeAdminister, 0x06: PrivilegeAdminister,
		0x07: PrivilegeAdminister, 0x09: PrivilegeAdminister, 0x0A: PrivilegeAdminister, 0x0B: PrivilegeAdminister,
		0x0C: PrivilegeAdminister, 0x0D: PrivilegeAdminister,
	},
	// GroupKeyManagement — KeySetWrite, KeySetRead, KeySetRemove, KeySetReadAllIndices "F A"
	// (group-key-management.element.ts)
	0x003F: {0x00: PrivilegeAdminister, 0x01: PrivilegeAdminister, 0x03: PrivilegeAdminister, 0x04: PrivilegeAdminister},
	// IcdManagement — RegisterClient, UnregisterClient "F M" (icd-management.element.ts)
	0x0046: {0x00: PrivilegeManage, 0x02: PrivilegeManage},
	// ScenesManagement — AddScene, RemoveScene, RemoveAllScenes, StoreScene, CopyScene "F M"
	// (scenes-management.element.ts)
	0x0062: {0x00: PrivilegeManage, 0x02: PrivilegeManage, 0x03: PrivilegeManage, 0x04: PrivilegeManage, 0x40: PrivilegeManage},
	// Groupcast — JoinGroup, LeaveGroup, UpdateGroupKey "F M"; ConfigureAuxiliaryAcl,
	// GroupcastTesting "F A" (groupcast.element.ts)
	0x0065: {0x00: PrivilegeManage, 0x01: PrivilegeManage, 0x03: PrivilegeManage, 0x04: PrivilegeAdminister, 0x05: PrivilegeAdminister},
	// DoorLock — the schedule, user and credential commands "A" / "A T"
	// (door-lock.element.ts)
	0x0101: {
		0x0B: PrivilegeAdminister, 0x0C: PrivilegeAdminister, 0x0D: PrivilegeAdminister, 0x0E: PrivilegeAdminister,
		0x0F: PrivilegeAdminister, 0x10: PrivilegeAdminister, 0x11: PrivilegeAdminister, 0x12: PrivilegeAdminister,
		0x13: PrivilegeAdminister, 0x1A: PrivilegeAdminister, 0x1B: PrivilegeAdminister, 0x1D: PrivilegeAdminister,
		0x22: PrivilegeAdminister, 0x24: PrivilegeAdminister, 0x26: PrivilegeAdminister, 0x28: PrivilegeAdminister,
		0x29: PrivilegeAdminister,
	},
	// ClosureControl — Calibrate "M T" (closure-control.element.ts)
	0x0104: {0x02: PrivilegeManage},
	// Thermostat — AddThermostatSuggestion, RemoveThermostatSuggestion "M" (thermostat.element.ts)
	0x0201: {0x07: PrivilegeManage, 0x08: PrivilegeManage},
}

// InvokePrivilege returns the privilege a subject needs to invoke the
// command: the matter.js access level of the command, Operate when matter.js
// gives it none above the default. Mirrors matter.js
// AccessControl(command).limits.writeLevel (AccessControl.ts).
func InvokePrivilege(clusterID, commandID uint32) uint8 {
	if p, ok := elevatedInvokePrivileges[clusterID][commandID]; ok {
		return p
	}
	return PrivilegeOperate
}

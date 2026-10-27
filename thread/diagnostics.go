// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread

// The diagnostics of a Thread node, after matter.js
// packages/thread-br-client/src/diagnostic/DiagnosticResponse.ts and the
// record types of packages/thread-br-client/src/tlv/diag/ at 862ad75f.
// Only the records a border router's REST surface carries and the
// ThreadNetworkDiagnostics server reads are here; their MeshCoP TLV codecs
// (the decode/encode functions of tlv/diag/) are not, since MeshCoP stays
// out of the module (ADR 0027 decision 5).

// DeviceRole is a Thread node's role as OpenThread names it: the string
// the OTBR REST surface answers on /node/state and in /node
// (OtbrRestClient.ts:275-282, "router", "leader", "child", "disabled"),
// and OpenThread's otDeviceRole that connectedhomeip maps to the
// RoutingRole attribute (DirectThreadNetworkDiagnosticsProvider.cpp:104-145
// at 6170af84).
type DeviceRole string

// The roles of otDeviceRole.
const (
	RoleDisabled DeviceRole = "disabled"
	RoleDetached DeviceRole = "detached"
	RoleChild    DeviceRole = "child"
	RoleRouter   DeviceRole = "router"
	RoleLeader   DeviceRole = "leader"
)

// Attached reports whether the role is one of an attached node: neither
// disabled nor detached — the completion an activation waits for (ADR
// 0027 decision 4; GenericThreadBorderRouterDelegate.cpp:145-157 at
// 6170af84 completes on kConnectivity_Established).
func (r DeviceRole) Attached() bool {
	switch r {
	case RoleChild, RoleRouter, RoleLeader:
		return true
	}
	return false
}

// Mode is the MLE Mode record (tlv/diag/Mode.ts:21-25).
type Mode struct {
	RxOnWhenIdle     bool
	FullThreadDevice bool
	FullNetworkData  bool
}

// LeaderData is the Leader Data record (tlv/diag/LeaderData.ts:21-27;
// OtbrRestClient.ts:13-19).
type LeaderData struct {
	PartitionID       uint32
	Weighting         uint8
	DataVersion       uint8
	StableDataVersion uint8
	LeaderRouterID    uint8
}

// Route64Entry is one router of a Route64 record
// (tlv/diag/Route64.ts:32-37).
type Route64Entry struct {
	RouterID       uint8
	LinkQualityIn  uint8
	LinkQualityOut uint8
	RouteCost      uint8
}

// Route64 is the Route64 record (tlv/diag/Route64.ts:39-42): the ID
// sequence and one entry per allocated router, in router-id order.
type Route64 struct {
	IDSequence uint8
	Entries    []Route64Entry
}

// ChildTableEntry is one child of a Child Table record
// (tlv/diag/ChildTable.ts:20-28).
type ChildTableEntry struct {
	TimeoutExponent     uint8
	TimeoutSeconds      uint32
	IncomingLinkQuality uint8
	ChildID             uint16
	Mode                Mode
}

// MacCounters is the MAC Counters record, the RFC 2863 interface counters
// (tlv/diag/MacCounters.ts:17-27).
type MacCounters struct {
	IfInUnknownProtos  uint32
	IfInErrors         uint32
	IfOutErrors        uint32
	IfInUcastPkts      uint32
	IfInBroadcastPkts  uint32
	IfInDiscards       uint32
	IfOutUcastPkts     uint32
	IfOutBroadcastPkts uint32
	IfOutDiscards      uint32
}

// MleCounters is the MLE Counters record (tlv/diag/MleCounters.ts:24-40):
// the role counts and the time in each role in milliseconds.
type MleCounters struct {
	DisabledRole                  uint16
	DetachedRole                  uint16
	ChildRole                     uint16
	RouterRole                    uint16
	LeaderRole                    uint16
	AttachAttempts                uint16
	PartitionIDChanges            uint16
	BetterPartitionAttachAttempts uint16
	ParentChanges                 uint16
	TrackedTime                   uint64
	DisabledTime                  uint64
	DetachedTime                  uint64
	ChildTime                     uint64
	RouterTime                    uint64
	LeaderTime                    uint64
}

// DiagnosticResponse is one node's diagnostics record
// (diagnostic/DiagnosticResponse.ts:19-46), the subset this module reads:
// a nil field is a record the response did not carry.
type DiagnosticResponse struct {
	ExtMACAddress []byte
	Rloc16        *uint16
	Mode          *Mode
	Route64       *Route64
	LeaderData    *LeaderData
	MacCounters   *MacCounters
	MleCounters   *MleCounters
	ChildTable    []ChildTableEntry
	// Version is the Thread Version TLV (DiagnosticResponse.ts:38).
	Version *uint16
}

// NodeDiagnostics is what a Thread Border Router reports about itself: its
// role, addresses and leader data (OTBR REST /node, OtbrRestClient.ts:
// 21-32), the diagnostics record it serves about itself (Self, nil when
// it serves none) and every record of the mesh it last collected (Mesh,
// Self among them), which names the extended addresses of the routers
// and children its own record lists by RLOC16.
type NodeDiagnostics struct {
	Role       DeviceRole
	ExtAddress []byte
	Rloc16     uint16
	LeaderData LeaderData
	Self       *DiagnosticResponse
	Mesh       []DiagnosticResponse
}

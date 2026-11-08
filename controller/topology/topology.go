// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package topology

import (
	"net/netip"
	"time"
)

// Kind is a node's kind — matterjs-server's TopologyNodeKind at 0bd06f22
// (packages/ws-client/src/models/model.ts:237).
type Kind string

// The kinds.
const (
	// KindMatter is a node commissioned on the controller's fabric.
	KindMatter Kind = "matter"
	// KindThreadUnknown is a Thread device a commissioned node lists as a
	// neighbour and no commissioned node or known border router matches.
	KindThreadUnknown Kind = "thread_unknown"
	// KindBorderRouter is a Thread neighbour the border-router registry
	// knows by its extended address (thread/brdiscovery).
	KindBorderRouter Kind = "border_router"
	// KindWiFiAP is the pseudo-node of one Wi-Fi access point (BSSID).
	KindWiFiAP Kind = "wifi_ap"
)

// NetworkType is a node's network — the network_type of matterjs-server's
// NetworkTopologyNode at 0bd06f22 (packages/ws-client/src/models/model.ts:278).
// A [Connection] carries [NetworkThread] or [NetworkWiFi].
type NetworkType string

// The network types.
const (
	NetworkThread   NetworkType = "thread"
	NetworkWiFi     NetworkType = "wifi"
	NetworkEthernet NetworkType = "ethernet"
	NetworkUnknown  NetworkType = "unknown"
)

// Role is a node's role — matterjs-server's TopologyRole at 0bd06f22
// (packages/ws-client/src/models/model.ts:243-251): the Thread roles of
// ThreadNetworkDiagnostics RoutingRole, station and ap for Wi-Fi. The
// empty Role is an unknown one.
type Role string

// The roles.
const (
	RoleLeader          Role = "leader"
	RoleRouter          Role = "router"
	RoleREED            Role = "reed"
	RoleEndDevice       Role = "end_device"
	RoleSleepyEndDevice Role = "sleepy_end_device"
	RoleUnassigned      Role = "unassigned"
	RoleStation         Role = "station"
	RoleAP              Role = "ap"
)

// Strength is a link's quality bucket — matterjs-server's
// TopologyStrength at 0bd06f22 (packages/ws-client/src/models/model.ts:258):
// [StrengthNone] is a link observed dead, [StrengthUnknown] one without a
// measurement.
type Strength string

// The strengths.
const (
	StrengthStrong  Strength = "strong"
	StrengthMedium  Strength = "medium"
	StrengthWeak    Strength = "weak"
	StrengthNone    Strength = "none"
	StrengthUnknown Strength = "unknown"
)

// Topology is one snapshot of the graph — matterjs-server's
// NetworkTopology at 0bd06f22 (packages/ws-client/src/models/model.ts:343-348).
// Its slices are shared with every observer of the snapshot: read-only.
type Topology struct {
	// CollectedAt is when the snapshot was built.
	CollectedAt time.Time
	// Nodes are the commissioned nodes in node-id order, then the linked
	// Thread externals, then the access points.
	Nodes []Node
	// Connections are the Thread pairs, then the Wi-Fi station links.
	Connections []Connection
}

// Node is one node of the graph — matterjs-server's NetworkTopologyNode
// at 0bd06f22 (packages/ws-client/src/models/model.ts:273-315), with the
// node's operational addresses as go-fabric-server's port carries them.
type Node struct {
	// ID is the graph id: the decimal node id of a commissioned node,
	// br_<extended address, 16 upper-case hex digits> of a known border
	// router, unknown_<extended address> of another Thread external,
	// ap_<BSSID hex without colons> of an access point.
	ID          string
	Kind        Kind
	NetworkType NetworkType
	// NodeID is the operational node id of a commissioned node; 0 for
	// the others.
	NodeID uint64
	Role   Role
	// Available: the controller is connected to the node (commissioned
	// nodes only).
	Available bool
	// IsBridge: an endpoint of the node lists the Aggregator device type.
	IsBridge bool
	// ExtAddress is the Thread extended (MAC) address; 0 while unknown.
	ExtAddress uint64
	// Rloc16 is the Thread short address; nil while unknown (0x0000 is a
	// router's address).
	Rloc16 *uint16
	// ExtPanID is the Thread extended PAN id; 0 while unknown.
	ExtPanID uint64
	// NetworkName is the Thread network name, or the BSSID of an access
	// point (matterjs-server keeps it so for older clients).
	NetworkName string
	// SSID is the Wi-Fi network's name, of a station and its access
	// point; empty while unknown.
	SSID string
	// BSSID is the access point's BSSID ("AA:BB:CC:DD:EE:FF"), of a
	// station and its access point.
	BSSID string
	// Addresses are a commissioned node's known operational addresses,
	// sorted.
	Addresses []netip.Addr
	// HostName is a border router's mDNS host name as a display label,
	// without the trailing dot and ".local"; empty when nothing is left.
	HostName string
	// VendorName and ModelName are a border router's `_meshcop` vn and mn.
	VendorName, ModelName string
	// LastSeen is when the registry last saw the border router; zero for
	// the other kinds.
	LastSeen time.Time
}

// Connection is one edge — matterjs-server's NetworkTopologyConnection at
// 0bd06f22 (packages/ws-client/src/models/model.ts:323-337) without the
// route-table fields. A Thread pair has Source < Target by string order;
// a Wi-Fi link runs from the station to its access point.
type Connection struct {
	Source, Target string
	Network        NetworkType
	// Strength is the strongest observed direction.
	Strength Strength
	// SourceToTarget and TargetToSource are each direction as observed,
	// nil when that side reported no link.
	SourceToTarget, TargetToSource *Direction
}

// Direction is one direction's observed link quality — matterjs-server's
// TopologyDirectionInfo at 0bd06f22 (packages/ws-client/src/models/model.ts:261-267).
type Direction struct {
	Strength Strength
	// LQI is the Thread link quality indicator (0–3 on OpenThread); 0 on
	// a Wi-Fi link, which has none.
	LQI uint8
	// RSSI is the signal strength in dBm; nil without a measurement.
	RSSI *int8
}

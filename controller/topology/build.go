// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package topology

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	generaldiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	threaddiagdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
)

// Build derives the graph of images, collected at now. It is pure: the
// same images give the same graph, whatever their order (they are taken
// in node-id order).
//
// It is matterjs-server's #build at 0bd06f22
// (NetworkTopologyService.ts:305-431) without the border-router registry:
// a node per image (:325-375), the Thread externals and pairs from the
// neighbour tables (:377-395), the Wi-Fi star (:397-428).
func Build(images []Image, now time.Time) Topology {
	imgs := slices.Clone(images)
	// matterjs-server keys its Thread nodes by the decimal id in a
	// Record, which JavaScript iterates in ascending numeric order; the
	// node-id order makes every "first" and "last" below the same.
	slices.SortStableFunc(imgs, func(a, b Image) int { return cmp.Compare(a.NodeID, b.NodeID) })

	t := Topology{CollectedAt: now, Nodes: []Node{}, Connections: []Connection{}}
	var thread []*Image
	var radios []radio
	for i := range imgs {
		n := matterNode(&imgs[i])
		switch n.NetworkType {
		case NetworkThread:
			thread = append(thread, &imgs[i])
		case NetworkWiFi:
			radios = append(radios, radio{id: n.ID, ssid: n.SSID, bssid: n.BSSID, rssi: imgs[i].WiFiNetworkDiagnostics.Rssi})
		case NetworkEthernet, NetworkUnknown:
		}
		t.Nodes = append(t.Nodes, n)
	}
	addThread(&t, thread)
	addWiFi(&t, radios)
	return t
}

// radio is a Wi-Fi station's link to its access point.
type radio struct {
	id, ssid, bssid string
	rssi            *int8
}

// matterNode is the node of a commissioned image — matterjs-server's node
// build at 0bd06f22 (NetworkTopologyService.ts:325-375): a Thread node with
// its role, extended address, RLOC16, extended PAN id and network name, a
// Wi-Fi station with its SSID and BSSID, any other node bare.
func matterNode(img *Image) Node {
	n := Node{
		ID: strconv.FormatUint(img.NodeID, 10), Kind: KindMatter, NodeID: img.NodeID,
		NetworkType: networkType(img.NetworkFeatures), Available: img.Available, IsBridge: img.IsBridge,
		Addresses: slices.Clone(img.Addresses),
	}
	switch n.NetworkType {
	case NetworkThread:
		td := &img.ThreadNetworkDiagnostics
		n.Role = threadRole(td.RoutingRole)
		n.ExtAddress = threadExtAddress(img.GeneralDiagnostics.NetworkInterfaces)
		if td.Rloc16 != nil {
			r := *td.Rloc16
			n.Rloc16 = &r
		}
		if td.ExtendedPanId != nil {
			n.ExtPanID = *td.ExtendedPanId
		}
		if td.NetworkName != nil {
			n.NetworkName = *td.NetworkName
		}
	case NetworkWiFi:
		n.Role = RoleStation
		n.SSID = wifiSSID(img.NetworkCommissioning.Networks)
		n.BSSID = formatBSSID(img.WiFiNetworkDiagnostics.Bssid)
	case NetworkEthernet, NetworkUnknown:
	}
	return n
}

// addThread adds the Thread externals and one connection per kept pair;
// an external is published only when a kept pair links it
// (NetworkTopologyService.ts:377-395, matterjs-server at 0bd06f22).
func addThread(t *Topology, thread []*Image) {
	m := newThreadMaps(thread)
	externals := m.unknowns(thread)
	linked := map[string]bool{}
	for _, p := range m.pairs(thread, externals) {
		c, ok := connectionOf(p)
		if !ok {
			continue
		}
		t.Connections = append(t.Connections, c)
		linked[c.Source], linked[c.Target] = true, true
	}
	for _, e := range externals {
		if !linked[e.id] {
			continue
		}
		n := Node{ID: e.id, Kind: KindThreadUnknown, NetworkType: NetworkThread, ExtAddress: e.ext, ExtPanID: e.extPanID}
		if e.router {
			n.Role = RoleRouter
		}
		t.Nodes = append(t.Nodes, n)
	}
}

// addWiFi adds one access point per BSSID and a link from each station
// (NetworkTopologyService.ts:397-428, matterjs-server at 0bd06f22). The
// AP takes the first SSID a station reports — the SSID names the
// network, so a station without one does not settle it (:417-418).
func addWiFi(t *Topology, radios []radio) {
	aps := map[string]int{}
	for _, r := range radios {
		if r.bssid == "" {
			continue
		}
		id := "ap_" + strings.ReplaceAll(r.bssid, ":", "")
		i, ok := aps[id]
		if !ok {
			t.Nodes = append(t.Nodes, Node{ID: id, Kind: KindWiFiAP, NetworkType: NetworkWiFi, Role: RoleAP, NetworkName: r.bssid, BSSID: r.bssid})
			i = len(t.Nodes) - 1
			aps[id] = i
		}
		if t.Nodes[i].SSID == "" {
			t.Nodes[i].SSID = r.ssid
		}
		s := rssiStrength(r.rssi)
		c := Connection{Source: r.id, Target: id, Network: NetworkWiFi, Strength: s}
		if r.rssi != nil {
			v := *r.rssi
			c.SourceToTarget = &Direction{Strength: s, RSSI: &v}
		}
		t.Connections = append(t.Connections, c)
	}
}

// networkType decides a node's network from its root NetworkCommissioning
// features, Thread before Wi-Fi before Ethernet — matterjs-server's
// getNetworkTypeFromAttributes at 0bd06f22 (topology-utils.ts:89-108).
func networkType(f netcommdef.Feature) NetworkType {
	switch {
	case f&netcommdef.FeatureThreadNetworkInterface != 0:
		return NetworkThread
	case f&netcommdef.FeatureWiFiNetworkInterface != 0:
		return NetworkWiFi
	case f&netcommdef.FeatureEthernetNetworkInterface != 0:
		return NetworkEthernet
	}
	return NetworkUnknown
}

// threadRole is matterjs-server's mapThreadRole at 0bd06f22
// (NetworkTopologyService.ts:435-452): Unspecified, null and an unknown
// value are no role.
func threadRole(r *threaddiagdef.RoutingRoleEnum) Role {
	if r == nil {
		return ""
	}
	switch *r {
	case threaddiagdef.RoutingRoleLeader:
		return RoleLeader
	case threaddiagdef.RoutingRoleRouter:
		return RoleRouter
	case threaddiagdef.RoutingRoleReed:
		return RoleREED
	case threaddiagdef.RoutingRoleEndDevice:
		return RoleEndDevice
	case threaddiagdef.RoutingRoleSleepyEndDevice:
		return RoleSleepyEndDevice
	case threaddiagdef.RoutingRoleUnassigned:
		return RoleUnassigned
	case threaddiagdef.RoutingRoleUnspecified:
	}
	return ""
}

// threadExtAddress is a Thread node's extended address from its
// GeneralDiagnostics NetworkInterfaces: the Thread interface's hardware
// address, else the first interface's; the last eight bytes big-endian; 0
// is none — matterjs-server's getThreadExtendedAddress and base64ToBigInt
// at 0bd06f22 (topology-utils.ts:197-222, :50-66).
func threadExtAddress(ifaces []generaldiagdef.NetworkInterface) uint64 {
	if len(ifaces) == 0 {
		return 0
	}
	iface := ifaces[0]
	for _, i := range ifaces {
		if i.Type == generaldiagdef.InterfaceTypeThread {
			iface = i
			break
		}
	}
	hw := iface.HardwareAddress
	if len(hw) > 8 {
		hw = hw[len(hw)-8:]
	}
	var v uint64
	for _, b := range hw {
		v = v<<8 | uint64(b)
	}
	return v
}

// lqiStrength classifies a Thread LQI on OpenThread's 0–3 scale: 0 none,
// above 2 strong, above 1 medium, else weak — matterjs-server's
// getSignalLevelFromLqi at 0bd06f22 (topology-utils.ts:754-759, thresholds
// LQI_STRONG_THRESHOLD and LQI_MEDIUM_THRESHOLD at :42-43).
func lqiStrength(lqi uint8) Strength {
	switch {
	case lqi == 0:
		return StrengthNone
	case lqi > 2:
		return StrengthStrong
	case lqi > 1:
		return StrengthMedium
	}
	return StrengthWeak
}

// rssiStrength classifies a Wi-Fi RSSI: above -70 dBm strong, above -85
// medium, else weak, no measurement unknown — matterjs-server's
// rssiToStrength at 0bd06f22 (NetworkTopologyService.ts:517-522).
func rssiStrength(rssi *int8) Strength {
	switch {
	case rssi == nil:
		return StrengthUnknown
	case *rssi > -70:
		return StrengthStrong
	case *rssi > -85:
		return StrengthMedium
	}
	return StrengthWeak
}

// levelRank orders the strengths, unknown lowest — matterjs-server's
// LEVEL_RANK at 0bd06f22 (NetworkTopologyService.ts:507).
var levelRank = map[Strength]int{StrengthUnknown: -1, StrengthNone: 0, StrengthWeak: 1, StrengthMedium: 2, StrengthStrong: 3}

// formatBSSID is the BSSID as upper-case hex pairs joined by colons; ""
// for none — matterjs-server's getWiFiDiagnostics at 0bd06f22
// (topology-utils.ts:1033-1046).
func formatBSSID(b *[]byte) string {
	if b == nil || len(*b) == 0 {
		return ""
	}
	parts := make([]string, len(*b))
	for i, c := range *b {
		parts[i] = fmt.Sprintf("%02X", c)
	}
	return strings.Join(parts, ":")
}

// wifiSSID is the SSID of the network the node is connected to: the
// NetworkCommissioning Networks entry with Connected set, its NetworkId
// of 1 to 32 bytes valid UTF-8 without control characters; "" otherwise —
// matterjs-server's getWiFiSsid at 0bd06f22 (topology-utils.ts:1076-1107).
func wifiSSID(networks []netcommdef.NetworkInfoStruct) string {
	i := slices.IndexFunc(networks, func(n netcommdef.NetworkInfoStruct) bool { return n.Connected })
	if i < 0 {
		return ""
	}
	id := networks[i].NetworkId
	if len(id) == 0 || len(id) > 32 || !utf8.Valid(id) {
		return ""
	}
	if strings.ContainsFunc(string(id), func(r rune) bool { return r <= 0x1f || r == 0x7f }) {
		return ""
	}
	return string(id)
}

// hex16 is v as 16 upper-case hex digits.
func hex16(v uint64) string { return fmt.Sprintf("%016X", v) }

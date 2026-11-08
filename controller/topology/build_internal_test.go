// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package topology

import (
	"net/netip"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	generaldiagdef "github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	netcommdef "github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	threaddiagdef "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdiagnostics"
	"github.com/SukramJ/go-fabric/thread/brdiscovery"
)

// The fixtures of matterjs-server's NetworkTopologyServiceTest.ts at
// 0bd06f22 (packages/ws-controller/test/NetworkTopologyServiceTest.ts:26-36).
const (
	extPan   = uint64(0x1122334455667788)
	node1Ext = uint64(0xAABBCCDDEEFF0001)
	node2Ext = uint64(0xAABBCCDDEEFF0002)
	brExt    = uint64(0x1122AABBCC334455)
	brID     = "br_1122AABBCC334455"
	brUnkID  = "unknown_1122AABBCC334455" // the same neighbour with no registry entry
	apID     = "ap_112233445566"
	bssidStr = "11:22:33:44:55:66"
)

var bssidBytes = []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}

// makeBr is the test's makeBr (NetworkTopologyServiceTest.ts:93-106,
// matterjs-server at 0bd06f22), the registry entry of the border router
// node 1 neighbours.
func makeBr() brdiscovery.Entry {
	return brdiscovery.Entry{
		ExtAddress: "1122AABBCC334455", ExtPanID: "1122334455667788", NetworkName: "OpenThread",
		VendorName: "Apple", ModelName: "eero", HostName: "Cuisine.local.", LastSeen: time.UnixMilli(1000),
	}
}

func ptr[T any](v T) *T { return &v }

func be8(v uint64) []byte {
	b := make([]byte, 8)
	for i := range b {
		b[7-i] = byte(v >> (8 * i))
	}
	return b
}

// neighbour is one NeighborTable entry (the test's neighbor, :39-47);
// rssi nil is null.
func neighbour(ext uint64, rloc uint16, lqi uint8, rssi *int8, rxOn bool) threaddiagdef.NeighborTableStruct {
	n := threaddiagdef.NeighborTableStruct{ExtAddress: ext, Rloc16: rloc, Lqi: lqi, RxOnWhenIdle: rxOn, LastRssi: spec.Nullable[int8]{Null: true}}
	if rssi == nil {
		n.AverageRssi.Null = true
	} else {
		n.AverageRssi.Value = *rssi
	}
	return n
}

// threadOpts is the test's ThreadNodeOpts (:49-57).
type threadOpts struct {
	role        *threaddiagdef.RoutingRoleEnum
	rloc16      *uint16
	ext         uint64
	neighbours  []threaddiagdef.NeighborTableStruct
	offline     bool
	bridge      bool
	networkName *string
}

// mkThread is the test's mkThread (:59-70): the Thread feature and the
// extended PAN id always, the rest when given.
func mkThread(id uint64, o threadOpts) Image {
	img := Image{NodeID: id, Available: !o.offline, IsBridge: o.bridge, NetworkFeatures: netcommdef.FeatureThreadNetworkInterface}
	td := &img.ThreadNetworkDiagnostics
	td.ExtendedPanId = ptr(extPan)
	td.RoutingRole, td.Rloc16, td.NeighborTable, td.NetworkName = o.role, o.rloc16, o.neighbours, o.networkName
	if o.ext != 0 {
		img.GeneralDiagnostics.NetworkInterfaces = []generaldiagdef.NetworkInterface{{HardwareAddress: be8(o.ext), Type: generaldiagdef.InterfaceTypeThread}}
	}
	return img
}

// mkWifi is the test's mkWifi (:72-87): SSID "MyWiFi" unless noSSID, the
// BSSID 11:22:33:44:55:66, the RSSI given (nil is null).
func mkWifi(id uint64, rssi *int8, noSSID bool) Image {
	img := Image{NodeID: id, Available: true, NetworkFeatures: netcommdef.FeatureWiFiNetworkInterface}
	if !noSSID {
		img.NetworkCommissioning.Networks = []netcommdef.NetworkInfoStruct{{NetworkId: []byte("MyWiFi"), Connected: true}}
	}
	img.WiFiNetworkDiagnostics.Bssid = ptr(slices.Clone(bssidBytes))
	img.WiFiNetworkDiagnostics.Rssi = rssi
	return img
}

func role(r threaddiagdef.RoutingRoleEnum) *threaddiagdef.RoutingRoleEnum { return &r }

// fullMesh is the test's fullMeshHarness (:215-233): a leader and a
// router that see each other (node 2 matched by RLOC16), the leader's
// border-router neighbour, a Wi-Fi station.
func fullMesh() []Image {
	return []Image{
		mkThread(1, threadOpts{role: role(threaddiagdef.RoutingRoleLeader), rloc16: ptr[uint16](1024), ext: node1Ext, neighbours: []threaddiagdef.NeighborTableStruct{
			neighbour(0, 1025, 3, ptr[int8](-40), false),
			neighbour(brExt, 61440, 2, ptr[int8](-70), true),
		}}),
		mkThread(2, threadOpts{role: role(threaddiagdef.RoutingRoleRouter), rloc16: ptr[uint16](1025), ext: node2Ext, neighbours: []threaddiagdef.NeighborTableStruct{
			neighbour(0, 1024, 2, ptr[int8](-60), false),
		}}),
		mkWifi(5, ptr[int8](-55), false),
	}
}

var at = time.UnixMilli(1735689600000)

func byID(t Topology) map[string]Node {
	m := map[string]Node{}
	for i := range t.Nodes {
		m[t.Nodes[i].ID] = t.Nodes[i]
	}
	return m
}

func connection(t Topology, source, target string) (Connection, bool) {
	for _, c := range t.Connections {
		if c.Source == source && c.Target == target {
			return c, true
		}
	}
	return Connection{}, false
}

// TestBuildSyntheticMesh ports "maps a synthetic mesh to matter /
// border_router / wifi_ap wire nodes" (NetworkTopologyServiceTest.ts:235-282,
// matterjs-server at 0bd06f22): the neighbour the registry knows is its
// border_router node, its host name stripped to a label, and node 1's
// network name is the registry's for its extended PAN id.
func TestBuildSyntheticMesh(t *testing.T) {
	topo := Build(fullMesh(), []brdiscovery.Entry{makeBr()}, at)
	if len(topo.Nodes) != 5 || !topo.CollectedAt.Equal(at) {
		t.Fatalf("nodes %d, collected %v; want 5 at %v", len(topo.Nodes), topo.CollectedAt, at)
	}
	m := byID(topo)
	want := Node{ID: "1", Kind: KindMatter, NetworkType: NetworkThread, NodeID: 1, Role: RoleLeader, Available: true, ExtAddress: node1Ext, Rloc16: ptr[uint16](1024), ExtPanID: extPan, NetworkName: "OpenThread"}
	if got := m["1"]; !reflect.DeepEqual(got, want) {
		t.Errorf("node 1 = %+v\nwant %+v", got, want)
	}
	if hex16(m["1"].ExtAddress) != "AABBCCDDEEFF0001" || hex16(m["1"].ExtPanID) != "1122334455667788" {
		t.Errorf("node 1 ext %s, xpan %s", hex16(m["1"].ExtAddress), hex16(m["1"].ExtPanID))
	}
	if m["2"].Role != RoleRouter {
		t.Errorf("node 2 role %q", m["2"].Role)
	}
	wifi := m["5"]
	if wifi.Kind != KindMatter || wifi.NetworkType != NetworkWiFi || wifi.Role != RoleStation || wifi.SSID != "MyWiFi" || wifi.BSSID != bssidStr {
		t.Errorf("station = %+v", wifi)
	}
	wantBR := Node{
		ID: brID, Kind: KindBorderRouter, NetworkType: NetworkThread, Role: RoleRouter, ExtAddress: brExt, ExtPanID: extPan,
		NetworkName: "OpenThread", HostName: "Cuisine", VendorName: "Apple", ModelName: "eero", LastSeen: time.UnixMilli(1000),
	}
	if br := m[brID]; !reflect.DeepEqual(br, wantBR) {
		t.Errorf("border router = %+v\nwant %+v", br, wantBR)
	}
	ap := m[apID]
	if want := (Node{ID: apID, Kind: KindWiFiAP, NetworkType: NetworkWiFi, Role: RoleAP, NetworkName: bssidStr, SSID: "MyWiFi", BSSID: bssidStr}); !reflect.DeepEqual(ap, want) {
		t.Errorf("AP = %+v\nwant %+v", ap, want)
	}
}

// TestBuildThreadPair ports "folds both directions of a Thread link and
// summarises the strongest" and "links a Matter node to the external
// Border Router it neighbours" (NetworkTopologyServiceTest.ts:347-368,
// matterjs-server at 0bd06f22).
func TestBuildThreadPair(t *testing.T) {
	topo := Build(fullMesh(), []brdiscovery.Entry{makeBr()}, at)
	link, ok := connection(topo, "1", "2")
	if !ok || link.Network != NetworkThread || link.Strength != StrengthStrong {
		t.Fatalf("link 1-2 = %+v (%v)", link, ok)
	}
	if want := (&Direction{Strength: StrengthStrong, LQI: 3, RSSI: ptr[int8](-40)}); !reflect.DeepEqual(link.SourceToTarget, want) {
		t.Errorf("1→2 = %+v", link.SourceToTarget)
	}
	if want := (&Direction{Strength: StrengthMedium, LQI: 2, RSSI: ptr[int8](-60)}); !reflect.DeepEqual(link.TargetToSource, want) {
		t.Errorf("2→1 = %+v", link.TargetToSource)
	}
	ext, ok := connection(topo, "1", brID)
	if !ok || ext.Strength != StrengthMedium || ext.TargetToSource != nil ||
		!reflect.DeepEqual(ext.SourceToTarget, &Direction{Strength: StrengthMedium, LQI: 2, RSSI: ptr[int8](-70)}) {
		t.Errorf("link 1-external = %+v (%v)", ext, ok)
	}
}

// TestBuildWiFiStar ports "builds a Wi-Fi star", "reports an unmeasured
// Wi-Fi RSSI as strength unknown with no direction detail", "omits ssid
// when the device reports no NetworkCommissioning network" and "names the
// AP from a later station when the first one reports no network"
// (NetworkTopologyServiceTest.ts:284-311, :370-388, matterjs-server at
// 0bd06f22).
func TestBuildWiFiStar(t *testing.T) {
	topo := Build(fullMesh(), nil, at)
	c, ok := connection(topo, "5", apID)
	if !ok || c.Network != NetworkWiFi || c.Strength != StrengthStrong ||
		!reflect.DeepEqual(c.SourceToTarget, &Direction{Strength: StrengthStrong, RSSI: ptr[int8](-55)}) || c.TargetToSource != nil {
		t.Errorf("Wi-Fi link = %+v (%v)", c, ok)
	}

	topo = Build([]Image{mkWifi(5, nil, false)}, nil, at)
	if c, _ := connection(topo, "5", apID); c.Strength != StrengthUnknown || c.SourceToTarget != nil {
		t.Errorf("unmeasured RSSI: %+v", c)
	}

	m := byID(Build([]Image{mkWifi(5, ptr[int8](-55), true)}, nil, at))
	if m[apID].SSID != "" || m[apID].BSSID != bssidStr || m["5"].SSID != "" {
		t.Errorf("no network: AP %+v, station %+v", m[apID], m["5"])
	}

	topo = Build([]Image{mkWifi(5, ptr[int8](-55), true), mkWifi(6, ptr[int8](-55), false)}, nil, at)
	m = byID(topo)
	if len(topo.Nodes) != 3 || len(topo.Connections) != 2 ||
		topo.Connections[0].Source != "5" || topo.Connections[1].Source != "6" || topo.Connections[1].Target != apID {
		t.Fatalf("two stations of one AP: %+v", topo)
	}
	if m[apID].SSID != "MyWiFi" || m["5"].SSID != "" || m["6"].SSID != "MyWiFi" {
		t.Errorf("AP %q, stations %q %q", m[apID].SSID, m["5"].SSID, m["6"].SSID)
	}
}

// TestBuildExternals ports "classifies an unmatched neighbour as
// thread_unknown when no BR matches" and "drops an external whose only
// neighbour record is a dead link" (NetworkTopologyServiceTest.ts:390-416,
// matterjs-server at 0bd06f22) and findUnknownDevices' "classifies an
// unmatched neighbor as an unknown device" (TopologyUtilsTest.ts:238-246):
// plus one neighbour two nodes see, which is one external with the first
// observer's extended PAN id, a router if either saw it rx-on-when-idle,
// and a pair per observer.
func TestBuildExternals(t *testing.T) {
	one := mkThread(1, threadOpts{role: role(threaddiagdef.RoutingRoleRouter), rloc16: ptr[uint16](1024), neighbours: []threaddiagdef.NeighborTableStruct{
		neighbour(brExt, 61440, 1, ptr[int8](-90), true),
	}})
	topo := Build([]Image{one}, nil, at)
	if n := byID(topo)[brUnkID]; n.Kind != KindThreadUnknown || n.NetworkType != NetworkThread || n.Role != RoleRouter {
		t.Errorf("unmatched neighbour = %+v", n)
	}

	dead := mkThread(1, threadOpts{role: role(threaddiagdef.RoutingRoleRouter), rloc16: ptr[uint16](1024), neighbours: []threaddiagdef.NeighborTableStruct{
		neighbour(brExt, 61440, 0, nil, true),
	}})
	topo = Build([]Image{dead}, nil, at)
	if len(topo.Connections) != 0 || len(topo.Nodes) != 1 || topo.Nodes[0].ID != "1" {
		t.Errorf("dead link: %+v", topo)
	}

	a := mkThread(1, threadOpts{rloc16: ptr[uint16](0x0400), neighbours: []threaddiagdef.NeighborTableStruct{neighbour(0xDEADBEEF, 0, 1, nil, false)}})
	b := mkThread(2, threadOpts{rloc16: ptr[uint16](0x0800), neighbours: []threaddiagdef.NeighborTableStruct{neighbour(0xDEADBEEF, 0, 3, nil, true)}})
	b.ThreadNetworkDiagnostics.ExtendedPanId = ptr[uint64](0x99)
	topo = Build([]Image{b, a}, nil, at)
	var ext []Node
	for _, n := range topo.Nodes {
		if n.Kind == KindThreadUnknown {
			ext = append(ext, n)
		}
	}
	if len(ext) != 1 || ext[0].ID != "unknown_00000000DEADBEEF" || ext[0].ExtPanID != extPan || ext[0].Role != RoleRouter {
		t.Fatalf("shared external = %+v", ext)
	}
	c1, ok1 := connection(topo, "1", "unknown_00000000DEADBEEF")
	c2, ok2 := connection(topo, "2", "unknown_00000000DEADBEEF")
	if !ok1 || !ok2 || c1.Strength != StrengthWeak || c2.Strength != StrengthStrong {
		t.Errorf("pairs to the external: %+v %+v", c1, c2)
	}
}

// TestBuildNodeFlags ports "prefers the node's own Thread network name",
// "marks an offline node unavailable and passes the bridge flag through"
// and "emits an unlinked node for an ethernet device"
// (NetworkTopologyServiceTest.ts:418-444, matterjs-server at 0bd06f22),
// and a node without the clusters, whose network is unknown.
func TestBuildNodeFlags(t *testing.T) {
	m := byID(Build([]Image{mkThread(1, threadOpts{networkName: ptr("OwnNet")})}, nil, at))
	if m["1"].NetworkName != "OwnNet" {
		t.Errorf("network name %q", m["1"].NetworkName)
	}
	m = byID(Build([]Image{
		mkThread(1, threadOpts{rloc16: ptr[uint16](1024), bridge: true}),
		mkThread(2, threadOpts{rloc16: ptr[uint16](1025), offline: true}),
	}, nil, at))
	if !m["1"].IsBridge || m["1"].Available != true || m["2"].Available {
		t.Errorf("flags: %+v %+v", m["1"], m["2"])
	}
	topo := Build([]Image{{NodeID: 1, Available: true, NetworkFeatures: netcommdef.FeatureEthernetNetworkInterface}, {NodeID: 7}}, nil, at)
	if len(topo.Nodes) != 2 || topo.Nodes[0].NetworkType != NetworkEthernet || topo.Nodes[1].NetworkType != NetworkUnknown || len(topo.Connections) != 0 {
		t.Errorf("ethernet and bare nodes: %+v", topo)
	}
	if topo.Nodes[1].Role != "" || topo.Nodes[1].Rloc16 != nil {
		t.Errorf("bare node = %+v", topo.Nodes[1])
	}
}

// TestBuildLikeTheServerPort holds the build to go-fabric-server's
// TestBuildTopology (controllerapi/topology_internal_test.go:26-87): two
// Thread nodes seeing each other by extended address and by RLOC16, an
// unknown router neighbour, a dead neighbour dropped, a Wi-Fi station
// with its SSID and addresses, an Ethernet node without links.
func TestBuildLikeTheServerPort(t *testing.T) {
	n1 := mkThread(1, threadOpts{role: role(threaddiagdef.RoutingRoleRouter), rloc16: ptr[uint16](1024), ext: 0x0102030405060708, networkName: ptr("Mesh"), neighbours: []threaddiagdef.NeighborTableStruct{
		neighbour(0x1011121314151617, 2048, 3, ptr[int8](-40), true),
		neighbour(0xDEADBEEF, 0, 1, nil, true),
		neighbour(0xFEE1DEAD, 0, 0, nil, false),
	}})
	n1.ThreadNetworkDiagnostics.ExtendedPanId = ptr[uint64](0x1111111111111111)
	n2 := mkThread(2, threadOpts{
		role: role(threaddiagdef.RoutingRoleSleepyEndDevice), rloc16: ptr[uint16](2048), ext: 0x1011121314151617, offline: true, bridge: true,
		neighbours: []threaddiagdef.NeighborTableStruct{neighbour(0, 1024, 2, nil, false)},
	})
	n2.ThreadNetworkDiagnostics.ExtendedPanId = nil
	n3 := Image{NodeID: 3, Available: true, NetworkFeatures: netcommdef.FeatureWiFiNetworkInterface, Addresses: []netip.Addr{netip.MustParseAddr("192.168.1.3")}}
	n3.WiFiNetworkDiagnostics.Bssid = ptr([]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55})
	n3.WiFiNetworkDiagnostics.Rssi = ptr[int8](-60)
	n3.NetworkCommissioning.Networks = []netcommdef.NetworkInfoStruct{{NetworkId: []byte("home"), Connected: true}}
	n4 := Image{NodeID: 4, Available: true, NetworkFeatures: netcommdef.FeatureEthernetNetworkInterface}
	topo := Build([]Image{n4, n3, n2, n1}, nil, at)

	want := Topology{CollectedAt: at, Nodes: []Node{
		{ID: "1", Kind: KindMatter, NetworkType: NetworkThread, NodeID: 1, Role: RoleRouter, Available: true, ExtAddress: 0x0102030405060708, Rloc16: ptr[uint16](1024), ExtPanID: 0x1111111111111111, NetworkName: "Mesh"},
		{ID: "2", Kind: KindMatter, NetworkType: NetworkThread, NodeID: 2, Role: RoleSleepyEndDevice, IsBridge: true, ExtAddress: 0x1011121314151617, Rloc16: ptr[uint16](2048)},
		{ID: "3", Kind: KindMatter, NetworkType: NetworkWiFi, NodeID: 3, Role: RoleStation, Available: true, SSID: "home", BSSID: "00:11:22:33:44:55", Addresses: []netip.Addr{netip.MustParseAddr("192.168.1.3")}},
		{ID: "4", Kind: KindMatter, NetworkType: NetworkEthernet, NodeID: 4, Available: true},
		{ID: "unknown_00000000DEADBEEF", Kind: KindThreadUnknown, NetworkType: NetworkThread, Role: RoleRouter, ExtAddress: 0xDEADBEEF, ExtPanID: 0x1111111111111111},
		{ID: "ap_001122334455", Kind: KindWiFiAP, NetworkType: NetworkWiFi, Role: RoleAP, NetworkName: "00:11:22:33:44:55", SSID: "home", BSSID: "00:11:22:33:44:55"},
	}, Connections: []Connection{
		{Source: "1", Target: "2", Network: NetworkThread, Strength: StrengthStrong, SourceToTarget: &Direction{Strength: StrengthStrong, LQI: 3, RSSI: ptr[int8](-40)}, TargetToSource: &Direction{Strength: StrengthMedium, LQI: 2}},
		{Source: "1", Target: "unknown_00000000DEADBEEF", Network: NetworkThread, Strength: StrengthWeak, SourceToTarget: &Direction{Strength: StrengthWeak, LQI: 1}},
		{Source: "3", Target: "ap_001122334455", Network: NetworkWiFi, Strength: StrengthStrong, SourceToTarget: &Direction{Strength: StrengthStrong, RSSI: ptr[int8](-60)}},
	}}
	if !reflect.DeepEqual(topo, want) {
		t.Errorf("Build =\n%+v\nwant\n%+v", topo, want)
	}
}

// TestNetworkTypeAndThresholds ports getNetworkType and
// getSignalLevelFromLqi (TopologyUtilsTest.ts:48-75, matterjs-server at
// 0bd06f22) and pins rssiToStrength's -70/-85 boundaries
// (NetworkTopologyService.ts:517-522) and mapThreadRole
// (NetworkTopologyService.ts:435-452).
func TestNetworkTypeAndThresholds(t *testing.T) {
	for f, want := range map[netcommdef.Feature]NetworkType{
		0: NetworkUnknown, netcommdef.FeatureThreadNetworkInterface: NetworkThread,
		netcommdef.FeatureWiFiNetworkInterface: NetworkWiFi, netcommdef.FeatureEthernetNetworkInterface: NetworkEthernet,
		netcommdef.FeatureWiFiNetworkInterface | netcommdef.FeatureThreadNetworkInterface:   NetworkThread,
		netcommdef.FeatureWiFiNetworkInterface | netcommdef.FeatureEthernetNetworkInterface: NetworkWiFi,
	} {
		if got := networkType(f); got != want {
			t.Errorf("networkType(%#x) = %q, want %q", f, got, want)
		}
	}
	for lqi, want := range map[uint8]Strength{0: StrengthNone, 1: StrengthWeak, 2: StrengthMedium, 3: StrengthStrong, 255: StrengthStrong} {
		if got := lqiStrength(lqi); got != want {
			t.Errorf("lqiStrength(%d) = %q, want %q", lqi, got, want)
		}
	}
	if rssiStrength(nil) != StrengthUnknown {
		t.Error("nil RSSI is not unknown")
	}
	for rssi, want := range map[int8]Strength{-55: StrengthStrong, -69: StrengthStrong, -70: StrengthMedium, -84: StrengthMedium, -85: StrengthWeak, -100: StrengthWeak} {
		if got := rssiStrength(&rssi); got != want {
			t.Errorf("rssiStrength(%d) = %q, want %q", rssi, got, want)
		}
	}
	for r, want := range map[threaddiagdef.RoutingRoleEnum]Role{0: "", 1: RoleUnassigned, 2: RoleSleepyEndDevice, 3: RoleEndDevice, 4: RoleREED, 5: RoleRouter, 6: RoleLeader, 7: ""} {
		if got := threadRole(&r); got != want {
			t.Errorf("threadRole(%d) = %q, want %q", r, got, want)
		}
	}
	if threadRole(nil) != "" {
		t.Error("a null role is a role")
	}
}

// TestThreadMatching ports buildExtAddrMap / buildRloc16Map and
// buildThreadEdgePairs' "merges the two directions" and "does not create
// self-edges" (TopologyUtilsTest.ts:179-227, matterjs-server at
// 0bd06f22), and the extended address getThreadExtendedAddress reads
// (topology-utils.ts:197-222): the Thread interface before the first, the
// last eight bytes of a longer hardware address, 0 for none.
func TestThreadMatching(t *testing.T) {
	one := mkThread(1, threadOpts{rloc16: ptr[uint16](1024), ext: 0xAABBCCDDEEFF0011})
	m := newThreadMaps([]*Image{&one})
	if m.byExt[0xAABBCCDDEEFF0011] != "1" || m.byRloc[1024] != "1" {
		t.Errorf("maps = %+v", m)
	}
	a := mkThread(1, threadOpts{rloc16: ptr[uint16](1024), neighbours: []threaddiagdef.NeighborTableStruct{neighbour(0, 1025, 3, ptr[int8](-40), false)}})
	b := mkThread(2, threadOpts{rloc16: ptr[uint16](1025), neighbours: []threaddiagdef.NeighborTableStruct{neighbour(0, 1024, 2, ptr[int8](-60), false)}})
	m = newThreadMaps([]*Image{&a, &b})
	ps := m.pairs([]*Image{&a, &b}, nil)
	if len(ps) != 1 || ps[0].a != "1" || ps[0].b != "2" || ps[0].ab.Strength != StrengthStrong || ps[0].ba.Strength != StrengthMedium {
		t.Errorf("pairs = %+v", ps)
	}
	self := mkThread(1, threadOpts{rloc16: ptr[uint16](1024), neighbours: []threaddiagdef.NeighborTableStruct{neighbour(0, 1024, 3, nil, false)}})
	m = newThreadMaps([]*Image{&self})
	if ps := m.pairs([]*Image{&self}, m.unknowns([]*Image{&self}, borderRouterIndex{})); len(ps) != 0 {
		t.Errorf("self-edge: %+v", ps)
	}

	wifiFirst := []generaldiagdef.NetworkInterface{
		{HardwareAddress: []byte{1, 2, 3, 4, 5, 6}, Type: generaldiagdef.InterfaceTypeWiFi},
		{HardwareAddress: append([]byte{0x99}, be8(0xAABBCCDDEEFF0011)...), Type: generaldiagdef.InterfaceTypeThread},
	}
	if got := threadExtAddress(wifiFirst); got != 0xAABBCCDDEEFF0011 {
		t.Errorf("ext = %#x", got)
	}
	if got := threadExtAddress(wifiFirst[:1]); got != 0x010203040506 {
		t.Errorf("first interface ext = %#x", got)
	}
	if threadExtAddress(nil) != 0 || threadExtAddress([]generaldiagdef.NetworkInterface{{}}) != 0 {
		t.Error("no hardware address is an address")
	}
	// The first direction an observer lists wins.
	dup := mkThread(1, threadOpts{rloc16: ptr[uint16](1024), neighbours: []threaddiagdef.NeighborTableStruct{neighbour(0, 1025, 1, nil, false), neighbour(0, 1025, 3, nil, false)}})
	m = newThreadMaps([]*Image{&dup, &b})
	if ps := m.pairs([]*Image{&dup}, nil); len(ps) != 1 || ps[0].ab.LQI != 1 {
		t.Errorf("first direction: %+v", ps)
	}
	// A neighbour's RSSI is its average, else its last.
	nb := neighbour(0, 0, 1, nil, false)
	nb.LastRssi = spec.Nullable[int8]{Value: -77}
	if r := neighbourRSSI(&nb); r == nil || *r != -77 {
		t.Errorf("last RSSI fallback = %v", r)
	}
}

// TestWiFiFields ports getWiFiDiagnostics' BSSID and getWiFiSsid
// (TopologyUtilsTest.ts:578-656, matterjs-server at 0bd06f22): the
// connected network's id when it is 1 to 32 bytes of UTF-8 without
// control characters. The two binary cases are checked connected too:
// there the list's entry is not connected, which alone makes them null.
func TestWiFiFields(t *testing.T) {
	if got := formatBSSID(ptr(bssidBytes)); got != bssidStr {
		t.Errorf("BSSID %q", got)
	}
	if formatBSSID(nil) != "" || formatBSSID(ptr([]byte{})) != "" {
		t.Error("an empty BSSID formats")
	}
	ni := func(id string, connected bool) netcommdef.NetworkInfoStruct {
		return netcommdef.NetworkInfoStruct{NetworkId: []byte(id), Connected: connected}
	}
	for _, c := range []struct {
		name string
		nets []netcommdef.NetworkInfoStruct
		want string
	}{
		{"utf-8", []netcommdef.NetworkInfoStruct{ni("we@home", true)}, "we@home"},
		{"connected of several", []netcommdef.NetworkInfoStruct{ni("Guest", false), ni("Home", true)}, "Home"},
		{"control bytes", []netcommdef.NetworkInfoStruct{ni("\x01\x02\x03\x04\x05\x06\x07\x08", true)}, ""},
		{"binary", []netcommdef.NetworkInfoStruct{ni("\xa7\x48\xdb\xb0\xec\xc9\x44\xdc", true)}, ""},
		{"saved only", []netcommdef.NetworkInfoStruct{ni("Old", false), ni("Older", false)}, ""},
		{"none", nil, ""},
		{"too long", []netcommdef.NetworkInfoStruct{ni("0123456789012345678901234567890123", true)}, ""},
		{"empty", []netcommdef.NetworkInfoStruct{ni("", true)}, ""},
	} {
		if got := wifiSSID(c.nets); got != c.want {
			t.Errorf("%s: SSID %q, want %q", c.name, got, c.want)
		}
	}
}

// TestBuildBorderRouters ports "omits host_name when the Border Router has
// no mDNS hostname", "omits host_name when the mDNS hostname strips to
// nothing" and "prefers the node's own Thread network name over the Border
// Router registry" (NetworkTopologyServiceTest.ts:313-345, :418-423,
// matterjs-server at 0bd06f22), "promotes a neighbor that matches the
// border-router registry" (TopologyUtilsTest.ts:248-262), and an unknown
// neighbour named by the registry's network for its extended PAN id
// (topology-utils.ts:592-600, :616-629).
func TestBuildBorderRouters(t *testing.T) {
	node1 := mkThread(1, threadOpts{role: role(5), rloc16: ptr[uint16](1024), neighbours: []threaddiagdef.NeighborTableStruct{
		neighbour(brExt, 61440, 2, ptr[int8](-70), true),
	}})
	for host, want := range map[string]string{"": "", ".local.": "", "Cuisine.LOCAL": "Cuisine", "br.example.": "br.example"} {
		br := makeBr()
		br.HostName = host
		n := byID(Build([]Image{node1}, []brdiscovery.Entry{br}, at))[brID]
		if n.Kind != KindBorderRouter || n.HostName != want || n.VendorName != "Apple" {
			t.Errorf("host %q: %+v, want host name %q", host, n, want)
		}
	}

	own := mkThread(1, threadOpts{role: role(5), rloc16: ptr[uint16](1024), networkName: ptr("OwnNet")})
	if got := Build([]Image{own}, []brdiscovery.Entry{makeBr()}, at).Nodes[0].NetworkName; got != "OwnNet" {
		t.Errorf("network name %q, want the node's own", got)
	}

	br := makeBr()
	br.NetworkName, br.ExtPanID = "TestNet", "1122334455667788"
	n := byID(Build([]Image{node1}, []brdiscovery.Entry{br}, at))[brID]
	if n.Kind != KindBorderRouter || n.ID != "br_1122AABBCC334455" || n.NetworkName != "TestNet" {
		t.Errorf("promoted neighbour = %+v", n)
	}

	other := makeBr()
	other.ExtAddress = "00000000DEADBEEF"
	unk := byID(Build([]Image{node1}, []brdiscovery.Entry{other}, at))[brUnkID]
	if unk.Kind != KindThreadUnknown || unk.NetworkName != "OpenThread" || unk.HostName != "" {
		t.Errorf("unknown neighbour = %+v, want named OpenThread by its PAN", unk)
	}
	bad := makeBr()
	bad.ExtAddress, bad.ExtPanID = "not hex", "short"
	if n := byID(Build([]Image{node1}, []brdiscovery.Entry{bad}, at))[brUnkID]; n.Kind != KindThreadUnknown || n.NetworkName != "" {
		t.Errorf("a malformed entry matched: %+v", n)
	}
}

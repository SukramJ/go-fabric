// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/node/NodePhysicalPropertiesTest.ts
// at 862ad75f: the physical properties of a node and the subscription
// interval bounds the controller chooses from them (NodePhysicalProperties,
// PhysicalDeviceProperties.subscriptionIntervalBoundsFor). matter.js builds
// MockServerNodes with the clusters under test; the port builds the same
// composition as the client image of such a node (Node.apply of its
// report): a root with Descriptor and BasicInformation (its specification
// version that of a 1.3 node, the mock's floor), the cluster on the root,
// on a utility endpoint below the root, or on a utility endpoint below an
// Aggregator. Values the matter.js servers default (a null BSSID, null
// Thread network details) are reported as such. The case that reads the
// device's own specification version runs on fabrictest in
// node_physical_properties_parity_matterjs_test.go.
package client

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/tlv"
)

const (
	dtPowerSourceEndpoint = 0x0011
	dtSecondaryNetwork    = 0x0019
	dtGenericSwitch       = 0x000F
	clSwitch              = 0x003B
)

// physicalCluster is one cluster of a test node: its id, FeatureMap and
// values.
type physicalCluster struct {
	id      uint32
	feature uint64
	values  map[uint32]spec.Node
}

// placement is where expectParamsWithCluster puts the cluster
// (NodePhysicalPropertiesTest.ts:221-250).
type placement int

const (
	onRoot placement = iota
	onUtility
	behindAggregator
)

// physicalNode is the image of a node with the clusters at the placement;
// the utility endpoint has device type utility.
func physicalNode(t *testing.T, at placement, utility uint64, clusters ...physicalCluster) *Node {
	t.Helper()
	n, _, _ := testNode(t, &MemoryStore{})
	b := &report{}
	rootServers := []uint32{descriptor.ClusterID, basicinformation.ClusterID}
	ids := make([]uint32, 0, len(clusters))
	for _, c := range clusters {
		ids = append(ids, c.id)
	}
	put := func(ep uint16) {
		for _, c := range clusters {
			b.cluster(ep, c.id, c.feature, c.values)
		}
	}
	switch at {
	case onRoot:
		b.endpoint(0, []uint64{dtRootNode}, append(rootServers, ids...))
		put(0)
	case onUtility:
		b.endpoint(0, []uint64{dtRootNode}, rootServers, 1)
		b.endpoint(1, []uint64{utility}, append([]uint32{descriptor.ClusterID}, ids...))
		put(1)
	case behindAggregator:
		b.endpoint(0, []uint64{dtRootNode}, rootServers, 1, 2)
		b.endpoint(1, []uint64{dtAggregator}, []uint32{descriptor.ClusterID}, 2)
		b.endpoint(2, []uint64{utility}, append([]uint32{descriptor.ClusterID}, ids...))
		put(2)
	}
	b.cluster(0, basicinformation.ClusterID, 0, map[uint32]spec.Node{basicinformation.AttrSpecificationVersion: uintNode(0x0103_0000)})
	n.apply(wildcard, &b.r)
	return n
}

// expectParams is NodePhysicalPropertiesTest.ts:252-265: the floor exactly,
// the ceiling within its one-sided jitter of max(10 %, 10 s), floored to
// whole seconds.
func expectParams(t *testing.T, n *Node, floor, ceiling time.Duration, reqFloor, reqCeiling *time.Duration) {
	t.Helper()
	gotFloor, gotCeiling := n.IntervalBounds(reqFloor, reqCeiling)
	if gotFloor != floor {
		t.Fatalf("floor %v, want %v", gotFloor, floor)
	}
	b := ceiling.Seconds()
	window := math.Max(b*0.1, 10)
	if gotCeiling < ceiling || gotCeiling > time.Duration(math.Floor(b+window))*time.Second {
		t.Fatalf("ceiling %v, want %v within its jitter", gotCeiling, ceiling)
	}
}

// expectParamsWithCluster is NodePhysicalPropertiesTest.ts:221-250: the
// cluster on the root and on a utility endpoint gives want; behind an
// aggregator it does not count (the default, floor 0 and a minute).
func expectParamsWithCluster(t *testing.T, c physicalCluster, utility uint64, floor, ceiling time.Duration) {
	t.Helper()
	expectParams(t, physicalNode(t, onRoot, utility, c), floor, ceiling, nil, nil)
	expectParams(t, physicalNode(t, onUtility, utility, c), floor, ceiling, nil, nil)
	expectParams(t, physicalNode(t, behindAggregator, utility, c), 0, time.Minute, nil, nil)
}

var (
	nullValue = spec.Node{El: tlv.Element{Type: tlv.TypeNull, IsNull: true}}
	bssid     = spec.Node{El: tlv.Element{Type: tlv.TypeOctetStr1, Octets: []byte{0x84, 0x78, 0x48, 0xcc, 0x0b, 0xde}}}
	// threadDefaults is ThreadNetworkDiagnosticsServer's defaults: no
	// network details, routing role Unspecified.
	threadDefaults = map[uint32]spec.Node{
		threadAttrChannel: nullValue, threadAttrRoutingRole: uintNode(0), threadAttrNetworkName: nullValue,
		threadAttrPanID: nullValue, threadAttrExtendedPanID: nullValue,
	}
)

func durationPtr(d time.Duration) *time.Duration { return &d }

func TestNodePhysicalPropertiesParity(t *testing.T) {
	t.Parallel()

	t.Run("chooses correct default intervals", func(t *testing.T) {
		t.Parallel()
		expectParams(t, physicalNode(t, onRoot, 0), 0, time.Minute, nil, nil)
	})

	t.Run("accepts request intervals", func(t *testing.T) {
		t.Parallel()
		expectParams(t, physicalNode(t, onRoot, 0), 2*time.Second, 2*time.Minute, durationPtr(2*time.Second), durationPtr(2*time.Minute))
	})

	t.Run("overrides ICD floor", func(t *testing.T) {
		t.Parallel()
		n := physicalNode(t, onRoot, 0, physicalCluster{id: icdmanagement.ClusterID, values: map[uint32]spec.Node{icdmanagement.AttrIdleModeDuration: uintNode(60)}})
		p := n.Properties()
		if !p.IntermittentlyConnected || p.IdleModeDuration == nil || *p.IdleModeDuration != time.Minute {
			t.Fatalf("properties %+v", p)
		}
		expectParams(t, n, 0, time.Minute, durationPtr(10*time.Second), nil)
	})

	t.Run("chooses correct intervals for battery", func(t *testing.T) {
		t.Parallel()
		expectParamsWithCluster(t, physicalCluster{id: powersource.ClusterID, feature: uint64(powersource.FeatureBattery), values: map[uint32]spec.Node{
			powersource.AttrStatus: uintNode(uint64(powersource.PowerSourceStatusActive)),
		}}, dtPowerSourceEndpoint, 0, 10*time.Minute)
	})

	t.Run("chooses correct intervals for mains + battery", func(t *testing.T) {
		t.Parallel()
		n, _, _ := testNode(t, &MemoryStore{})
		b := &report{}
		b.endpoint(0, []uint64{dtRootNode}, []uint32{descriptor.ClusterID, basicinformation.ClusterID}, 1, 2)
		b.cluster(0, basicinformation.ClusterID, 0, map[uint32]spec.Node{basicinformation.AttrSpecificationVersion: uintNode(0x0103_0000)})
		active := map[uint32]spec.Node{powersource.AttrStatus: uintNode(uint64(powersource.PowerSourceStatusActive))}
		b.endpoint(1, []uint64{dtPowerSourceEndpoint}, []uint32{descriptor.ClusterID, powersource.ClusterID})
		b.cluster(1, powersource.ClusterID, uint64(powersource.FeatureWired), active)
		b.endpoint(2, []uint64{dtPowerSourceEndpoint}, []uint32{descriptor.ClusterID, powersource.ClusterID})
		b.cluster(2, powersource.ClusterID, uint64(powersource.FeatureBattery), active)
		n.apply(wildcard, &b.r)
		expectParams(t, n, 0, time.Minute, nil, nil)
	})

	t.Run("chooses correct intervals for wifi", func(t *testing.T) {
		t.Parallel()
		expectParamsWithCluster(t, physicalCluster{id: wifiDiagnostics, values: map[uint32]spec.Node{wifiAttrBssid: nullValue}}, dtSecondaryNetwork, 0, time.Minute)
	})

	t.Run("chooses correct intervals for sleepy thread", func(t *testing.T) {
		t.Parallel()
		values := map[uint32]spec.Node{}
		for k, v := range threadDefaults {
			values[k] = v
		}
		values[threadAttrRoutingRole] = uintNode(threadRoutingSleepyEndDevice)
		expectParamsWithCluster(t, physicalCluster{id: threadDiagnostics, values: values}, dtSecondaryNetwork, 0, 3*time.Minute)
	})

	t.Run("chooses correct intervals for caffeinated thread", func(t *testing.T) {
		t.Parallel()
		expectParamsWithCluster(t, physicalCluster{id: threadDiagnostics, values: threadDefaults}, dtSecondaryNetwork, 0, time.Minute)
	})

	t.Run("infers wifi from root endpoint diagnostics when Network Commissioning is absent", func(t *testing.T) {
		t.Parallel()
		p := physicalNode(t, onRoot, 0, physicalCluster{id: wifiDiagnostics, values: map[uint32]spec.Node{wifiAttrBssid: bssid}}).Properties()
		if !p.SupportsWifi || p.SupportsThread || p.SupportsEthernet {
			t.Fatalf("properties %+v", p)
		}
	})

	t.Run("does not infer wifi from an unassociated interface", func(t *testing.T) {
		t.Parallel()
		if p := physicalNode(t, onRoot, 0, physicalCluster{id: wifiDiagnostics, values: map[uint32]spec.Node{wifiAttrBssid: nullValue}}).Properties(); p.SupportsWifi {
			t.Fatalf("properties %+v", p)
		}
	})

	t.Run("infers thread from root endpoint diagnostics when Network Commissioning is absent", func(t *testing.T) {
		t.Parallel()
		values := map[uint32]spec.Node{}
		for k, v := range threadDefaults {
			values[k] = v
		}
		values[threadAttrChannel], values[threadAttrPanID] = uintNode(15), spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt2, Uint: 0x1234}}
		p := physicalNode(t, onRoot, 0, physicalCluster{id: threadDiagnostics, values: values}).Properties()
		if !p.SupportsThread || p.SupportsWifi || p.ThreadActive == nil || !*p.ThreadActive || p.ThreadChannel == nil || *p.ThreadChannel != 15 {
			t.Fatalf("properties %+v", p)
		}
	})

	t.Run("does not infer thread from an unjoined interface", func(t *testing.T) {
		t.Parallel()
		p := physicalNode(t, onRoot, 0, physicalCluster{id: threadDiagnostics, values: threadDefaults}).Properties()
		if p.SupportsThread || (p.ThreadActive != nil && *p.ThreadActive) {
			t.Fatalf("properties %+v", p)
		}
	})

	t.Run("does not infer a medium from diagnostics on a non-root endpoint", func(t *testing.T) {
		t.Parallel()
		if p := physicalNode(t, onUtility, dtSecondaryNetwork, physicalCluster{id: wifiDiagnostics, values: map[uint32]spec.Node{wifiAttrBssid: bssid}}).Properties(); p.SupportsWifi {
			t.Fatalf("properties %+v", p)
		}
	})

	t.Run("does not infer a medium when Network Commissioning reports one", func(t *testing.T) {
		t.Parallel()
		thread := map[uint32]spec.Node{}
		for k, v := range threadDefaults {
			thread[k] = v
		}
		thread[threadAttrChannel], thread[threadAttrPanID] = uintNode(15), spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt2, Uint: 0x1234}}
		p := physicalNode(t, onRoot, 0,
			physicalCluster{id: networkcommissioning.ClusterID, feature: uint64(networkcommissioning.FeatureEthernetNetworkInterface), values: map[uint32]spec.Node{0: uintNode(1)}},
			physicalCluster{id: wifiDiagnostics, values: map[uint32]spec.Node{wifiAttrBssid: bssid}},
			physicalCluster{id: threadDiagnostics, values: thread},
		).Properties()
		if !p.SupportsEthernet || p.SupportsWifi || p.SupportsThread {
			t.Fatalf("properties %+v", p)
		}
	})

	t.Run("collects the device types present on endpoints", func(t *testing.T) {
		t.Parallel()
		p := physicalNode(t, onUtility, dtGenericSwitch, physicalCluster{id: clSwitch, values: map[uint32]spec.Node{0: uintNode(2)}}).Properties()
		if !slices.Contains(p.DeviceTypes, dtGenericSwitch) {
			t.Fatalf("device types %v", p.DeviceTypes)
		}
	})

	t.Run("collects device types behind an aggregator", func(t *testing.T) {
		t.Parallel()
		p := physicalNode(t, behindAggregator, dtGenericSwitch, physicalCluster{id: clSwitch, values: map[uint32]spec.Node{0: uintNode(2)}}).Properties()
		if !slices.Contains(p.DeviceTypes, dtGenericSwitch) {
			t.Fatalf("device types %v", p.DeviceTypes)
		}
	})
}

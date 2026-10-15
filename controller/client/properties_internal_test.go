// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/generaldiagnostics"
	"github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/cluster/spec/networkcommissioning"
	"github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/tlv"
)

func dur(d time.Duration) *time.Duration { return &d }

// TestIntervalBounds is the table of PhysicalDeviceProperties.ts:64-164.
func TestIntervalBounds(t *testing.T) {
	t.Parallel()
	thread := Properties{SupportsThread: true}
	icd := Properties{IntermittentlyConnected: true, IdleModeDuration: dur(300 * time.Second)}
	cases := []struct {
		name                string
		p                   Properties
		floor, ceiling      *time.Duration
		rand                float64
		wantFloor, wantCeil time.Duration
		wantLog             string
	}{
		{"default: Wi-Fi, a minute and jitter of at least 10 s", Properties{}, nil, nil, 0.5, 0, 65 * time.Second, ""},
		{"default without jitter", Properties{}, nil, nil, 0, 0, time.Minute, ""},
		{"jitter in whole seconds", Properties{}, nil, nil, 0.33, 0, 63 * time.Second, ""},
		{"an explicit ceiling below the default, no jitter", Properties{}, nil, dur(30 * time.Second), 0.5, 0, 30 * time.Second, "matter.controller.client.ceiling_below_default"},
		{"an explicit ceiling below the floor", Properties{}, dur(5 * time.Second), dur(2 * time.Second), 0.5, 5 * time.Second, 5 * time.Second, ""},
		{"an explicit ceiling past the wire maximum", Properties{}, nil, dur(70000 * time.Second), 0, 0, 65535 * time.Second, ""},
		{"ICD: floor 0, announced; the idle duration", icd, dur(5 * time.Second), nil, 0.5, 0, 300 * time.Second, "matter.controller.client.icd_floor"},
		{"ICD: a long idle duration clamped", Properties{IntermittentlyConnected: true, IdleModeDuration: dur(100000 * time.Second)}, nil, nil, 0.5, 0, 65535 * time.Second, ""},
		{"ICD without an idle duration", Properties{IntermittentlyConnected: true}, dur(0), nil, 0, 0, time.Minute, ""},
		{"battery", Properties{BatteryPowered: true}, nil, nil, 0.5, 0, 630 * time.Second, ""},
		{"battery and mains", Properties{BatteryPowered: true, MainsPowered: true}, nil, nil, 0, 0, time.Minute, ""},
		{"sleepy Thread", Properties{ThreadSleepyEndDevice: true}, nil, nil, 0, time.Second, 3 * time.Minute, ""},
		{"Thread before 1.3: 1 s floor", thread, nil, nil, 0, time.Second, time.Minute, ""},
		{"Thread at 1.3", Properties{SupportsThread: true, SpecificationVersion: ptr(uint32(0x0103_0000))}, nil, nil, 0, 0, time.Minute, ""},
		{"Thread with a Generic Switch", Properties{SupportsThread: true, DeviceTypes: []uint32{0x000F}}, nil, nil, 0, 0, time.Minute, ""},
		{"Thread supported, inactive", Properties{SupportsThread: true, ThreadActive: ptr(false)}, nil, nil, 0, 0, time.Minute, ""},
		{"Thread active", Properties{ThreadActive: ptr(true)}, nil, nil, 0, time.Second, time.Minute, ""},
	}
	for _, tc := range cases {
		logs := &logRecorder{}
		floor, ceiling := intervalBounds(tc.p, tc.floor, tc.ceiling, func() float64 { return tc.rand }, slog.New(logs))
		if floor != tc.wantFloor || ceiling != tc.wantCeil {
			t.Errorf("%s: %v, %v; want %v, %v", tc.name, floor, ceiling, tc.wantFloor, tc.wantCeil)
		}
		if tc.wantLog != "" && !logs.has(tc.wantLog) {
			t.Errorf("%s: no %s", tc.name, tc.wantLog)
		}
	}
}

func networkInterfaces(t *testing.T, nis ...generaldiagnostics.NetworkInterface) spec.Node {
	t.Helper()
	e := tlv.NewEncoder()
	spec.List[generaldiagnostics.NetworkInterface](nis).EncodeTLV(e, tlv.AnonymousTag())
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	n, err := decodeValue(b)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

var nullNode = spec.Node{El: tlv.Element{Type: tlv.TypeNull, IsNull: true}}

// TestProperties: NodePhysicalProperties over an image
// (NodePhysicalProperties.ts:30-194) — the root's network, power, ICD and
// specification version; the device types of every endpoint; nothing of
// what sits behind an aggregator.
func TestProperties(t *testing.T) {
	t.Parallel()
	n, _, _ := testNode(t, &MemoryStore{})
	b := &report{}
	b.endpoint(0, []uint64{dtRootNode}, []uint32{
		descriptor.ClusterID, basicinformation.ClusterID, icdmanagement.ClusterID,
		networkcommissioning.ClusterID, generaldiagnostics.ClusterID, powersource.ClusterID,
	}, 1, 2)
	b.cluster(0, basicinformation.ClusterID, 0, map[uint32]spec.Node{basicinformation.AttrSpecificationVersion: uintNode(0x0102_0000)})
	b.cluster(0, icdmanagement.ClusterID, uint64(icdmanagement.FeatureLongIdleTimeSupport), map[uint32]spec.Node{
		icdmanagement.AttrIdleModeDuration: uintNode(120), icdmanagement.AttrOperatingMode: uintNode(1),
	})
	b.cluster(0, networkcommissioning.ClusterID, uint64(networkcommissioning.FeatureWiFiNetworkInterface), map[uint32]spec.Node{0: uintNode(1)})
	b.cluster(0, generaldiagnostics.ClusterID, 0, map[uint32]spec.Node{generaldiagnostics.AttrNetworkInterfaces: networkInterfaces(t,
		generaldiagnostics.NetworkInterface{
			Name: "wlan0", Type: generaldiagnostics.InterfaceTypeWiFi, IsOperational: true, HardwareAddress: make([]byte, 6),
			OffPremiseServicesReachableIPv4: spec.NullOf[bool](), OffPremiseServicesReachableIPv6: spec.NullOf[bool](),
		},
		generaldiagnostics.NetworkInterface{
			Name: "eth0", Type: generaldiagnostics.InterfaceTypeEthernet, HardwareAddress: make([]byte, 6),
			OffPremiseServicesReachableIPv4: spec.NullOf[bool](), OffPremiseServicesReachableIPv6: spec.NullOf[bool](),
		},
	)})
	b.cluster(0, powersource.ClusterID, uint64(powersource.FeatureWired), map[uint32]spec.Node{powersource.AttrStatus: uintNode(1)})
	b.endpoint(1, []uint64{dtAggregator}, []uint32{descriptor.ClusterID}, 2)
	b.endpoint(2, []uint64{dtBridged, dtPlug}, []uint32{descriptor.ClusterID, powersource.ClusterID, threadDiagnostics})
	b.cluster(2, powersource.ClusterID, uint64(powersource.FeatureBattery), map[uint32]spec.Node{powersource.AttrStatus: uintNode(1)})
	b.cluster(2, threadDiagnostics, 0, map[uint32]spec.Node{threadAttrRoutingRole: uintNode(2), threadAttrExtendedPanID: uintNode(7)})
	n.apply(wildcard, &b.r)
	p := n.Properties()
	if !p.IntermittentlyConnected || !p.LongIdleTimeOperating || p.IdleModeDuration == nil || *p.IdleModeDuration != 2*time.Minute ||
		p.SpecificationVersion == nil || *p.SpecificationVersion != 0x0102_0000 || !p.SupportsWifi || p.SupportsThread ||
		p.WifiActive == nil || !*p.WifiActive || p.EthernetActive == nil || *p.EthernetActive || !p.MainsPowered || p.BatteryPowered ||
		p.ThreadSleepyEndDevice || p.ThreadActive != nil {
		t.Fatalf("properties %+v", p)
	}
	if !slices.Equal(p.DeviceTypes, []uint32{dtAggregator, dtBridged, dtRootNode, dtPlug}) || len(p.RootEndpointServerList) != 6 {
		t.Fatalf("device types %v server list %v", p.DeviceTypes, p.RootEndpointServerList)
	}
	floor, ceiling := n.IntervalBounds(nil, nil)
	if floor != 0 || ceiling != 2*time.Minute {
		t.Fatalf("bounds %v %v", floor, ceiling)
	}

	// Frozen while stopped (ClientNodePhysicalProperties.ts:106-111).
	ctx := context.Background()
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	n.apply(wildcard, &(&report{}).attr(0, powersource.ClusterID, powersource.AttrStatus, uintNode(2), 2).r)
	if !n.Properties().MainsPowered {
		t.Fatal("the properties changed while stopped")
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if n.Properties().MainsPowered {
		t.Fatal("the properties did not follow the image after Start")
	}
}

// TestPropertiesFromDiagnostics: without a Network Commissioning feature
// the root's diagnostics name the medium (NodePhysicalProperties.ts:70-99);
// a node's own Thread diagnostics and battery count (:143-184).
func TestPropertiesFromDiagnostics(t *testing.T) {
	t.Parallel()
	n, _, _ := testNode(t, &MemoryStore{})
	b := &report{}
	b.endpoint(0, []uint64{dtRootNode}, []uint32{descriptor.ClusterID, threadDiagnostics, powersource.ClusterID})
	b.cluster(0, threadDiagnostics, 0, map[uint32]spec.Node{threadAttrChannel: uintNode(15), threadAttrExtendedPanID: nullNode, threadAttrRoutingRole: uintNode(2)})
	b.cluster(0, powersource.ClusterID, 0, map[uint32]spec.Node{powersource.AttrStatus: uintNode(0)})
	n.apply(wildcard, &b.r)
	p := n.Properties()
	if !p.SupportsThread || p.ThreadActive == nil || !*p.ThreadActive || p.ThreadChannel == nil || *p.ThreadChannel != 15 ||
		!p.ThreadSleepyEndDevice || !p.BatteryPowered || p.ThreadPan != nil {
		t.Fatalf("properties %+v", p)
	}
	n2, _, _ := testNode(t, &MemoryStore{})
	b = &report{}
	b.endpoint(0, []uint64{dtRootNode}, []uint32{descriptor.ClusterID, wifiDiagnostics, threadDiagnostics})
	b.cluster(0, wifiDiagnostics, 0, map[uint32]spec.Node{wifiAttrBssid: {El: tlv.Element{Type: tlv.TypeOctetStr1, Octets: make([]byte, 6)}}})
	b.cluster(0, threadDiagnostics, 0, map[uint32]spec.Node{threadAttrNetworkName: {El: tlv.Element{Type: tlv.TypeUTF8Str1, String: "mesh"}}, threadAttrExtendedPanID: uintNode(9), threadAttrChannel: uintNode(20)})
	n2.apply(wildcard, &b.r)
	p = n2.Properties()
	if !p.SupportsWifi || !p.SupportsThread || p.ThreadPan == nil || *p.ThreadPan != 9 || *p.ThreadChannel != 20 || !*p.ThreadActive {
		t.Fatalf("properties %+v", p)
	}
}

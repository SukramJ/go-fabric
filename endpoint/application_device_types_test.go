// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarm"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/fan"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/schema"
)

// The application device types the cluster/alarm, cluster/fan,
// cluster/pump and cluster/measurement servers exist for, assembled the
// way a host assembles them: the host supplies its device's application
// clusters (or a measurement source), the assembler adds Identify,
// Groups where the device type mandates it, Descriptor and
// BridgedDeviceBasicInformation. Each case must come out carrying every
// server cluster the matter.js Device Library makes mandatory for its
// type (schema.DeviceTypeMandatoryServerClusters, generated from
// packages/model/src/standard/elements/<device>.element.ts) and a
// Descriptor that says so.

// flowReading is a host's flow meter.
type flowReading struct{}

func (flowReading) MatterMeasurementClass() contract.MeasurementClass {
	return contract.MeasurementFlow
}
func (flowReading) MatterFloatValue() (float64, bool) { return 1.5, true }

// applicationDeviceCase is one device type and what the host supplies.
type applicationDeviceCase struct {
	name        string
	deviceType  uint16
	source      contract.EndpointSource
	measurement contract.MeasurementSource
}

// assembleApplicationDevice runs a single device through the assembler
// with the node's group state configured, as a host with groups does.
func assembleApplicationDevice(t *testing.T, tc applicationDeviceCase) *Endpoint {
	t.Helper()
	mgr, err := groups.NewManager(nopGroupStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(&oneEndpointStore{}, Config{VendorID: 1, ProductID: 1, NodeLabel: "x", Groups: mgr}, nil)
	if err != nil {
		t.Fatal(err)
	}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{{
		StableKey: StringKey(tc.name), DeviceType: tc.deviceType, FriendlyName: tc.name,
		Source: tc.source, Measurement: tc.measurement,
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	bridged := topo.Bridged()
	if len(bridged) != 1 {
		t.Fatalf("assembled %d bridged endpoints, want 1", len(bridged))
	}
	return bridged[0]
}

// assertMandatoryClusterSet checks one assembled endpoint against the
// Device Library.
func assertMandatoryClusterSet(t *testing.T, ep *Endpoint, deviceType uint16) {
	t.Helper()
	servers := ClusterServers(ep)
	mounted := clusterIDs(servers)

	mandatory, known := schema.DeviceTypeMandatoryServerClusters[uint32(deviceType)]
	if !known {
		t.Fatalf("device type 0x%04X has no mandatory-cluster entry in the snapshot", deviceType)
	}
	for _, id := range mandatory {
		if !slices.Contains(mounted, id) {
			t.Errorf("device type 0x%04X: mandatory server cluster 0x%04X is not mounted; mounted %v", deviceType, id, mounted)
		}
	}
	// Every mounted cluster is one the device type permits as a server,
	// apart from BridgedDeviceBasicInformation, which BridgedNode adds.
	for _, id := range mounted {
		if id == mattercore.BridgedDeviceBasicInformationClusterID {
			continue
		}
		if ok, _ := schema.DeviceTypeAllowsServerCluster(uint32(deviceType), id); !ok {
			t.Errorf("device type 0x%04X: cluster 0x%04X is mounted but not a server cluster of the type", deviceType, id)
		}
	}
	// Each cluster appears once.
	seen := map[uint32]bool{}
	for _, id := range mounted {
		if seen[id] {
			t.Errorf("cluster 0x%04X mounted twice: %v", id, mounted)
		}
		seen[id] = true
	}

	var desc *mattercore.Descriptor
	for _, srv := range servers {
		if d, ok := srv.(*mattercore.Descriptor); ok {
			desc = d
		}
	}
	if desc == nil {
		t.Fatal("no Descriptor mounted")
	}
	v, _ := desc.MatterRead(0x0001) // ServerList
	if list, _ := v.([]uint32); !slices.Equal(list, mounted) {
		t.Errorf("ServerList = %v, want the mounted set %v", list, mounted)
	}
	v, _ = desc.MatterRead(0x0000) // DeviceTypeList
	types, _ := v.([]mattercore.DeviceTypeStruct)
	wantRev, _ := schema.DeviceTypeRevision(uint32(deviceType))
	if len(types) == 0 || types[0].DeviceType != uint32(deviceType) || types[0].Revision != wantRev {
		t.Errorf("DeviceTypeList = %+v, want the primary 0x%04X rev %d first", types, deviceType, wantRev)
	}
}

// smokeReading is a host smoke/CO alarm's state port.
type smokeReading struct{}

func (smokeReading) SmokeCOState() alarm.State { return alarm.State{} }

// fanReading is a host fan's port.
type fanReading struct{}

func (fanReading) FanState() fan.State                                  { return fan.State{} }
func (fanReading) ApplyFanSettings(context.Context, fan.Settings) error { return nil }

func fanServer(dt uint16) contract.ClusterServer {
	srv, err := fan.NewServer(fan.Config{Source: fanReading{}, Sequence: fan.SequenceOffLowMedHigh, DeviceType: dt})
	if err != nil {
		panic(err)
	}
	return srv
}

func applicationDeviceCases() []applicationDeviceCase {
	smoke, err := alarm.NewServer(alarm.Config{Source: smokeReading{}, Features: alarm.FeatureSmokeAlarm | alarm.FeatureCOAlarm})
	if err != nil {
		panic(err)
	}
	return []applicationDeviceCase{
		{name: "FlowSensor", deviceType: 0x0306, measurement: flowReading{}},
		{name: "SmokeCoAlarm", deviceType: alarm.DeviceTypeSmokeCoAlarm, source: deviceTypeSource{
			dt: alarm.DeviceTypeSmokeCoAlarm, servers: []contract.ClusterServer{smoke},
		}},
		// Fan mandates Groups, which the assembler mounts from the
		// node's group state; the host supplies FanControl alone.
		{name: "Fan", deviceType: fan.DeviceTypeFan, source: deviceTypeSource{
			dt: fan.DeviceTypeFan, servers: []contract.ClusterServer{fanServer(fan.DeviceTypeFan)},
		}},
		// AirPurifier's HEPA / activated-carbon filter monitoring and
		// ExtractorHood's are optional and not built here.
		{name: "AirPurifier", deviceType: fan.DeviceTypeAirPurifier, source: deviceTypeSource{
			dt: fan.DeviceTypeAirPurifier, servers: []contract.ClusterServer{fanServer(fan.DeviceTypeAirPurifier)},
		}},
		{name: "ExtractorHood", deviceType: fan.DeviceTypeExtractorHood, source: deviceTypeSource{
			dt: fan.DeviceTypeExtractorHood, servers: []contract.ClusterServer{fanServer(fan.DeviceTypeExtractorHood)},
		}},
	}
}

// TestApplicationDeviceTypesMountTheirMandatoryClusterSet assembles each
// application device type and checks the mandatory set.
func TestApplicationDeviceTypesMountTheirMandatoryClusterSet(t *testing.T) {
	t.Parallel()
	for _, tc := range applicationDeviceCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ep := assembleApplicationDevice(t, tc)
			if ep.DeviceType != tc.deviceType {
				t.Fatalf("assembled device type 0x%04X, want 0x%04X", ep.DeviceType, tc.deviceType)
			}
			assertMandatoryClusterSet(t, ep, tc.deviceType)
		})
	}
}

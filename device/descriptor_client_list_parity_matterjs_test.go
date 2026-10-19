// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

// A port of matter.js packages/node/test/behaviors/descriptor/DescriptorServerClientListTest.ts
// at 862ad75f: the Descriptor's ClientList of a device type's endpoint
// (DescriptorServer.ts #clientList :286-299, MutableEndpoint.ts:53-61).
//
// matter.js builds each device type on a MockEndpoint and reads
// state.descriptor; the port builds the device-layer type, assembles it
// (the assembler mounts the Descriptor) and reads ServerList and ClientList
// from the endpoint's Descriptor server. An explicit .with(<Client>) is
// WithClientClusters. "includes auto-merged mandatory client clusters
// (Doorbell -> Chime)" is TestClientListCarriesTheMandatoryClientClusters
// (device_test.go) and cluster/core:TestDescriptor_ReadClientList.

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
)

// descriptorList reads one Descriptor list attribute (ServerList 0x0001,
// ClientList 0x0002) of the device's endpoint.
func descriptorList(t *testing.T, topo *endpoint.Topology, d *Device, attr uint32) []uint32 {
	t.Helper()
	for _, ep := range topo.Endpoints {
		if ep.Source != contract.EndpointSource(d) {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			if srv.MatterClusterID() == 0x001D {
				v, _ := srv.MatterRead(attr)
				l, _ := v.([]uint32)
				return l
			}
		}
	}
	t.Fatal("no Descriptor for the device")
	return nil
}

// TestDescriptorServerClientList ports the three cases of
// "DescriptorServer clientList" not covered elsewhere.
func TestDescriptorServerClientList(t *testing.T) {
	t.Parallel()
	const occupancySensing, identify = 0x0406, 0x0003

	// :17-20
	t.Run("is empty when no client clusters declared", func(t *testing.T) {
		t.Parallel()
		sensor, err := NewTemperatureSensor(testOptions("temp"))
		if err != nil {
			t.Fatal(err)
		}
		if got := descriptorList(t, assemble(t, sensor.Device), sensor.Device, 0x0002); len(got) != 0 {
			t.Errorf("ClientList = %v, want []", got)
		}
	})

	// :22-25
	t.Run("includes explicit .with(<Client>) entries", func(t *testing.T) {
		t.Parallel()
		light, err := NewOnOffLight(testOptions("light"), WithClientClusters(occupancySensing))
		if err != nil {
			t.Fatal(err)
		}
		if got := descriptorList(t, assemble(t, light.Device), light.Device, 0x0002); !slices.Contains(got, occupancySensing) {
			t.Errorf("ClientList = %v, want it to contain OccupancySensing (0x%04X)", got, occupancySensing)
		}
	})

	// :32-36
	t.Run("lists same cluster id in both serverList and clientList (dimmer-switch / Identify)", func(t *testing.T) {
		t.Parallel()
		sw, err := NewDimmerSwitch(testOptions("dimmer"))
		if err != nil {
			t.Fatal(err)
		}
		topo := assemble(t, sw.Device)
		if got := descriptorList(t, topo, sw.Device, 0x0001); !slices.Contains(got, identify) {
			t.Errorf("ServerList = %v, want it to contain Identify", got)
		}
		if got := descriptorList(t, topo, sw.Device, 0x0002); !slices.Contains(got, identify) {
			t.Errorf("ClientList = %v, want it to contain Identify", got)
		}
	})
}

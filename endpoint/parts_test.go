// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
)

// TestSpecPartsComposeAChildEndpoint pins the composition of matter.js
// examples/device-smoke-co-alarm: the alarm endpoint carries a
// PowerSourceEndpoint part. The part has its own persisted number, sits in
// the alarm's PartsList and the Aggregator's full-family set, names the
// alarm as its parent, lists its own device type alone (no BridgedNode) and
// serves PowerSource and a Descriptor, no BridgedDeviceBasicInformation; the
// alarm then meets its PowerSource component requirement.
func TestSpecPartsComposeAChildEndpoint(t *testing.T) {
	t.Parallel()
	const dtSmokeCoAlarm, dtPowerSource = 0x0076, 0x0011
	store := newInternalFakeStore()
	a, err := New(store, Config{VendorID: 1, ProductID: 1, NodeLabel: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		StableKey: StringKey("alarm"), DeviceType: dtSmokeCoAlarm, FriendlyName: "Alarm",
		Source: deviceTypeSource{dt: dtSmokeCoAlarm, servers: []contract.ClusterServer{dtServer{id: 0x005C}}},
		Parts: []Spec{{
			StableKey: StringKey("alarm:battery"), DeviceType: dtPowerSource,
			PowerSource: measurementSource{contract.MeasurementBattery},
		}},
	}
	assemble := func() *Topology {
		topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", ModelComplete: true, Endpoints: []Spec{spec}}})
		if err != nil {
			t.Fatal(err)
		}
		return topo
	}
	topo := assemble()
	if len(topo.Endpoints) != 4 {
		t.Fatalf("endpoints = %d, want root, aggregator, alarm, part", len(topo.Endpoints))
	}
	alarm, part := topo.Endpoints[2], topo.Endpoints[3]
	if alarm.Part || !part.Part || part.ParentEndpointID != alarm.ID || !part.HasParentEndpointID || alarm.ParentEndpointID != 1 {
		t.Fatalf("alarm %+v / part %+v: wrong parent chain", alarm, part)
	}
	if !slices.Equal(alarm.PartIDs, []uint16{part.ID}) {
		t.Fatalf("alarm PartIDs = %v, want [%d]", alarm.PartIDs, part.ID)
	}
	if got := topo.Bridged(); len(got) != 1 || got[0] != alarm {
		t.Fatalf("Bridged() = %v; a part is no bridged device", got)
	}

	read := func(ep *Endpoint, cluster, attr uint32) (any, bool) {
		for _, srv := range ClusterServers(ep) {
			if srv.MatterClusterID() == cluster {
				return srv.MatterRead(attr)
			}
		}
		return nil, false
	}
	if v, _ := read(alarm, 0x001D, 0x0003); !slices.Equal(v.([]uint16), []uint16{part.ID}) {
		t.Errorf("alarm PartsList = %v, want [%d]", v, part.ID)
	}
	if v, _ := read(part, 0x001D, 0x0000); len(v.([]core.DeviceTypeStruct)) != 1 || v.([]core.DeviceTypeStruct)[0].DeviceType != dtPowerSource {
		t.Errorf("part DeviceTypeList = %v, want PowerSource alone", v)
	}
	if _, ok := read(part, 0x002F, 0x0000); !ok {
		t.Error("part serves no PowerSource")
	}
	if _, ok := read(part, 0x0039, 0x0005); ok {
		t.Error("part serves BridgedDeviceBasicInformation")
	}
	if _, ok := read(alarm, 0x002F, 0x0000); ok {
		t.Error("alarm serves PowerSource itself; the part carries it")
	}
	if !endpointHasDeviceType(part, dtPowerSource) || endpointHasDeviceType(part, matterDeviceTypeBridgedNode) {
		t.Error("ACL device-type targeting must see the part as PowerSource only")
	}
	for _, v := range ValidateDeviceTypes(topo) {
		if v.Key() == "instanceCount device:PowerSource" {
			t.Errorf("the PowerSource part is not counted: %s", v)
		}
		if v.Endpoint == part.ID {
			t.Errorf("part violation: %s", v)
		}
	}

	// Numbers persist across reassembly.
	again := assemble()
	if again.Endpoints[3].ID != part.ID || again.Endpoints[2].ID != alarm.ID {
		t.Fatal("reassembly renumbered the alarm or its part")
	}
}

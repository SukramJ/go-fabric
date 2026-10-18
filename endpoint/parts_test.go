// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
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

	// A wildcard read reaches the part like any endpoint.
	resolved := NewTopologyDispatcher(topo).resolveEndpoints(im.ConcreteAttributePath{})
	wild := make([]uint16, 0, len(resolved))
	for _, ep := range resolved {
		wild = append(wild, ep.ID)
	}
	if !slices.Contains(wild, part.ID) {
		t.Errorf("wildcard endpoints %v leave out the part %d", wild, part.ID)
	}

	// Numbers persist across reassembly.
	again := assemble()
	if again.Endpoints[3].ID != part.ID || again.Endpoints[2].ID != alarm.ID {
		t.Fatal("reassembly renumbered the alarm or its part")
	}
}

// TestComposedSourceWithoutClustersIsBridged: a source that serves no
// cluster of its own — an Oven, whose device type mandates none and whose
// cavity is a part — still mounts the bridged node's surface (Identify,
// Descriptor with its PartsList, BridgedDeviceBasicInformation).
func TestComposedSourceWithoutClustersIsBridged(t *testing.T) {
	t.Parallel()
	const dtOven, dtCabinet = 0x007B, 0x0071
	a, err := New(newInternalFakeStore(), Config{VendorID: 1, ProductID: 1, NodeLabel: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", ModelComplete: true, Endpoints: []Spec{{
		StableKey: StringKey("oven"), DeviceType: dtOven, FriendlyName: "Oven",
		Source: deviceTypeSource{dt: dtOven},
		Parts: []Spec{{
			StableKey: StringKey("oven:cavity"), DeviceType: dtCabinet,
			Source: deviceTypeSource{dt: dtCabinet, servers: []contract.ClusterServer{dtServer{id: 0x0056}}},
		}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	oven, cavity := topo.Endpoints[2], topo.Endpoints[3]
	ids := clusterIDs(ClusterServers(oven))
	for _, want := range []uint32{0x0003, 0x001D, 0x0039} {
		if !slices.Contains(ids, want) {
			t.Errorf("oven mounts %v, missing 0x%04X", ids, want)
		}
	}
	for _, srv := range ClusterServers(oven) {
		if srv.MatterClusterID() == 0x001D {
			if v, _ := srv.MatterRead(0x0003); !slices.Equal(v.([]uint16), []uint16{cavity.ID}) {
				t.Errorf("oven PartsList = %v", v)
			}
		}
	}
	// No source at all is still malformed and mounts nothing.
	if got := ClusterServers(&Endpoint{ID: 9}); got != nil {
		t.Errorf("sourceless endpoint mounts %v", got)
	}
}

// TestBridgedNodePartsListIsFullFamily pins the one model change of the
// 862ad75f lift: BridgedNode composes its full family (matter.js #4735,
// packages/model/src/standard/elements/bridged-node.element.ts:17,
// Device Library § 2.5.6, the refrigerator example). A bridged
// Refrigerator with two cabinets, one of which has a part of its own,
// lists all three descendants in its PartsList; the intermediate cabinet,
// whose TemperatureControlledCabinet composes a tree, lists its own child
// only, and the leaves list none. Mirrors matter.js
// packages/node/src/behaviors/descriptor/DescriptorServer.ts:227-254
// (#currentPartsListNumbers) and DescriptorServer.ts:262-270
// (#composesFullFamily).
func TestBridgedNodePartsListIsFullFamily(t *testing.T) {
	t.Parallel()
	const dtRefrigerator, dtCabinet, dtPowerSource = 0x0070, 0x0071, 0x0011
	a, err := New(newInternalFakeStore(), Config{VendorID: 1, ProductID: 1, NodeLabel: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cabinet := func(key string, parts ...Spec) Spec {
		return Spec{
			StableKey: StringKey(key), DeviceType: dtCabinet,
			Source: deviceTypeSource{dt: dtCabinet, servers: []contract.ClusterServer{dtServer{id: 0x0056}}},
			Parts:  parts,
		}
	}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", ModelComplete: true, Endpoints: []Spec{{
		StableKey: StringKey("fridge"), DeviceType: dtRefrigerator, FriendlyName: "Fridge",
		Source: deviceTypeSource{dt: dtRefrigerator},
		Parts: []Spec{
			cabinet("fridge:cooler", Spec{
				StableKey: StringKey("fridge:cooler:battery"), DeviceType: dtPowerSource,
				PowerSource: measurementSource{contract.MeasurementBattery},
			}),
			cabinet("fridge:freezer"),
		},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(topo.Endpoints) != 6 {
		t.Fatalf("endpoints = %d, want root, aggregator, fridge, cooler, its battery, freezer", len(topo.Endpoints))
	}
	fridge, cooler, battery, freezer := topo.Endpoints[2], topo.Endpoints[3], topo.Endpoints[4], topo.Endpoints[5]
	if battery.ParentEndpointID != cooler.ID || cooler.ParentEndpointID != fridge.ID || freezer.ParentEndpointID != fridge.ID {
		t.Fatalf("parent chain: cooler→%d battery→%d freezer→%d", cooler.ParentEndpointID, battery.ParentEndpointID, freezer.ParentEndpointID)
	}
	partsList := func(ep *Endpoint) []uint16 {
		t.Helper()
		for _, srv := range ClusterServers(ep) {
			if srv.MatterClusterID() == 0x001D {
				v, _ := srv.MatterRead(0x0003)
				return v.([]uint16)
			}
		}
		t.Fatalf("endpoint %d mounts no Descriptor", ep.ID)
		return nil
	}
	want := []uint16{cooler.ID, battery.ID, freezer.ID}
	slices.Sort(want)
	if got := partsList(fridge); !slices.Equal(got, want) {
		t.Errorf("bridged node PartsList = %v, want every descendant %v", got, want)
	}
	if got := partsList(cooler); !slices.Equal(got, []uint16{battery.ID}) {
		t.Errorf("intermediate part PartsList = %v, want its child [%d] only", got, battery.ID)
	}
	if got := partsList(battery); len(got) != 0 {
		t.Errorf("leaf PartsList = %v, want empty", got)
	}
	if got := partsList(freezer); len(got) != 0 {
		t.Errorf("childless part PartsList = %v, want empty", got)
	}
}

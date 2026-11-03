// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	bdbidef "github.com/SukramJ/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	descriptordef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
)

// TestBridgeSimFlag: with --bridge-sim the node bridges matter.js's bridge
// test instance's devices after every other device
// (BridgeTestInstance.ts:148-172 at 862ad75f) — free of device-type
// violations but the TagList the device kit cannot declare; the composed
// device lists Power Source beside Bridged Node, its sensors are its own
// PartsList and carry no BridgedDeviceBasicInformation, its PowerSource
// names the three endpoints.
func TestBridgeSimFlag(t *testing.T) {
	f := newFleet()
	sim, err := newDemoBridgeSim(f)
	if err != nil {
		t.Fatal(err)
	}
	f.bridgeSim = sim
	n := testNode(t, f, filepath.Join(t.TempDir(), "bridgesim.db"))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sim.numberPowerSource(); err != nil {
		t.Fatal(err)
	}
	br := n.Bridge()
	byID := func(id uint16) *endpoint.Endpoint {
		for _, ep := range br.Topology().Endpoints {
			if ep != nil && ep.ID == id {
				return ep
			}
		}
		return nil
	}
	number := func(d *device.Device) uint16 {
		t.Helper()
		no, ok := d.EndpointNumber()
		if !ok {
			t.Fatalf("%s has no endpoint number", d.Spec().StableKey)
		}
		return no
	}

	// The composition: every simulated endpoint after the daemon's own.
	light1 := mountedEndpoint(t, br, "demo-bridgesim-light1-1")
	for _, ep := range br.Topology().Endpoints {
		if ep != nil && ep.ID > light1.ID && !ep.Part && !strings.HasPrefix(ep.DeviceAddress, "demo-bridgesim-") {
			t.Errorf("endpoint %d (%s) after the simulated bridge's first: the simulation must come last", ep.ID, ep.DeviceAddress)
		}
	}
	composed := byID(number(sim.composed.Device))
	partNos := []uint16{number(sim.sensors[2].Device), number(sim.sensors[3].Device)}
	for _, v := range endpoint.ValidateDeviceTypes(br.Topology()) {
		if v.Endpoint < light1.ID {
			continue
		}
		// The composed device's sensors share a device type, so Base
		// requires a TagList of each (matter.js gives them
		// CommonNumberTag One and Two, BridgeTestInstance.ts:306-317); the
		// device kit has no TagList declaration (notes/parity/
		// matterjs_tests.md, SemanticNamespaceTest.ts row).
		if slices.Contains(partNos, v.Endpoint) && strings.Contains(v.String(), "Descriptor.TAGLIST") {
			continue
		}
		t.Errorf("device type violation: %s", v)
	}
	types := readMounted(t, composed, descriptordef.ClusterID, descriptordef.AttrDeviceTypeList).([]mattercore.DeviceTypeStruct)
	ids := make([]uint32, 0, len(types))
	for _, dt := range types {
		ids = append(ids, dt.DeviceType)
	}
	if !slices.Contains(ids, uint32(device.PowerSourceID)) || !slices.Contains(ids, uint32(device.BridgedNodeID)) {
		t.Errorf("composed DeviceTypeList %v, want Power Source 0x0011 and Bridged Node 0x0013 (:330-335)", ids)
	}
	if parts := readMounted(t, composed, descriptordef.ClusterID, descriptordef.AttrPartsList); !slices.Equal(parts.([]uint16), partNos) {
		t.Errorf("composed PartsList %v, want its two sensors %v", parts, partNos)
	}
	for _, no := range partNos {
		part := byID(no)
		if !part.Part || part.DeviceType != uint16(device.TemperatureSensorID) {
			t.Errorf("composed part %d: part %v, device type 0x%04X", no, part.Part, part.DeviceType)
		}
		for _, srv := range endpoint.ClusterServers(part) {
			if srv.MatterClusterID() == bdbidef.ClusterID {
				t.Errorf("composed part %d serves BridgedDeviceBasicInformation (:159-161)", no)
			}
		}
	}
	if got := readMounted(t, composed, psdef.ClusterID, psdef.AttrEndpointList); !slices.Equal(got.([]uint16), append([]uint16{composed.ID}, partNos...)) {
		t.Errorf("composed EndpointList %v, want %d and its parts %v (:344-348)", got, composed.ID, partNos)
	}
	if got := sim.composed.PowerSource().Get(); got.BatChargeLevel != psdef.BatChargeLevelOk || got.Description != "Battery" {
		t.Errorf("composed PowerSource BatChargeLevel %v, Description %q", got.BatChargeLevel, got.Description)
	}
	if label := readMounted(t, composed, bdbidef.ClusterID, bdbidef.AttrNodeLabel); label != bridgeSimComposedLabel {
		t.Errorf("composed NodeLabel %v", label)
	}
}

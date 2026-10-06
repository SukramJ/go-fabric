// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
)

// TestParityMatterJS_BridgedVendorNameComesFromTheHost pins where a bridged
// endpoint's BridgedDeviceBasicInformation VendorName (0x0001) comes from:
// the host's Spec.VendorName, else the node's own VendorName
// (Config.VendorName). With neither, the attribute is not served — matter.js
// treats vendorName as optional on a bridged device
// (basic-information-validators.ts) and its bridge example
// (examples/device-bridge-onoff/src/BridgedDevicesNode.ts) sets none.
func TestParityMatterJS_BridgedVendorNameComesFromTheHost(t *testing.T) {
	t.Parallel()
	read := func(t *testing.T, cfg endpoint.Config, spec endpoint.Spec) (any, bool) {
		t.Helper()
		a, err := endpoint.New(endpointtest.NewFakeStore(), cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		topo, err := a.Assemble(context.Background(), []endpoint.Snapshot{{Scope: "s", ModelComplete: true, Endpoints: []endpoint.Spec{spec}}})
		if err != nil {
			t.Fatal(err)
		}
		d := endpoint.NewTopologyDispatcher(topo)
		ep := topo.Bridged()[0].ID
		list := d.Read(context.Background(), im.ConcreteAttributePath{
			Endpoint: ep, Cluster: 0x0039, Attribute: 0xFFFB,
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
		})
		ids, _ := list[0].Value.Value.([]uint32)
		res := d.Read(context.Background(), im.ConcreteAttributePath{
			Endpoint: ep, Cluster: 0x0039, Attribute: 0x0001,
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
		})
		if res[0].Status != im.StatusSuccess {
			if slices.Contains(ids, 0x0001) {
				t.Fatalf("VendorName listed in AttributeList but read as %v", res[0].Status)
			}
			return nil, false
		}
		if !slices.Contains(ids, 0x0001) {
			t.Fatal("VendorName served but missing from AttributeList")
		}
		return res[0].Value.Value, true
	}
	spec := func(vendor string) endpoint.Spec {
		return endpoint.Spec{
			StableKey: endpoint.StringKey("k1"), FriendlyName: "Flow Meter", VendorName: vendor,
			DeviceType: contract.MeasurementClassDeviceType(contract.MeasurementFlow), Measurement: labelFlow{},
		}
	}
	base := endpoint.Config{VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "Bridge"}
	withNode := base
	withNode.VendorName = "Node Vendor"

	if v, ok := read(t, withNode, spec("Device Maker")); !ok || v != "Device Maker" {
		t.Errorf("VendorName with a host value = %v (served %v), want the host's", v, ok)
	}
	if v, ok := read(t, withNode, spec("")); !ok || v != "Node Vendor" {
		t.Errorf("VendorName without a host value = %v (served %v), want the node's", v, ok)
	}
	if v, ok := read(t, base, spec("")); ok {
		t.Errorf("VendorName with neither = %v, want the attribute not served", v)
	}
	long := strings.Repeat("v", 40)
	if v, ok := read(t, base, spec(long)); !ok || v != long[:32] {
		t.Errorf("a 40-byte VendorName read %v, want the 32-byte constraint applied", v)
	}
}

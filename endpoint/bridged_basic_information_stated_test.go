// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"context"
	"testing"

	bdbidef "thingfabric.dev/go-fabric/cluster/spec/bridgeddevicebasicinformation"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/endpoint/endpointtest"
	"thingfabric.dev/go-fabric/im"
)

// TestBridgedBasicInformationServesWhatTheHostStated (review II,
// R2-P5-apidev-02, F-db021da0-28, decision 143): the device's address is
// never served as its SerialNumber; what the host states in
// Spec.BridgedBasicInformation is served ahead of the fallbacks, an empty
// NodeLabel it states included.
func TestBridgedBasicInformationServesWhatTheHostStated(t *testing.T) {
	t.Parallel()
	str := func(s string) *string { return &s }
	vid := uint16(0x1234)
	for name, tc := range map[string]struct {
		stated bdbidef.Patch
		want   map[uint32]any // nil value: not served
	}{
		"nothing stated": {bdbidef.Patch{}, map[uint32]any{
			bdbidef.AttrSerialNumber: nil, bdbidef.AttrNodeLabel: nil, bdbidef.AttrProductName: "ADDR-1",
		}},
		"stated": {bdbidef.Patch{SerialNumber: str("SN-7"), NodeLabel: str(""), ProductName: str("Lamp"), VendorId: &vid, PartNumber: str("P-1")}, map[uint32]any{
			bdbidef.AttrSerialNumber: "SN-7", bdbidef.AttrNodeLabel: "", bdbidef.AttrProductName: "Lamp",
			bdbidef.AttrVendorId: uint16(0x1234), bdbidef.AttrPartNumber: "P-1",
		}},
	} {
		a, err := endpoint.New(endpointtest.NewFakeStore(), endpoint.Config{VendorID: 0xFFF1, ProductID: 0x8001}, nil)
		if err != nil {
			t.Fatal(err)
		}
		spec := endpoint.Spec{
			StableKey: endpoint.StringKey("dev"), DeviceAddress: "ADDR-1", BridgedBasicInformation: tc.stated,
			DeviceType: contract.MeasurementClassDeviceType(contract.MeasurementFlow), Measurement: labelFlow{},
		}
		topo, err := a.Assemble(context.Background(), []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{spec}}})
		if err != nil {
			t.Fatal(err)
		}
		ep := topo.Bridged()[0]
		d := endpoint.NewTopologyDispatcher(topo)
		for attr, want := range tc.want {
			res := d.Read(context.Background(), im.ConcreteAttributePath{
				Endpoint: ep.ID, Cluster: bdbidef.ClusterID, Attribute: attr,
				HasEndpoint: true, HasCluster: true, HasAttribute: true,
			})
			if len(res) != 1 {
				t.Fatalf("%s: read 0x%04X: %d reports", name, attr, len(res))
			}
			switch {
			case want == nil && res[0].Status != im.StatusUnsupportedAttribute:
				t.Errorf("%s: attribute 0x%04X = %v (status %v), want not served", name, attr, res[0].Value.Value, res[0].Status)
			case want != nil && (res[0].Status != im.StatusSuccess || res[0].Value.Value != want):
				t.Errorf("%s: attribute 0x%04X = %v (status %v), want %v", name, attr, res[0].Value.Value, res[0].Status, want)
			}
		}
	}
}

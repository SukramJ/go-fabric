// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
)

// TestBridgedEndpointWithoutNameServesDescriptor: a bridged endpoint whose
// spec carries no FriendlyName (a device.Options without Name) serves its
// Descriptor and a BridgedDeviceBasicInformation with an empty NodeLabel.
// matter.js: every attribute of the bridged basic information cluster but
// Reachable is optional (BridgedDeviceBasicInformationServer.ts:20 at
// 862ad75f). Before
// 2026-10-30 the constructor refused the empty label and the materialiser
// returned the inner clusters only, with no Descriptor (S4 report, observed).
func TestBridgedEndpointWithoutNameServesDescriptor(t *testing.T) {
	t.Parallel()
	store := endpointtest.NewFakeStore()
	a, err := endpoint.New(store, endpoint.Config{VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "Bridge"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := endpoint.Spec{
		StableKey:  endpoint.StringKey("unnamed"),
		DeviceType: contract.MeasurementClassDeviceType(contract.MeasurementFlow), Measurement: labelFlow{},
	}
	topo, err := a.Assemble(context.Background(), []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{spec}}})
	if err != nil {
		t.Fatal(err)
	}
	ep := topo.Bridged()[0]
	d := endpoint.NewTopologyDispatcher(topo)
	read := func(cluster, attr uint32) im.ReadResult {
		t.Helper()
		res := d.Read(context.Background(), im.ConcreteAttributePath{
			Endpoint: ep.ID, Cluster: cluster, Attribute: attr,
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
		})
		if len(res) != 1 {
			t.Fatalf("read %#x/%#x: %d reports", cluster, attr, len(res))
		}
		return res[0]
	}
	if r := read(0x001D, 0x0000); r.Status != im.StatusSuccess {
		t.Fatalf("Descriptor.DeviceTypeList: status %v", r.Status)
	}
	if r := read(0x001D, 0x0001); r.Status != im.StatusSuccess {
		t.Fatalf("Descriptor.ServerList: status %v", r.Status)
	}
	if r := read(0x0039, 0x0005); r.Status != im.StatusSuccess || r.Value.Value != "" {
		t.Fatalf("BridgedDeviceBasicInformation.NodeLabel = %v (status %v), want \"\"", r.Value.Value, r.Status)
	}
	clusters := endpoint.ClusterServers(ep)
	if !hasCluster(clusters, 0x001D) || !hasCluster(clusters, 0x0039) {
		t.Fatalf("ClusterServers lacks Descriptor or BridgedDeviceBasicInformation: %d servers", len(clusters))
	}
}

func hasCluster(servers []contract.ClusterServer, id uint32) bool {
	for _, s := range servers {
		if s.MatterClusterID() == id {
			return true
		}
	}
	return false
}

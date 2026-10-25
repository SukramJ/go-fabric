// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// TestSpecClientClustersReadBackInTheDescriptor: Spec.ClientClusters is the
// Descriptor's ClientList, on a bridged endpoint and on a part — matter.js
// MutableEndpoint.ts:53-61 registers a type's mandatory client clusters,
// DescriptorServer.ts:290-296 lists them. A Spec without any serves an
// empty ClientList, as before the field existed.
func TestSpecClientClustersReadBackInTheDescriptor(t *testing.T) {
	t.Parallel()
	const dtDoorbell, dtPowerSource, chime = 0x0148, 0x0011, 0x0556
	a, err := New(newInternalFakeStore(), Config{VendorID: 1, ProductID: 1, NodeLabel: "x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{
		{
			StableKey: StringKey("bell"), DeviceType: dtDoorbell, FriendlyName: "Bell", ClientClusters: []uint32{chime},
			Source: deviceTypeSource{dt: dtDoorbell, servers: []contract.ClusterServer{dtServer{id: 0x003B}}},
			Parts: []Spec{{
				StableKey: StringKey("bell:ps"), DeviceType: dtPowerSource, ClientClusters: []uint32{0x0006},
				Source: deviceTypeSource{dt: dtPowerSource, servers: []contract.ClusterServer{dtServer{id: 0x002F}}},
			}},
		},
		{
			StableKey: StringKey("plain"), DeviceType: dtPowerSource, FriendlyName: "Plain",
			Source: deviceTypeSource{dt: dtPowerSource, servers: []contract.ClusterServer{dtServer{id: 0x002F}}},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	clientList := func(ep *Endpoint) []uint32 {
		t.Helper()
		for _, srv := range ClusterServers(ep) {
			if srv.MatterClusterID() == 0x001D {
				v, _ := srv.MatterRead(0x0002)
				l, _ := v.([]uint32)
				return l
			}
		}
		t.Fatalf("endpoint %d has no Descriptor", ep.ID)
		return nil
	}
	want := map[string][]uint32{"bell": {chime}, "bell:ps": {0x0006}, "plain": nil}
	for _, ep := range topo.Endpoints {
		if ep.IsRoot() || ep.IsAggregator() {
			continue
		}
		w := want[ep.SourceKey.String()]
		if got := clientList(ep); !slices.Equal(got, w) || len(got) != len(w) {
			t.Errorf("%s ClientList = %v, want %v", ep.SourceKey, got, w)
		}
	}
}

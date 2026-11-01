// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

// The Aggregator is a role of an endpoint, not endpoint number 1: matter.js
// assigns it a number like any endpoint
// (packages/node/src/storage/server/ServerEndpointStores.ts:109-130) and
// keys behaviour on its type
// (packages/node/src/behaviors/descriptor/DescriptorServer.ts:227-254).
// Review 2026-11 W2-api-03.

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

func TestAggregatorRoleParity(t *testing.T) {
	t.Parallel()

	t.Run("a host endpoint numbered 1 keeps its own clusters", func(t *testing.T) {
		t.Parallel()
		device := &Endpoint{ID: 1, DeviceType: 0x010A, Source: singleServerSource{srv: &globalAttrServer{id: 0x0006}}}
		topo := &Topology{Endpoints: []*Endpoint{{ID: 0}, device}}
		if device.IsAggregator() || topo.Aggregator() != nil {
			t.Fatal("endpoint 1 without the role was taken for the Aggregator")
		}
		published := &globalAttrServer{id: 0x0008}
		device.PublishClusterServers([]contract.ClusterServer{published})
		var hasOnOff bool
		for _, s := range ClusterServers(device) {
			if s == contract.ClusterServer(published) {
				t.Fatal("endpoint 1 served the published (Aggregator) set instead of its own clusters")
			}
			hasOnOff = hasOnOff || s.MatterClusterID() == 0x0006
		}
		if !hasOnOff {
			t.Fatal("endpoint 1 lost its source's OnOff server")
		}
	})

	t.Run("the assembler's Aggregator carries the role", func(t *testing.T) {
		t.Parallel()
		a, err := New(newInternalFakeStore(), Config{VendorID: 1, ProductID: 1}, nil)
		if err != nil {
			t.Fatal(err)
		}
		topo, err := a.Assemble(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		agg := topo.Aggregator()
		if agg == nil || agg.ID != 1 || !agg.IsAggregator() || agg.DeviceType != deviceTypeAggregator {
			t.Fatalf("Aggregator() = %+v, want endpoint 1 with the Aggregator role", agg)
		}
	})
}

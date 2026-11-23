// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/mdns"
)

// TestReattachedDescriptorKeepsTheHostsProvider: after the host's
// PartsList override, a later AttachRootClusters /
// AttachAggregatorClusters without a second override hands the fresh
// Descriptor the host's provider — before, it got neither that nor the
// bridge's default and served its static list (review II,
// R2-C2-api-0601).
func TestReattachedDescriptorKeepsTheHostsProvider(t *testing.T) {
	t.Parallel()
	for _, ep := range []struct {
		name       string
		deviceType uint32
		attach     func(*Bridge, []contract.ClusterServer)
		provide    func(*Bridge, func() []uint16) bool
	}{
		{"root", 0x0016, (*Bridge).AttachRootClusters, (*Bridge).AttachRootPartsListProvider},
		{"Aggregator", 0x000E, (*Bridge).AttachAggregatorClusters, (*Bridge).AttachAggregatorPartsListProvider},
	} {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()
			b, err := New(withBridgedEndpoint, mdns.NewNoop(), Config{Listen: "127.0.0.1:0"}, nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if err := b.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v", err)
			}
			t.Cleanup(func() { _ = b.Stop(context.Background()) })
			descriptor := func() *mattercore.Descriptor {
				d, err := mattercore.NewDescriptor([]mattercore.DeviceTypeStruct{{DeviceType: ep.deviceType, Revision: 1}}, []uint32{0x001D}, nil, nil)
				if err != nil {
					t.Fatalf("NewDescriptor: %v", err)
				}
				return d
			}
			ep.attach(b, []contract.ClusterServer{descriptor()})
			if !ep.provide(b, func() []uint16 { return []uint16{42} }) {
				t.Fatal("the host's provider was not installed")
			}
			fresh := descriptor()
			ep.attach(b, []contract.ClusterServer{fresh})
			v, ok := fresh.MatterRead(0x0003)
			if got, _ := v.([]uint16); !ok || !slices.Equal(got, []uint16{42}) {
				t.Errorf("re-attached Descriptor's PartsList = %v (ok %v), want the host's [42]", v, ok)
			}
		})
	}
}

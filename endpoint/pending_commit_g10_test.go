// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bridge"
	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/endpoint/endpointtest"
	"thingfabric.dev/go-fabric/mdns"
)

type g10Flow struct{}

func (g10Flow) MatterMeasurementClass() contract.MeasurementClass { return contract.MeasurementFlow }
func (g10Flow) MatterFloatValue() (float64, bool)                 { return 1, true }

// g10Singleton is a BasicInformation-id server: on the Aggregator it is a
// misplaced RootNode singleton, which refuses the assembly in every mode.
type g10Singleton struct{}

func (g10Singleton) MatterClusterID() uint32                                { return 0x0028 }
func (g10Singleton) MatterRead(uint32) (any, bool)                          { return nil, false }
func (g10Singleton) MatterWrite(context.Context, uint32, any) error         { return nil }
func (g10Singleton) MatterInvoke(context.Context, uint32, any) (any, error) { return nil, nil }
func (g10Singleton) MatterReportable() []uint32                             { return nil }

func g10EndpointA(t *testing.T, topo *endpoint.Topology) *endpoint.Endpoint {
	t.Helper()
	for _, ep := range topo.Endpoints {
		if ep.SourceKey != nil && ep.SourceKey.String() == "a" {
			return ep
		}
	}
	t.Fatal("key a is not in the served topology")
	return nil
}

// TestRefusedReassemblyKeepsTheRegistry (P10-fsm-6001): the bridge refuses
// a topology after the host's Snapshotter assembled it, so the assembler's
// registry changes travel with the topology and are committed only where
// the bridge swaps it in. Snapshot 2 drops key "a" and is refused; "a" of
// the served topology keeps its state, and snapshot 3, which restores it,
// serves the same DataVersion — not a fresh state.
func TestRefusedReassemblyKeepsTheRegistry(t *testing.T) {
	t.Parallel()
	asm, err := endpoint.New(endpointtest.NewFakeStore(), endpointtest.AssemblerConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var withA atomic.Bool
	withA.Store(true)
	snap := func(ctx context.Context) (*endpoint.Topology, error) {
		var specs []endpoint.Spec
		if withA.Load() {
			specs = append(specs, endpoint.Spec{StableKey: endpoint.StringKey("a"), DeviceType: 0x0306, FriendlyName: "Flow", Measurement: g10Flow{}})
		}
		return asm.AssemblePending(ctx, []endpoint.Snapshot{{Scope: "s", Endpoints: specs}})
	}
	b, err := bridge.New(snap, mdns.NewNoop(), bridge.Config{Listen: "127.0.0.1:0"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = b.Stop(ctx)
	})
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	const flowCluster = 0x0404
	before := g10EndpointA(t, b.Topology()).ClusterDataVersion(flowCluster)

	descriptor, err := mattercore.NewDescriptor([]mattercore.DeviceTypeStruct{{DeviceType: 0x000E, Revision: 2}}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	withA.Store(false)
	b.AttachAggregatorClusters([]contract.ClusterServer{descriptor, g10Singleton{}})
	var refusal *endpoint.DeviceTypeConformanceError
	if err := b.Reassemble(context.Background()); !errors.As(err, &refusal) {
		t.Fatalf("Reassemble with a misplaced singleton = %v, want a refusal", err)
	}

	withA.Store(true)
	b.AttachAggregatorClusters([]contract.ClusterServer{descriptor})
	if err := b.Reassemble(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := g10EndpointA(t, b.Topology()).ClusterDataVersion(flowCluster); got != before {
		t.Errorf("DataVersion of a after the refused reassembly = %d, want %d: the refused topology released a's state", got, before)
	}
}

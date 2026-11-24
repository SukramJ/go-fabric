// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/endpoint/endpointtest"
	"thingfabric.dev/go-fabric/im"
)

// R5-conc-5107 (fix-regression of #589, decision 640): a controller's write
// of key-held state (NodeLabel) that completes through the dispatcher of
// the topology a same-key change of instance superseded — a write in
// flight at the swap — lands in the key's state, which the new instance
// serves; the new instance's DataVersion advances with it, or a
// DataVersionFilter at the served version hides the new label.
func TestLateWriteOnSupersededInstanceAdvancesServedVersion(t *testing.T) {
	t.Parallel()
	type inst struct{ n int }
	for _, tc := range []struct {
		name        string
		first, next any
	}{{"same instance", &inst{1}, nil}, {"new instance", &inst{1}, &inst{2}}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, err := endpoint.New(endpointtest.NewFakeStore(), endpoint.Config{VendorID: 0xFFF1, ProductID: 0x8001}, nil)
			if err != nil {
				t.Fatal(err)
			}
			assemble := func(instance any) (*endpoint.TopologyDispatcher, uint16) {
				t.Helper()
				s := endpoint.Spec{
					StableKey: endpoint.StringKey("k1"), FriendlyName: "Flow Meter", Instance: instance,
					DeviceType: contract.MeasurementClassDeviceType(contract.MeasurementFlow), Measurement: labelFlow{},
				}
				topo, err := a.Assemble(context.Background(), []endpoint.Snapshot{{Scope: "s", Endpoints: []endpoint.Spec{s}}})
				if err != nil {
					t.Fatal(err)
				}
				return endpoint.NewTopologyDispatcher(topo), topo.Bridged()[0].ID
			}
			path := im.ConcreteAttributePath{Cluster: 0x0039, Attribute: 0x0005, HasEndpoint: true, HasCluster: true, HasAttribute: true}
			version := func(d *endpoint.TopologyDispatcher) uint32 {
				v, _ := d.CurrentDataVersion(context.Background(), path.Endpoint, path.Cluster)
				return v
			}
			old, ep := assemble(tc.first)
			path.Endpoint = ep
			next := tc.next
			if next == nil {
				next = tc.first
			}
			live, _ := assemble(next)
			oldBefore, liveBefore := version(old), version(live)
			res := old.Write(context.Background(), path, im.AttributeValue{Value: "Garden"})
			if len(res) != 1 || res[0].Status != im.StatusSuccess {
				t.Fatalf("write %+v", res)
			}
			if got := live.Read(context.Background(), path)[0].Value.Value; got != "Garden" {
				t.Fatalf("served NodeLabel = %v, want Garden", got)
			}
			if version(live) == liveBefore {
				t.Error("the served instance's DataVersion did not advance with the label it serves")
			}
			if version(old) == oldBefore {
				t.Error("the writing instance's DataVersion did not advance")
			}
			want := liveBefore + 1
			if want == 0 {
				want = 1 // the tracker skips 0
			}
			if tc.next == nil && version(live) != want {
				t.Errorf("same instance advanced %d -> %d, want once", liveBefore, version(live))
			}
		})
	}
}

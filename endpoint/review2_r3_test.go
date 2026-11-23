// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/contract"
)

// spareCapSource returns the same slice on every call: one server and spare
// capacity it keeps — a cached field built with append, which
// [contract.EndpointSource] does not forbid.
type spareCapSource struct{ srvs []contract.ClusterServer }

func (s spareCapSource) MatterDeviceType() uint16                       { return 0x0306 }
func (s spareCapSource) MatterClusterServers() []contract.ClusterServer { return s.srvs }

// TestClusterServersLeavesTheSourceSliceAlone (R3-conc-3401, a regression of
// the R2-C2-err-0450 fix): ClusterServers appends the measurement servers to
// a copy, never into the spare capacity of the slice the source returned —
// that backing array is the host's, and two dispatches at once would write
// the same element.
func TestClusterServersLeavesTheSourceSliceAlone(t *testing.T) {
	t.Parallel()
	backing := make([]contract.ClusterServer, 1, 8)
	backing[0] = &fakeServerFull{id: 0x0006}
	src := spareCapSource{srvs: backing}
	s := Spec{
		StableKey: StringKey("cap"), DeviceType: 0x0306, FriendlyName: "cap",
		Source: src, Measurement: measurementSource{class: contract.MeasurementFlow},
	}
	topo, err := c3Assembler(t).Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{s}}})
	if err != nil {
		t.Fatal(err)
	}
	ep := topo.Endpoints[2]
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_ = ClusterServers(ep)
			}
		}()
	}
	wg.Wait()
	for i, srv := range backing[1:cap(backing)] {
		if srv != nil {
			t.Fatalf("the source's spare capacity at %d holds cluster %#x after ClusterServers", i+1, srv.MatterClusterID())
		}
	}
	if got := len(ClusterServers(ep)); got < 2 {
		t.Fatalf("ClusterServers = %d servers, want the source's and the measurement's", got)
	}
}

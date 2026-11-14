// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Every cluster instance has a DataVersion (review II, decision 46,
// R2-P2-ctr-05): the endpoint keeps one for every (endpoint, cluster), the
// root and the Aggregator included, and a server with its own tracker
// (contract.ClusterDataVersion) overrides it. matter.js gives every
// Datasource a version (packages/node/src/behavior/state/managed/Datasource.ts:597
// `this.version = options.entropy.randomUint32`).

// TestRootAndAggregatorClustersHaveAVersion — a root or Aggregator server
// without a tracker of its own reports the endpoint-held version, stable
// across calls; one with its own reports that one.
func TestRootAndAggregatorClustersHaveAVersion(t *testing.T) {
	t.Parallel()
	agg := &Endpoint{ID: 1, Aggregator: true, DeviceType: 0x000E}
	agg.PublishClusterServers([]contract.ClusterServer{&fakeServerFull{id: 0x0039}})
	top := &Topology{
		Endpoints: []*Endpoint{
			rootEndpointWith(&fakeServerFull{id: 0x0038}, &dvServer{id: 0x0008, ver: 7}),
			agg,
		},
		VendorID: 0xFFF1, ProductID: 0x8000,
	}
	d := NewTopologyDispatcher(top)
	ctx := context.Background()
	for _, c := range []struct {
		ep      uint16
		cluster uint32
	}{{0, 0x0038}, {1, 0x0039}} {
		v1, ok := d.CurrentDataVersion(ctx, c.ep, c.cluster)
		if !ok {
			t.Fatalf("endpoint %d cluster %#x without a tracker of its own: no version", c.ep, c.cluster)
		}
		if v2, _ := d.CurrentDataVersion(ctx, c.ep, c.cluster); v2 != v1 {
			t.Fatalf("endpoint %d cluster %#x: version moved without a change, %d → %d", c.ep, c.cluster, v1, v2)
		}
	}
	if v, ok := d.CurrentDataVersion(ctx, 0, 0x0008); !ok || v != 7 {
		t.Fatalf("root server with its own tracker: (%d, %v), want (7, true)", v, ok)
	}
}

// TestRootWriteAdvancesTheEndpointHeldVersion — a successful write to a root
// server without a tracker of its own advances the endpoint-held version; a
// write to one with its own leaves the endpoint-held one alone (the server
// advances its own).
func TestRootWriteAdvancesTheEndpointHeldVersion(t *testing.T) {
	t.Parallel()
	plain := &fakeServerFull{id: 0x0006}
	root := rootEndpointWith(plain)
	d := NewTopologyDispatcher(&Topology{
		Endpoints: []*Endpoint{root, {ID: 1, Aggregator: true, DeviceType: 0x000E}},
		VendorID:  0xFFF1, ProductID: 0x8000,
	})
	ctx := context.Background()
	before, _ := d.CurrentDataVersion(ctx, 0, 0x0006)
	res := d.Write(ctx, concreteAttrPath(0, 0x0006, 0x0001), im.AttributeValue{Value: uint8(1)})
	if len(res) != 1 || res[0].Status != im.StatusSuccess {
		t.Fatalf("write: %+v", res)
	}
	if after, ok := d.CurrentDataVersion(ctx, 0, 0x0006); !ok || after == before {
		t.Fatalf("version after a successful write (%d, %v), want it advanced from %d", after, ok, before)
	}
}

// TestRootReadCarriesTheVersion — a read of a root server without a
// tracker of its own carries the endpoint-held version, flagged as present.
func TestRootReadCarriesTheVersion(t *testing.T) {
	t.Parallel()
	d := NewTopologyDispatcher(&Topology{
		Endpoints: []*Endpoint{rootEndpointWith(&fakeServerFull{id: 0x0038, readVal: uint64(1), readOK: true}), {ID: 1, Aggregator: true, DeviceType: 0x000E}},
		VendorID:  0xFFF1, ProductID: 0x8000,
	})
	ctx := context.Background()
	want, _ := d.CurrentDataVersion(ctx, 0, 0x0038)
	res := d.Read(ctx, concreteAttrPath(0, 0x0038, 0x0000))
	if len(res) != 1 || res[0].Status != im.StatusSuccess {
		t.Fatalf("read: %+v", res)
	}
	if !res[0].HasDataVersion || res[0].DataVersion != want {
		t.Fatalf("read DataVersion (%d, has %v), want (%d, true)", res[0].DataVersion, res[0].HasDataVersion, want)
	}
}

// TestRootAndAggregatorVersionsSurviveReassembly — the root's and the
// Aggregator's endpoint-held versions are the assembler's, not the
// assembled endpoint's: a reassembly keeps them, as it keeps a bridged
// endpoint's.
func TestRootAndAggregatorVersionsSurviveReassembly(t *testing.T) {
	t.Parallel()
	a, err := New(newInternalFakeStore(), Config{VendorID: 1, ProductID: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.Assemble(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	root, agg := first.Endpoints[0].ClusterDataVersion(0x0038), first.Endpoints[1].ClusterDataVersion(0x0039)
	second, err := a.Assemble(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := second.Endpoints[0].ClusterDataVersion(0x0038); got != root {
		t.Fatalf("root version after reassembly %d, want %d", got, root)
	}
	if got := second.Endpoints[1].ClusterDataVersion(0x0039); got != agg {
		t.Fatalf("Aggregator version after reassembly %d, want %d", got, agg)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"slices"
	"testing"

	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/schema"
)

// TestEndpointsEqualsTheWire: Node.Endpoints of a node with an Aggregator,
// a bridged light and a bridged node with a part lists the root first and
// every endpoint by number, each with the PartsList, DeviceTypeList and
// ServerList a controller's wildcard Descriptor read reports for it
// (fabrictest.ReadCluster), the tree edges the PartsLists imply, the
// schema's device-type revisions and the bridged devices' keys.
func TestEndpointsEqualsTheWire(t *testing.T) {
	t.Parallel()
	lamp, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("lamp"), Name: "lamp"})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey("owner"), Name: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	part, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("owner/light"), Name: "light"})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.AddPart(part.Device); err != nil {
		t.Fatal(err)
	}
	site := quietSite(t)
	p := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{lamp, owner}}})
	eps := p.Device.Node().Endpoints()
	topo := p.Device.Node().Topology()
	if len(eps) != 5 || eps[0].Endpoint != 0 || eps[0].HasParent {
		t.Fatalf("Endpoints %+v, want the root first and five endpoints", eps)
	}
	if !slices.IsSortedFunc(eps, func(a, b node.EndpointInfo) int { return int(a.Endpoint) - int(b.Endpoint) }) {
		t.Fatalf("Endpoints not by number: %+v", eps)
	}
	if root := eps[0]; len(root.Parts) != 4 {
		t.Errorf("root Parts %v, want every other endpoint", root.Parts)
	}
	byNumber := map[uint16]node.EndpointInfo{}
	for _, e := range eps {
		byNumber[e.Endpoint] = e
	}
	ctx := parityCtx(t)
	for _, e := range eps {
		v, err := fabrictest.ReadCluster(ctx, p, e.Endpoint, descdef.ClusterID)
		if err != nil {
			t.Fatalf("endpoint %d: %v", e.Endpoint, err)
		}
		st, _ := descdef.StateFrom(v)
		if !slices.Equal(e.Parts, st.PartsList) {
			t.Errorf("endpoint %d Parts %v, wire PartsList %v", e.Endpoint, e.Parts, st.PartsList)
		}
		types := make([]node.DeviceTypeInfo, 0, len(st.DeviceTypeList))
		for _, dt := range st.DeviceTypeList {
			types = append(types, node.DeviceTypeInfo{ID: dt.DeviceType, Revision: dt.Revision})
			if want, ok := schema.DeviceTypeRevision(schema.DeviceTypeID(dt.DeviceType)); ok && dt.Revision != want {
				t.Errorf("endpoint %d device type 0x%04X revision %d, schema %d", e.Endpoint, dt.DeviceType, dt.Revision, want)
			}
		}
		if !slices.Equal(e.DeviceTypes, types) || len(types) == 0 {
			t.Errorf("endpoint %d DeviceTypes %v, wire DeviceTypeList %v", e.Endpoint, e.DeviceTypes, types)
		}
		ids := make([]uint32, 0, len(e.Clusters))
		for _, c := range e.Clusters {
			ids = append(ids, c.ID)
		}
		if !slices.Equal(ids, st.ServerList) {
			t.Errorf("endpoint %d Clusters %v, wire ServerList %v", e.Endpoint, ids, st.ServerList)
		}
		// The parent is a listing endpoint, and no endpoint between lists it.
		if e.HasParent {
			parent, ok := byNumber[e.Parent]
			if !ok || !slices.Contains(parent.Parts, e.Endpoint) {
				t.Errorf("endpoint %d parent %d does not list it", e.Endpoint, e.Parent)
			}
		} else if e.Endpoint != 0 {
			t.Errorf("endpoint %d has no parent", e.Endpoint)
		}
	}
	if len(topo.Devices) != 2 {
		t.Fatalf("Topology %+v, want two bridged devices", topo)
	}
	agg := byNumber[topo.Aggregator]
	if !agg.HasParent || agg.Parent != 0 || agg.Key != nil {
		t.Errorf("Aggregator %+v, want the root's child without a key", agg)
	}
	for _, d := range topo.Devices {
		e := byNumber[d.Endpoint]
		if !e.HasParent || e.Parent != topo.Aggregator || e.Key == nil || e.Key.String() != d.Key.String() || e.Name != d.Name {
			t.Errorf("bridged endpoint %+v, want the Aggregator's child keyed %v named %q", e, d.Key, d.Name)
		}
		for _, pt := range d.Parts {
			if e := byNumber[pt.Endpoint]; !e.HasParent || e.Parent != d.Endpoint {
				t.Errorf("part %+v, want the child of %d", e, d.Endpoint)
			}
		}
	}
}

// TestEndpointsBeforeStart: a node not started serves no endpoint.
func TestEndpointsBeforeStart(t *testing.T) {
	t.Parallel()
	n := newNode(t, minimalConfig(openDB(t, t.TempDir()+"/node.db")))
	if eps := n.Endpoints(); len(eps) != 0 {
		t.Fatalf("Endpoints before Start %+v, want none", eps)
	}
}

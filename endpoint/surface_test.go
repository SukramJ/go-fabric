// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/surface"
)

func TestClusterSurfaceReadsAsTheDispatcher(t *testing.T) {
	g := ClusterSurface(&globalAttrServer{id: 0x0006})
	want := surface.Cluster{
		ID: 0x0006, Revision: 7, FeatureMap: 0x42,
		Attributes: []uint32{
			cluster.AttrGlobalGeneratedCommandList, cluster.AttrGlobalAcceptedCommandList,
			cluster.AttrGlobalAttributeList, cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision,
		},
		Accepted: []uint32{}, Generated: []uint32{}, Events: []uint32{},
	}
	if !reflect.DeepEqual(g, want) {
		t.Fatalf("globals-only server:\n got %+v\nwant %+v", g, want)
	}
	s := dtServer{id: 0x0008, features: uint8(3), attrs: []uint32{0x4000, 0}, cmds: []uint32{4, 0}, events: []uint32{2, 1}}
	c := ClusterSurface(s)
	if c.FeatureMap != 3 || !reflect.DeepEqual(c.Accepted, []uint32{0, 4}) || !reflect.DeepEqual(c.Events, []uint32{1, 2}) ||
		!reflect.DeepEqual(c.Attributes, []uint32{0, 0x4000, 0xFFF8, 0xFFF9, 0xFFFB, 0xFFFC, 0xFFFD}) || c.Revision != 0 {
		t.Fatalf("listing server: %+v", c)
	}
	// A server whose AttributeList is no id list reads as none, as the
	// write gate reads it.
	if l := ClusterSurface(listedServer{s}); len(l.Attributes) != 0 {
		t.Fatalf("unreadable AttributeList: %v", l.Attributes)
	}
	for v, want := range map[any]uint32{uint32(5): 5, uint16(5): 5, uint8(5): 5, uint64(5): 5, 5: 5, -1: 0, "x": 0} {
		if got := uintOf(v); got != want {
			t.Errorf("uintOf(%#v) = %d, want %d", v, got, want)
		}
	}
}

func TestSurfaceEndpoint(t *testing.T) {
	desc := descriptorFor(t, nil, 0x0100, 0x0013)
	first := dtServer{id: 0x0006, attrs: []uint32{0}}
	shadow := dtServer{id: 0x0006, attrs: []uint32{0, 0x4000}}
	ep := SurfaceEndpoint("light", []contract.ClusterServer{first, nil, desc, shadow, dtServer{id: 0x0003}})
	if ep.Key != "light" || ep.Number != 0 {
		t.Fatalf("endpoint %+v", ep)
	}
	if want := []surface.DeviceTypeRef{{ID: 0x0100, Revision: 1}, {ID: 0x0013, Revision: 1}}; !reflect.DeepEqual(ep.DeviceTypes, want) {
		t.Fatalf("device types %v", ep.DeviceTypes)
	}
	ids := make([]uint32, 0, len(ep.Clusters))
	for _, c := range ep.Clusters {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, []uint32{0x0003, 0x0006, 0x001D}) {
		t.Fatalf("clusters %v", ids)
	}
	if got := ep.Clusters[1].Attributes; len(got) != 6 {
		t.Fatalf("the first server of a cluster id answers, got %v", got)
	}
	if none := SurfaceEndpoint("", []contract.ClusterServer{first}); none.DeviceTypes != nil {
		t.Fatalf("device types without a Descriptor: %v", none.DeviceTypes)
	}
	if RenderSourceKey(StringKey("k")) != "k" || RenderSourceKey(nil) != "" {
		t.Fatal("RenderSourceKey")
	}
}

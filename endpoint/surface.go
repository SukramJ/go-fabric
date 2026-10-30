// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"cmp"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/surface"
)

// descriptorClusterID is the Descriptor cluster (descriptor.element.ts,
// 0x001D); descriptorAttrDeviceTypeList its DeviceTypeList (0x0000).
const (
	descriptorClusterID          uint32 = 0x001D
	descriptorAttrDeviceTypeList uint32 = 0x0000
)

// GlobalList reads a list-valued global attribute of srv — AttributeList,
// AcceptedCommandList or GeneratedCommandList — as the dispatcher serves
// it: the server's own answer, the dispatcher's synthesis
// (synthesizeGlobalRead) otherwise, in the order of [readOne]. Nil when
// neither answers a plain id list.
func GlobalList(srv contract.ClusterServer, attrID uint32) []uint32 {
	v, ok := srv.MatterRead(attrID)
	if !ok {
		v, ok = synthesizeGlobalRead(srv, attrID)
	}
	ids, isList := v.([]uint32)
	if !ok || !isList {
		return nil
	}
	return ids
}

// ClusterSurface reads srv as a controller's wildcard read of it returns:
// its ClusterRevision and FeatureMap as the server answers them (0 when it
// answers none), its AttributeList, AcceptedCommandList and
// GeneratedCommandList through [GlobalList], and the events the dispatcher
// accepts an event path for ([contract.ClusterEventLister]). Every list is
// a sorted copy. The surface snapshot and the wire read the same function,
// so they cannot disagree.
func ClusterSurface(srv contract.ClusterServer) surface.Cluster {
	c := surface.Cluster{
		ID:         srv.MatterClusterID(),
		FeatureMap: featureMapOf(srv),
		Attributes: sortedIDs(GlobalList(srv, cluster.AttrGlobalAttributeList)),
		Accepted:   sortedIDs(GlobalList(srv, cluster.AttrGlobalAcceptedCommandList)),
		Generated:  sortedIDs(GlobalList(srv, cluster.AttrGlobalGeneratedCommandList)),
		Events:     []uint32{},
	}
	if v, ok := srv.MatterRead(cluster.AttrGlobalClusterRevision); ok {
		c.Revision = uint16(uintOf(v)) //nolint:gosec // a ClusterRevision is a uint16
	}
	if lister, ok := srv.(contract.ClusterEventLister); ok {
		c.Events = sortedIDs(lister.MatterEvents())
	}
	return c
}

// SurfaceEndpoint is the surface of an endpoint with key that mounts
// servers: one [ClusterSurface] per cluster id — the first server of an
// id, as the dispatcher routes a path — ordered by id, and the device
// types the endpoint's Descriptor lists, in its order; none without a
// Descriptor among servers.
func SurfaceEndpoint(key string, servers []contract.ClusterServer) surface.Endpoint {
	ep := surface.Endpoint{Key: key}
	seen := map[uint32]bool{}
	for _, srv := range servers {
		if srv == nil {
			continue
		}
		id := srv.MatterClusterID()
		if seen[id] {
			continue
		}
		seen[id] = true
		ep.Clusters = append(ep.Clusters, ClusterSurface(srv))
		if id != descriptorClusterID {
			continue
		}
		if v, ok := srv.MatterRead(descriptorAttrDeviceTypeList); ok {
			if list, ok := v.([]descdef.DeviceTypeStruct); ok {
				for _, dt := range list {
					ep.DeviceTypes = append(ep.DeviceTypes, surface.DeviceTypeRef{ID: dt.DeviceType, Revision: dt.Revision})
				}
			}
		}
	}
	slices.SortStableFunc(ep.Clusters, func(a, b surface.Cluster) int { return cmp.Compare(a.ID, b.ID) })
	return ep
}

// RenderSourceKey renders an endpoint's StableKey as the endpoint
// identity: the rendering the assembler derives an endpoint's UniqueID
// from (renderSourceKey).
func RenderSourceKey(key any) string { return renderSourceKey(key) }

// sortedIDs is an ascending copy of ids; [] for none.
func sortedIDs(ids []uint32) []uint32 {
	out := append(make([]uint32, 0, len(ids)), ids...)
	slices.Sort(out)
	return out
}

// uintOf reads an unsigned global attribute value of any integer type; 0
// for a value that is none.
func uintOf(v any) uint32 {
	switch x := v.(type) {
	case uint32:
		return x
	case uint16:
		return uint32(x)
	case uint8:
		return uint32(x)
	case uint64:
		return uint32(x) //nolint:gosec // the global attributes read here are at most 32 bits wide
	case int:
		if x < 0 {
			return 0
		}
		return uint32(x) //nolint:gosec // bounded by the attribute's type
	default:
		return 0
	}
}

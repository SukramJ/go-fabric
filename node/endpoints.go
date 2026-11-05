// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"slices"

	descdef "github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
)

// EndpointInfo is one endpoint the node serves, as [Node.Endpoints]
// returns it: its place in the endpoint tree and what its Descriptor
// serves.
type EndpointInfo struct {
	// Endpoint is the endpoint number.
	Endpoint uint16
	// Parent is the endpoint whose part this one is — the root for the
	// Aggregator, the Aggregator for a bridged device, the device for one
	// of its parts — valid when HasParent is set. The root has none.
	Parent    uint16
	HasParent bool
	// Parts is the Descriptor PartsList the endpoint serves, in its
	// order: every endpoint below it (the root's lists every other
	// endpoint, a bridged device's its parts), as a controller reads it.
	Parts []uint16
	// DeviceTypes is the Descriptor DeviceTypeList the endpoint serves,
	// in its order, each with the revision it reports.
	DeviceTypes []DeviceTypeInfo
	// Clusters are the cluster servers the endpoint serves, in the order
	// of its Descriptor ServerList.
	Clusters []ClusterInfo
	// Name is the endpoint's node label, empty for one without.
	Name string
	// Key is the source key of the host device the endpoint was assembled
	// from; nil for an endpoint no host device declared (the root, the
	// Aggregator).
	Key device.SourceKey
	// Scope is the snapshot scope the endpoint was assembled from, as
	// [BridgedDevice].Scope names it.
	Scope string
}

// DeviceTypeInfo is one entry of a Descriptor DeviceTypeList: the device
// type id (a 32-bit devtype-id on the wire) and the revision the endpoint
// reports for it.
type DeviceTypeInfo struct {
	ID       uint32
	Revision uint16
}

// Endpoints returns every endpoint the node serves now — the root first,
// then by endpoint number — with its tree edges and its Descriptor's
// lists: empty before [Node.Start]. It reads the assembled endpoint tree
// (Node.Bridge().Topology()) and each endpoint's Descriptor server as a
// controller's wildcard Descriptor read reports it.
//
// It is the controller's view, and [Node.Topology] the host's: Topology
// lists the bridged devices by the key the host added them under, with
// their reachability, tags and labels; Endpoints lists every endpoint —
// the root and the Aggregator included — by number, with the PartsList
// and DeviceTypeList a controller walks. Neither is derived from the
// other's purpose.
func (n *Node) Endpoints() []EndpointInfo {
	topo := n.br.Topology()
	if topo == nil {
		return nil
	}
	eps := make([]*endpoint.Endpoint, 0, len(topo.Endpoints))
	for _, ep := range topo.Endpoints {
		if ep != nil {
			eps = append(eps, ep)
		}
	}
	slices.SortFunc(eps, func(a, b *endpoint.Endpoint) int { return int(a.ID) - int(b.ID) })
	out := make([]EndpointInfo, 0, len(eps))
	for _, ep := range eps {
		info := EndpointInfo{
			Endpoint: ep.ID, Clusters: mountedClusters(ep), Name: ep.NodeLabel(), Key: ep.SourceKey, Scope: ep.Scope,
		}
		info.DeviceTypes, info.Parts = descriptorLists(ep)
		out = append(out, info)
	}
	// The parent of an endpoint is the endpoint with the shortest
	// PartsList that lists it: an ancestor lists every endpoint below it
	// (the full-family pattern), so the nearest ancestor lists the fewest.
	for i := range out {
		best := -1
		for j := range out {
			if j != i && slices.Contains(out[j].Parts, out[i].Endpoint) && (best < 0 || len(out[j].Parts) < len(out[best].Parts)) {
				best = j
			}
		}
		if best >= 0 {
			out[i].Parent, out[i].HasParent = out[best].Endpoint, true
		}
	}
	return out
}

// descriptorLists are the DeviceTypeList and PartsList ep's Descriptor
// serves, read as a controller would (MatterRead).
func descriptorLists(ep *endpoint.Endpoint) (types []DeviceTypeInfo, parts []uint16) {
	for _, srv := range endpoint.ClusterServers(ep) {
		if srv == nil || srv.MatterClusterID() != descdef.ClusterID {
			continue
		}
		if v, ok := srv.MatterRead(descdef.AttrDeviceTypeList); ok {
			if list, ok := v.([]descdef.DeviceTypeStruct); ok {
				for _, dt := range list {
					types = append(types, DeviceTypeInfo{ID: dt.DeviceType, Revision: dt.Revision})
				}
			}
		}
		if v, ok := srv.MatterRead(descdef.AttrPartsList); ok {
			if list, ok := v.([]uint16); ok {
				parts = slices.Clone(list)
			}
		}
		return types, parts
	}
	return nil, nil
}

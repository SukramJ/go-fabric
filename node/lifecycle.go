// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/internal/deviceseam"
	"github.com/SukramJ/go-fabric/schema"
)

// The node's half of a device's lifecycle (ADR 0025 principle 2): the
// Aggregator binds a device.Device it adds to the node (its clock, its
// queue, its endpoint number, reachable and rename) through
// internal/deviceseam, and unbinds it when it removes or replaces it and
// when the node stops.

// nodeHost is the node as the host of the devices it holds
// ([deviceseam.Host]).
type nodeHost struct{ n *Node }

var _ deviceseam.Host = nodeHost{}

// Clock implements [deviceseam.Host].
func (h nodeHost) Clock() clock.Clock { return h.n.cfg.Clock }

// EndpointNumber implements [deviceseam.Host].
func (h nodeHost) EndpointNumber(key endpoint.SourceKey) (uint16, bool) {
	if ep := h.n.assembled(key); ep != nil {
		return ep.ID, true
	}
	return 0, false
}

// NotifyReachable implements [deviceseam.Host].
func (h nodeHost) NotifyReachable(_ context.Context, key endpoint.SourceKey, reachable bool) {
	if ep := h.n.assembled(key); ep != nil {
		h.n.br.NotifyEndpointReachable(ep.ID, reachable)
	}
}

// Rename implements [deviceseam.Host]: the assembled endpoint's NodeLabel,
// or, before the node assembled it, the persisted label the assembly
// restores (Aggregator.snapshot).
func (h nodeHost) Rename(_ context.Context, key endpoint.SourceKey, name string) {
	if ep := h.n.assembled(key); ep != nil {
		h.n.br.NotifyEndpointNodeLabel(ep.ID, name)
		return
	}
	h.n.endpoints.OnNodeLabelWritten(key, name)
}

// HostName implements [deviceseam.Host]: the host's current name of the
// assembled endpoint (bridge.Bridge.NotifyEndpointHostName), persisted
// nowhere; before the node assembled it the assembly reads the name from
// the device's Spec.
func (h nodeHost) HostName(_ context.Context, key endpoint.SourceKey, name string) {
	if ep := h.n.assembled(key); ep != nil {
		h.n.br.NotifyEndpointHostName(ep.ID, name)
	}
}

// assembled returns the endpoint of the running topology with key — a
// bridged endpoint or a part — or nil.
func (n *Node) assembled(key endpoint.SourceKey) *endpoint.Endpoint {
	if key == nil {
		return nil
	}
	topo := n.br.Topology()
	if topo == nil {
		return nil
	}
	want := key.String()
	for _, ep := range topo.Endpoints {
		if ep != nil && ep.SourceKey != nil && ep.SourceKey.String() == want {
			return ep
		}
	}
	return nil
}

// BridgeTopology is the host's view of the node's bridge: the Aggregator's
// endpoint number and the bridged devices under it, each with its endpoint
// number — what [Node.Topology] returns.
type BridgeTopology struct {
	// Aggregator is the Aggregator's endpoint number; 0 before the node
	// assembled its endpoints.
	Aggregator uint16
	// Devices are the bridged devices, by endpoint number.
	Devices []BridgedDevice
}

// BridgedDevice is one bridged device of a [BridgeTopology], or a part of
// one.
type BridgedDevice struct {
	// Key is the device's stable key ([device.Options].StableKey).
	Key device.SourceKey
	// Endpoint is its endpoint number.
	Endpoint uint16
	// DeviceType is its device type id, DeviceTypeRevision the revision
	// its Descriptor's DeviceTypeList serves for it (the generated
	// schema's, schema.DeviceTypeRevision; 0 for an id the schema does
	// not know).
	DeviceType         uint16
	DeviceTypeRevision uint16
	// Clusters are the cluster servers mounted on the endpoint, in the
	// order its Descriptor's ServerList lists them — for every endpoint,
	// parts and the endpoints of a host's own [Snapshotter] included.
	Clusters []ClusterInfo
	// Name is the NodeLabel it serves: a controller's or the host's
	// rename, else its Name.
	Name string
	// Reachable is its Reachable as a controller reads it now.
	Reachable bool
	// Added is what the host added to the Aggregator under Key (a
	// device.Device, the generated type around one, or any [Endpoint]);
	// nil for a part and for an endpoint of a host's own [Snapshotter].
	Added Endpoint
	// Parts are its parts, by endpoint number.
	Parts []BridgedDevice
}

// ClusterInfo names one cluster server a [BridgedDevice] serves.
type ClusterInfo struct {
	// ID is the cluster id.
	ID uint32
	// Name is the cluster's name in the generated schema
	// (schema.ClusterName: "OnOff", "LevelControl", …); empty for a
	// cluster the schema does not know (a manufacturer-specific one).
	Name string
	// Revision is the schema's ClusterRevision of the cluster
	// (schema.ClusterRevision); 0 for a cluster the schema does not know.
	Revision uint16
}

// Topology returns the host's view of the bridge as the node serves it
// now: empty before [Node.Start]. It is the facade's route to what the
// assembled endpoint tree (Node.Bridge().Topology()) holds — the endpoint
// numbers matter.js's node assigns its endpoints
// (packages/node/src/node/server/ServerEndpointInitializer.ts:34-42).
func (n *Node) Topology() BridgeTopology {
	topo := n.br.Topology()
	if topo == nil {
		return BridgeTopology{}
	}
	out := BridgeTopology{}
	if agg := topo.Aggregator(); agg != nil {
		out.Aggregator = agg.ID
	}
	added := map[string]Endpoint{}
	if n.agg != nil {
		for _, ep := range n.agg.Endpoints() {
			if key, err := keyOf(ep.Spec()); err == nil {
				added[key] = ep
			}
		}
	}
	for _, ep := range topo.Bridged() {
		d := bridgedDevice(topo, ep)
		if ep.SourceKey != nil {
			d.Added = added[ep.SourceKey.String()]
		}
		out.Devices = append(out.Devices, d)
	}
	return out
}

// mountedClusters are the cluster servers the assembled endpoint serves
// (endpoint.ClusterServers: the set its Descriptor's ServerList is derived
// from), each once, with the generated schema's name and revision.
// Walking the mounted servers rather than the declaration
// (Added.Spec().Source) covers parts and a host Snapshotter's endpoints,
// and the servers the assembler adds itself (Descriptor,
// BridgedDeviceBasicInformation, Identify, Groups, …). matter.js reads
// the same from the endpoint's behaviors — DescriptorServer's serverList
// is the endpoint's supported cluster behaviors
// (packages/node/src/behaviors/descriptor/DescriptorServer.ts:275
// #serverList at 862ad75f).
func mountedClusters(ep *endpoint.Endpoint) []ClusterInfo {
	servers := endpoint.ClusterServers(ep)
	out := make([]ClusterInfo, 0, len(servers))
	seen := make(map[uint32]bool, len(servers))
	for _, srv := range servers {
		if srv == nil {
			continue
		}
		id := srv.MatterClusterID()
		if seen[id] {
			continue
		}
		seen[id] = true
		info := ClusterInfo{ID: id}
		info.Name, _ = schema.ClusterName(id)
		info.Revision, _ = schema.ClusterRevision(id)
		out = append(out, info)
	}
	return out
}

// bridgedDevice is ep with its parts, recursively.
func bridgedDevice(topo *endpoint.Topology, ep *endpoint.Endpoint) BridgedDevice {
	d := BridgedDevice{
		Key: ep.SourceKey, Endpoint: ep.ID, DeviceType: ep.DeviceType,
		Name: ep.NodeLabel(), Reachable: ep.Availability == nil || ep.Availability(),
		Clusters: mountedClusters(ep),
	}
	if id, ok := schema.DeviceTypeIDOf(uint32(ep.DeviceType)); ok {
		d.DeviceTypeRevision, _ = schema.DeviceTypeRevision(id)
	}
	// Its parts are the endpoints whose parent it is (the PartsList of a
	// bridged node lists every descendant).
	for _, part := range topo.Endpoints {
		if part != nil && part.Part && part.HasParentEndpointID && part.ParentEndpointID == ep.ID {
			d.Parts = append(d.Parts, bridgedDevice(topo, part))
		}
	}
	return d
}

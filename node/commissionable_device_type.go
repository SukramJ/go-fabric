// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"slices"
	"sync"

	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/schema"
)

// commissionableDeviceType is the device type of the commissionable
// advertisement when [ProductDescription].DeviceType is zero: inferred
// from the endpoints the node serves each time it advertises, until a type
// is found, and that type from then on. Mirrors matter.js
// packages/node/src/behavior/system/product-description/
// ProductDescriptionServer.ts:43-63 at db91886c (#setDeviceType: a type
// the application set is kept; otherwise inferred, and while nothing is
// found inferred again on the next change of the endpoint tree; once
// found, the state is no longer UNKNOWN_DEVICE_TYPE and the inference does
// not run again). matter.js reads the state at each window it opens
// (DeviceCommissioner.ts:244-254, #openWindow), so a window opened after
// the endpoints changed carries the type then found; this module
// evaluates it at the same point, the announcement of a window.
//
// While no type is found matter.js does not advertise at all
// (CommissioningServer.ts:280-283, :309-314); this module advertises
// RootNode (0x0016) instead, as it did before it inferred.
type commissionableDeviceType struct {
	topology func() *endpoint.Topology
	// mu guards found.
	mu    sync.Mutex
	found uint32 // 0 until the inference found a type
}

// resolve returns the type the commissionable record carries now.
func (c *commissionableDeviceType) resolve() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.found == 0 && c.topology != nil {
		if dt, ok := inferDeviceType(c.topology()); ok {
			c.found = dt
		}
	}
	if c.found != 0 {
		return c.found
	}
	return deviceTypeRootNode
}

// inferDeviceType walks topo from the root as matter.js inferDeviceType
// walks the endpoint tree (ProductDescriptionServer.ts:93-126 at
// db91886c): an endpoint whose DeviceTypeList holds RootNode, BridgedNode
// or Aggregator is descended into, part by part; at an endpoint whose own
// device type is of the simple class every other entry of its
// DeviceTypeList is a candidate, and the first one is the answer. The
// list is the endpoint's Descriptor DeviceTypeList as it is served, the
// parts are its direct parts in the order its PartsList serves them, and
// the class is the snapshot's classification of the endpoint's device type
// (matter.js's EndpointType deviceClass, EndpointType.ts:31 at db91886c —
// Simple unless the endpoint type states another, as the utility and node
// types of packages/node/src/endpoints/ do, the ones the snapshot
// classifies "utility" and "node"). A device type the snapshot does not
// hold states no class, so its endpoint is of the simple class.
func inferDeviceType(topo *endpoint.Topology) (uint32, bool) {
	infos := endpointInfos(topo)
	if len(infos) == 0 {
		return 0, false
	}
	byID := make(map[uint16]int, len(infos))
	for i := range infos {
		byID[infos[i].Endpoint] = i
	}
	var walk func(i int) (uint32, bool)
	walk = func(i int) (uint32, bool) {
		info := &infos[i]
		simple := false
		if ep := topo.FindByID(info.Endpoint); ep != nil {
			def, ok := schema.DeviceTypeDefinitionOf(schema.DeviceTypeID(ep.DeviceType))
			simple = !ok || def.Classification == "simple"
		}
		recurse := false
		for _, dt := range info.DeviceTypes {
			switch dt.ID {
			case deviceTypeRootNode, uint32(device.BridgedNodeID), deviceTypeAggregator:
				recurse = true
			default:
				if simple {
					return dt.ID, true
				}
			}
		}
		if !recurse {
			return 0, false
		}
		for _, part := range info.Parts {
			j, ok := byID[part]
			if !ok || !infos[j].HasParent || infos[j].Parent != info.Endpoint {
				continue // a part of a part: reached through its own parent
			}
			if dt, ok := walk(j); ok {
				return dt, true
			}
		}
		return 0, false
	}
	root := slices.IndexFunc(infos, func(e EndpointInfo) bool { return !e.HasParent && e.Endpoint == 0 })
	if root < 0 {
		return 0, false
	}
	return walk(root)
}

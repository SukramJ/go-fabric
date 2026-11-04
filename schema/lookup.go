// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package schema

import "slices"

// ClusterRevision returns the matter.js HEAD revision for the given cluster
// id, or (0, false) if the cluster is not present in the snapshot.
//
// Mirrors matter.js packages/model/src/standard/elements/<name>.element.ts
// ClusterRevision attribute defaults.
func ClusterRevision(id uint32) (uint16, bool) {
	rev, ok := ClusterRevisions[id]
	return rev, ok
}

// ClusterName returns the canonical matter.js cluster name for the given id,
// or ("", false) if the cluster is not present in the snapshot.
func ClusterName(id uint32) (string, bool) {
	name, ok := ClusterNames[id]
	return name, ok
}

// DeviceTypeRevision returns the matter.js HEAD revision for the given
// device-type id, or (0, false) if the device type is not present in the
// snapshot.
//
// This is the codegen'd counterpart of endpoint.deviceTypeRevision: callers
// that need to advertise a device-type revision in Descriptor.DeviceTypeList
// should prefer this function so updates to the matter.js snapshot propagate
// automatically on the next `go generate ./schema/...` run.
//
// Mirrors matter.js packages/node/src/devices/<name>.ts revision fields.
func DeviceTypeRevision(id DeviceTypeID) (uint16, bool) {
	rev, ok := DeviceTypeRevisions[id]
	return rev, ok
}

// DeviceTypeIDOf returns the device type id a Descriptor's 32-bit
// devtype-id carries: the id and true for one whose upper half is zero (a
// standard device type), false for a manufacturer-specific id, which no
// table here holds.
func DeviceTypeIDOf(devtype uint32) (DeviceTypeID, bool) {
	if devtype > 0xFFFF {
		return 0, false
	}
	return DeviceTypeID(devtype), true
}

// DeviceTypeName returns the canonical matter.js device-type name for the
// given id, or ("", false) if the device type is not present in the snapshot.
func DeviceTypeName(id DeviceTypeID) (string, bool) {
	name, ok := DeviceTypeNames[id]
	return name, ok
}

// DeviceTypeAllowsServerCluster reports whether the Matter Device Library
// permits clusterID to be mounted as a SERVER cluster on an endpoint whose
// primary device type is deviceType, and whether deviceType is known at all.
//
// A cluster the device type specifies only as a CLIENT requirement is NOT
// permitted: a client requirement means the type consumes that cluster from
// another endpoint. Thermostat (0x0301) is the canonical case — it names
// TemperatureMeasurement and RelativeHumidityMeasurement as clientCluster, so
// a thermostat that serves either is non-conformant even though the cluster
// appears in its requirement list.
//
// Callers must distinguish the two return values: an unknown device type
// (known=false) means the snapshot cannot answer the question, which is not
// the same as a definite "no".
func DeviceTypeAllowsServerCluster(deviceType DeviceTypeID, clusterID uint32) (allowed, known bool) {
	ids, ok := DeviceTypeServerClusters[deviceType]
	if !ok {
		return false, false
	}
	if slices.Contains(ids, clusterID) {
		return true, true
	}
	return false, true
}

// DeviceTypeRequiresServerCluster reports whether the Matter Device Library
// makes clusterID an unconditionally mandatory server cluster of
// deviceType (conformance M) — a cluster matter.js mounts on every
// endpoint of that type by default (packages/node/src/devices/<name>.ts,
// `requirements.server.mandatory`). An unknown device type requires
// nothing.
func DeviceTypeRequiresServerCluster(deviceType DeviceTypeID, clusterID uint32) bool {
	return slices.Contains(DeviceTypeMandatoryServerClusters[deviceType], clusterID)
}

// globalAttributeNames names the global attributes every cluster carries
// (Matter Core §7.13). parity/schema.json lists FeatureMap and
// ClusterRevision under each cluster but none of the id lists, which
// matter.js models once as global elements: GeneratedCommandList
// (generated-command-list.element.ts:14), AcceptedCommandList
// (accepted-command-list.element.ts:14), EventList (event-list.element.ts:12,
// conformance D), AttributeList (attribute-list.element.ts:13), FeatureMap
// (feature-map.element.ts:13) and ClusterRevision
// (cluster-revision.element.ts:13), all under
// packages/model/src/standard/elements/ at 862ad75f.
var globalAttributeNames = map[uint32]string{
	0xFFF8: "GeneratedCommandList",
	0xFFF9: "AcceptedCommandList",
	0xFFFA: "EventList",
	0xFFFB: "AttributeList",
	0xFFFC: "FeatureMap",
	0xFFFD: "ClusterRevision",
}

// AttributeName returns the matter.js name of a cluster's attribute — a
// global attribute (AttributeList, FeatureMap, …) for any cluster id — or
// ("", false) for an attribute the snapshot does not name.
func AttributeName(cluster, attribute uint32) (string, bool) {
	if name, ok := attributeNames[cluster][attribute]; ok {
		return name, true
	}
	name, ok := globalAttributeNames[attribute]
	return name, ok
}

// CommandName returns the matter.js name of a cluster's request command —
// one a client invokes, as AcceptedCommandList lists it — or ("", false)
// for a command the snapshot does not name. A response command's id lives
// in its own space: [ResponseCommandName].
func CommandName(cluster, command uint32) (string, bool) {
	name, ok := commandNames[cluster][command]
	return name, ok
}

// ResponseCommandName returns the matter.js name of a cluster's response
// command — one a server answers with, as GeneratedCommandList lists it —
// or ("", false) for a response the snapshot does not name.
func ResponseCommandName(cluster, command uint32) (string, bool) {
	name, ok := responseCommandNames[cluster][command]
	return name, ok
}

// EventName returns the matter.js name of a cluster's event, or ("", false)
// for an event the snapshot does not name.
func EventName(cluster, event uint32) (string, bool) {
	name, ok := eventNames[cluster][event]
	return name, ok
}

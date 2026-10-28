// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// The Host interfaces of the Joint Fabric servers (brief 26-D): the
// datastore's operations and setters, connectedhomeip's
// JointFabricDatastore methods the administrator application calls, and
// the administrator's state the host reads. What Get reads (the
// attributes) and the node's wiring stay out.
var jointFabricHosts = map[string]hostDecl{
	"JointFabricDatastore": {
		server: "cluster/jointfabric Datastore",
		methods: []string{
			"// SetAnchorRootCA, SetAnchorNodeID, SetAnchorVendorID,",
			"// SetAnchorFabricIndex, SetFriendlyName and SetStatus set the",
			"// anchor and the datastore's own record (chip's setters).",
			"SetAnchorRootCA(ctx context.Context, rcac []byte) error",
			"SetAnchorNodeID(ctx context.Context, nodeID uint64) error",
			"SetAnchorVendorID(ctx context.Context, vendorID uint16) error",
			"SetAnchorFabricIndex(ctx context.Context, fabricIndex uint8) error",
			"SetFriendlyName(ctx context.Context, name string) error",
			"SetStatus(ctx context.Context, state DatastoreStateEnum, updateTimestamp uint32, failureCode uint8) error",
			"// AnchorFabricIndex is the Joint Fabric's index on this node.",
			"AnchorFabricIndex() uint8",
			"// AddPendingNode, UpdateNode, RemoveNode and RefreshNode keep the",
			"// node list; RefreshNode reconciles a node through the",
			"// DatastoreSync before it returns.",
			"AddPendingNode(ctx context.Context, nodeID uint64, name string) error",
			"UpdateNode(ctx context.Context, nodeID uint64, name string) error",
			"RemoveNode(ctx context.Context, nodeID uint64) error",
			"RefreshNode(ctx context.Context, nodeID uint64) error",
			"// The key sets.",
			"AddGroupKeySetEntry(ctx context.Context, ks DatastoreGroupKeySetStruct) error",
			"UpdateGroupKeySetEntry(ctx context.Context, ks DatastoreGroupKeySetStruct) error",
			"RemoveGroupKeySetEntry(ctx context.Context, id uint16) error",
			"IsGroupKeySetEntryPresent(id uint16) bool",
			"// ForceAddNodeKeySetEntry adds a Committed key set entry, the",
			"// administrator's own.",
			"ForceAddNodeKeySetEntry(ctx context.Context, ks uint16, nodeID uint64) error",
			"// The administrators.",
			"AddAdmin(ctx context.Context, a DatastoreAdministratorInformationEntryStruct) error",
			"UpdateAdmin(ctx context.Context, nodeID uint64, name *string, icac *[]byte) error",
			"RemoveAdmin(ctx context.Context, nodeID uint64) error",
			"// The groups; ForceAddGroup also adds the administrator and anchor",
			"// CAT groups AddGroup refuses.",
			"AddGroup(ctx context.Context, req AddGroupRequest) error",
			"ForceAddGroup(ctx context.Context, req AddGroupRequest) error",
			"UpdateGroup(ctx context.Context, req UpdateGroupRequest) error",
			"RemoveGroup(ctx context.Context, groupID uint16) error",
			"// The endpoints' names, memberships and bindings and the nodes'",
			"// ACL entries; each pushes through the DatastoreSync before it",
			"// returns.",
			"UpdateEndpointForNode(ctx context.Context, nodeID uint64, endpointID uint16, name string) error",
			"AddGroupIDToEndpointForNode(ctx context.Context, nodeID uint64, endpointID, groupID uint16) error",
			"RemoveGroupIDFromEndpointForNode(ctx context.Context, nodeID uint64, endpointID, groupID uint16) error",
			"AddBindingToEndpointForNode(ctx context.Context, nodeID uint64, endpointID uint16, binding DatastoreBindingTargetStruct) error",
			"RemoveBindingFromEndpointForNode(ctx context.Context, listID uint16, nodeID uint64, endpointID uint16) error",
			"AddACLToNode(ctx context.Context, nodeID uint64, entry DatastoreAccessControlEntryStruct) error",
			"RemoveACLFromNode(ctx context.Context, listID uint16, nodeID uint64) error",
			"// Wait waits for the pushes and refreshes the commands started.",
			"Wait()",
		},
	},
	"JointFabricAdministrator": {
		server: "cluster/jointfabric Administrator",
		methods: []string{
			"// PeerAdministratorEndpoint is the announcing commissioner's",
			"// JointFabricAdministrator endpoint, 0xFFFF when none.",
			"PeerAdministratorEndpoint() uint16",
			"// VIDVerifiedFabric is the fabric whose VID verification",
			"// succeeded, 0 when none.",
			"VIDVerifiedFabric() uint8",
		},
	},
}

func init() {
	for name, h := range jointFabricHosts {
		hosts[name] = h
	}
}

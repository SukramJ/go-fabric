// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/spec"
	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/im"
)

// The 20 commands, each onto its operation and answered SUCCESS or the
// operation's status — joint-fabric-datastore-server.cpp:293-785 at
// 6170af84. A command's pushes run after its answer ([Datastore]).

func badPayload(name string, fields any) error {
	return spec.Errorf(im.StatusInvalidCommand, "JointFabricDatastore: %s payload %T", name, fields)
}

// handler adapts a typed command onto an operation.
func handler[T any](name string, run func(ctx context.Context, req T) error) spec.CommandHandler {
	return func(ctx context.Context, fields any) (any, error) {
		req, ok := spec.RequestOf[T](fields)
		if !ok {
			return nil, badPayload(name, fields)
		}
		return nil, commandError(run(ctx, req))
	}
}

func (d *Datastore) commands() map[uint32]spec.CommandHandler {
	return map[uint32]spec.CommandHandler{
		dsdef.CmdAddKeySet: handler("AddKeySet", func(ctx context.Context, r dsdef.AddKeySetRequest) error { // :293-314
			return d.AddGroupKeySetEntry(ctx, r.GroupKeySet)
		}),
		dsdef.CmdUpdateKeySet: handler("UpdateKeySet", func(ctx context.Context, r dsdef.UpdateKeySetRequest) error { // :316-339
			if !d.IsGroupKeySetEntryPresent(r.GroupKeySet.GroupKeySetId) { // :324
				return notFoundStatus("group key set %d is not in the datastore", r.GroupKeySet.GroupKeySetId)
			}
			return d.doAsync(ctx, func(p *[]pushFn) error { return d.updateKeySetLocked(r.GroupKeySet, p) })
		}),
		dsdef.CmdRemoveKeySet: handler("RemoveKeySet", func(ctx context.Context, r dsdef.RemoveKeySetRequest) error { // :341-364
			if !d.IsGroupKeySetEntryPresent(r.GroupKeySetId) { // :349
				return notFoundStatus("group key set %d is not in the datastore", r.GroupKeySetId)
			}
			return d.RemoveGroupKeySetEntry(ctx, r.GroupKeySetId)
		}),
		dsdef.CmdAddGroup: handler("AddGroup", func(ctx context.Context, r dsdef.AddGroupRequest) error { // :366-387
			return d.AddGroup(ctx, r)
		}),
		dsdef.CmdUpdateGroup: handler("UpdateGroup", func(ctx context.Context, r dsdef.UpdateGroupRequest) error { // :389-410
			return d.doAsync(ctx, func(p *[]pushFn) error { return d.updateGroupLocked(r, p) })
		}),
		dsdef.CmdRemoveGroup: handler("RemoveGroup", func(ctx context.Context, r dsdef.RemoveGroupRequest) error { // :412-433
			return d.RemoveGroup(ctx, r.GroupId)
		}),
		dsdef.CmdAddAdmin: handler("AddAdmin", func(ctx context.Context, r dsdef.AddAdminRequest) error { // :435-465
			return d.AddAdmin(ctx, AdminEntry{NodeId: r.NodeId, FriendlyName: r.FriendlyName, VendorId: r.VendorId, Icac: r.Icac})
		}),
		dsdef.CmdUpdateAdmin: handler("UpdateAdmin", func(ctx context.Context, r dsdef.UpdateAdminRequest) error { // :467-489
			return d.UpdateAdmin(ctx, r.NodeId, r.FriendlyName, r.Icac)
		}),
		dsdef.CmdRemoveAdmin: handler("RemoveAdmin", func(ctx context.Context, r dsdef.RemoveAdminRequest) error { // :491-513
			return d.RemoveAdmin(ctx, r.NodeId)
		}),
		dsdef.CmdAddPendingNode: handler("AddPendingNode", func(ctx context.Context, r dsdef.AddPendingNodeRequest) error { // :515-537
			return d.AddPendingNode(ctx, r.NodeId, r.FriendlyName)
		}),
		dsdef.CmdRefreshNode: handler("RefreshNode", func(ctx context.Context, r dsdef.RefreshNodeRequest) error { // :539-562
			if err := d.beginRefresh(ctx, r.NodeId); err != nil {
				return err
			}
			bg := context.WithoutCancel(ctx)
			d.pushes.Add(1)
			go func() {
				defer d.pushes.Done()
				_ = d.runRefresh(bg, r.NodeId)
			}()
			return nil
		}),
		dsdef.CmdUpdateNode: handler("UpdateNode", func(ctx context.Context, r dsdef.UpdateNodeRequest) error { // :564-584
			return d.UpdateNode(ctx, r.NodeId, r.FriendlyName)
		}),
		dsdef.CmdRemoveNode: handler("RemoveNode", func(ctx context.Context, r dsdef.RemoveNodeRequest) error { // :586-605
			return d.RemoveNode(ctx, r.NodeId)
		}),
		dsdef.CmdUpdateEndpointForNode: handler("UpdateEndpointForNode", func(ctx context.Context, r dsdef.UpdateEndpointForNodeRequest) error { // :607-631
			return d.UpdateEndpointForNode(ctx, r.NodeId, r.EndpointId, r.FriendlyName)
		}),
		dsdef.CmdAddGroupIdToEndpointForNode: handler("AddGroupIDToEndpointForNode", func(ctx context.Context, r dsdef.AddGroupIdToEndpointForNodeRequest) error { // :633-657
			return d.doAsync(ctx, func(p *[]pushFn) error { return d.addGroupIDToEndpointLocked(r.NodeId, r.EndpointId, r.GroupId, p) })
		}),
		dsdef.CmdRemoveGroupIdFromEndpointForNode: handler("RemoveGroupIDFromEndpointForNode", func(ctx context.Context, r dsdef.RemoveGroupIdFromEndpointForNodeRequest) error { // :659-683
			return d.doAsync(ctx, func(p *[]pushFn) error {
				return d.removeGroupIDFromEndpointLocked(r.NodeId, r.EndpointId, r.GroupId, p)
			})
		}),
		dsdef.CmdAddBindingToEndpointForNode: handler("AddBindingToEndpointForNode", func(ctx context.Context, r dsdef.AddBindingToEndpointForNodeRequest) error { // :685-709
			return d.doAsync(ctx, func(p *[]pushFn) error { return d.addBindingLocked(r.NodeId, r.EndpointId, r.Binding, p) })
		}),
		dsdef.CmdRemoveBindingFromEndpointForNode: handler("RemoveBindingFromEndpointForNode", func(ctx context.Context, r dsdef.RemoveBindingFromEndpointForNodeRequest) error { // :711-735
			return d.doAsync(ctx, func(p *[]pushFn) error { return d.removeBindingLocked(r.ListId, r.NodeId, r.EndpointId, p) })
		}),
		dsdef.CmdAddAclToNode: handler("AddACLToNode", func(ctx context.Context, r dsdef.AddAclToNodeRequest) error { // :737-760
			return d.doAsync(ctx, func(p *[]pushFn) error { return d.addACLLocked(r.NodeId, r.AclEntry, p) })
		}),
		dsdef.CmdRemoveAclFromNode: handler("RemoveACLFromNode", func(ctx context.Context, r dsdef.RemoveAclFromNodeRequest) error { // :762-785
			return d.doAsync(ctx, func(p *[]pushFn) error { return d.removeACLLocked(r.ListId, r.NodeId, p) })
		}),
	}
}

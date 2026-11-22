// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"context"

	"thingfabric.dev/go-fabric/cluster/spec"
	dsdef "thingfabric.dev/go-fabric/cluster/spec/jointfabricdatastore"
	"thingfabric.dev/go-fabric/im"
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

// anchorOnly answers a request of a fabric other than the recorded anchor
// fabric, or of any fabric while none is recorded, UNSUPPORTED_ACCESS
// before the command runs (owner decision 129, R2-P6-sec-1103; ahead of
// connectedhomeip, whose joint-fabric-datastore-server.cpp asks no
// accessing fabric). A call with no request behind it — the host's — is
// not fenced.
func (d *Datastore) anchorOnly(h spec.CommandHandler) spec.CommandHandler {
	return func(ctx context.Context, fields any) (any, error) {
		if _, fabric, request := im.LookupFabricFilter(ctx); request && !d.admits(ctx, fabric) {
			return nil, spec.Errorf(im.StatusUnsupportedAccess, "JointFabricDatastore: fabric %d is not the anchor fabric", fabric)
		}
		return h(ctx, fields)
	}
}

func (d *Datastore) commands() map[uint32]spec.CommandHandler {
	cmds := d.commandTable()
	for id, h := range cmds {
		cmds[id] = d.anchorOnly(h)
	}
	return cmds
}

func (d *Datastore) commandTable() map[uint32]spec.CommandHandler {
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
			return d.AddAdmin(ctx, AdminEntry(r))
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
			rctx, err := d.beginRefresh(ctx, r.NodeId)
			if err != nil {
				return err
			}
			bg := context.WithoutCancel(rctx)
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

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"context"
	"errors"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/im"
)

// The datastore's operations, connectedhomeip's JointFabricDatastore.cpp
// at 6170af84. Each is a change under the lock that collects its pushes;
// the host method runs them before it returns, the command after it
// answered (see [Datastore]).

const (
	statePending       = dsdef.DatastoreStatePending
	stateCommitted     = dsdef.DatastoreStateCommitted
	stateDeletePending = dsdef.DatastoreStateDeletePending
	stateCommitFailed  = dsdef.DatastoreStateCommitFailed
)

// nodeIndexLocked is IsNodeIDInDatastore (:1140-1152).
func (d *Datastore) nodeIndexLocked(nodeID uint64) int {
	return slices.IndexFunc(d.t.Nodes, func(e NodeEntry) bool { return e.NodeId == nodeID })
}

// groupIndexLocked is IsGroupIDInDatastore (:1609-1621).
func (d *Datastore) groupIndexLocked(groupID uint16) int {
	return slices.IndexFunc(d.t.Groups, func(e GroupEntry) bool { return e.GroupId == uint64(groupID) })
}

// hasEndpointLocked is IsNodeIdAndEndpointInEndpointInformationEntries
// (:1651-1664): ConstraintError for an endpoint the datastore does not
// hold.
func (d *Datastore) hasEndpointLocked(nodeID uint64, endpointID uint16) error {
	if slices.ContainsFunc(d.t.Endpoints, func(e EndpointEntry) bool { return e.NodeId == nodeID && e.EndpointId == endpointID }) {
		return nil
	}
	return constraintError("endpoint %d of node 0x%016X is not in the datastore", endpointID, nodeID)
}

// setNodeLocked is SetNode (:1132-1138).
func (d *Datastore) setNodeLocked(nodeID uint64, state State) error {
	i := d.nodeIndexLocked(nodeID)
	if i < 0 {
		return ErrNotFound
	}
	d.t.Nodes[i].CommissioningStatusEntry.State = state
	d.markLocked(dsdef.AttrNodeList)
	return nil
}

// AddPendingNode adds a node, Pending: a full NodeList is refused, a node
// already present is a ConstraintError. Mirrors :181-199.
func (d *Datastore) AddPendingNode(ctx context.Context, nodeID uint64, name string) error {
	return d.update(ctx, func() error { return d.addPendingNodeLocked(nodeID, name) })
}

func (d *Datastore) addPendingNodeLocked(nodeID uint64, name string) error {
	if len(d.t.Nodes) >= MaxNodes { // :183
		return ErrNoMemory
	}
	if d.nodeIndexLocked(nodeID) >= 0 { // :185-188
		return constraintError("node 0x%016X is already in the datastore", nodeID)
	}
	d.t.Nodes = append(d.t.Nodes, NodeEntry{NodeId: nodeID, FriendlyName: friendlyName(name), CommissioningStatusEntry: StatusEntry{State: statePending}})
	d.markLocked(dsdef.AttrNodeList)
	return nil
}

// UpdateNode renames a node; a node not present is a ConstraintError.
// Mirrors :201-219.
func (d *Datastore) UpdateNode(ctx context.Context, nodeID uint64, name string) error {
	return d.update(ctx, func() error {
		i := d.nodeIndexLocked(nodeID)
		if i < 0 {
			return constraintError("node 0x%016X is not in the datastore", nodeID)
		}
		d.t.Nodes[i].FriendlyName = friendlyName(name)
		d.markLocked(dsdef.AttrNodeList)
		return nil
	})
}

// RemoveNode removes a node; a node not present is a ConstraintError. Its
// endpoints, groups, bindings and ACLs stay, as connectedhomeip keeps them.
// Mirrors :221-239.
func (d *Datastore) RemoveNode(ctx context.Context, nodeID uint64) error {
	return d.update(ctx, func() error {
		i := d.nodeIndexLocked(nodeID)
		if i < 0 {
			return constraintError("node 0x%016X is not in the datastore", nodeID)
		}
		d.t.Nodes = slices.Delete(d.t.Nodes, i, i+1)
		d.markLocked(dsdef.AttrNodeList)
		return nil
	})
}

// copyKeySet is CopyGroupKeySetWithOwnedSpans (:38-61): an epoch key over
// 16 bytes is a ConstraintError before anything changes; a null key stays
// null, a null start time leaves the destination's. The multicast policy
// is not copied, as connectedhomeip copies it not.
func copyKeySet(src GroupKeySet, dst *GroupKeySet) error {
	for _, k := range []spec.Nullable[[]byte]{src.EpochKey0, src.EpochKey1, src.EpochKey2} { // :43-45
		if !k.Null && len(k.Value) > epochKeySize {
			return constraintError("group key set %d: epoch key over %d bytes", src.GroupKeySetId, epochKeySize)
		}
	}
	dst.GroupKeySetId, dst.GroupKeySecurityPolicy = src.GroupKeySetId, src.GroupKeySecurityPolicy
	copyKey := func(s spec.Nullable[[]byte], d *spec.Nullable[[]byte]) { // :119-134
		if s.Null {
			*d = spec.Nullable[[]byte]{Null: true}
			return
		}
		*d = spec.Nullable[[]byte]{Value: slices.Clone(s.Value)}
	}
	copyKey(src.EpochKey0, &dst.EpochKey0)
	copyKey(src.EpochKey1, &dst.EpochKey1)
	copyKey(src.EpochKey2, &dst.EpochKey2)
	copyTime := func(s spec.Nullable[uint64], d *spec.Nullable[uint64]) { // CopyNullableValue, JointFabricDatastore.h
		if !s.Null {
			*d = s
		}
	}
	copyTime(src.EpochStartTime0, &dst.EpochStartTime0)
	copyTime(src.EpochStartTime1, &dst.EpochStartTime1)
	copyTime(src.EpochStartTime2, &dst.EpochStartTime2)
	return nil
}

// newKeySet is a key set whose start times start null (a default
// DataModel::Nullable).
func newKeySet() GroupKeySet {
	null := spec.Nullable[uint64]{Null: true}
	return GroupKeySet{EpochStartTime0: null, EpochStartTime1: null, EpochStartTime2: null}
}

func (d *Datastore) keySetIndexLocked(id uint16) int {
	return slices.IndexFunc(d.t.GroupKeySets, func(k GroupKeySet) bool { return k.GroupKeySetId == id })
}

// IsGroupKeySetEntryPresent reports whether the key set is held (:1169-1180).
func (d *Datastore) IsGroupKeySetEntryPresent(id uint16) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.keySetIndexLocked(id) >= 0
}

// AddGroupKeySetEntry adds a key set: a duplicate is a ConstraintError, a
// full list is refused. Mirrors :1154-1167.
func (d *Datastore) AddGroupKeySetEntry(ctx context.Context, ks GroupKeySet) error {
	return d.update(ctx, func() error { return d.addKeySetLocked(ks) })
}

func (d *Datastore) addKeySetLocked(ks GroupKeySet) error {
	if d.keySetIndexLocked(ks.GroupKeySetId) >= 0 { // :1158
		return constraintError("group key set %d is already in the datastore", ks.GroupKeySetId)
	}
	if len(d.t.GroupKeySets) >= MaxGroupKeySet { // :1159
		return ErrNoMemory
	}
	stored := newKeySet()
	if err := copyKeySet(ks, &stored); err != nil {
		return err
	}
	d.t.GroupKeySets = append(d.t.GroupKeySets, stored)
	d.markLocked(dsdef.AttrGroupKeySetList)
	return nil
}

// RemoveGroupKeySetEntry removes a key set: id 0 is a ConstraintError, one
// not held NotFound. Mirrors :1182-1197.
func (d *Datastore) RemoveGroupKeySetEntry(ctx context.Context, id uint16) error {
	return d.update(ctx, func() error { return d.removeKeySetLocked(id) })
}

func (d *Datastore) removeKeySetLocked(id uint16) error {
	if id == 0 { // :1184
		return constraintError("group key set 0 is not removable")
	}
	i := d.keySetIndexLocked(id)
	if i < 0 {
		return notFoundStatus("group key set %d is not in the datastore", id) // :1196
	}
	d.t.GroupKeySets = slices.Delete(d.t.GroupKeySets, i, i+1)
	d.markLocked(dsdef.AttrGroupKeySetList)
	return nil
}

// UpdateGroupKeySetEntry replaces a held key set: each node's key set entry
// of it is pushed the new key set first (an invalid security policy marks
// the entry CommitFailed); an invalid policy is then a ConstraintError,
// a key set not held ErrNotFound. Mirrors :1199-1220 with
// UpdateNodeKeySetList :1295-1338.
func (d *Datastore) UpdateGroupKeySetEntry(ctx context.Context, ks GroupKeySet) error {
	return d.do(ctx, func(p *[]pushFn) error { return d.updateKeySetLocked(ks, p) })
}

func validSecurityPolicy(p dsdef.DatastoreGroupKeySecurityPolicyEnum) bool {
	return p == dsdef.DatastoreGroupKeySecurityPolicyTrustFirst
}

func (d *Datastore) updateKeySetLocked(ks GroupKeySet, p *[]pushFn) error {
	i := d.keySetIndexLocked(ks.GroupKeySetId)
	if i < 0 {
		return ErrNotFound // :1219
	}
	d.updateNodeKeySetListLocked(ks, p)                  // :1207, its error only logged
	if !validSecurityPolicy(ks.GroupKeySecurityPolicy) { // :1209-1211
		return constraintError("group key set %d: security policy %d", ks.GroupKeySetId, ks.GroupKeySecurityPolicy)
	}
	if err := copyKeySet(ks, &d.t.GroupKeySets[i]); err != nil { // :1213
		return err
	}
	d.markLocked(dsdef.AttrGroupKeySetList)
	return nil
}

// updateNodeKeySetListLocked is UpdateNodeKeySetList (:1295-1338).
func (d *Datastore) updateNodeKeySetListLocked(ks GroupKeySet, p *[]pushFn) {
	if d.sync == nil { // :1298
		return
	}
	for i := range d.t.NodeKeySets {
		e := &d.t.NodeKeySets[i]
		if e.GroupKeySetId != ks.GroupKeySetId {
			continue
		}
		if !validSecurityPolicy(ks.GroupKeySecurityPolicy) { // :1329-1333
			e.StatusEntry.State = stateCommitFailed
			d.markLocked(dsdef.AttrNodeKeySetList)
			return
		}
		*p = append(*p, d.pushKeySet(e.NodeId, cloneKeySet(ks)))
	}
}

// pushKeySet pushes a key set to a node and commits the node's key set
// entry of it (:1311-1322, :841-853).
func (d *Datastore) pushKeySet(nodeID uint64, ks GroupKeySet) pushFn {
	return func(ctx context.Context) error {
		if err := d.sync.SyncGroupKeySet(ctx, nodeID, ks); err != nil {
			return err
		}
		d.commit(ctx, func() {
			if markCommitted(d.t.NodeKeySets, nodeKeySetStatus, func(e NodeKeySetEntry) bool {
				return e.NodeId == nodeID && e.GroupKeySetId == ks.GroupKeySetId
			}) {
				d.markLocked(dsdef.AttrNodeKeySetList)
			}
		})
		return nil
	}
}

func nodeKeySetStatus(e *NodeKeySetEntry) *StatusEntry         { return &e.StatusEntry }
func endpointGroupStatus(e *EndpointGroupIDEntry) *StatusEntry { return &e.StatusEntry }
func bindingStatus(e *EndpointBindingEntry) *StatusEntry       { return &e.StatusEntry }
func aclStatus(e *aclEntry) *StatusEntry                       { return &e.StatusEntry }
func sameEndpointGroup(a, b EndpointGroupIDEntry) bool {
	return a.NodeId == b.NodeId && a.EndpointId == b.EndpointId && a.GroupId == b.GroupId
}
func sameNodeKeySet(nodeID uint64, id uint16) func(NodeKeySetEntry) bool {
	return func(e NodeKeySetEntry) bool { return e.NodeId == nodeID && e.GroupKeySetId == id }
}

// AddAdmin adds an administrator: a duplicate is a ConstraintError, a full
// list is refused. Mirrors :1222-1238.
func (d *Datastore) AddAdmin(ctx context.Context, a AdminEntry) error {
	return d.update(ctx, func() error {
		if slices.ContainsFunc(d.t.Admins, func(e AdminEntry) bool { return e.NodeId == a.NodeId }) { // :1226
			return constraintError("administrator 0x%016X is already in the datastore", a.NodeId)
		}
		if len(d.t.Admins) >= MaxAdminNodes { // :1227
			return ErrNoMemory
		}
		a.Icac = slices.Clone(a.Icac)
		d.t.Admins = append(d.t.Admins, a)
		d.markLocked(dsdef.AttrAdminList)
		return nil
	})
}

// UpdateAdmin changes an administrator's friendly name and ICAC, each when
// given; one not held is ErrNotFound. Mirrors :1253-1278.
func (d *Datastore) UpdateAdmin(ctx context.Context, nodeID uint64, name *string, icac *[]byte) error {
	return d.update(ctx, func() error {
		i := slices.IndexFunc(d.t.Admins, func(e AdminEntry) bool { return e.NodeId == nodeID })
		if i < 0 {
			return ErrNotFound
		}
		if name != nil {
			d.t.Admins[i].FriendlyName = *name
		}
		if icac != nil {
			d.t.Admins[i].Icac = slices.Clone(*icac)
		}
		d.markLocked(dsdef.AttrAdminList)
		return nil
	})
}

// RemoveAdmin removes an administrator; one not held is ErrNotFound.
// Mirrors :1280-1293.
func (d *Datastore) RemoveAdmin(ctx context.Context, nodeID uint64) error {
	return d.update(ctx, func() error {
		i := slices.IndexFunc(d.t.Admins, func(e AdminEntry) bool { return e.NodeId == nodeID })
		if i < 0 {
			return ErrNotFound
		}
		d.t.Admins = slices.Delete(d.t.Admins, i, i+1)
		d.markLocked(dsdef.AttrAdminList)
		return nil
	})
}

// validPrivilege is a privilege of the enum — connectedhomeip's decoder
// makes any other kUnknownEnumValue (:1510-1512).
func validPrivilege(p dsdef.DatastoreAccessControlEntryPrivilegeEnum) bool {
	for _, v := range dsdef.DatastoreAccessControlEntryPrivilegeEnumDef.Values {
		if v.Value == uint64(p) {
			return true
		}
	}
	return false
}

func reservedCAT(cat spec.Nullable[uint16]) bool {
	v := uint16(0)
	if !cat.Null {
		v = cat.Value
	}
	return v == AdminCATIdentifier || v == AnchorCATIdentifier
}

// AddGroup adds a group: a group id already held, or the administrator or
// anchor CAT, is a ConstraintError. Mirrors :1360-1385.
func (d *Datastore) AddGroup(ctx context.Context, req AddGroupRequest) error {
	return d.update(ctx, func() error { return d.addGroupLocked(req, true) })
}

// ForceAddGroup adds a group of any CAT — the administrator's own groups
// of the administrator and anchor CATs, which AddGroup refuses. Mirrors
// ForceAddGroup (:1387-1407), which the administrator application calls
// (examples/jf-admin-app/linux/JFAManager.cpp:185-197).
func (d *Datastore) ForceAddGroup(ctx context.Context, req AddGroupRequest) error {
	return d.update(ctx, func() error { return d.addGroupLocked(req, false) })
}

func (d *Datastore) addGroupLocked(req AddGroupRequest, checkCAT bool) error {
	if d.groupIndexLocked(req.GroupId) >= 0 { // :1364-1365, :1392-1393
		return constraintError("group %d is already in the datastore", req.GroupId)
	}
	if checkCAT && reservedCAT(req.GroupCat) { // :1367-1371
		return constraintError("group %d: the administrator and anchor CATs are reserved", req.GroupId)
	}
	d.t.Groups = append(d.t.Groups, GroupEntry{
		GroupId: uint64(req.GroupId), FriendlyName: req.FriendlyName, GroupKeySetId: req.GroupKeySetId,
		GroupCat: req.GroupCat, GroupCatVersion: req.GroupCatVersion, GroupPermission: req.GroupPermission,
	})
	d.markLocked(dsdef.AttrGroupList)
	return nil
}

// UpdateGroup changes a group's fields that are not null: a new friendly
// name marks every endpoint's membership of the group Pending and pushes
// it; a new key set adds the members' key set entries (and removes the
// old one's); a changed CAT, CAT version or permission marks every ACL
// entry whose subjects name the group Pending with the group's permission
// and pushes it. A group not held, or one of the administrator or anchor
// CAT, is a ConstraintError. Mirrors :1409-1583.
func (d *Datastore) UpdateGroup(ctx context.Context, req UpdateGroupRequest) error {
	return d.do(ctx, func(p *[]pushFn) error { return d.updateGroupLocked(req, p) })
}

func (d *Datastore) updateGroupLocked(req UpdateGroupRequest, p *[]pushFn) error {
	if d.sync == nil { // :1412
		return ErrNoSync
	}
	gi := d.groupIndexLocked(req.GroupId)
	if gi < 0 { // :1416
		return constraintError("group %d is not in the datastore", req.GroupId)
	}
	g := &d.t.Groups[gi]
	if reservedCAT(g.GroupCat) { // :1418-1423
		return constraintError("group %d: the administrator and anchor CATs are reserved", req.GroupId)
	}
	d.markLocked(dsdef.AttrGroupList)
	if !req.FriendlyName.Null && g.FriendlyName != req.FriendlyName.Value { // :1426-1471
		for i := range d.t.EndpointGroupIDs {
			e := &d.t.EndpointGroupIDs[i]
			if uint64(e.GroupId) != uint64(req.GroupId) {
				continue
			}
			e.StatusEntry.State = statePending
			d.markLocked(dsdef.AttrEndpointGroupIdList)
			*p = append(*p, d.pushEndpointGroup(e.NodeId, *e))
		}
		g.FriendlyName = req.FriendlyName.Value
	}
	if !req.GroupKeySetId.Null && (g.GroupKeySetId.Null || g.GroupKeySetId.Value != req.GroupKeySetId.Value) { // :1473-1487
		if err := d.addNodeKeySetEntryLocked(req.GroupId, req.GroupKeySetId.Value, p); err != nil {
			return err
		}
		if !g.GroupKeySetId.Null {
			_ = d.removeNodeKeySetEntryLocked(req.GroupId, g.GroupKeySetId.Value, p) // logged only
		}
		g.GroupKeySetId = req.GroupKeySetId
	}
	catChanged := false // :1489-1520
	if !req.GroupCat.Null {
		catChanged = catChanged || g.GroupCat.Null || g.GroupCat.Value != req.GroupCat.Value
		g.GroupCat = req.GroupCat
	}
	if !req.GroupCatVersion.Null {
		catChanged = catChanged || g.GroupCatVersion.Null || g.GroupCatVersion.Value != req.GroupCatVersion.Value
		g.GroupCatVersion = req.GroupCatVersion
	}
	if !req.GroupPermission.Null && validPrivilege(req.GroupPermission.Value) {
		catChanged = catChanged || g.GroupPermission != req.GroupPermission.Value
		g.GroupPermission = req.GroupPermission.Value
	}
	if !catChanged {
		return nil
	}
	for i := range d.t.ACLs { // :1522-1580
		a := &d.t.ACLs[i]
		if !slices.Contains(a.Subjects, uint64(req.GroupId)) {
			continue
		}
		a.Privilege = g.GroupPermission
		a.StatusEntry.State = statePending
		d.markLocked(dsdef.AttrNodeAclList)
		*p = append(*p, d.pushACL(a.NodeID, a.encode()))
	}
	return nil
}

// pushEndpointGroup pushes a membership and commits it (:1447-1457).
func (d *Datastore) pushEndpointGroup(nodeID uint64, e EndpointGroupIDEntry) pushFn {
	return func(ctx context.Context) error {
		if err := d.sync.SyncEndpointGroupID(ctx, nodeID, e); err != nil {
			return err
		}
		d.commit(ctx, func() {
			if markCommitted(d.t.EndpointGroupIDs, endpointGroupStatus, func(x EndpointGroupIDEntry) bool { return sameEndpointGroup(x, e) }) {
				d.markLocked(dsdef.AttrEndpointGroupIdList)
			}
		})
		return nil
	}
}

// pushACL pushes an ACL entry and commits it (:1568-1578, :2169-2185).
func (d *Datastore) pushACL(nodeID uint64, e ACLEntry) pushFn {
	return func(ctx context.Context) error {
		if err := d.sync.SyncACL(ctx, nodeID, e); err != nil {
			return err
		}
		d.commit(ctx, func() {
			if markCommitted(d.t.ACLs, aclStatus, func(x aclEntry) bool { return x.NodeID == e.NodeId && x.ListID == e.ListId }) {
				d.markLocked(dsdef.AttrNodeAclList)
			}
		})
		return nil
	}
}

// RemoveGroup removes a group: one not held, or of the administrator or
// anchor CAT, is a ConstraintError. Mirrors :1585-1607.
func (d *Datastore) RemoveGroup(ctx context.Context, groupID uint16) error {
	return d.update(ctx, func() error {
		i := d.groupIndexLocked(groupID)
		if i < 0 {
			return constraintError("group %d is not in the datastore", groupID)
		}
		if reservedCAT(d.t.Groups[i].GroupCat) { // :1596-1600
			return constraintError("group %d: the administrator and anchor CATs are reserved", groupID)
		}
		d.t.Groups = slices.Delete(d.t.Groups, i, i+1)
		d.markLocked(dsdef.AttrGroupList)
		return nil
	})
}

// UpdateEndpointForNode renames a node's endpoint; one not held is a
// ConstraintError. Mirrors :1637-1649.
func (d *Datastore) UpdateEndpointForNode(ctx context.Context, nodeID uint64, endpointID uint16, name string) error {
	return d.update(ctx, func() error {
		i := slices.IndexFunc(d.t.Endpoints, func(e EndpointEntry) bool { return e.NodeId == nodeID && e.EndpointId == endpointID })
		if i < 0 {
			return constraintError("endpoint %d of node 0x%016X is not in the datastore", endpointID, nodeID)
		}
		d.t.Endpoints[i].FriendlyName = name
		d.markLocked(dsdef.AttrNodeEndpointList)
		return nil
	})
}

// AddGroupIDToEndpointForNode makes a node's endpoint a member of a group,
// Pending: an endpoint or group not held is a ConstraintError, a
// membership already held succeeds unchanged, a full list is refused.
// When the node lacks the group's key set, a Pending key set entry is
// added and pushed first; the membership is pushed and committed after.
// Mirrors :1666-1757.
func (d *Datastore) AddGroupIDToEndpointForNode(ctx context.Context, nodeID uint64, endpointID, groupID uint16) error {
	return d.do(ctx, func(p *[]pushFn) error { return d.addGroupIDToEndpointLocked(nodeID, endpointID, groupID, p) })
}

func (d *Datastore) addGroupIDToEndpointLocked(nodeID uint64, endpointID, groupID uint16, p *[]pushFn) error {
	if d.sync == nil { // :1668
		return ErrNoSync
	}
	if err := d.hasEndpointLocked(nodeID, endpointID); err != nil { // :1671
		return err
	}
	gi := d.groupIndexLocked(groupID)
	if gi < 0 { // :1673
		return constraintError("group %d is not in the datastore", groupID)
	}
	entry := EndpointGroupIDEntry{NodeId: nodeID, EndpointId: endpointID, GroupId: groupID, StatusEntry: StatusEntry{State: statePending}}
	if slices.ContainsFunc(d.t.EndpointGroupIDs, func(e EndpointGroupIDEntry) bool { return sameEndpointGroup(e, entry) }) { // :1676-1682
		return nil
	}
	if len(d.t.EndpointGroupIDs) >= MaxGroups { // :1684
		return ErrNoMemory
	}
	d.t.EndpointGroupIDs = append(d.t.EndpointGroupIDs, entry)
	d.markLocked(dsdef.AttrEndpointGroupIdList)
	if g := d.t.Groups[gi]; !g.GroupKeySetId.Null { // :1697-1746
		ks := g.GroupKeySetId.Value
		if !slices.ContainsFunc(d.t.NodeKeySets, sameNodeKeySet(nodeID, ks)) {
			nks := NodeKeySetEntry{NodeId: nodeID, GroupKeySetId: ks, StatusEntry: StatusEntry{State: statePending}}
			d.t.NodeKeySets = append(d.t.NodeKeySets, nks)
			d.markLocked(dsdef.AttrNodeKeySetList)
			groupPush := d.pushEndpointGroup(nodeID, entry)
			*p = append(*p, func(ctx context.Context) error {
				// The key set entry stays Pending: the refresh finalises it
				// (:1726-1727); the membership is pushed once it is there.
				if err := d.sync.SyncNodeKeySet(ctx, nodeID, nks); err != nil {
					return err
				}
				return groupPush(ctx)
			})
			return nil
		}
	}
	*p = append(*p, d.pushEndpointGroup(nodeID, entry)) // :1748-1756
	return nil
}

// RemoveGroupIDFromEndpointForNode ends a node's endpoint's membership: it
// goes DeletePending and is pushed, then erased; the node's key set entry
// of the group's key set goes the same way. An endpoint not held is a
// ConstraintError, a membership not held NotFound. Mirrors :1759-1827.
func (d *Datastore) RemoveGroupIDFromEndpointForNode(ctx context.Context, nodeID uint64, endpointID, groupID uint16) error {
	return d.do(ctx, func(p *[]pushFn) error { return d.removeGroupIDFromEndpointLocked(nodeID, endpointID, groupID, p) })
}

func (d *Datastore) removeGroupIDFromEndpointLocked(nodeID uint64, endpointID, groupID uint16, p *[]pushFn) error {
	if d.sync == nil { // :1761
		return ErrNoSync
	}
	if err := d.hasEndpointLocked(nodeID, endpointID); err != nil { // :1764
		return err
	}
	key := EndpointGroupIDEntry{NodeId: nodeID, EndpointId: endpointID, GroupId: groupID}
	i := slices.IndexFunc(d.t.EndpointGroupIDs, func(e EndpointGroupIDEntry) bool { return sameEndpointGroup(e, key) })
	if i < 0 {
		return notFoundStatus("group %d is not on endpoint %d of node 0x%016X", groupID, endpointID, nodeID) // :1826
	}
	d.t.EndpointGroupIDs[i].StatusEntry.State = stateDeletePending // :1770
	d.markLocked(dsdef.AttrEndpointGroupIdList)
	e := d.t.EndpointGroupIDs[i]
	*p = append(*p, func(ctx context.Context) error { // :1774-1789
		if err := d.sync.SyncEndpointGroupID(ctx, nodeID, e); err != nil {
			return err
		}
		d.commit(ctx, func() {
			if j := slices.IndexFunc(d.t.EndpointGroupIDs, func(x EndpointGroupIDEntry) bool { return sameEndpointGroup(x, e) }); j >= 0 {
				d.t.EndpointGroupIDs = slices.Delete(d.t.EndpointGroupIDs, j, j+1)
				d.markLocked(dsdef.AttrEndpointGroupIdList)
			}
		})
		return nil
	})
	if gi := d.groupIndexLocked(groupID); gi >= 0 && !d.t.Groups[gi].GroupKeySetId.Null { // :1791-1820
		ks := d.t.Groups[gi].GroupKeySetId.Value
		if j := slices.IndexFunc(d.t.NodeKeySets, sameNodeKeySet(nodeID, ks)); j >= 0 {
			d.t.NodeKeySets[j].StatusEntry.State = stateDeletePending
			d.markLocked(dsdef.AttrNodeKeySetList)
			nks := d.t.NodeKeySets[j]
			*p = append(*p, d.pushNodeKeySetRemoval(nodeID, nks, nks.NodeId, nks.GroupKeySetId))
		}
	}
	return nil
}

// pushNodeKeySetRemoval pushes the removal of a node's key set entry and
// erases it (:1801-1815, :2307-2319).
func (d *Datastore) pushNodeKeySetRemoval(nodeID uint64, sent NodeKeySetEntry, eraseNode uint64, eraseID uint16) pushFn {
	return func(ctx context.Context) error {
		if err := d.sync.SyncNodeKeySet(ctx, nodeID, sent); err != nil {
			return err
		}
		d.commit(ctx, func() {
			n := len(d.t.NodeKeySets)
			d.t.NodeKeySets = slices.DeleteFunc(d.t.NodeKeySets, sameNodeKeySet(eraseNode, eraseID))
			if len(d.t.NodeKeySets) != n {
				d.markLocked(dsdef.AttrNodeKeySetList)
			}
		})
		return nil
	}
}

// nextListIDLocked is GenerateAndAssignAUniqueListID (:1829-1851): one
// above the highest list id of the bindings and the ACL entries.
func (d *Datastore) nextListIDLocked() uint16 {
	var next uint16
	for _, e := range d.t.Bindings {
		if e.ListId >= next {
			next = e.ListId + 1
		}
	}
	for _, e := range d.t.ACLs {
		if e.ListID >= next {
			next = e.ListID + 1
		}
	}
	return next
}

func optEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// bindingMatches is BindingMatches (:1853-1906): node, group, endpoint and
// cluster each absent on both or present and equal.
func bindingMatches(a, b BindingTarget) bool {
	return optEqual(a.Node, b.Node) && optEqual(a.Group, b.Group) && optEqual(a.Endpoint, b.Endpoint) && optEqual(a.Cluster, b.Cluster)
}

func cloneBinding(b BindingTarget) BindingTarget {
	clonePtr := func(p *uint64) *uint64 {
		if p == nil {
			return nil
		}
		v := *p
		return &v
	}
	clone16 := func(p *uint16) *uint16 {
		if p == nil {
			return nil
		}
		v := *p
		return &v
	}
	var c *uint32
	if b.Cluster != nil {
		v := *b.Cluster
		c = &v
	}
	return BindingTarget{Node: clonePtr(b.Node), Group: clone16(b.Group), Endpoint: clone16(b.Endpoint), Cluster: c}
}

// AddBindingToEndpointForNode adds a binding to a node's endpoint, Pending
// with a new list id, pushes it and commits it: an endpoint not held is a
// ConstraintError, a matching binding succeeds unchanged, a full list
// (MaxGroups, as connectedhomeip bounds it) is refused. Mirrors
// :1908-1953.
func (d *Datastore) AddBindingToEndpointForNode(ctx context.Context, nodeID uint64, endpointID uint16, binding BindingTarget) error {
	return d.do(ctx, func(p *[]pushFn) error { return d.addBindingLocked(nodeID, endpointID, binding, p) })
}

func (d *Datastore) addBindingLocked(nodeID uint64, endpointID uint16, binding BindingTarget, p *[]pushFn) error {
	if d.sync == nil { // :1913
		return ErrNoSync
	}
	if err := d.hasEndpointLocked(nodeID, endpointID); err != nil { // :1916
		return err
	}
	if slices.ContainsFunc(d.t.Bindings, func(e EndpointBindingEntry) bool { // :1919-1928
		return e.NodeId == nodeID && e.EndpointId == endpointID && bindingMatches(e.Binding, binding)
	}) {
		return nil
	}
	if len(d.t.Bindings) >= MaxGroups { // :1930
		return ErrNoMemory
	}
	entry := EndpointBindingEntry{NodeId: nodeID, EndpointId: endpointID, Binding: cloneBinding(binding), ListId: d.nextListIDLocked(), StatusEntry: StatusEntry{State: statePending}}
	d.t.Bindings = append(d.t.Bindings, entry)
	d.markLocked(dsdef.AttrEndpointBindingList)
	*p = append(*p, func(ctx context.Context) error { // :1943-1952
		if err := d.sync.SyncBinding(ctx, nodeID, entry); err != nil {
			return err
		}
		d.commit(ctx, func() {
			if markCommitted(d.t.Bindings, bindingStatus, func(e EndpointBindingEntry) bool {
				return e.NodeId == nodeID && e.EndpointId == endpointID && e.ListId == entry.ListId
			}) {
				d.markLocked(dsdef.AttrEndpointBindingList)
			}
		})
		return nil
	})
	return nil
}

// RemoveBindingFromEndpointForNode removes a node's endpoint's binding: it
// goes DeletePending, is pushed, then erased. An endpoint not held is a
// ConstraintError, a binding not held ErrNotFound. Mirrors :1955-1986.
func (d *Datastore) RemoveBindingFromEndpointForNode(ctx context.Context, listID uint16, nodeID uint64, endpointID uint16) error {
	return d.do(ctx, func(p *[]pushFn) error { return d.removeBindingLocked(listID, nodeID, endpointID, p) })
}

func (d *Datastore) removeBindingLocked(listID uint16, nodeID uint64, endpointID uint16, p *[]pushFn) error {
	if d.sync == nil { // :1958
		return ErrNoSync
	}
	if err := d.hasEndpointLocked(nodeID, endpointID); err != nil { // :1961
		return err
	}
	match := func(e EndpointBindingEntry) bool {
		return e.NodeId == nodeID && e.ListId == listID && e.EndpointId == endpointID
	}
	i := slices.IndexFunc(d.t.Bindings, match)
	if i < 0 {
		return ErrNotFound // :1985
	}
	d.t.Bindings[i].StatusEntry.State = stateDeletePending // :1967
	d.markLocked(dsdef.AttrEndpointBindingList)
	e := d.t.Bindings[i]
	*p = append(*p, func(ctx context.Context) error { // :1970-1981
		if err := d.sync.SyncBinding(ctx, nodeID, e); err != nil {
			return err
		}
		d.commit(ctx, func() {
			n := len(d.t.Bindings)
			d.t.Bindings = slices.DeleteFunc(d.t.Bindings, match)
			if len(d.t.Bindings) != n {
				d.markLocked(dsdef.AttrEndpointBindingList)
			}
		})
		return nil
	})
	return nil
}

func nullEqual[T comparable](a, b spec.Nullable[T]) bool {
	if a.Null || b.Null {
		return a.Null == b.Null
	}
	return a.Value == b.Value
}

// aclTargetMatches is ACLTargetMatches (:1988-2029).
func aclTargetMatches(a, b AccessControlTarget) bool {
	return nullEqual(a.Cluster, b.Cluster) && nullEqual(a.Endpoint, b.Endpoint) && nullEqual(a.DeviceType, b.DeviceType)
}

// aclMatches is ACLMatches (:2031-2100): privilege, auth mode, and the
// subjects and targets in order, a null list as an empty one.
func aclMatches(a aclEntry, b AccessControlEntry) bool {
	if a.Privilege != b.Privilege || a.AuthMode != b.AuthMode {
		return false
	}
	var subjects []uint64
	if !b.Subjects.Null {
		subjects = b.Subjects.Value
	}
	if !slices.Equal(a.Subjects, subjects) {
		return false
	}
	var targets []AccessControlTarget
	if !b.Targets.Null {
		targets = b.Targets.Value
	}
	return slices.EqualFunc(a.Targets, targets, aclTargetMatches)
}

// AddACLToNode adds an ACL entry to a node, Pending with a new list id,
// pushes it and commits it: a node not held is a ConstraintError, a
// matching entry succeeds unchanged, a full list is refused. Mirrors
// :2102-2186.
func (d *Datastore) AddACLToNode(ctx context.Context, nodeID uint64, entry AccessControlEntry) error {
	return d.do(ctx, func(p *[]pushFn) error { return d.addACLLocked(nodeID, entry, p) })
}

func (d *Datastore) addACLLocked(nodeID uint64, entry AccessControlEntry, p *[]pushFn) error {
	if d.sync == nil { // :2106
		return ErrNoSync
	}
	if d.nodeIndexLocked(nodeID) < 0 { // :2109, IsNodeIdInNodeInformationEntries :1623-1635
		return constraintError("node 0x%016X is not in the datastore", nodeID)
	}
	if slices.ContainsFunc(d.t.ACLs, func(a aclEntry) bool { return a.NodeID == nodeID && aclMatches(a, entry) }) { // :2112-2120
		return nil
	}
	if len(d.t.ACLs) >= MaxACLs { // :2122
		return ErrNoMemory
	}
	a := aclEntry{NodeID: nodeID, Privilege: entry.Privilege, AuthMode: entry.AuthMode, StatusEntry: StatusEntry{State: statePending}}
	if !entry.Subjects.Null {
		a.Subjects = slices.Clone(entry.Subjects.Value)
	}
	if !entry.Targets.Null {
		a.Targets = slices.Clone(entry.Targets.Value)
	}
	a.ListID = d.nextListIDLocked() // :2151
	d.t.ACLs = append(d.t.ACLs, a)
	d.markLocked(dsdef.AttrNodeAclList)
	*p = append(*p, d.pushACL(nodeID, a.encode())) // :2157-2185
	return nil
}

// RemoveACLFromNode removes a node's ACL entry: it goes DeletePending, its
// ids are pushed with that state, then it is erased. A node not held is a
// ConstraintError, an entry not held ErrNotFound. Mirrors :2188-2223.
func (d *Datastore) RemoveACLFromNode(ctx context.Context, listID uint16, nodeID uint64) error {
	return d.do(ctx, func(p *[]pushFn) error { return d.removeACLLocked(listID, nodeID, p) })
}

func (d *Datastore) removeACLLocked(listID uint16, nodeID uint64, p *[]pushFn) error {
	if d.sync == nil { // :2190
		return ErrNoSync
	}
	if d.nodeIndexLocked(nodeID) < 0 { // :2193
		return constraintError("node 0x%016X is not in the datastore", nodeID)
	}
	match := func(a aclEntry) bool { return a.NodeID == nodeID && a.ListID == listID }
	i := slices.IndexFunc(d.t.ACLs, match)
	if i < 0 {
		return ErrNotFound // :2222
	}
	d.t.ACLs[i].StatusEntry.State = stateDeletePending // :2199
	d.markLocked(dsdef.AttrNodeAclList)
	sent := ACLEntry{NodeId: nodeID, ListId: listID, StatusEntry: StatusEntry{State: stateDeletePending}} // :2203-2206
	*p = append(*p, func(ctx context.Context) error {
		if err := d.sync.SyncACL(ctx, nodeID, sent); err != nil {
			return err
		}
		d.commit(ctx, func() {
			n := len(d.t.ACLs)
			d.t.ACLs = slices.DeleteFunc(d.t.ACLs, match)
			if len(d.t.ACLs) != n {
				d.markLocked(dsdef.AttrNodeAclList)
			}
		})
		return nil
	})
	return nil
}

// groupMembersLocked are the nodes of the group's memberships, ascending
// (connectedhomeip collects them in an unordered_set, :2230-2237).
func (d *Datastore) groupMembersLocked(groupID uint16) []uint64 {
	var nodes []uint64
	for _, e := range d.t.EndpointGroupIDs {
		if e.GroupId == groupID && !slices.Contains(nodes, e.NodeId) {
			nodes = append(nodes, e.NodeId)
		}
	}
	slices.Sort(nodes)
	return nodes
}

// addNodeKeySetEntryLocked gives each member of the group without one a
// Pending key set entry of ks and pushes it. Mirrors AddNodeKeySetEntry
// (:2225-2280).
func (d *Datastore) addNodeKeySetEntryLocked(groupID, ks uint16, p *[]pushFn) error {
	if d.sync == nil { // :2227
		return ErrNoSync
	}
	for _, nodeID := range d.groupMembersLocked(groupID) {
		if slices.ContainsFunc(d.t.NodeKeySets, sameNodeKeySet(nodeID, ks)) {
			continue
		}
		e := NodeKeySetEntry{NodeId: nodeID, GroupKeySetId: ks, StatusEntry: StatusEntry{State: statePending}}
		d.t.NodeKeySets = append(d.t.NodeKeySets, e)
		d.markLocked(dsdef.AttrNodeKeySetList)
		*p = append(*p, d.pushNodeKeySet(nodeID, e))
	}
	return nil
}

// pushNodeKeySet pushes a node's key set entry and commits it (:2268-2275).
func (d *Datastore) pushNodeKeySet(nodeID uint64, e NodeKeySetEntry) pushFn {
	return func(ctx context.Context) error {
		if err := d.sync.SyncNodeKeySet(ctx, nodeID, e); err != nil {
			return err
		}
		d.commit(ctx, func() {
			if markCommitted(d.t.NodeKeySets, nodeKeySetStatus, sameNodeKeySet(nodeID, e.GroupKeySetId)) {
				d.markLocked(dsdef.AttrNodeKeySetList)
			}
		})
		return nil
	}
}

// removeNodeKeySetEntryLocked pushes the removal of the first key set entry
// of ks that a member of the group holds, a zero entry, and erases it once
// pushed; none is ErrNotFound. Mirrors RemoveNodeKeySetEntry (:2282-2327).
func (d *Datastore) removeNodeKeySetEntryLocked(groupID, ks uint16, p *[]pushFn) error {
	members := d.groupMembersLocked(groupID)
	for _, e := range d.t.NodeKeySets {
		if e.GroupKeySetId == ks && slices.Contains(members, e.NodeId) {
			*p = append(*p, d.pushNodeKeySetRemoval(e.NodeId, NodeKeySetEntry{}, e.NodeId, ks))
			return nil
		}
	}
	return ErrNotFound
}

// ForceAddNodeKeySetEntry adds a Committed key set entry for a node — the
// administrator's own (ForceAddNodeKeySetEntry :2364-2373, called by the
// administrator application, JFAManager.cpp:182).
func (d *Datastore) ForceAddNodeKeySetEntry(ctx context.Context, ks uint16, nodeID uint64) error {
	return d.update(ctx, func() error {
		d.t.NodeKeySets = append(d.t.NodeKeySets, NodeKeySetEntry{NodeId: nodeID, GroupKeySetId: ks, StatusEntry: StatusEntry{State: stateCommitted}})
		d.markLocked(dsdef.AttrNodeKeySetList)
		return nil
	})
}

// addEndpointEntry adds a node's endpoint (TestAddEndpointEntry,
// :2352-2362); the refresh adds them otherwise.
func (d *Datastore) addEndpointEntry(ctx context.Context, endpointID uint16, nodeID uint64, name string) {
	_ = d.update(ctx, func() error {
		d.t.Endpoints = append(d.t.Endpoints, EndpointEntry{EndpointId: endpointID, NodeId: nodeID, FriendlyName: name})
		d.markLocked(dsdef.AttrNodeEndpointList)
		return nil
	})
}

// commandError is the status a command answers for an operation's error:
// an IM status as it is, any other FAILURE (ClusterStatusCode(CHIP_ERROR),
// StatusCode.cpp:41-62; joint-fabric-datastore-server.cpp :307-311 and
// the like).
func commandError(err error) error {
	if err == nil {
		return nil
	}
	var sc im.StatusCodeError
	if errors.As(err, &sc) {
		return err
	}
	return spec.Errorf(im.StatusFailure, "JointFabricDatastore: %v", err)
}

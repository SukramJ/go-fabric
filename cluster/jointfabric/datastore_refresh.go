// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"context"
	"fmt"
	"slices"

	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
)

// rootEndpoint is the node's root endpoint (kRootEndpointId).
const rootEndpoint uint16 = 0

// RefreshNode reconciles a node's lists with what the node holds, through
// the [DatastoreSync]: the node goes Pending; its endpoints are fetched
// and taken over; each endpoint's groups and bindings are fetched and
// merged, the root endpoint's too; the pending memberships are pushed,
// the bindings pushed as the node's whole list; the node's key sets are
// fetched and taken over, and every node's pending key set entries pushed;
// its ACL entries are fetched, merged and pushed as the whole list; the
// node goes Committed. A fetch that fails ends the refresh with the node
// Pending. Without a Sync, with a refresh running, or for a node not held,
// it is refused. Mirrors JointFabricDatastore.cpp:241-1130 at 6170af84
// (RefreshNode, ContinueRefresh) — run through here, where
// connectedhomeip re-enters ContinueRefresh from each delegate callback.
func (d *Datastore) RefreshNode(ctx context.Context, nodeID uint64) error {
	if err := d.beginRefresh(ctx, nodeID); err != nil {
		return err
	}
	return d.runRefresh(ctx, nodeID)
}

// beginRefresh is RefreshNode (:241-252) and the Idle state's SetNode
// (:262): a Sync, no refresh running, the node held and now Pending. A
// node not held ends the refresh here; connectedhomeip keeps
// mRefreshingNodeId set on that path (:247-249), which refuses every
// later refresh.
func (d *Datastore) beginRefresh(ctx context.Context, nodeID uint64) error {
	return d.update(ctx, func() error {
		if d.sync == nil { // :243
			return ErrNoSync
		}
		if d.inRefresh { // :244-245
			return ErrRefreshBusy
		}
		if err := d.setNodeLocked(nodeID, statePending); err != nil { // :262
			return err
		}
		d.inRefresh, d.refreshing = true, nodeID
		return nil
	})
}

// endRefresh tears the refresh down (mRefreshingNodeId = kUndefinedNodeId,
// mRefreshState = kIdle).
func (d *Datastore) endRefresh() {
	d.mu.Lock()
	d.inRefresh, d.refreshing = false, 0
	d.mu.Unlock()
}

// runRefresh runs the refresh's states after Idle.
func (d *Datastore) runRefresh(ctx context.Context, nodeID uint64) (err error) {
	defer d.endRefresh()
	endpoints, err := d.sync.FetchEndpointList(ctx, nodeID) // :269-295
	if err != nil {
		return fmt.Errorf("%w: FetchEndpointList: %w", ErrSync, err)
	}
	var refreshing []uint16
	d.commit(ctx, func() { refreshing = d.refreshEndpointsLocked(nodeID, endpoints) })
	for _, ep := range refreshing { // kRefreshingGroups :355-426
		groups, err := d.sync.FetchEndpointGroupList(ctx, nodeID, ep)
		if err != nil {
			continue // :368: a failed fetch leaves the endpoint's memberships
		}
		d.commit(ctx, func() { d.mergeGroupsLocked(nodeID, ep, groups) })
	}
	d.syncGroupsOfNode(ctx, nodeID) // :428-511
	for _, ep := range refreshing { // kRefreshingBindings :522-600
		bindings, err := d.sync.FetchEndpointBindingList(ctx, nodeID, ep)
		if err != nil {
			continue // :536
		}
		d.commit(ctx, func() { d.mergeBindingsLocked(nodeID, ep, bindings) })
	}
	if err := d.syncBindingsOfNode(ctx, nodeID); err != nil { // :602-684
		return err
	}
	ids, err := d.sync.FetchGroupKeySetList(ctx, nodeID) // kFetchingGroupKeySetList :686-712
	if err != nil {
		return fmt.Errorf("%w: FetchGroupKeySetList: %w", ErrSync, err)
	}
	for _, id := range ids { // kFetchingGroupKeySets :714-767
		ks, err := d.sync.FetchGroupKeySet(ctx, nodeID, id)
		if err != nil {
			return fmt.Errorf("%w: FetchGroupKeySet %d: %w", ErrSync, id, err)
		}
		d.commit(ctx, func() { d.takeKeySetLocked(ks) })
	}
	d.syncKeySets(ctx)                            // kRefreshingGroupKeySets :769-916
	acls, err := d.sync.FetchACLList(ctx, nodeID) // :918-999
	if err != nil {
		return fmt.Errorf("%w: FetchACLList: %w", ErrSync, err)
	}
	d.commit(ctx, func() { d.mergeACLsLocked(nodeID, acls) })
	return d.syncACLsOfNode(ctx, nodeID) // kRefreshingACLs :1002-1126
}

// refreshEndpointsLocked takes over the node's endpoints and returns the
// ones to fetch groups and bindings of, the root endpoint added when the
// node did not list it (:298-352).
func (d *Datastore) refreshEndpointsLocked(nodeID uint64, endpoints []EndpointEntry) []uint16 {
	ids := make([]uint16, 0, len(endpoints)+1)
	for _, ep := range endpoints {
		ids = append(ids, ep.EndpointId)
		if !slices.ContainsFunc(d.t.Endpoints, func(e EndpointEntry) bool { return e.NodeId == nodeID && e.EndpointId == ep.EndpointId }) { // :300-314
			d.t.Endpoints = append(d.t.Endpoints, EndpointEntry{EndpointId: ep.EndpointId, NodeId: nodeID})
		}
	}
	d.t.Endpoints = slices.DeleteFunc(d.t.Endpoints, func(e EndpointEntry) bool { // :318-335
		return e.NodeId == nodeID && !slices.Contains(ids, e.EndpointId)
	})
	d.markLocked(dsdef.AttrNodeEndpointList)
	if !slices.Contains(ids, rootEndpoint) { // :337-344
		ids = append(ids, rootEndpoint)
	}
	return ids
}

// mergeGroupsLocked adds the endpoint's fetched groups as Committed
// memberships and drops the Committed or DeletePending memberships of the
// endpoint the node no longer has (:368-412).
func (d *Datastore) mergeGroupsLocked(nodeID uint64, ep uint16, groups []GroupEntry) {
	for _, g := range groups {
		e := EndpointGroupIDEntry{NodeId: nodeID, EndpointId: ep, GroupId: uint16(g.GroupId)} //nolint:gosec // a group id (:387)
		if !slices.ContainsFunc(d.t.EndpointGroupIDs, func(x EndpointGroupIDEntry) bool { return sameEndpointGroup(x, e) }) {
			e.StatusEntry.State = stateCommitted
			d.t.EndpointGroupIDs = append(d.t.EndpointGroupIDs, e)
		}
	}
	d.t.EndpointGroupIDs = slices.DeleteFunc(d.t.EndpointGroupIDs, func(e EndpointGroupIDEntry) bool {
		if e.NodeId != nodeID || e.EndpointId != ep {
			return false
		}
		if e.StatusEntry.State != stateCommitted && e.StatusEntry.State != stateDeletePending {
			return false
		}
		return !slices.ContainsFunc(groups, func(g GroupEntry) bool { return uint64(e.GroupId) == g.GroupId })
	})
	d.markLocked(dsdef.AttrEndpointGroupIdList)
}

// syncGroupsOfNode pushes the node's Pending memberships, committing each
// pushed, and the removal of its DeletePending ones as a zero entry,
// erasing each pushed; a CommitFailed one stays (:428-511).
func (d *Datastore) syncGroupsOfNode(ctx context.Context, nodeID uint64) {
	d.mu.Lock()
	var entries []EndpointGroupIDEntry
	for _, e := range d.t.EndpointGroupIDs {
		if e.NodeId == nodeID && (e.StatusEntry.State == statePending || e.StatusEntry.State == stateDeletePending) {
			entries = append(entries, e)
		}
	}
	d.mu.Unlock()
	for _, e := range entries {
		if e.StatusEntry.State == statePending { // :433-458
			_ = d.pushEndpointGroup(nodeID, e)(ctx)
			continue
		}
		if err := d.sync.SyncEndpointGroupID(ctx, nodeID, EndpointGroupIDEntry{}); err != nil { // :460-491
			continue
		}
		d.commit(ctx, func() {
			n := len(d.t.EndpointGroupIDs)
			d.t.EndpointGroupIDs = slices.DeleteFunc(d.t.EndpointGroupIDs, func(x EndpointGroupIDEntry) bool { return sameEndpointGroup(x, e) })
			if len(d.t.EndpointGroupIDs) != n {
				d.markLocked(dsdef.AttrEndpointGroupIdList)
			}
		})
	}
}

// mergeBindingsLocked adds the endpoint's fetched bindings as Committed
// entries with new list ids and drops the endpoint's Committed or
// DeletePending bindings the node no longer has (:536-586). The removal
// is bounded to the endpoint, as the groups' is (:397); connectedhomeip's
// predicate (:567-585) also drops the other endpoints' bindings of the
// node.
func (d *Datastore) mergeBindingsLocked(nodeID uint64, ep uint16, bindings []EndpointBindingEntry) {
	for _, b := range bindings {
		if slices.ContainsFunc(d.t.Bindings, func(e EndpointBindingEntry) bool {
			return e.NodeId == nodeID && e.EndpointId == b.EndpointId && bindingMatches(e.Binding, b.Binding)
		}) {
			continue
		}
		d.t.Bindings = append(d.t.Bindings, EndpointBindingEntry{
			NodeId: nodeID, EndpointId: b.EndpointId, Binding: cloneBinding(b.Binding), ListId: d.nextListIDLocked(),
			StatusEntry: StatusEntry{State: stateCommitted},
		})
	}
	d.t.Bindings = slices.DeleteFunc(d.t.Bindings, func(e EndpointBindingEntry) bool {
		if e.NodeId != nodeID || e.EndpointId != ep {
			return false
		}
		if e.StatusEntry.State != stateCommitted && e.StatusEntry.State != stateDeletePending {
			return false
		}
		return !slices.ContainsFunc(bindings, func(b EndpointBindingEntry) bool {
			return e.EndpointId == b.EndpointId && bindingMatches(e.Binding, b.Binding)
		})
	})
	d.markLocked(dsdef.AttrEndpointBindingList)
}

// permanent reports a failure a retry would not cure: ConstraintError or
// ResourceExhausted (:497-498, :615-616, :872-873, :1034-1035).
func permanent(code dsdef.Status) bool {
	return code == dsdef.StatusConstraintError || code == dsdef.StatusResourceExhausted
}

// syncBindingsOfNode pushes the node's Pending, Committed and retryable
// CommitFailed bindings as its whole list (a permanently failed one is
// erased); once pushed, each Pending or CommitFailed entry takes the
// state the port left in the list, and the node's DeletePending bindings
// are erased. A push that fails ends the refresh (:602-684).
func (d *Datastore) syncBindingsOfNode(ctx context.Context, nodeID uint64) error {
	var list []EndpointBindingEntry
	d.commit(ctx, func() {
		d.t.Bindings = slices.DeleteFunc(d.t.Bindings, func(e EndpointBindingEntry) bool {
			return e.NodeId == nodeID && e.StatusEntry.State == stateCommitFailed && permanent(e.StatusEntry.FailureCode)
		})
		for _, e := range d.t.Bindings {
			if e.NodeId == nodeID && e.StatusEntry.State != stateDeletePending {
				list = append(list, e)
			}
		}
		d.markLocked(dsdef.AttrEndpointBindingList)
	})
	if err := d.sync.SyncBindings(ctx, nodeID, list); err != nil { // :630-641
		return fmt.Errorf("%w: SyncBindings: %w", ErrSync, err)
	}
	d.commit(ctx, func() {
		for i := range d.t.Bindings { // :642-666
			e := &d.t.Bindings[i]
			if e.NodeId != nodeID || (e.StatusEntry.State != statePending && e.StatusEntry.State != stateCommitFailed) {
				continue
			}
			j := slices.IndexFunc(list, func(b EndpointBindingEntry) bool {
				return e.EndpointId == b.EndpointId && bindingMatches(e.Binding, b.Binding)
			})
			if j < 0 {
				continue
			}
			if list[j].StatusEntry.State == stateCommitFailed {
				e.StatusEntry.State, e.StatusEntry.FailureCode = stateCommitFailed, list[j].StatusEntry.FailureCode
			} else {
				e.StatusEntry.State, e.StatusEntry.FailureCode = stateCommitted, 0
			}
		}
		d.t.Bindings = slices.DeleteFunc(d.t.Bindings, func(e EndpointBindingEntry) bool { // :668-675
			return e.NodeId == nodeID && e.StatusEntry.State == stateDeletePending
		})
		d.markLocked(dsdef.AttrEndpointBindingList)
	})
	return nil
}

// takeKeySetLocked adds a fetched key set or updates the held one
// (:724-746); an epoch key over 16 bytes is only logged there.
func (d *Datastore) takeKeySetLocked(ks GroupKeySet) {
	if i := d.keySetIndexLocked(ks.GroupKeySetId); i >= 0 {
		_ = copyKeySet(ks, &d.t.GroupKeySets[i])
	} else {
		stored := newKeySet()
		_ = copyKeySet(ks, &stored)
		d.t.GroupKeySets = append(d.t.GroupKeySets, stored)
	}
	d.markLocked(dsdef.AttrGroupKeySetList)
}

// syncKeySets pushes, for every held key set, the removal of each node's
// DeletePending key set entry of it (a zero entry, erased once pushed),
// then the key set to each node whose entry of it is Pending or
// retryably CommitFailed (committed once pushed); a permanently failed
// entry is erased. The entries are every node's, not only the refreshed
// one's, as connectedhomeip walks them (:769-916).
func (d *Datastore) syncKeySets(ctx context.Context) {
	type removal struct {
		node uint64
		id   uint16
	}
	var removals []removal
	d.mu.Lock()
	for i := range d.t.GroupKeySets { // :771-784
		ks := &d.t.GroupKeySets[i]
		for _, e := range d.t.NodeKeySets {
			if e.GroupKeySetId == ks.GroupKeySetId && e.StatusEntry.State == stateDeletePending {
				removals = append(removals, removal{e.NodeId, e.GroupKeySetId})
			}
		}
	}
	d.mu.Unlock()
	for _, r := range removals { // :786-824
		_ = d.pushNodeKeySetRemoval(r.node, NodeKeySetEntry{}, r.node, r.id)(ctx)
	}
	type keyPush struct {
		node uint64
		ks   GroupKeySet
	}
	var pushes []keyPush
	d.commit(ctx, func() {
		for i := range d.t.GroupKeySets { // :826-913
			ks := &d.t.GroupKeySets[i]
			d.t.NodeKeySets = slices.DeleteFunc(d.t.NodeKeySets, func(e NodeKeySetEntry) bool {
				return e.GroupKeySetId == ks.GroupKeySetId && e.StatusEntry.State == stateCommitFailed && permanent(e.StatusEntry.FailureCode)
			})
			for _, e := range d.t.NodeKeySets {
				if e.GroupKeySetId == ks.GroupKeySetId && (e.StatusEntry.State == statePending || e.StatusEntry.State == stateCommitFailed) {
					pushes = append(pushes, keyPush{e.NodeId, cloneKeySet(*ks)})
				}
			}
		}
		d.markLocked(dsdef.AttrNodeKeySetList)
	})
	for i := range pushes {
		_ = d.pushKeySet(pushes[i].node, pushes[i].ks)(ctx)
	}
}

// mergeACLsLocked adds the node's fetched ACL entries it does not hold by
// list id as Committed and drops its Committed or DeletePending entries
// the node no longer has (:923-984).
func (d *Datastore) mergeACLsLocked(nodeID uint64, acls []ACLEntry) {
	for _, a := range acls {
		if slices.ContainsFunc(d.t.ACLs, func(e aclEntry) bool { return e.NodeID == nodeID && e.ListID == a.ListId }) {
			continue
		}
		e := aclEntry{NodeID: nodeID, ListID: a.ListId, Privilege: a.AclEntry.Privilege, AuthMode: a.AclEntry.AuthMode, StatusEntry: StatusEntry{State: stateCommitted}}
		if !a.AclEntry.Subjects.Null {
			e.Subjects = slices.Clone(a.AclEntry.Subjects.Value)
		}
		if !a.AclEntry.Targets.Null {
			e.Targets = slices.Clone(a.AclEntry.Targets.Value)
		}
		d.t.ACLs = append(d.t.ACLs, e)
	}
	d.t.ACLs = slices.DeleteFunc(d.t.ACLs, func(e aclEntry) bool {
		if e.NodeID != nodeID || (e.StatusEntry.State != stateCommitted && e.StatusEntry.State != stateDeletePending) {
			return false
		}
		return !slices.ContainsFunc(acls, func(a ACLEntry) bool { return e.ListID == a.ListId })
	})
	d.markLocked(dsdef.AttrNodeAclList)
}

// syncACLsOfNode pushes the node's Pending, Committed and retryable
// CommitFailed ACL entries as its whole list (a permanently failed one is
// erased); once pushed, each Pending or CommitFailed entry in the list is
// Committed, the DeletePending ones are erased and the node is Committed.
// A push that fails ends the refresh (:1002-1126).
func (d *Datastore) syncACLsOfNode(ctx context.Context, nodeID uint64) error {
	var list []ACLEntry
	d.commit(ctx, func() {
		d.t.ACLs = slices.DeleteFunc(d.t.ACLs, func(e aclEntry) bool {
			return e.NodeID == nodeID && e.StatusEntry.State == stateCommitFailed && permanent(e.StatusEntry.FailureCode)
		})
		for _, e := range d.t.ACLs {
			if e.NodeID == nodeID && e.StatusEntry.State != stateDeletePending {
				list = append(list, e.encode())
			}
		}
		d.markLocked(dsdef.AttrNodeAclList)
	})
	if err := d.sync.SyncACLs(ctx, nodeID, list); err != nil { // :1062-1072
		return fmt.Errorf("%w: SyncACLs: %w", ErrSync, err)
	}
	var err error
	d.commit(ctx, func() {
		for i := range d.t.ACLs { // :1073-1088
			e := &d.t.ACLs[i]
			if e.NodeID == nodeID && (e.StatusEntry.State == statePending || e.StatusEntry.State == stateCommitFailed) &&
				slices.ContainsFunc(list, func(a ACLEntry) bool { return a.ListId == e.ListID }) {
				e.StatusEntry.State = stateCommitted
			}
		}
		d.t.ACLs = slices.DeleteFunc(d.t.ACLs, func(e aclEntry) bool { // :1090-1097
			return e.NodeID == nodeID && e.StatusEntry.State == stateDeletePending
		})
		d.markLocked(dsdef.AttrNodeAclList)
		err = d.setNodeLocked(nodeID, stateCommitted) // :1099-1104
	})
	return err
}

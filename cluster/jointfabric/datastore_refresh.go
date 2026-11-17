// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"context"
	"errors"
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
// fetched and taken into nothing (below), and every node's pending key
// set entries pushed;
// its ACL entries are fetched, merged and pushed as the whole list; the
// node goes Committed. A fetch that fails ends the refresh with the node
// Pending. Without a Sync, with a refresh running, or for a node not held,
// it is refused. Mirrors JointFabricDatastore.cpp:241-1130 at 6170af84
// (RefreshNode, ContinueRefresh) — run through here, where
// connectedhomeip re-enters ContinueRefresh from each delegate callback.
//
// A store that refuses a step's save ends the refresh with its error
// (ErrStore, R2-P6-err-1120); a wipe of the datastore while the refresh
// runs ends it before its next step commits (R2-P6-conc-1107); a node
// that lists more entries in one list than a command lets that list hold
// ends it with ErrNoMemory before anything is pushed (R2-P6-bound-1113).
func (d *Datastore) RefreshNode(ctx context.Context, nodeID uint64) error {
	rctx, err := d.beginRefresh(ctx, nodeID)
	if err != nil {
		return err
	}
	return d.runRefresh(rctx, nodeID)
}

// beginRefresh is RefreshNode (:241-252) and the Idle state's SetNode
// (:262): a Sync, no refresh running, the node held and now Pending. A
// node not held ends the refresh here; connectedhomeip keeps
// mRefreshingNodeId set on that path (:247-249), which refuses every
// later refresh. The returned context carries the tables' generation the
// refresh commits against.
func (d *Datastore) beginRefresh(ctx context.Context, nodeID uint64) (context.Context, error) {
	var gen uint64
	err := d.update(ctx, func() error {
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
		gen = d.gen
		return nil
	})
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, genKey{}, gen), nil
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
	if err := d.commit(ctx, func() { refreshing = d.refreshEndpointsLocked(nodeID, endpoints) }); err != nil {
		return err
	}
	for _, ep := range refreshing { // kRefreshingGroups :355-426
		groups, err := d.sync.FetchEndpointGroupList(ctx, nodeID, ep)
		if err != nil {
			continue // :368: a failed fetch leaves the endpoint's memberships
		}
		if err := d.commitMerge(ctx, func() error { return d.mergeGroupsLocked(nodeID, ep, groups) }); err != nil {
			return err
		}
	}
	if err := d.syncGroupsOfNode(ctx, nodeID); err != nil { // :428-511
		return err
	}
	for _, ep := range refreshing { // kRefreshingBindings :522-600
		bindings, err := d.sync.FetchEndpointBindingList(ctx, nodeID, ep)
		if err != nil {
			continue // :536
		}
		if err := d.commitMerge(ctx, func() error { return d.mergeBindingsLocked(nodeID, ep, bindings) }); err != nil {
			return err
		}
	}
	if err := d.syncBindingsOfNode(ctx, nodeID); err != nil { // :602-684
		return err
	}
	ids, err := d.sync.FetchGroupKeySetList(ctx, nodeID) // kFetchingGroupKeySetList :686-712
	if err != nil {
		return fmt.Errorf("%w: FetchGroupKeySetList: %w", ErrSync, err)
	}
	for _, id := range ids { // kFetchingGroupKeySets :714-767
		// The key set is fetched, as connectedhomeip fetches it, and taken
		// into nothing: the datastore is the source of the Joint Fabric's
		// epoch keys, so a held key set's keys and policy are never
		// replaced by what the node reports (a KeySetRead answers its keys
		// null), a key set the datastore does not hold is not adopted, and
		// the node's say-so enrols it as no recipient of a key set
		// (NodeKeySetList is written by the commands alone). Owner decision
		// 129, R2-P6-sec-1106; connectedhomeip updates the held entry with
		// the node's and appends an unknown one
		// (src/app/server/JointFabricDatastore.cpp:724-743 at 6170af84).
		if _, err := d.sync.FetchGroupKeySet(ctx, nodeID, id); err != nil {
			return fmt.Errorf("%w: FetchGroupKeySet %d: %w", ErrSync, id, err)
		}
	}
	if err := d.syncKeySets(ctx); err != nil { // kRefreshingGroupKeySets :769-916
		return err
	}
	acls, err := d.sync.FetchACLList(ctx, nodeID) // :918-999
	if err != nil {
		return fmt.Errorf("%w: FetchACLList: %w", ErrSync, err)
	}
	if err := d.commitMerge(ctx, func() error { return d.mergeACLsLocked(nodeID, acls) }); err != nil {
		return err
	}
	return d.syncACLsOfNode(ctx, nodeID) // kRefreshingACLs :1002-1126
}

// commitMerge commits a merge that refuses before it changes anything;
// the merge's error, else the commit's.
func (d *Datastore) commitMerge(ctx context.Context, merge func() error) error {
	var merr error
	if err := d.commit(ctx, func() { merr = merge() }); err != nil {
		return err
	}
	return merr
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
// endpoint the node no longer has (:368-412). A fetched list longer than
// MaxGroups, the most one list holds through AddGroupIDToEndpointForNode
// (:1684), is ErrNoMemory before anything changes; the table's total is
// not bounded, as connectedhomeip's merge appends without one (:389)
// (R2-P6-bound-1113, the module's per-list bound).
func (d *Datastore) mergeGroupsLocked(nodeID uint64, ep uint16, groups []GroupEntry) error {
	if len(groups) > MaxGroups {
		return fmt.Errorf("%w: node %d endpoint %d lists %d groups", ErrNoMemory, nodeID, ep, len(groups))
	}
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
	return nil
}

// syncGroupsOfNode pushes the node's Pending memberships, committing each
// pushed, and the removal of its DeletePending ones as a zero entry,
// erasing each pushed; a CommitFailed one stays (:428-511). A push that
// fails is left for the next refresh; a refused save is the error.
func (d *Datastore) syncGroupsOfNode(ctx context.Context, nodeID uint64) error {
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
			if err := d.pushEndpointGroup(nodeID, e)(ctx); isCommitError(err) {
				return err
			}
			continue
		}
		if err := d.live(ctx); err != nil {
			return err
		}
		if err := d.sync.SyncEndpointGroupID(ctx, nodeID, EndpointGroupIDEntry{}); err != nil { // :460-491
			continue
		}
		if err := d.commit(ctx, func() {
			n := len(d.t.EndpointGroupIDs)
			d.t.EndpointGroupIDs = slices.DeleteFunc(d.t.EndpointGroupIDs, func(x EndpointGroupIDEntry) bool { return sameEndpointGroup(x, e) })
			if len(d.t.EndpointGroupIDs) != n {
				d.markLocked(dsdef.AttrEndpointGroupIdList)
			}
		}); err != nil {
			return err
		}
	}
	return nil
}

// isCommitError reports a push's error that is its commit's — a refused
// save or a wipe — not the node's: a refresh ends on it.
func isCommitError(err error) bool {
	return errors.Is(err, ErrStore) || errors.Is(err, errWiped)
}

// mergeBindingsLocked adds the endpoint's fetched bindings as Committed
// entries with new list ids and drops the endpoint's Committed or
// DeletePending bindings the node no longer has (:536-586). The removal
// is bounded to the endpoint, as the groups' is (:397); connectedhomeip's
// predicate (:567-585) also drops the other endpoints' bindings of the
// node. A fetched list longer than MaxGroups, the most one list holds
// through AddBindingToEndpointForNode (:1930), is ErrNoMemory before
// anything changes — the refresh then writes no truncated list to the
// node; the table's total is not bounded, as connectedhomeip's merge
// appends without one (:562) (R2-P6-bound-1113, the module's per-list
// bound).
func (d *Datastore) mergeBindingsLocked(nodeID uint64, ep uint16, bindings []EndpointBindingEntry) error {
	if len(bindings) > MaxGroups {
		return fmt.Errorf("%w: node %d endpoint %d lists %d bindings", ErrNoMemory, nodeID, ep, len(bindings))
	}
	for _, b := range bindings {
		if slices.ContainsFunc(d.t.Bindings, func(e EndpointBindingEntry) bool {
			return e.NodeId == nodeID && e.EndpointId == b.EndpointId && bindingMatches(e.Binding, b.Binding)
		}) {
			continue
		}
		d.t.Bindings = append(d.t.Bindings, EndpointBindingEntry{
			NodeId: nodeID, EndpointId: b.EndpointId, Binding: b.Binding.Clone(), ListId: d.nextListIDLocked(),
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
	return nil
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
	// The node's binding lock, from before the list is taken until it is
	// written: a binding push in flight writes first, or after this write
	// with the list this write left (review II, R2-P1-clu-05).
	unlock, err := d.lockNodeBindings(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("%w: binding list of node %d: %w", ErrSync, nodeID, err)
	}
	var list []EndpointBindingEntry
	if err := d.commit(ctx, func() {
		d.t.Bindings = slices.DeleteFunc(d.t.Bindings, func(e EndpointBindingEntry) bool {
			return e.NodeId == nodeID && e.StatusEntry.State == stateCommitFailed && permanent(e.StatusEntry.FailureCode)
		})
		for _, e := range d.t.Bindings {
			if e.NodeId == nodeID && e.StatusEntry.State != stateDeletePending {
				list = append(list, e)
			}
		}
		d.markLocked(dsdef.AttrEndpointBindingList)
	}); err != nil {
		unlock()
		return err
	}
	err = d.sync.SyncBindings(ctx, nodeID, list) // :630-641
	unlock()
	if err != nil {
		return fmt.Errorf("%w: SyncBindings: %w", ErrSync, err)
	}
	return d.commit(ctx, func() {
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
}

// syncKeySets pushes, for every held key set, the removal of each node's
// DeletePending key set entry of it (a zero entry, erased once pushed),
// then the key set to each node whose entry of it is Pending or
// retryably CommitFailed (committed once pushed); a permanently failed
// entry is erased. The entries are every node's, not only the refreshed
// one's, as connectedhomeip walks them (:769-916). A push that fails is
// left for the next refresh; a refused save is the error.
func (d *Datastore) syncKeySets(ctx context.Context) error {
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
		if err := d.pushNodeKeySetRemoval(r.node, NodeKeySetEntry{}, r.node, r.id)(ctx); isCommitError(err) {
			return err
		}
	}
	type keyPush struct {
		node uint64
		ks   GroupKeySet
	}
	var pushes []keyPush
	if err := d.commit(ctx, func() {
		for i := range d.t.GroupKeySets { // :826-913
			ks := &d.t.GroupKeySets[i]
			d.t.NodeKeySets = slices.DeleteFunc(d.t.NodeKeySets, func(e NodeKeySetEntry) bool {
				return e.GroupKeySetId == ks.GroupKeySetId && e.StatusEntry.State == stateCommitFailed && permanent(e.StatusEntry.FailureCode)
			})
			for _, e := range d.t.NodeKeySets {
				if e.GroupKeySetId == ks.GroupKeySetId && (e.StatusEntry.State == statePending || e.StatusEntry.State == stateCommitFailed) {
					pushes = append(pushes, keyPush{e.NodeId, ks.Clone()})
				}
			}
		}
		d.markLocked(dsdef.AttrNodeKeySetList)
	}); err != nil {
		return err
	}
	for i := range pushes {
		if err := d.pushKeySet(pushes[i].node, pushes[i].ks)(ctx); isCommitError(err) {
			return err
		}
	}
	return nil
}

// mergeACLsLocked adds the node's fetched ACL entries it does not hold by
// list id as Committed and drops its Committed or DeletePending entries
// the node no longer has (:923-984). A fetched list longer than MaxACLs,
// the most the list holds through AddACLToNode (:2122), is ErrNoMemory
// before anything changes — the refresh then writes no truncated list to
// the node; the table's total is not bounded, as connectedhomeip's merge
// appends without one (:959) (R2-P6-bound-1113, the module's per-list
// bound).
func (d *Datastore) mergeACLsLocked(nodeID uint64, acls []ACLEntry) error {
	if len(acls) > MaxACLs {
		return fmt.Errorf("%w: node %d lists %d ACL entries", ErrNoMemory, nodeID, len(acls))
	}
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
	return nil
}

// syncACLsOfNode pushes the node's Pending, Committed and retryable
// CommitFailed ACL entries as its whole list (a permanently failed one is
// erased); once pushed, each Pending or CommitFailed entry in the list is
// Committed, the DeletePending ones are erased and the node is Committed.
// A push that fails ends the refresh (:1002-1126).
func (d *Datastore) syncACLsOfNode(ctx context.Context, nodeID uint64) error {
	var list []ACLEntry
	if err := d.commit(ctx, func() {
		d.t.ACLs = slices.DeleteFunc(d.t.ACLs, func(e aclEntry) bool {
			return e.NodeID == nodeID && e.StatusEntry.State == stateCommitFailed && permanent(e.StatusEntry.FailureCode)
		})
		for _, e := range d.t.ACLs {
			if e.NodeID == nodeID && e.StatusEntry.State != stateDeletePending {
				list = append(list, e.encode())
			}
		}
		d.markLocked(dsdef.AttrNodeAclList)
	}); err != nil {
		return err
	}
	if err := d.sync.SyncACLs(ctx, nodeID, list); err != nil { // :1062-1072
		return fmt.Errorf("%w: SyncACLs: %w", ErrSync, err)
	}
	var err error
	if cerr := d.commit(ctx, func() {
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
	}); cerr != nil {
		return cerr
	}
	return err
}

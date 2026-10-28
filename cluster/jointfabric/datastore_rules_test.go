// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// JointFabricDatastore rules connectedhomeip's unit tests leave open: the
// commands' statuses (joint-fabric-datastore-server.cpp at 6170af84 and
// JointFabricDatastore.cpp's status codes, which the Python cases
// TC_JFDS_2_2…2_5 check), the refresh's merges, the change reports and
// the persistence.
package jointfabric

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/im"
)

func statusOf(err error) im.StatusCode {
	if err == nil {
		return im.StatusSuccess
	}
	var sc im.StatusCodeError
	if errors.As(err, &sc) {
		return sc.MatterStatusCode()
	}
	return 0xFF
}

func invoke(t *testing.T, d *Datastore, cmd uint32, fields any) im.StatusCode {
	t.Helper()
	_, err := d.MatterInvoke(t.Context(), cmd, fields)
	d.Wait()
	return statusOf(err)
}

func keySet(id uint16) GroupKeySet {
	return GroupKeySet{GroupKeySetId: id, EpochKey0: spec.Nullable[[]byte]{Value: make([]byte, 16)}, EpochKey1: spec.Nullable[[]byte]{Null: true}, EpochKey2: spec.Nullable[[]byte]{Null: true}}
}

// TestKeySetCommandStatuses is TC_JFDS_2_2: AddKeySet twice, UpdateKeySet
// and RemoveKeySet of a key set not held, RemoveKeySet of 0, an invalid
// security policy (JointFabricDatastore.cpp:1158, :1184, :1196,
// :1209-1211; joint-fabric-datastore-server.cpp:324, :349).
func TestKeySetCommandStatuses(t *testing.T) {
	d := newStore(t, newTrackingSync())
	for _, c := range []struct {
		name   string
		cmd    uint32
		fields any
		want   im.StatusCode
	}{
		{"add", dsdef.CmdAddKeySet, dsdef.AddKeySetRequest{GroupKeySet: keySet(0x0a)}, im.StatusSuccess},
		{"add twice", dsdef.CmdAddKeySet, dsdef.AddKeySetRequest{GroupKeySet: keySet(0x0a)}, im.StatusConstraintError},
		{"update", dsdef.CmdUpdateKeySet, dsdef.UpdateKeySetRequest{GroupKeySet: keySet(0x0a)}, im.StatusSuccess},
		{"update bad policy", dsdef.CmdUpdateKeySet, dsdef.UpdateKeySetRequest{GroupKeySet: GroupKeySet{GroupKeySetId: 0x0a, GroupKeySecurityPolicy: 7}}, im.StatusConstraintError},
		{"update unknown", dsdef.CmdUpdateKeySet, dsdef.UpdateKeySetRequest{GroupKeySet: keySet(0x0FFF)}, im.StatusNotFound},
		{"remove unknown", dsdef.CmdRemoveKeySet, dsdef.RemoveKeySetRequest{GroupKeySetId: 0x0FFF}, im.StatusNotFound},
		{"remove", dsdef.CmdRemoveKeySet, dsdef.RemoveKeySetRequest{GroupKeySetId: 0x0a}, im.StatusSuccess},
		{"add 0", dsdef.CmdAddKeySet, dsdef.AddKeySetRequest{GroupKeySet: keySet(0)}, im.StatusSuccess},
		{"remove 0", dsdef.CmdRemoveKeySet, dsdef.RemoveKeySetRequest{GroupKeySetId: 0}, im.StatusConstraintError},
	} {
		if got := invoke(t, d, c.cmd, c.fields); got != c.want {
			t.Errorf("%s: status %#x, want %#x", c.name, got, c.want)
		}
	}
	long := keySet(5)
	long.EpochKey0.Value = make([]byte, 17)
	if err := d.AddGroupKeySetEntry(t.Context(), long); statusOf(err) != im.StatusConstraintError { // :43-45
		t.Errorf("an epoch key of 17 bytes: %v", err)
	}
}

func addGroup(id uint16, cat spec.Nullable[uint16]) dsdef.AddGroupRequest {
	return dsdef.AddGroupRequest{GroupId: id, FriendlyName: "g", GroupKeySetId: nullU16(), GroupCat: cat, GroupCatVersion: nullU16(), GroupPermission: dsdef.DatastoreAccessControlEntryPrivilegeView}
}

// TestGroupCommandStatuses is TC_JFDS_2_3: the administrator and anchor
// CAT groups are not the datastore's to add, update or remove
// (JointFabricDatastore.cpp:1364-1371, :1416-1423, :1590-1600);
// ForceAddGroup adds them.
func TestGroupCommandStatuses(t *testing.T) {
	d := newStore(t, newTrackingSync())
	upd := func(id uint16) dsdef.UpdateGroupRequest {
		return dsdef.UpdateGroupRequest{GroupId: id, FriendlyName: spec.Nullable[string]{Value: "other"}, GroupKeySetId: nullU16(), GroupCat: nullU16(), GroupCatVersion: nullU16(), GroupPermission: spec.Nullable[dsdef.DatastoreAccessControlEntryPrivilegeEnum]{Null: true}}
	}
	for _, c := range []struct {
		name   string
		cmd    uint32
		fields any
		want   im.StatusCode
	}{
		{"add", dsdef.CmdAddGroup, addGroup(0x0a, nullU16()), im.StatusSuccess},
		{"add twice", dsdef.CmdAddGroup, addGroup(0x0a, nullU16()), im.StatusConstraintError},
		{"update", dsdef.CmdUpdateGroup, upd(0x0a), im.StatusSuccess},
		{"update unknown", dsdef.CmdUpdateGroup, upd(0x0b), im.StatusConstraintError},
		{"remove", dsdef.CmdRemoveGroup, dsdef.RemoveGroupRequest{GroupId: 0x0a}, im.StatusSuccess},
		{"remove unknown", dsdef.CmdRemoveGroup, dsdef.RemoveGroupRequest{GroupId: 0x0a}, im.StatusConstraintError},
		{"add admin CAT", dsdef.CmdAddGroup, addGroup(0x0c, spec.Nullable[uint16]{Value: AdminCATIdentifier}), im.StatusConstraintError},
		{"add anchor CAT", dsdef.CmdAddGroup, addGroup(0x0d, spec.Nullable[uint16]{Value: AnchorCATIdentifier}), im.StatusConstraintError},
	} {
		if got := invoke(t, d, c.cmd, c.fields); got != c.want {
			t.Errorf("%s: status %#x, want %#x", c.name, got, c.want)
		}
	}
	mustOK(t, d.ForceAddGroup(t.Context(), addGroup(0x0c, spec.Nullable[uint16]{Value: AdminCATIdentifier})))
	if got := invoke(t, d, dsdef.CmdUpdateGroup, upd(0x0c)); got != im.StatusConstraintError {
		t.Errorf("update of the admin CAT group: %#x", got)
	}
	if got := invoke(t, d, dsdef.CmdRemoveGroup, dsdef.RemoveGroupRequest{GroupId: 0x0c}); got != im.StatusConstraintError {
		t.Errorf("remove of the admin CAT group: %#x", got)
	}
	// Without a Sync, UpdateGroup answers FAILURE (:1412).
	if got := invoke(t, newStore(t, nil), dsdef.CmdUpdateGroup, upd(0x0a)); got != im.StatusFailure {
		t.Errorf("UpdateGroup without a Sync: %#x", got)
	}
}

// TestAdminAndNodeCommandStatuses is TC_JFDS_2_4 and the node steps of
// TC_JFDS_2_5 (JointFabricDatastore.cpp:181-239, :1222-1293).
func TestAdminAndNodeCommandStatuses(t *testing.T) {
	d := newStore(t, newTrackingSync())
	name := "renamed"
	for _, c := range []struct {
		name   string
		cmd    uint32
		fields any
		want   im.StatusCode
	}{
		{"add admin", dsdef.CmdAddAdmin, dsdef.AddAdminRequest{NodeId: 0x0a, FriendlyName: "a", VendorId: 0xFFF1, Icac: []byte{1}}, im.StatusSuccess},
		{"add admin twice", dsdef.CmdAddAdmin, dsdef.AddAdminRequest{NodeId: 0x0a}, im.StatusConstraintError},
		{"update admin", dsdef.CmdUpdateAdmin, dsdef.UpdateAdminRequest{NodeId: 0x0a, FriendlyName: &name}, im.StatusSuccess},
		{"update unknown admin", dsdef.CmdUpdateAdmin, dsdef.UpdateAdminRequest{NodeId: 0x0b}, im.StatusFailure},
		{"remove admin", dsdef.CmdRemoveAdmin, dsdef.RemoveAdminRequest{NodeId: 0x0a}, im.StatusSuccess},
		{"remove unknown admin", dsdef.CmdRemoveAdmin, dsdef.RemoveAdminRequest{NodeId: 0x0a}, im.StatusFailure},
		{"add node", dsdef.CmdAddPendingNode, dsdef.AddPendingNodeRequest{NodeId: 0x0a, FriendlyName: "n"}, im.StatusSuccess},
		{"add node twice", dsdef.CmdAddPendingNode, dsdef.AddPendingNodeRequest{NodeId: 0x0a}, im.StatusConstraintError},
		{"update unknown node", dsdef.CmdUpdateNode, dsdef.UpdateNodeRequest{NodeId: 0x0b, FriendlyName: "x"}, im.StatusConstraintError},
		{"remove node", dsdef.CmdRemoveNode, dsdef.RemoveNodeRequest{NodeId: 0x0a}, im.StatusSuccess},
		{"remove unknown node", dsdef.CmdRemoveNode, dsdef.RemoveNodeRequest{NodeId: 0x0a}, im.StatusConstraintError},
		{"refresh unknown node", dsdef.CmdRefreshNode, dsdef.RefreshNodeRequest{NodeId: 0x0a}, im.StatusFailure},
		{"ACL of unknown node", dsdef.CmdAddAclToNode, dsdef.AddAclToNodeRequest{NodeId: 0x0a, AclEntry: viewCASE}, im.StatusConstraintError},
		{"remove ACL of unknown node", dsdef.CmdRemoveAclFromNode, dsdef.RemoveAclFromNodeRequest{NodeId: 0x0a, ListId: 1}, im.StatusConstraintError},
		{"group of unknown endpoint", dsdef.CmdAddGroupIdToEndpointForNode, dsdef.AddGroupIdToEndpointForNodeRequest{NodeId: 0x0a, EndpointId: 1, GroupId: 1}, im.StatusConstraintError},
		{"remove group of unknown endpoint", dsdef.CmdRemoveGroupIdFromEndpointForNode, dsdef.RemoveGroupIdFromEndpointForNodeRequest{NodeId: 0x0a, EndpointId: 1, GroupId: 1}, im.StatusConstraintError},
		{"binding of unknown endpoint", dsdef.CmdAddBindingToEndpointForNode, dsdef.AddBindingToEndpointForNodeRequest{NodeId: 0x0a, EndpointId: 1}, im.StatusConstraintError},
		{"remove binding of unknown endpoint", dsdef.CmdRemoveBindingFromEndpointForNode, dsdef.RemoveBindingFromEndpointForNodeRequest{NodeId: 0x0a, EndpointId: 1}, im.StatusConstraintError},
		{"update unknown endpoint", dsdef.CmdUpdateEndpointForNode, dsdef.UpdateEndpointForNodeRequest{NodeId: 0x0a, EndpointId: 1, FriendlyName: "e"}, im.StatusConstraintError},
	} {
		if got := invoke(t, d, c.cmd, c.fields); got != c.want {
			t.Errorf("%s: status %#x, want %#x", c.name, got, c.want)
		}
	}
	// Without a Sync, the pushing operations answer FAILURE.
	n := newStore(t, nil)
	mustOK(t, n.AddPendingNode(t.Context(), 1, "n"))
	if got := invoke(t, n, dsdef.CmdAddAclToNode, dsdef.AddAclToNodeRequest{NodeId: 1, AclEntry: viewCASE}); got != im.StatusFailure {
		t.Errorf("AddACLToNode without a Sync: %#x", got)
	}
	if got := invoke(t, n, dsdef.CmdRefreshNode, dsdef.RefreshNodeRequest{NodeId: 1}); got != im.StatusFailure {
		t.Errorf("RefreshNode without a Sync: %#x", got)
	}
}

// TestDatastoreCapacities: a full table refuses with FAILURE
// (CHIP_ERROR_NO_MEMORY; JointFabricDatastore.cpp:183, :1227, :1684,
// :1930, :2122).
func TestDatastoreCapacities(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, newTrackingSync())
	for i := range MaxNodes {
		mustOK(t, d.AddPendingNode(ctx, uint64(i+1), "n"))
	}
	if err := d.AddPendingNode(ctx, 0xFFFF, "n"); !errors.Is(err, ErrNoMemory) {
		t.Errorf("node %d: %v", MaxNodes+1, err)
	}
	for i := range MaxAdminNodes {
		mustOK(t, d.AddAdmin(ctx, AdminEntry{NodeId: uint64(i + 1)}))
	}
	if err := d.AddAdmin(ctx, AdminEntry{NodeId: 0xFFFF}); !errors.Is(err, ErrNoMemory) {
		t.Errorf("admin %d: %v", MaxAdminNodes+1, err)
	}
	d.addEndpointEntry(ctx, 1, 1, "e")
	for i := range MaxGroups {
		mustOK(t, d.AddBindingToEndpointForNode(ctx, 1, 1, BindingTarget{Group: ptr(uint16(i + 1))}))
	}
	if err := d.AddBindingToEndpointForNode(ctx, 1, 1, BindingTarget{Group: ptr[uint16](0xFF)}); !errors.Is(err, ErrNoMemory) {
		t.Errorf("binding %d: %v", MaxGroups+1, err)
	}
	if err := d.SetFriendlyName(ctx, string(make([]byte, MaxFriendlyNameSize))); !errors.Is(err, ErrInvalidArgument) { // :342-345
		t.Errorf("a 32-byte friendly name: %v", err)
	}
	if err := d.SetAnchorRootCA(ctx, make([]byte, maxDERCertLength)); !errors.Is(err, ErrInvalidArgument) { // :289-292
		t.Errorf("a 600-byte root: %v", err)
	}
	if err := d.AddPendingNode(ctx, 0, string(make([]byte, 40))); !errors.Is(err, ErrNoMemory) {
		t.Errorf("full: %v", err)
	}
}

// TestUpdateGroupPushesACLsOfTheGroup: a changed permission re-marks the
// ACL entries whose subjects name the group Pending with the group's
// permission and pushes them (JointFabricDatastore.cpp:1510-1580).
func TestUpdateGroupPushesACLsOfTheGroup(t *testing.T) {
	ctx := t.Context()
	s := newTrackingSync()
	d := newStore(t, s)
	mustOK(t, d.AddGroup(ctx, addGroup(0x10, nullU16())))
	mustOK(t, d.AddPendingNode(ctx, 1, "n"))
	group := viewCASE
	group.AuthMode = dsdef.DatastoreAccessControlEntryAuthModeGroup
	group.Subjects = spec.Nullable[[]uint64]{Value: []uint64{0x10}}
	mustOK(t, d.AddACLToNode(ctx, 1, group))
	s.reset()
	mustOK(t, d.UpdateGroup(ctx, dsdef.UpdateGroupRequest{GroupId: 0x10, FriendlyName: spec.Nullable[string]{Null: true}, GroupKeySetId: nullU16(), GroupCat: nullU16(), GroupCatVersion: nullU16(),
		GroupPermission: spec.Nullable[dsdef.DatastoreAccessControlEntryPrivilegeEnum]{Value: dsdef.DatastoreAccessControlEntryPrivilegeOperate}}))
	if e := s.lastACL; e == nil || e.AclEntry.Privilege != dsdef.DatastoreAccessControlEntryPrivilegeOperate || e.StatusEntry.State != statePending {
		t.Fatalf("ACL push %+v", e)
	}
	if a := d.NodeACLList(); a[0].AclEntry.Privilege != dsdef.DatastoreAccessControlEntryPrivilegeOperate || a[0].StatusEntry.State != stateCommitted {
		t.Fatalf("NodeACLList %+v", a)
	}
}

// refreshSync is a node for the refresh: its endpoints, each endpoint's
// groups and bindings, its ACL entries; a fetch can fail.
type refreshSync struct {
	trackingSync
	groups      map[uint16][]GroupEntry
	bindings    map[uint16][]EndpointBindingEntry
	acls        []ACLEntry
	failACL     bool
	failBinding bool
	syncedBind  []EndpointBindingEntry
	markFailed  bool
	syncedACLs  []ACLEntry
	mu2         sync.Mutex
}

func (r *refreshSync) FetchEndpointGroupList(_ context.Context, _ uint64, ep uint16) ([]GroupEntry, error) {
	return r.groups[ep], nil
}

func (r *refreshSync) FetchEndpointBindingList(_ context.Context, _ uint64, ep uint16) ([]EndpointBindingEntry, error) {
	return r.bindings[ep], nil
}

func (r *refreshSync) SyncBinding(context.Context, uint64, EndpointBindingEntry) error {
	if r.failBinding {
		return errors.New("refresh: node unreachable")
	}
	return nil
}

func (r *refreshSync) FetchACLList(context.Context, uint64) ([]ACLEntry, error) {
	if r.failACL {
		return nil, errors.New("refresh: no ACL")
	}
	return r.acls, nil
}

func (r *refreshSync) SyncBindings(_ context.Context, _ uint64, entries []EndpointBindingEntry) error {
	r.mu2.Lock()
	defer r.mu2.Unlock()
	if r.markFailed && len(entries) > 0 {
		entries[0].StatusEntry.State, entries[0].StatusEntry.FailureCode = stateCommitFailed, dsdef.StatusFailure
	}
	r.syncedBind = slices.Clone(entries)
	return nil
}

func (r *refreshSync) SyncACLs(_ context.Context, _ uint64, entries []ACLEntry) error {
	r.mu2.Lock()
	defer r.mu2.Unlock()
	r.syncedACLs = slices.Clone(entries)
	return nil
}

// TestRefreshMerges: the refresh takes the node's endpoints over, merges
// each endpoint's groups and bindings (the root endpoint's too), keeps a
// binding of another endpoint, merges and pushes the ACL entries, and
// commits the node (JointFabricDatastore.cpp:298-1126).
func TestRefreshMerges(t *testing.T) {
	ctx := t.Context()
	r := &refreshSync{trackingSync: *newTrackingSync()}
	r.endpointsToFetch = []EndpointEntry{{EndpointId: 1}, {EndpointId: 2}}
	r.groups = map[uint16][]GroupEntry{1: {{GroupId: 7}}}
	r.bindings = map[uint16][]EndpointBindingEntry{1: {{EndpointId: 1, Binding: BindingTarget{Group: ptr[uint16](7)}}}, 0: {{EndpointId: 0, Binding: BindingTarget{Group: ptr[uint16](9)}}}}
	r.acls = []ACLEntry{{ListId: 40, AclEntry: viewCASE}}
	d := newStore(t, r)
	mustOK(t, d.AddPendingNode(ctx, 5, "n"))
	d.addEndpointEntry(ctx, 3, 5, "gone")
	mustOK(t, d.RefreshNode(ctx, 5))
	eps := d.NodeEndpointList()
	if len(eps) != 2 || eps[0].EndpointId != 1 || eps[1].EndpointId != 2 {
		t.Fatalf("NodeEndpointList = %+v", eps)
	}
	if g := d.EndpointGroupIDList(); len(g) != 1 || g[0].GroupId != 7 || g[0].StatusEntry.State != stateCommitted {
		t.Fatalf("EndpointGroupIDList = %+v", g)
	}
	b := d.EndpointBindingList()
	if len(b) != 2 {
		t.Fatalf("EndpointBindingList = %+v, want the bindings of endpoints 1 and 0", b)
	}
	if len(r.syncedBind) != 2 || len(r.syncedACLs) != 1 {
		t.Fatalf("pushed %d bindings, %d ACL entries", len(r.syncedBind), len(r.syncedACLs))
	}
	if a := d.NodeACLList(); len(a) != 1 || a[0].ListId != 40 || a[0].StatusEntry.State != stateCommitted {
		t.Fatalf("NodeACLList = %+v", a)
	}
	if n := d.NodeList(); n[0].CommissioningStatusEntry.State != stateCommitted {
		t.Fatalf("node %+v", n[0])
	}
	// A second refresh with the endpoint-1 binding gone drops it.
	r.bindings[1] = nil
	mustOK(t, d.RefreshNode(ctx, 5))
	if b := d.EndpointBindingList(); len(b) != 1 || b[0].EndpointId != 0 {
		t.Fatalf("EndpointBindingList = %+v", b)
	}
}

// TestRefreshFailureLeavesNodePending: a fetch that fails ends the
// refresh with the node Pending, and a new refresh may start
// (JointFabricDatastore.cpp:988-992).
func TestRefreshFailureLeavesNodePending(t *testing.T) {
	ctx := t.Context()
	r := &refreshSync{trackingSync: *newTrackingSync(), failACL: true}
	d := newStore(t, r)
	mustOK(t, d.AddPendingNode(ctx, 5, "n"))
	if err := d.RefreshNode(ctx, 5); !errors.Is(err, ErrSync) {
		t.Fatalf("RefreshNode = %v, want ErrSync", err)
	}
	if n := d.NodeList(); n[0].CommissioningStatusEntry.State != statePending {
		t.Fatalf("node %+v", n[0])
	}
	r.failACL = false
	mustOK(t, d.RefreshNode(ctx, 5))
	if err := d.RefreshNode(ctx, 99); err == nil {
		t.Fatal("refresh of an unknown node succeeded")
	}
	mustOK(t, d.RefreshNode(ctx, 5)) // the unknown node's refresh left none running
}

// TestRefreshKeepsCommitFailedFromThePort: a binding whose push failed
// stays Pending; the refresh pushes it with the node's list, and the port
// may mark it CommitFailed in that list, which the entry then takes
// (JointFabricDatastore.cpp:642-666).
func TestRefreshKeepsCommitFailedFromThePort(t *testing.T) {
	ctx := t.Context()
	r := &refreshSync{trackingSync: *newTrackingSync(), markFailed: true, failBinding: true}
	d := newStore(t, r)
	mustOK(t, d.AddPendingNode(ctx, 5, "n"))
	d.addEndpointEntry(ctx, 1, 5, "e")
	r.endpointsToFetch = []EndpointEntry{{EndpointId: 1}}
	r.bindings = map[uint16][]EndpointBindingEntry{1: {{EndpointId: 1, Binding: BindingTarget{Group: ptr[uint16](7)}}}}
	if err := d.AddBindingToEndpointForNode(ctx, 5, 1, BindingTarget{Group: ptr[uint16](8)}); !errors.Is(err, ErrSync) {
		t.Fatalf("AddBindingToEndpointForNode = %v, want ErrSync", err)
	}
	if b := d.EndpointBindingList(); len(b) != 1 || b[0].StatusEntry.State != statePending {
		t.Fatalf("EndpointBindingList = %+v", b)
	}
	mustOK(t, d.RefreshNode(ctx, 5))
	states := map[uint16]State{}
	for _, e := range d.EndpointBindingList() {
		states[*e.Binding.Group] = e.StatusEntry.State
		if *e.Binding.Group == 8 && e.StatusEntry.FailureCode != dsdef.StatusFailure {
			t.Errorf("failure code %d", e.StatusEntry.FailureCode)
		}
	}
	if states[8] != stateCommitFailed || states[7] != stateCommitted {
		t.Fatalf("binding states %v", states)
	}
}

// TestRefreshCommandRunsAfterTheAnswer: RefreshNode answers once the node
// is Pending; the refresh completes after (joint-fabric-datastore-server.cpp
// :539-562).
func TestRefreshCommandRunsAfterTheAnswer(t *testing.T) {
	r := &refreshSync{trackingSync: *newTrackingSync()}
	d := newStore(t, r)
	mustOK(t, d.AddPendingNode(t.Context(), 5, "n"))
	if got := invoke(t, d, dsdef.CmdRefreshNode, dsdef.RefreshNodeRequest{NodeId: 5}); got != im.StatusSuccess {
		t.Fatalf("RefreshNode: %#x", got)
	}
	if n := d.NodeList(); n[0].CommissioningStatusEntry.State != stateCommitted {
		t.Fatalf("node %+v after the refresh", n[0])
	}
}

// memStore is a settings map.
type memStore struct {
	mu sync.Mutex
	m  map[string]string
}

func (m *memStore) GetSetting(_ context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.m[key]
	return v, ok, nil
}

func (m *memStore) SetSetting(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		m.m = map[string]string{}
	}
	m.m[key] = value
	return nil
}

// TestDatastorePersists: the tables survive a new server on the same
// store and key.
func TestDatastorePersists(t *testing.T) {
	ctx := t.Context()
	st := &memStore{}
	d, err := NewDatastore(ctx, DatastoreConfig{Sync: newTrackingSync(), Store: st, Key: "ep1"})
	mustOK(t, err)
	mustOK(t, d.SetAnchorNodeID(ctx, 0x77))
	mustOK(t, d.AddPendingNode(ctx, 5, "n"))
	mustOK(t, d.AddACLToNode(ctx, 5, viewCASE))
	mustOK(t, d.AddGroupKeySetEntry(ctx, keySet(3)))
	again, err := NewDatastore(ctx, DatastoreConfig{Store: st, Key: "ep1"})
	mustOK(t, err)
	if again.AnchorNodeID() != 0x77 || len(again.NodeList()) != 1 || len(again.NodeACLList()) != 1 || len(again.GroupKeySetList()) != 1 {
		t.Fatalf("reloaded %d %v %v %v", again.AnchorNodeID(), again.NodeList(), again.NodeACLList(), again.GroupKeySetList())
	}
	if other, _ := NewDatastore(ctx, DatastoreConfig{Store: st, Key: "ep2"}); len(other.NodeList()) != 0 {
		t.Fatal("another key loaded the record")
	}
}

// TestDatastoreAttributes: each attribute reads from the tables, and a
// change advances the data version.
func TestDatastoreAttributes(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, newTrackingSync())
	v0 := d.MatterDataVersion()
	mustOK(t, d.AddPendingNode(ctx, 5, "n"))
	if d.MatterDataVersion() == v0 {
		t.Fatal("NodeList change did not advance the data version")
	}
	mustOK(t, d.SetStatus(ctx, stateCommitted, 99, 0))
	for _, id := range []uint32{dsdef.AttrAnchorRootCa, dsdef.AttrAnchorNodeId, dsdef.AttrAnchorVendorId, dsdef.AttrFriendlyName, dsdef.AttrGroupKeySetList, dsdef.AttrGroupList,
		dsdef.AttrNodeList, dsdef.AttrAdminList, dsdef.AttrStatus, dsdef.AttrEndpointGroupIdList, dsdef.AttrEndpointBindingList, dsdef.AttrNodeKeySetList, dsdef.AttrNodeAclList, dsdef.AttrNodeEndpointList} {
		if _, ok := d.MatterRead(id); !ok {
			t.Errorf("attribute %#x not served", id)
		}
	}
	if v, _ := d.MatterRead(dsdef.AttrNodeList); len(v.(spec.List[NodeEntry])) != 1 {
		t.Fatalf("NodeList = %v", v)
	}
	if v, _ := d.MatterRead(dsdef.AttrStatus); v.(StatusEntry).UpdateTimestamp != 99 {
		t.Fatalf("Status = %v", v)
	}
}

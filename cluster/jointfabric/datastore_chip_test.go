// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// JointFabricDatastore: a port of connectedhomeip
// src/app/server/tests/TestJointFabricDatastore.cpp at 6170af84 (26
// TEST), by meaning. chip's JointFabricDatastore is the Datastore's host
// methods; its TrackingDelegate (:45-198) is trackingSync, which answers
// each push at once and records the last one of each kind; its
// DummyListener (:36-43) is a NodeList change subscription. The span
// ownership cases (:256, :283, :325) hold here as "the datastore keeps
// its own copy of what it was given". The deferred-completion cases
// (:676-876) run the push in a goroutine whose port call blocks until the
// test releases it, while other operations run.
package jointfabric

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/contract"
)

// trackingSync is TrackingDelegate (:45-198).
type trackingSync struct {
	mu                    sync.Mutex
	endpointsToFetch      []EndpointEntry
	fetchedGroupKeySetIDs []uint16
	epochKey0, epochKey1  []byte

	fetchEndpointListCalls, fetchGroupKeySetListCalls, fetchGroupKeySetCalls, fetchACLListCalls int

	lastEndpointGroup *EndpointGroupIDEntry
	lastNodeKeySet    *NodeKeySetEntry
	lastBinding       *EndpointBindingEntry
	lastACL           *ACLEntry

	// defer* make the next push of the kind wait until released.
	deferBinding, deferACL bool
	deferred               chan struct{} // closed by release
	waiting                chan struct{} // closed when the deferred push waits
}

func newTrackingSync() *trackingSync {
	return &trackingSync{epochKey0: []byte{0x10, 0x11, 0x12}, epochKey1: []byte{0x20, 0x21}}
}

// deferNext arms the deferral of the next binding or ACL push.
func (s *trackingSync) deferNext(binding bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deferBinding, s.deferACL = binding, !binding
	s.deferred, s.waiting = make(chan struct{}), make(chan struct{})
}

func (s *trackingSync) wait(kind *bool) {
	s.mu.Lock()
	if !*kind {
		s.mu.Unlock()
		return
	}
	*kind = false
	deferred, waiting := s.deferred, s.waiting
	s.mu.Unlock()
	close(waiting)
	<-deferred
}

func (s *trackingSync) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastEndpointGroup, s.lastNodeKeySet, s.lastBinding, s.lastACL = nil, nil, nil, nil
}

func (s *trackingSync) SyncEndpointGroupID(_ context.Context, _ uint64, e EndpointGroupIDEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastEndpointGroup = &e
	return nil
}

func (s *trackingSync) SyncNodeKeySet(_ context.Context, _ uint64, e NodeKeySetEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastNodeKeySet = &e
	return nil
}

func (s *trackingSync) SyncBinding(_ context.Context, _ uint64, e EndpointBindingEntry) error {
	s.wait(&s.deferBinding)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastBinding = &e
	return nil
}

func (*trackingSync) SyncBindings(context.Context, uint64, []EndpointBindingEntry) error { return nil }

func (s *trackingSync) SyncACL(_ context.Context, _ uint64, e ACLEntry) error {
	s.wait(&s.deferACL)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastACL = &e
	return nil
}

func (*trackingSync) SyncACLs(context.Context, uint64, []ACLEntry) error         { return nil }
func (*trackingSync) SyncGroupKeySet(context.Context, uint64, GroupKeySet) error { return nil }
func (*trackingSync) FetchEndpointBindingList(context.Context, uint64, uint16) ([]EndpointBindingEntry, error) {
	return nil, nil
}

func (s *trackingSync) FetchEndpointList(context.Context, uint64) ([]EndpointEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fetchEndpointListCalls++
	return s.endpointsToFetch, nil
}

func (*trackingSync) FetchEndpointGroupList(context.Context, uint64, uint16) ([]GroupEntry, error) {
	return nil, nil
}

func (s *trackingSync) FetchGroupKeySetList(context.Context, uint64) ([]uint16, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fetchGroupKeySetListCalls++
	return s.fetchedGroupKeySetIDs, nil
}

func (s *trackingSync) FetchGroupKeySet(_ context.Context, _ uint64, id uint16) (GroupKeySet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fetchGroupKeySetCalls++
	return GroupKeySet{
		GroupKeySetId: id,
		EpochKey0:     spec.Nullable[[]byte]{Value: s.epochKey0},
		EpochKey1:     spec.Nullable[[]byte]{Value: s.epochKey1},
		EpochKey2:     spec.Nullable[[]byte]{Null: true},
	}, nil
}

func (s *trackingSync) FetchACLList(context.Context, uint64) ([]ACLEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fetchACLListCalls++
	return nil, nil
}

func newStore(t *testing.T, sync DatastoreSync) *Datastore {
	t.Helper()
	d, err := NewDatastore(t.Context(), DatastoreConfig{Sync: sync})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// nodeListChanges counts NodeList change reports — DummyListener (:36-43).
func nodeListChanges(d *Datastore) (count func() int, unsubscribe func()) {
	var mu sync.Mutex
	n := 0
	unsub := d.OnAttributeChange(func(c contract.Change) {
		if slices.Contains(c.Attributes, dsdef.AttrNodeList) {
			mu.Lock()
			n++
			mu.Unlock()
		}
	})
	return func() int { mu.Lock(); defer mu.Unlock(); return n }, unsub
}

func mustOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestAddPendingNodeNotifiesListener is :200-210.
func TestAddPendingNodeNotifiesListener(t *testing.T) {
	d := newStore(t, nil)
	count, unsub := nodeListChanges(d)
	defer unsub()
	mustOK(t, d.AddPendingNode(t.Context(), 123, "controller-a"))
	if count() == 0 {
		t.Fatal("NodeList change not reported")
	}
}

// TestRemoveListenerPreventsNotification is :212-224.
func TestRemoveListenerPreventsNotification(t *testing.T) {
	d := newStore(t, nil)
	count, unsub := nodeListChanges(d)
	unsub()
	mustOK(t, d.AddPendingNode(t.Context(), 456, "controller-b"))
	if count() != 0 {
		t.Fatal("an unsubscribed listener heard the change")
	}
}

// TestRefreshNonExistentNodeFails is :226-235.
func TestRefreshNonExistentNodeFails(t *testing.T) {
	d := newStore(t, newTrackingSync())
	if err := d.RefreshNode(t.Context(), 999); err == nil {
		t.Fatal("refresh of an unknown node succeeded")
	}
}

// TestUpdateNodeChangesFriendlyNameAndNotifiesListener is :237-254.
func TestUpdateNodeChangesFriendlyNameAndNotifiesListener(t *testing.T) {
	d := newStore(t, newTrackingSync())
	mustOK(t, d.AddPendingNode(t.Context(), 123, "original-name"))
	count, unsub := nodeListChanges(d)
	defer unsub()
	mustOK(t, d.UpdateNode(t.Context(), 123, "updated-name"))
	if count() == 0 {
		t.Fatal("NodeList change not reported")
	}
	if nodes := d.NodeList(); len(nodes) != 1 || nodes[0].FriendlyName != "updated-name" {
		t.Fatalf("NodeList = %+v", nodes)
	}
}

// TestAddGroupKeySetEntryOwnsSpanData is :256-281: the stored key set is
// the datastore's copy.
func TestAddGroupKeySetEntryOwnsSpanData(t *testing.T) {
	d := newStore(t, nil)
	k0, k1 := []byte{0x01, 0x02, 0x03}, []byte{0x11, 0x12}
	mustOK(t, d.AddGroupKeySetEntry(t.Context(), GroupKeySet{GroupKeySetId: 11, EpochKey0: spec.Nullable[[]byte]{Value: k0}, EpochKey1: spec.Nullable[[]byte]{Value: k1}, EpochKey2: spec.Nullable[[]byte]{Null: true}}))
	k0[0], k1[1] = 0xEE, 0xFF
	list := d.GroupKeySetList()
	if len(list) != 1 {
		t.Fatalf("GroupKeySetList = %+v", list)
	}
	if s := list[0]; string(s.EpochKey0.Value) != "\x01\x02\x03" || string(s.EpochKey1.Value) != "\x11\x12" || !s.EpochKey2.Null {
		t.Fatalf("stored key set %+v", s)
	}
}

// TestAddAndUpdateAdminOwnsFriendlyNameAndIcac is :283-323.
func TestAddAndUpdateAdminOwnsFriendlyNameAndIcac(t *testing.T) {
	d := newStore(t, newTrackingSync())
	icac := []byte{0x01, 0x02, 0x03}
	mustOK(t, d.AddAdmin(t.Context(), AdminEntry{NodeId: 100, VendorId: 55, FriendlyName: "admin-one", Icac: icac}))
	icac[0] = 0xAA
	if a := d.AdminList(); len(a) != 1 || a[0].FriendlyName != "admin-one" || string(a[0].Icac) != "\x01\x02\x03" {
		t.Fatalf("AdminList = %+v", a)
	}
	name, icac2 := "admin-two", []byte{0x0A, 0x0B}
	mustOK(t, d.UpdateAdmin(t.Context(), 100, &name, &icac2))
	icac2[0] = 0xCC
	if a := d.AdminList(); a[0].FriendlyName != "admin-two" || string(a[0].Icac) != "\x0A\x0B" {
		t.Fatalf("AdminList = %+v", a)
	}
}

// TestAddGroupAndUpdateEndpointOwnBufferBackedFriendlyNames is :325-357.
func TestAddGroupAndUpdateEndpointOwnBufferBackedFriendlyNames(t *testing.T) {
	d := newStore(t, newTrackingSync())
	mustOK(t, d.AddGroup(t.Context(), AddGroupRequest{GroupId: 10, FriendlyName: "living-room", GroupKeySetId: spec.Nullable[uint16]{Value: 99}, GroupPermission: dsdef.DatastoreAccessControlEntryPrivilegeView}))
	if g := d.GroupList(); len(g) != 1 || g[0].FriendlyName != "living-room" {
		t.Fatalf("GroupList = %+v", g)
	}
	d.addEndpointEntry(t.Context(), 2, 123, "switch-1")
	if e := d.NodeEndpointList(); len(e) != 1 || e[0].FriendlyName != "switch-1" {
		t.Fatalf("NodeEndpointList = %+v", e)
	}
	mustOK(t, d.UpdateEndpointForNode(t.Context(), 123, 2, "switch-main"))
	if e := d.NodeEndpointList(); e[0].FriendlyName != "switch-main" {
		t.Fatalf("NodeEndpointList = %+v", e)
	}
}

func nullU16() spec.Nullable[uint16] { return spec.Nullable[uint16]{Null: true} }

// updateGroupWithNull is :370-443: a field AddGroup stored null takes the
// UpdateGroup's value.
func updateGroupWithNull(t *testing.T, add AddGroupRequest, upd UpdateGroupRequest, check func(GroupEntry) bool) {
	t.Helper()
	d := newStore(t, newTrackingSync())
	mustOK(t, d.AddGroup(t.Context(), add))
	upd.GroupId, upd.FriendlyName, upd.GroupPermission = add.GroupId, spec.Nullable[string]{Null: true}, spec.Nullable[dsdef.DatastoreAccessControlEntryPrivilegeEnum]{Null: true}
	mustOK(t, d.UpdateGroup(t.Context(), upd))
	if g := d.GroupList(); len(g) != 1 || !check(g[0]) {
		t.Fatalf("GroupList = %+v", g)
	}
}

// TestUpdateGroupWithNullStoredGroupKeySetIDSucceeds is :370-393.
func TestUpdateGroupWithNullStoredGroupKeySetIDSucceeds(t *testing.T) {
	updateGroupWithNull(t,
		AddGroupRequest{GroupId: 10, FriendlyName: "group-a", GroupKeySetId: nullU16(), GroupCat: nullU16(), GroupCatVersion: nullU16(), GroupPermission: dsdef.DatastoreAccessControlEntryPrivilegeView},
		UpdateGroupRequest{GroupKeySetId: spec.Nullable[uint16]{Value: 5}, GroupCat: nullU16(), GroupCatVersion: nullU16()},
		func(g GroupEntry) bool { return !g.GroupKeySetId.Null && g.GroupKeySetId.Value == 5 })
}

// TestUpdateGroupWithNullStoredGroupCATSucceeds is :395-418.
func TestUpdateGroupWithNullStoredGroupCATSucceeds(t *testing.T) {
	updateGroupWithNull(t,
		AddGroupRequest{GroupId: 11, FriendlyName: "group-b", GroupKeySetId: nullU16(), GroupCat: nullU16(), GroupCatVersion: nullU16(), GroupPermission: dsdef.DatastoreAccessControlEntryPrivilegeView},
		UpdateGroupRequest{GroupKeySetId: nullU16(), GroupCat: spec.Nullable[uint16]{Value: 0x1234}, GroupCatVersion: nullU16()},
		func(g GroupEntry) bool { return !g.GroupCat.Null && g.GroupCat.Value == 0x1234 })
}

// TestUpdateGroupWithNullStoredGroupCATVersionSucceeds is :420-443.
func TestUpdateGroupWithNullStoredGroupCATVersionSucceeds(t *testing.T) {
	updateGroupWithNull(t,
		AddGroupRequest{GroupId: 12, FriendlyName: "group-c", GroupKeySetId: nullU16(), GroupCat: nullU16(), GroupCatVersion: nullU16(), GroupPermission: dsdef.DatastoreAccessControlEntryPrivilegeView},
		UpdateGroupRequest{GroupKeySetId: nullU16(), GroupCat: nullU16(), GroupCatVersion: spec.Nullable[uint16]{Value: 7}},
		func(g GroupEntry) bool { return !g.GroupCatVersion.Null && g.GroupCatVersion.Value == 7 })
}

// TestRefreshNodeFetchesGroupKeySetsAndCommitsNode is :445-476.
func TestRefreshNodeFetchesGroupKeySetsAndCommitsNode(t *testing.T) {
	s := newTrackingSync()
	s.fetchedGroupKeySetIDs = []uint16{77}
	d := newStore(t, s)
	mustOK(t, d.AddPendingNode(t.Context(), 123, "controller-a"))
	mustOK(t, d.RefreshNode(t.Context(), 123))
	if s.fetchEndpointListCalls != 1 || s.fetchGroupKeySetListCalls != 1 || s.fetchGroupKeySetCalls != 1 || s.fetchACLListCalls != 1 {
		t.Fatalf("fetches %d %d %d %d", s.fetchEndpointListCalls, s.fetchGroupKeySetListCalls, s.fetchGroupKeySetCalls, s.fetchACLListCalls)
	}
	s.epochKey0[0], s.epochKey1[0] = 0xAA, 0xBB
	list := d.GroupKeySetList()
	if len(list) != 1 || list[0].GroupKeySetId != 77 || string(list[0].EpochKey0.Value) != "\x10\x11\x12" || string(list[0].EpochKey1.Value) != "\x20\x21" {
		t.Fatalf("GroupKeySetList = %+v", list)
	}
	if n := d.NodeList(); len(n) != 1 || n[0].CommissioningStatusEntry.State != stateCommitted {
		t.Fatalf("NodeList = %+v", n)
	}
}

// groupOnEndpoint is the setup of :478-583: node 123, endpoint 1, group 10
// with key set 55.
func groupOnEndpoint(t *testing.T) (*Datastore, *trackingSync) {
	t.Helper()
	s := newTrackingSync()
	d := newStore(t, s)
	mustOK(t, d.AddPendingNode(t.Context(), 123, "controller-a"))
	d.addEndpointEntry(t.Context(), 1, 123, "endpoint-a")
	mustOK(t, d.AddGroup(t.Context(), AddGroupRequest{GroupId: 10, FriendlyName: "group-a", GroupKeySetId: spec.Nullable[uint16]{Value: 55}, GroupCat: nullU16(), GroupCatVersion: nullU16(), GroupPermission: dsdef.DatastoreAccessControlEntryPrivilegeView}))
	mustOK(t, d.AddGroupIDToEndpointForNode(t.Context(), 123, 1, 10))
	return d, s
}

// TestAddGroupIDToEndpointForNodeAddsMissingNodeKeySet is :478-514.
func TestAddGroupIDToEndpointForNodeAddsMissingNodeKeySet(t *testing.T) {
	d, s := groupOnEndpoint(t)
	if len(d.EndpointGroupIDList()) != 1 {
		t.Fatalf("EndpointGroupIDList = %+v", d.EndpointGroupIDList())
	}
	nks := d.NodeKeySetList()
	if len(nks) != 1 || nks[0].NodeId != 123 || nks[0].GroupKeySetId != 55 || nks[0].StatusEntry.State != statePending {
		t.Fatalf("NodeKeySetList = %+v", nks)
	}
	if e := s.lastNodeKeySet; e == nil || e.NodeId != 123 || e.GroupKeySetId != 55 || e.StatusEntry.State != statePending {
		t.Fatalf("last node key set push %+v", e)
	}
	if e := s.lastEndpointGroup; e == nil || e.NodeId != 123 || e.EndpointId != 1 || e.GroupId != 10 || e.StatusEntry.State != statePending {
		t.Fatalf("last endpoint group push %+v", e)
	}
}

// TestAddGroupIDToEndpointForNodeIsIdempotentForDuplicateAssignments is
// :516-544.
func TestAddGroupIDToEndpointForNodeIsIdempotentForDuplicateAssignments(t *testing.T) {
	d, s := groupOnEndpoint(t)
	s.reset()
	mustOK(t, d.AddGroupIDToEndpointForNode(t.Context(), 123, 1, 10))
	if len(d.EndpointGroupIDList()) != 1 || len(d.NodeKeySetList()) != 1 || s.lastEndpointGroup != nil || s.lastNodeKeySet != nil {
		t.Fatalf("a duplicate assignment changed or pushed: %+v %+v", d.EndpointGroupIDList(), d.NodeKeySetList())
	}
}

// TestRemoveGroupIdFromEndpointSyncsDeletePendingEntries is :546-583.
func TestRemoveGroupIdFromEndpointSyncsDeletePendingEntries(t *testing.T) {
	d, s := groupOnEndpoint(t)
	s.reset()
	mustOK(t, d.RemoveGroupIDFromEndpointForNode(t.Context(), 123, 1, 10))
	if e := s.lastEndpointGroup; e == nil || e.NodeId != 123 || e.EndpointId != 1 || e.GroupId != 10 || e.StatusEntry.State != stateDeletePending {
		t.Fatalf("last endpoint group push %+v", e)
	}
	if e := s.lastNodeKeySet; e == nil || e.NodeId != 123 || e.GroupKeySetId != 55 || e.StatusEntry.State != stateDeletePending {
		t.Fatalf("last node key set push %+v", e)
	}
	if len(d.EndpointGroupIDList()) != 0 || len(d.NodeKeySetList()) != 0 {
		t.Fatalf("entries left: %+v %+v", d.EndpointGroupIDList(), d.NodeKeySetList())
	}
}

func ptr[T any](v T) *T { return &v }

func nodeWithEndpoint(t *testing.T, s *trackingSync) *Datastore {
	t.Helper()
	d := newStore(t, s)
	mustOK(t, d.AddPendingNode(t.Context(), 123, "controller-a"))
	d.addEndpointEntry(t.Context(), 1, 123, "endpoint-a")
	return d
}

// TestAddBindingAssignsListIdAndStoresCommittedEntry is :585-608.
func TestAddBindingAssignsListIdAndStoresCommittedEntry(t *testing.T) {
	s := newTrackingSync()
	d := nodeWithEndpoint(t, s)
	mustOK(t, d.AddBindingToEndpointForNode(t.Context(), 123, 1, BindingTarget{Node: ptr[uint64](0x1111), Endpoint: ptr[uint16](2)}))
	if s.lastBinding == nil {
		t.Fatal("binding not pushed")
	}
	b := d.EndpointBindingList()
	if len(b) != 1 || b[0].NodeId != 123 || b[0].EndpointId != 1 || b[0].Binding.Node == nil || *b[0].Binding.Node != 0x1111 || b[0].StatusEntry.State != stateCommitted {
		t.Fatalf("EndpointBindingList = %+v", b)
	}
}

// TestRemoveBindingSyncsDeletePendingPayload is :610-635.
func TestRemoveBindingSyncsDeletePendingPayload(t *testing.T) {
	s := newTrackingSync()
	d := nodeWithEndpoint(t, s)
	mustOK(t, d.AddBindingToEndpointForNode(t.Context(), 123, 1, BindingTarget{Group: ptr[uint16](10)}))
	listID := d.EndpointBindingList()[0].ListId
	s.reset()
	mustOK(t, d.RemoveBindingFromEndpointForNode(t.Context(), listID, 123, 1))
	if e := s.lastBinding; e == nil || e.NodeId != 123 || e.EndpointId != 1 || e.ListId != listID || e.StatusEntry.State != stateDeletePending {
		t.Fatalf("last binding push %+v", e)
	}
	if len(d.EndpointBindingList()) != 0 {
		t.Fatalf("EndpointBindingList = %+v", d.EndpointBindingList())
	}
}

var viewCASE = AccessControlEntry{Privilege: dsdef.DatastoreAccessControlEntryPrivilegeView, AuthMode: dsdef.DatastoreAccessControlEntryAuthModeCase, Subjects: spec.Nullable[[]uint64]{Null: true}, Targets: spec.Nullable[[]AccessControlTarget]{Null: true}}

// TestAddAclDeduplicatesAndRemoveAclSyncsDeletePayload is :637-666.
func TestAddAclDeduplicatesAndRemoveAclSyncsDeletePayload(t *testing.T) {
	s := newTrackingSync()
	d := newStore(t, s)
	mustOK(t, d.AddPendingNode(t.Context(), 123, "controller-a"))
	mustOK(t, d.AddACLToNode(t.Context(), 123, viewCASE))
	acls := d.NodeACLList()
	if len(acls) != 1 || acls[0].StatusEntry.State != stateCommitted {
		t.Fatalf("NodeACLList = %+v", acls)
	}
	listID := acls[0].ListId
	s.reset()
	mustOK(t, d.AddACLToNode(t.Context(), 123, viewCASE))
	if len(d.NodeACLList()) != 1 || s.lastACL != nil {
		t.Fatal("a duplicate ACL entry was added or pushed")
	}
	mustOK(t, d.RemoveACLFromNode(t.Context(), listID, 123))
	if e := s.lastACL; e == nil || e.NodeId != 123 || e.ListId != listID || e.StatusEntry.State != stateDeletePending {
		t.Fatalf("last ACL push %+v", e)
	}
	if len(d.NodeACLList()) != 0 {
		t.Fatalf("NodeACLList = %+v", d.NodeACLList())
	}
}

// deferred runs op with its push held at the port until the returned
// release is called; it waits until the push is held.
func deferred(t *testing.T, s *trackingSync, binding bool, op func() error) (release func()) {
	t.Helper()
	s.deferNext(binding)
	done := make(chan error, 1)
	go func() { done <- op() }()
	<-s.waiting
	return func() {
		close(s.deferred)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

// TestRemoveBindingIteratorUseAfterFree is :708-749: the removal's commit
// finds the entry by its key after other bindings were added meanwhile.
func TestRemoveBindingIteratorUseAfterFree(t *testing.T) {
	s := newTrackingSync()
	d := nodeWithEndpoint(t, s)
	mustOK(t, d.AddBindingToEndpointForNode(t.Context(), 123, 1, BindingTarget{Node: ptr[uint64](0x1000), Endpoint: ptr[uint16](2)}))
	listID0 := d.EndpointBindingList()[0].ListId
	release := deferred(t, s, true, func() error { return d.RemoveBindingFromEndpointForNode(context.Background(), listID0, 123, 1) })
	for i := uint64(1); i <= 12; i++ {
		mustOK(t, d.AddBindingToEndpointForNode(t.Context(), 123, 1, BindingTarget{Node: ptr(0x2000 + i), Endpoint: ptr[uint16](2)}))
	}
	release()
	for _, e := range d.EndpointBindingList() {
		if e.ListId == listID0 {
			t.Fatalf("the removed binding %d is still listed", listID0)
		}
	}
}

// TestRemoveAclIteratorUseAfterFree is :787-827.
func TestRemoveAclIteratorUseAfterFree(t *testing.T) {
	s := newTrackingSync()
	d := newStore(t, s)
	mustOK(t, d.AddPendingNode(t.Context(), 123, "controller-a"))
	mustOK(t, d.AddACLToNode(t.Context(), 123, viewCASE))
	listID0 := d.NodeACLList()[0].ListId
	release := deferred(t, s, false, func() error { return d.RemoveACLFromNode(context.Background(), listID0, 123) })
	for i := uint64(1); i <= 12; i++ {
		mustOK(t, d.AddPendingNode(t.Context(), 200+i, "controller-b"))
		mustOK(t, d.AddACLToNode(t.Context(), 200+i, viewCASE))
	}
	release()
	for _, e := range d.NodeACLList() {
		if e.NodeId == 123 && e.ListId == listID0 {
			t.Fatalf("the removed ACL entry %d is still listed", listID0)
		}
	}
}

// TestAddBindingBackReferenceMarksWrongEntry is :835-876: the deferred
// commit marks the binding that was pushed, not the newest one.
func TestAddBindingBackReferenceMarksWrongEntry(t *testing.T) {
	s := newTrackingSync()
	d := nodeWithEndpoint(t, s)
	release := deferred(t, s, true, func() error {
		return d.AddBindingToEndpointForNode(context.Background(), 123, 1, BindingTarget{Node: ptr[uint64](0x1000), Endpoint: ptr[uint16](2)})
	})
	listID1 := d.EndpointBindingList()[0].ListId
	for i := uint64(2); i <= 13; i++ {
		mustOK(t, d.AddBindingToEndpointForNode(t.Context(), 123, 1, BindingTarget{Node: ptr(0x2000 + i), Endpoint: ptr[uint16](2)}))
	}
	release()
	found := false
	for _, e := range d.EndpointBindingList() {
		if e.ListId == listID1 {
			found = true
			if e.StatusEntry.State != stateCommitted {
				t.Fatalf("binding %d is %d, want Committed", listID1, e.StatusEntry.State)
			}
		}
	}
	if !found {
		t.Fatalf("binding %d not listed", listID1)
	}
}

// TestOnFabricRemovedWipesDatastoreOnlyForAnchorFabric is :880-941.
func TestOnFabricRemovedWipesDatastoreOnlyForAnchorFabric(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, newTrackingSync())
	mustOK(t, d.SetAnchorFabricIndex(ctx, 1))
	mustOK(t, d.SetAnchorNodeID(ctx, 0xABCD))
	mustOK(t, d.AddGroupKeySetEntry(ctx, GroupKeySet{GroupKeySetId: 11, EpochKey0: spec.Nullable[[]byte]{Value: []byte{1, 2, 3}}, EpochKey1: spec.Nullable[[]byte]{Null: true}, EpochKey2: spec.Nullable[[]byte]{Null: true}}))
	mustOK(t, d.AddAdmin(ctx, AdminEntry{NodeId: 100, VendorId: 7, FriendlyName: "admin", Icac: []byte{0x0A, 0x0B}}))
	mustOK(t, d.AddPendingNode(ctx, 123, "controller"))
	d.addEndpointEntry(ctx, 1, 123, "endpoint")
	mustOK(t, d.AddBindingToEndpointForNode(ctx, 123, 1, BindingTarget{Group: ptr[uint16](10)}))
	mustOK(t, d.AddACLToNode(ctx, 123, viewCASE))
	counts := func() [6]int {
		return [6]int{len(d.GroupKeySetList()), len(d.AdminList()), len(d.NodeList()), len(d.NodeACLList()), len(d.EndpointBindingList()), len(d.NodeEndpointList())}
	}
	d.OnFabricRemoved(ctx, 2)
	if c := counts(); c != [6]int{1, 1, 1, 1, 1, 1} || d.AnchorFabricIndex() != 1 {
		t.Fatalf("another fabric's removal changed the datastore: %v", c)
	}
	d.OnFabricRemoved(ctx, 1)
	if c := counts(); c != [6]int{} || d.AnchorFabricIndex() != 0 || d.AnchorNodeID() != 0 {
		t.Fatalf("the anchor fabric's removal left %v, anchor %d/%d", c, d.AnchorFabricIndex(), d.AnchorNodeID())
	}
}

// fakeEntry is the minimal entry of :947-968.
type fakeEntry struct {
	key    int
	status StatusEntry
}

func fakeStatus(e *fakeEntry) *StatusEntry { return &e.status }

func pending(keys ...int) []fakeEntry {
	out := make([]fakeEntry, len(keys))
	for i, k := range keys {
		out[i] = fakeEntry{key: k, status: StatusEntry{State: statePending}}
	}
	return out
}

func matchKey(k int) func(fakeEntry) bool { return func(e fakeEntry) bool { return e.key == k } }

// TestMarkEntryCommittedIfFound is the five MarkEntryCommittedIfFoundTest
// cases (:970-1020).
func TestMarkEntryCommittedIfFound(t *testing.T) {
	states := func(es []fakeEntry) []State {
		out := make([]State, len(es))
		for i, e := range es {
			out[i] = e.status.State
		}
		return out
	}
	eq := func(a, b []State) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	t.Run("MarksMatchingEntryCommitted", func(t *testing.T) { // :970-977
		es := pending(1)
		markCommitted(es, fakeStatus, matchKey(1))
		if !eq(states(es), []State{stateCommitted}) {
			t.Fatal(states(es))
		}
	})
	t.Run("LeavesNonMatchingEntriesUntouched", func(t *testing.T) { // :979-988
		es := pending(1, 2, 3)
		markCommitted(es, fakeStatus, matchKey(2))
		if !eq(states(es), []State{statePending, stateCommitted, statePending}) {
			t.Fatal(states(es))
		}
	})
	t.Run("NoMatchIsNoOp", func(t *testing.T) { // :990-998
		es := pending(1, 2)
		if markCommitted(es, fakeStatus, matchKey(99)) || !eq(states(es), []State{statePending, statePending}) {
			t.Fatal(states(es))
		}
	})
	t.Run("EmptyVectorIsNoOp", func(t *testing.T) { // :1000-1008
		if markCommitted([]fakeEntry(nil), fakeStatus, matchKey(1)) {
			t.Fatal("an empty list matched")
		}
	})
	t.Run("MarksOnlyFirstMatch", func(t *testing.T) { // :1010-1020
		es := pending(7, 7)
		markCommitted(es, fakeStatus, matchKey(7))
		if !eq(states(es), []State{stateCommitted, statePending}) {
			t.Fatal(states(es))
		}
	})
}

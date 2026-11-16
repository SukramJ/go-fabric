// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

// peerOf is a request of a node of the fabric over CASE.
func peerOf(fabric uint8) context.Context {
	return im.WithSubject(im.WithFabricFilter(context.Background(), true, fabric), 0x77, nil)
}

// TestDatastoreCommandsAreTheAnchorFabrics (R2-P6-sec-1103, owner
// decision 129): every command of a request of a fabric other than the
// anchor fabric, or of any fabric while none is recorded, answers
// UNSUPPORTED_ACCESS before it runs; the anchor fabric's and the host's
// calls run. The loop over the whole command table is the guard for a
// command added later.
func TestDatastoreCommandsAreTheAnchorFabrics(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, &refreshSync{trackingSync: *newTrackingSync()})
	for id := range d.commandTable() {
		if _, err := d.MatterInvoke(peerOf(2), id, nil); statusOf(err) != im.StatusUnsupportedAccess {
			t.Errorf("command 0x%02X without an anchor: %v", id, err)
		}
	}
	mustOK(t, d.SetAnchorFabricIndex(ctx, 1))
	for id := range d.commandTable() {
		if _, err := d.MatterInvoke(peerOf(2), id, nil); statusOf(err) != im.StatusUnsupportedAccess {
			t.Errorf("command 0x%02X of fabric 2: %v", id, err)
		}
	}
	_, err := d.MatterInvoke(peerOf(2), dsdef.CmdAddPendingNode, dsdef.AddPendingNodeRequest{NodeId: 9, FriendlyName: "theirs"})
	if statusOf(err) != im.StatusUnsupportedAccess || len(d.NodeList()) != 0 {
		t.Fatalf("AddPendingNode of fabric 2: %v, nodes %v", err, d.NodeList())
	}
	if _, err := d.MatterInvoke(peerOf(1), dsdef.CmdAddPendingNode, dsdef.AddPendingNodeRequest{NodeId: 9, FriendlyName: "ours"}); err != nil {
		t.Fatalf("AddPendingNode of the anchor fabric: %v", err)
	}
	if _, err := d.MatterInvoke(ctx, dsdef.CmdAddPendingNode, dsdef.AddPendingNodeRequest{NodeId: 10, FriendlyName: "host"}); err != nil {
		t.Fatalf("AddPendingNode of the host: %v", err)
	}
	if n := len(d.NodeList()); n != 2 {
		t.Fatalf("%d nodes", n)
	}
}

// keySetSync reports key sets of the refreshed node.
type keySetSync struct {
	*refreshSync
	ids    []uint16
	keySet func(id uint16) GroupKeySet
	onEPs  func()
}

func (p *keySetSync) FetchGroupKeySetList(context.Context, uint64) ([]uint16, error) {
	return p.ids, nil
}

func (p *keySetSync) FetchGroupKeySet(_ context.Context, _ uint64, id uint16) (GroupKeySet, error) {
	return p.keySet(id), nil
}

func (p *keySetSync) FetchEndpointList(ctx context.Context, n uint64) ([]EndpointEntry, error) {
	if p.onEPs != nil {
		p.onEPs()
	}
	return p.refreshSync.FetchEndpointList(ctx, n)
}

// TestRefreshKeepsTheDatastoresKeys (R2-P6-sec-1106, owner decision 129):
// a refresh replaces none of the datastore's keys — a KeySetRead's null
// keys erase nothing, a node's own key substitutes nothing — adopts no
// key set the datastore does not hold, and enrols the node as no
// recipient of a key set on its own say-so (NodeKeySetList unchanged).
func TestRefreshKeepsTheDatastoresKeys(t *testing.T) {
	ctx := t.Context()
	null := spec.Nullable[[]byte]{Null: true}
	p := &keySetSync{refreshSync: &refreshSync{trackingSync: *newTrackingSync()}, ids: []uint16{5, 600}}
	p.keySet = func(id uint16) GroupKeySet {
		if id == 5 {
			return GroupKeySet{GroupKeySetId: 5, EpochKey0: null, EpochKey1: null, EpochKey2: null}
		}
		return GroupKeySet{GroupKeySetId: id, EpochKey0: spec.Nullable[[]byte]{Value: []byte("node-chosen-key!")}, EpochKey1: null, EpochKey2: null}
	}
	d := newStore(t, p)
	key := make([]byte, 16)
	key[0] = 0xAA
	mustOK(t, d.AddGroupKeySetEntry(ctx, GroupKeySet{GroupKeySetId: 5, GroupKeySecurityPolicy: dsdef.DatastoreGroupKeySecurityPolicyTrustFirst, EpochKey0: spec.Nullable[[]byte]{Value: key}}))
	mustOK(t, d.AddPendingNode(ctx, 7, "n"))
	mustOK(t, d.RefreshNode(ctx, 7))
	list := d.GroupKeySetList()
	if len(list) != 1 || list[0].GroupKeySetId != 5 || list[0].EpochKey0.Null || list[0].EpochKey0.Value[0] != 0xAA {
		t.Fatalf("GroupKeySetList after the refresh: %+v", list)
	}
	if nks := d.NodeKeySetList(); len(nks) != 0 {
		t.Fatalf("NodeKeySetList after the refresh: %+v", nks)
	}
}

// TestRefreshAfterTheWipe (R2-P6-conc-1107): the anchor fabric's removal
// while a refresh runs ends the refresh; nothing it fetched lands in the
// wiped tables.
func TestRefreshAfterTheWipe(t *testing.T) {
	ctx := t.Context()
	p := &keySetSync{refreshSync: &refreshSync{trackingSync: *newTrackingSync()}, ids: []uint16{7}}
	p.keySet = func(id uint16) GroupKeySet {
		return GroupKeySet{GroupKeySetId: id, EpochKey0: spec.Nullable[[]byte]{Value: []byte("0123456789abcdef")}}
	}
	p.endpointsToFetch = []EndpointEntry{{EndpointId: 1}}
	p.acls = []ACLEntry{{ListId: 40, AclEntry: viewCASE}}
	d := newStore(t, p)
	mustOK(t, d.SetAnchorFabricIndex(ctx, 1))
	mustOK(t, d.AddPendingNode(ctx, 5, "n"))
	p.onEPs = func() { mustOK(t, d.MatterDropFabric(ctx, 1)) }
	if err := d.RefreshNode(ctx, 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RefreshNode across the wipe: %v", err)
	}
	if len(d.NodeList())+len(d.NodeEndpointList())+len(d.NodeACLList())+len(d.GroupKeySetList()) != 0 {
		t.Fatalf("after the wipe: nodes %d endpoints %d ACLs %d key sets %d",
			len(d.NodeList()), len(d.NodeEndpointList()), len(d.NodeACLList()), len(d.GroupKeySetList()))
	}
}

// TestDatastoreIsFabricScopedState (R2-P6-store-1114): the datastore holds
// the anchor fabric's state and drops it through the node's one removal
// path, the reconcile's included.
func TestDatastoreIsFabricScopedState(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, nil)
	var s contract.FabricScopedState = d
	if held := s.MatterHeldFabrics(); len(held) != 0 {
		t.Fatalf("held %v without an anchor", held)
	}
	mustOK(t, d.SetAnchorFabricIndex(ctx, 3))
	mustOK(t, d.AddPendingNode(ctx, 9, "n"))
	if held := s.MatterHeldFabrics(); len(held) != 1 || held[0] != 3 {
		t.Fatalf("held %v", held)
	}
	mustOK(t, s.MatterDropFabric(ctx, 4))
	if len(d.NodeList()) != 1 {
		t.Fatal("another fabric's removal wiped the datastore")
	}
	mustOK(t, s.MatterDropFabric(ctx, 3))
	if len(d.NodeList()) != 0 || len(s.MatterHeldFabrics()) != 0 {
		t.Fatal("the anchor fabric's removal left the datastore")
	}
}

// TestAdministratorFabricIndexAfterRemoval (R2-P6-fsm-1104): the anchor
// fabric's removal sets AdministratorFabricIndex null, and the next
// anchor fabric is taken at its commissioning.
func TestAdministratorFabricIndexAfterRemoval(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, nil)
	fx := newAdminFixture(t, AdministratorConfig{Datastore: d})
	anchor := newJFFabric(t, 4, adminCAT, anchorCAT)
	fx.a.AttachNode(&fakeFabrics{
		fabrics: []store.FabricRecord{fx.f.record(), anchor.record()},
		ids:     map[uint8]store.IdentityRecord{3: {NOC: fx.f.noc}, 4: {NOC: anchor.noc}},
	}, fx.window)
	fx.a.OnCommissioningComplete(ctx)
	if got := fx.a.administratorFabricIndex(); got != 4 {
		t.Fatalf("AdministratorFabricIndex %d", got)
	}
	fx.a.OnFabricRemoved(ctx, 4)
	mustOK(t, d.MatterDropFabric(ctx, 4))
	if got := fx.a.administratorFabricIndex(); got != 0 {
		t.Fatalf("AdministratorFabricIndex %d after its fabric's removal", got)
	}
	next := newJFFabric(t, 5, adminCAT, anchorCAT)
	fx.a.AttachNode(&fakeFabrics{
		fabrics: []store.FabricRecord{fx.f.record(), next.record()},
		ids:     map[uint8]store.IdentityRecord{3: {NOC: fx.f.noc}, 5: {NOC: next.noc}},
	}, fx.window)
	fx.a.OnCommissioningComplete(ctx)
	if got := fx.a.administratorFabricIndex(); got != 5 || d.AnchorFabricIndex() != 5 {
		t.Fatalf("after the next anchor: AdministratorFabricIndex %d, datastore anchor %d", got, d.AnchorFabricIndex())
	}
	// Another fabric's removal leaves the attribute.
	fx.a.OnFabricRemoved(ctx, 3)
	if got := fx.a.administratorFabricIndex(); got != 5 {
		t.Fatalf("AdministratorFabricIndex %d after fabric 3's removal", got)
	}
}

// TestAdministratorFabricIndexAfterRestart (R2-P6-store-1105): after a
// restart the attribute is the datastore's persisted anchor once the node
// is attached and its fabric table holds that fabric; null when it does
// not; without a datastore, the fabric of the two CATs.
func TestAdministratorFabricIndexAfterRestart(t *testing.T) {
	ctx := t.Context()
	st := &memStore{}
	d, err := NewDatastore(ctx, DatastoreConfig{Store: st, Key: "dev"})
	mustOK(t, err)
	fx := newAdminFixture(t, AdministratorConfig{Datastore: d})
	anchor := newJFFabric(t, 4, adminCAT, anchorCAT)
	table := &fakeFabrics{
		fabrics: []store.FabricRecord{fx.f.record(), anchor.record()},
		ids:     map[uint8]store.IdentityRecord{3: {NOC: fx.f.noc}, 4: {NOC: anchor.noc}},
	}
	fx.a.AttachNode(table, fx.window)
	fx.a.OnCommissioningComplete(ctx)
	if got := fx.a.administratorFabricIndex(); got != 4 {
		t.Fatalf("before the restart: %d", got)
	}
	d2, err := NewDatastore(ctx, DatastoreConfig{Store: st, Key: "dev"})
	mustOK(t, err)
	a2, err := NewAdministrator(AdministratorConfig{Datastore: d2})
	mustOK(t, err)
	a2.AttachNode(table, fx.window)
	if got := a2.administratorFabricIndex(); got != 4 {
		t.Fatalf("after the restart: AdministratorFabricIndex %d, datastore anchor %d", got, d2.AnchorFabricIndex())
	}
	a3, err := NewAdministrator(AdministratorConfig{Datastore: d2})
	mustOK(t, err)
	a3.AttachNode(&fakeFabrics{fabrics: []store.FabricRecord{fx.f.record()}, ids: map[uint8]store.IdentityRecord{3: {NOC: fx.f.noc}}}, fx.window)
	if got := a3.administratorFabricIndex(); got != 0 {
		t.Fatalf("the anchor fabric gone: AdministratorFabricIndex %d", got)
	}
	a4, err := NewAdministrator(AdministratorConfig{})
	mustOK(t, err)
	a4.AttachNode(table, fx.window)
	if got := a4.administratorFabricIndex(); got != 4 {
		t.Fatalf("without a datastore: AdministratorFabricIndex %d", got)
	}
}

// TestRemovalReachesTheDatastoreIdempotently (R2-P6-err-1120 a): the
// administrator hands the removal on (a host outside the node reaches the
// datastore so), and the node's second call, as a FabricScopedState,
// changes nothing: no error, no data version.
func TestRemovalReachesTheDatastoreIdempotently(t *testing.T) {
	ctx := t.Context()
	st := &switchStore{}
	d, err := NewDatastore(ctx, DatastoreConfig{Store: st, Key: "dev"})
	mustOK(t, err)
	mustOK(t, d.SetAnchorFabricIndex(ctx, 4))
	mustOK(t, d.AddPendingNode(ctx, 9, "n"))
	fx := newAdminFixture(t, AdministratorConfig{Datastore: d})
	fx.a.OnFabricRemoved(ctx, 4)
	if len(d.NodeList()) != 0 || d.AnchorFabricIndex() != 0 {
		t.Fatal("the administrator's removal left the datastore")
	}
	dv := d.MatterDataVersion()
	st.fmu.Lock()
	st.fail = true // a second save would fail: there is none
	st.fmu.Unlock()
	mustOK(t, d.MatterDropFabric(ctx, 4))
	if d.MatterDataVersion() != dv {
		t.Fatal("the second removal changed the datastore")
	}
}

// switchStore refuses a save once fail is set.
type switchStore struct {
	memStore
	fmu  sync.Mutex
	fail bool
}

func (f *switchStore) SetSetting(ctx context.Context, key, value string) error {
	f.fmu.Lock()
	fail := f.fail
	f.fmu.Unlock()
	if fail {
		return errors.New("disk full")
	}
	return f.memStore.SetSetting(ctx, key, value)
}

// TestRefreshEndsOnARefusedSave (R2-P6-err-1120 b): a store that refuses
// a refresh step's save ends the refresh with ErrStore, not a silent
// success.
func TestRefreshEndsOnARefusedSave(t *testing.T) {
	ctx := t.Context()
	r := &refreshSync{trackingSync: *newTrackingSync()}
	r.endpointsToFetch = []EndpointEntry{{EndpointId: 1}}
	st := &switchStore{}
	d, err := NewDatastore(ctx, DatastoreConfig{Sync: r, Store: st, Key: "dev"})
	mustOK(t, err)
	mustOK(t, d.AddPendingNode(ctx, 5, "n"))
	st.fmu.Lock()
	st.fail = true
	st.fmu.Unlock()
	if err := d.runRefresh(ctx, 5); !errors.Is(err, ErrStore) {
		t.Fatalf("refresh over a refusing store: %v", err)
	}
}

// TestAddGroupAndMergesAreBounded (R2-P6-bound-1113): AddGroup holds
// GroupList to MaxGroups; a refresh whose node lists more ACL entries
// than MaxACLs ends with ErrNoMemory before it writes a list to the node;
// a datastore whose ACL table is full of other nodes' entries still
// refreshes a further node within its own list.
func TestAddGroupAndMergesAreBounded(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, nil)
	for i := range MaxGroups {
		mustOK(t, d.AddGroup(ctx, AddGroupRequest{GroupId: uint16(i + 1)}))
	}
	if err := d.AddGroup(ctx, AddGroupRequest{GroupId: MaxGroups + 1}); !errors.Is(err, ErrNoMemory) {
		t.Fatalf("AddGroup past MaxGroups: %v", err)
	}
	r := &refreshSync{trackingSync: *newTrackingSync()}
	for i := range MaxACLs + 1 {
		r.acls = append(r.acls, ACLEntry{ListId: uint16(i), AclEntry: viewCASE})
	}
	d2 := newStore(t, r)
	mustOK(t, d2.AddPendingNode(ctx, 5, "n"))
	if err := d2.RefreshNode(ctx, 5); !errors.Is(err, ErrNoMemory) {
		t.Fatalf("refresh past MaxACLs: %v", err)
	}
	if len(d2.NodeACLList()) != 0 || r.syncedACLs != nil {
		t.Fatalf("ACLs %d, pushed %d", len(d2.NodeACLList()), len(r.syncedACLs))
	}
	r3 := &refreshSync{trackingSync: *newTrackingSync()}
	r3.acls = []ACLEntry{{ListId: 1000, AclEntry: viewCASE}}
	d3 := newStore(t, r3)
	for i := range MaxACLs {
		d3.t.ACLs = append(d3.t.ACLs, aclEntry{NodeID: 100 + uint64(i), ListID: uint16(i), StatusEntry: StatusEntry{State: stateCommitted}})
	}
	mustOK(t, d3.AddPendingNode(ctx, 5, "n"))
	mustOK(t, d3.RefreshNode(ctx, 5))
	if n := len(d3.NodeACLList()); n != MaxACLs+1 {
		t.Fatalf("%d ACL entries after the refresh of a further node", n)
	}
	if nodes := d3.NodeList(); len(nodes) != 1 || nodes[0].CommissioningStatusEntry.State != stateCommitted {
		t.Fatalf("the further node: %+v", nodes)
	}
}

// TestNextListIDAtTheTop (R2-P6-bound-1116): with an entry at 0xFFFF the
// next list id is the lowest free one, never an id an entry holds.
func TestNextListIDAtTheTop(t *testing.T) {
	d := newStore(t, nil)
	d.t.Bindings = []EndpointBindingEntry{{ListId: 0}, {ListId: 0xFFFF}}
	if got := d.nextListIDLocked(); got != 1 {
		t.Fatalf("next list id %d", got)
	}
	d.t.ACLs = []aclEntry{{ListID: 1}}
	if got := d.nextListIDLocked(); got != 2 {
		t.Fatalf("next list id %d", got)
	}
	d.t.Bindings, d.t.ACLs = nil, nil
	if got := d.nextListIDLocked(); got != 0 {
		t.Fatalf("next list id of empty lists %d", got)
	}
}

// TestAccessingIsCASE (R2-P6-sec-1111): a group message has a fabric and
// is not PASE, and is not CASE either.
func TestAccessingIsCASE(t *testing.T) {
	base := im.WithFabricFilter(context.Background(), true, 3)
	for name, tc := range map[string]struct {
		ctx  context.Context
		want bool
	}{
		"CASE":       {im.WithSubject(base, 0x77, nil), true},
		"PASE":       {im.WithAuthModePASE(im.WithSubject(base, 0x77, nil)), false},
		"group":      {im.WithGroupSubject(base, im.GroupSubject{GroupID: 1}), false},
		"no subject": {base, false},
		"no fabric":  {im.WithSubject(context.Background(), 0x77, nil), false},
	} {
		if _, got := accessing(tc.ctx); got != tc.want {
			t.Errorf("%s: CASE %v", name, got)
		}
	}
}

// parkingReach parks the n-th ReadAdministratorFabricIndex until its
// release closes, whatever its context.
type parkingReach struct {
	*fakeReach
	mu       sync.Mutex
	calls    int
	entered  []chan struct{}
	released []chan struct{}
}

func (r *parkingReach) ReadAdministratorFabricIndex(ctx context.Context, p CommissionerPeer, ep uint16) (fabricIndex uint8, null bool, err error) {
	r.mu.Lock()
	n := r.calls
	r.calls++
	r.mu.Unlock()
	if n < len(r.entered) {
		close(r.entered[n])
		<-r.released[n]
	}
	return r.fakeReach.ReadAdministratorFabricIndex(ctx, p, ep)
}

// TestAnnouncementClearsOnlyItsOwn (R2-P6-conc-1112 a): after the
// fail-safe's expiry ended a first announcement and a second began, the
// first one's end does not cancel the second.
func TestAnnouncementClearsOnlyItsOwn(t *testing.T) {
	fx := newAdminFixture(t, AdministratorConfig{})
	r := &parkingReach{
		fakeReach: &fakeReach{f: fx.f, adminIndex: 3},
		entered:   []chan struct{}{make(chan struct{}), make(chan struct{})},
		released:  []chan struct{}{make(chan struct{}), make(chan struct{})},
	}
	fx.a.reach = r
	first, second := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := fx.a.MatterInvoke(caseCtx(context.Background(), 3), AdministratorCmdAnnounce, announce(1))
		first <- err
	}()
	<-r.entered[0]
	fx.a.OnFailSafeExpired(context.Background())
	go func() {
		_, err := fx.a.MatterInvoke(caseCtx(context.Background(), 3), AdministratorCmdAnnounce, announce(1))
		second <- err
	}()
	<-r.entered[1]
	close(r.released[0])
	if err := <-first; statusOf(err) != im.StatusFailure {
		t.Errorf("the announcement the expiry ended: %v", err)
	}
	close(r.released[1])
	if err := <-second; err != nil {
		t.Fatalf("the second announcement: %v", err)
	}
	if fx.a.VIDVerifiedFabric() != 3 {
		t.Fatalf("VID verified %d", fx.a.VIDVerifiedFabric())
	}
}

// TestDatastoreReadsAreTheAnchorFabrics (R2-P6-sec-1103 reads, owner
// decision 129, option (b)): a request of another fabric, or of any
// fabric while no anchor is recorded, reads no epoch key — every list
// empty, every other attribute zero; the anchor fabric and the host read
// the tables.
func TestDatastoreReadsAreTheAnchorFabrics(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, nil)
	key := make([]byte, 16)
	key[0] = 0xAA
	mustOK(t, d.AddGroupKeySetEntry(ctx, GroupKeySet{GroupKeySetId: 5, GroupKeySecurityPolicy: dsdef.DatastoreGroupKeySecurityPolicyTrustFirst, EpochKey0: spec.Nullable[[]byte]{Value: key}}))
	mustOK(t, d.SetAnchorRootCA(ctx, []byte{1, 2, 3}))
	keySets := func(ctx context.Context) int {
		v, ok := d.MatterReadFiltered(ctx, dsdef.AttrGroupKeySetList)
		if !ok {
			t.Fatal("GroupKeySetList not read")
		}
		return len(v.(spec.List[GroupKeySet]))
	}
	if n := keySets(peerOf(2)); n != 0 {
		t.Fatalf("without an anchor fabric 2 read %d key sets", n)
	}
	mustOK(t, d.SetAnchorFabricIndex(ctx, 1))
	if n := keySets(peerOf(2)); n != 0 {
		t.Fatalf("fabric 2 read %d key sets", n)
	}
	for id := range attrIDs() {
		v, ok := d.MatterReadFiltered(peerOf(2), id)
		if !ok || v == nil {
			t.Errorf("attribute 0x%04X of fabric 2: %v %v", id, v, ok)
		}
	}
	if v, _ := d.MatterReadFiltered(peerOf(2), dsdef.AttrAnchorRootCa); len(v.([]byte)) != 0 {
		t.Fatalf("fabric 2 read AnchorRootCA %X", v)
	}
	if n := keySets(peerOf(1)); n != 1 {
		t.Fatalf("the anchor fabric read %d key sets", n)
	}
	if n := keySets(ctx); n != 1 {
		t.Fatalf("the host read %d key sets", n)
	}
}

// attrIDs are the cluster's 14 attributes.
func attrIDs() map[uint32]bool {
	return map[uint32]bool{
		dsdef.AttrAnchorRootCa: true, dsdef.AttrAnchorNodeId: true, dsdef.AttrAnchorVendorId: true, dsdef.AttrFriendlyName: true,
		dsdef.AttrGroupKeySetList: true, dsdef.AttrGroupList: true, dsdef.AttrNodeList: true, dsdef.AttrAdminList: true,
		dsdef.AttrStatus: true, dsdef.AttrEndpointGroupIdList: true, dsdef.AttrEndpointBindingList: true,
		dsdef.AttrNodeKeySetList: true, dsdef.AttrNodeAclList: true, dsdef.AttrNodeEndpointList: true,
	}
}

// TestAnchorIsRecordedBeforeTheAttribute (administrator, round 2): with a
// datastore that records no anchor, the fabric of the two CATs is recorded
// in the datastore and then set as AdministratorFabricIndex — at
// AttachNode too; a refused save leaves both unset, and the next
// AttachNode records it.
func TestAnchorIsRecordedBeforeTheAttribute(t *testing.T) {
	ctx := t.Context()
	st := &switchStore{}
	d, err := NewDatastore(ctx, DatastoreConfig{Store: st, Key: "dev"})
	mustOK(t, err)
	fx := newAdminFixture(t, AdministratorConfig{Datastore: d})
	anchor := newJFFabric(t, 4, adminCAT, anchorCAT)
	table := &fakeFabrics{
		fabrics: []store.FabricRecord{fx.f.record(), anchor.record()},
		ids:     map[uint8]store.IdentityRecord{3: {NOC: fx.f.noc}, 4: {NOC: anchor.noc}},
	}
	st.fmu.Lock()
	st.fail = true
	st.fmu.Unlock()
	fx.a.AttachNode(table, fx.window)
	fx.a.OnCommissioningComplete(ctx)
	if got := fx.a.administratorFabricIndex(); got != 0 || d.AnchorFabricIndex() != 0 {
		t.Fatalf("a refused save: AdministratorFabricIndex %d, datastore anchor %d", got, d.AnchorFabricIndex())
	}
	st.fmu.Lock()
	st.fail = false
	st.fmu.Unlock()
	fx.a.AttachNode(table, fx.window)
	if got := fx.a.administratorFabricIndex(); got != 4 || d.AnchorFabricIndex() != 4 || d.AnchorNodeID() != anchor.nodeID {
		t.Fatalf("at AttachNode: AdministratorFabricIndex %d, datastore anchor %d", got, d.AnchorFabricIndex())
	}
}

// TestAnchoredDatastoreIsNeverRepointed (administrator, round 2): a
// datastore that records an anchor keeps it: AdministratorFabricIndex
// follows it while the fabric table holds it, and stays null when it does
// not — another fabric whose NOC carries the two CATs takes nothing.
func TestAnchoredDatastoreIsNeverRepointed(t *testing.T) {
	ctx := t.Context()
	d := newStore(t, nil)
	mustOK(t, d.SetAnchorFabricIndex(ctx, 4))
	fx := newAdminFixture(t, AdministratorConfig{Datastore: d})
	old := newJFFabric(t, 4, adminCAT, anchorCAT)
	other := newJFFabric(t, 5, adminCAT, anchorCAT)
	fx.a.AttachNode(&fakeFabrics{
		fabrics: []store.FabricRecord{fx.f.record(), other.record(), old.record()},
		ids:     map[uint8]store.IdentityRecord{3: {NOC: fx.f.noc}, 4: {NOC: old.noc}, 5: {NOC: other.noc}},
	}, fx.window)
	fx.a.OnCommissioningComplete(ctx)
	if got := fx.a.administratorFabricIndex(); got != 4 || d.AnchorFabricIndex() != 4 {
		t.Fatalf("anchor held: AdministratorFabricIndex %d, datastore anchor %d", got, d.AnchorFabricIndex())
	}
	a2, err := NewAdministrator(AdministratorConfig{Datastore: d})
	mustOK(t, err)
	a2.AttachNode(&fakeFabrics{
		fabrics: []store.FabricRecord{fx.f.record(), other.record()},
		ids:     map[uint8]store.IdentityRecord{3: {NOC: fx.f.noc}, 5: {NOC: other.noc}},
	}, fx.window)
	a2.OnCommissioningComplete(ctx)
	if got := a2.administratorFabricIndex(); got != 0 || d.AnchorFabricIndex() != 4 {
		t.Fatalf("anchor gone: AdministratorFabricIndex %d, datastore anchor %d", got, d.AnchorFabricIndex())
	}
}

// TestFailedWipeStaysWiped (R2-P6-conc-1107 round 2): a wipe whose save
// fails keeps the tables wiped in memory; the next change saves the empty
// record.
func TestFailedWipeStaysWiped(t *testing.T) {
	ctx := t.Context()
	st := &switchStore{}
	d, err := NewDatastore(ctx, DatastoreConfig{Store: st, Key: "dev"})
	mustOK(t, err)
	mustOK(t, d.SetAnchorFabricIndex(ctx, 4))
	mustOK(t, d.AddPendingNode(ctx, 9, "n"))
	st.fmu.Lock()
	st.fail = true
	st.fmu.Unlock()
	if err := d.MatterDropFabric(ctx, 4); !errors.Is(err, ErrStore) {
		t.Fatalf("wipe over a refusing store: %v", err)
	}
	if len(d.NodeList()) != 0 || d.AnchorFabricIndex() != 0 {
		t.Fatal("the refused save restored the wiped tables")
	}
	st.fmu.Lock()
	st.fail = false
	st.fmu.Unlock()
	mustOK(t, d.SetFriendlyName(ctx, "x"))
	d2, err := NewDatastore(ctx, DatastoreConfig{Store: st, Key: "dev"})
	mustOK(t, err)
	if len(d2.NodeList()) != 0 || d2.AnchorFabricIndex() != 0 {
		t.Fatal("the next change did not save the wipe")
	}
}

// wipingSync wipes the datastore in its first SyncGroupKeySet and counts
// the calls.
type wipingSync struct {
	*trackingSync
	d     *Datastore
	calls int
}

func (w *wipingSync) SyncGroupKeySet(ctx context.Context, _ uint64, _ GroupKeySet) error {
	w.calls++
	if w.calls == 1 {
		_ = w.d.MatterDropFabric(ctx, 1)
	}
	return nil
}

// TestPushAfterTheWipeCallsNoPort (R2-P6-conc-1107 round 2): a push
// collected before a wipe carries the removed fabric's key set to no node.
func TestPushAfterTheWipeCallsNoPort(t *testing.T) {
	ctx := t.Context()
	w := &wipingSync{trackingSync: newTrackingSync()}
	d := newStore(t, w)
	w.d = d
	mustOK(t, d.SetAnchorFabricIndex(ctx, 1))
	key := make([]byte, 16)
	mustOK(t, d.AddGroupKeySetEntry(ctx, GroupKeySet{GroupKeySetId: 5, GroupKeySecurityPolicy: dsdef.DatastoreGroupKeySecurityPolicyTrustFirst, EpochKey0: spec.Nullable[[]byte]{Value: key}}))
	d.mu.Lock()
	d.t.NodeKeySets = []NodeKeySetEntry{{NodeId: 7, GroupKeySetId: 5}, {NodeId: 8, GroupKeySetId: 5}}
	d.mu.Unlock()
	key2 := make([]byte, 16)
	key2[0] = 1
	_ = d.UpdateGroupKeySetEntry(ctx, GroupKeySet{GroupKeySetId: 5, GroupKeySecurityPolicy: dsdef.DatastoreGroupKeySecurityPolicyTrustFirst, EpochKey0: spec.Nullable[[]byte]{Value: key2}})
	if w.calls != 1 {
		t.Fatalf("%d SyncGroupKeySet calls; the push after the wipe reached the port", w.calls)
	}
}

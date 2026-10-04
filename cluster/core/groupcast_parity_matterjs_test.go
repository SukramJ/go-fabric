// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

// Behavioural parity for the Groupcast cluster and the AccessControl
// Auxiliary feature against matter.js HEAD
// packages/node/src/behaviors/groupcast/GroupcastServer.ts and
// packages/node/src/behaviors/access-control/AccessControlServer.ts, as
// matter.js's own packages/node/test/behaviors/groupcast/
// GroupcastServerTest.ts exercises them. Each case names what it pins.

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	matterparity "github.com/SukramJ/go-fabric/parity"
	mstore "github.com/SukramJ/go-fabric/store"
)

// gcEvent is one event a cluster emitted.
type gcEvent struct {
	cluster, event uint32
	data           any
}

type gcRecordingEmitter struct {
	mu     sync.Mutex
	events []gcEvent
}

func (r *gcRecordingEmitter) MatterEmitEvent(_ uint16, cluster, event uint32, data any, _ contract.EventPriority) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, gcEvent{cluster, event, data})
}

func (r *gcRecordingEmitter) take() []gcEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

type testingTimer struct {
	d       time.Duration
	fire    func()
	stopped bool
}

// gcFixture is a root with GroupKeyManagement, AccessControl, Groupcast and
// a Groups server on endpoint 3, over fabrics 1..n.
type gcFixture struct {
	fs      *fakeStore
	mgr     *groups.Manager
	gkm     *core.GroupKeyManagement
	acl     *core.AccessControl
	gc      *core.Groupcast
	ep3     *core.Groups
	emitter *gcRecordingEmitter
	timers  []*testingTimer
}

const gcAdminNode uint64 = 0x0000_0000_0000_0077

func newGCFixture(t *testing.T, fabrics int) *gcFixture {
	t.Helper()
	ctx := context.Background()
	f := &gcFixture{fs: newFakeStore(), emitter: &gcRecordingEmitter{}}
	for i := range fabrics {
		if _, err := f.fs.AddFabric(ctx, mstore.FabricRecord{FabricID: uint64(0x0FA0 + i), CompressedID: [8]byte{byte(i), 2, 3, 4, 5, 6, 7, 8}}); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	if f.mgr, err = groups.NewManager(f.fs, nil); err != nil {
		t.Fatal(err)
	}
	if f.gkm, err = core.NewGroupKeyManagement(f.fs, core.GroupKeyMgmtConfig{Groups: f.mgr}); err != nil {
		t.Fatal(err)
	}
	if f.acl, err = core.NewAccessControl(f.fs); err != nil {
		t.Fatal(err)
	}
	f.acl.SetMatterEventEmitter(f.emitter)
	if f.gc, err = core.NewGroupcast(core.GroupcastConfig{
		Groups: f.mgr, GroupKeyManagement: f.gkm, AccessControl: f.acl,
		TestingTimer: func(d time.Duration, fire func()) func() {
			tm := &testingTimer{d: d, fire: fire}
			f.timers = append(f.timers, tm)
			return func() { tm.stopped = true }
		},
	}); err != nil {
		t.Fatal(err)
	}
	f.gc.SetMatterEventEmitter(f.emitter)
	if f.ep3, err = core.NewGroups(3, f.mgr, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.gc.Close)
	return f
}

// as is a CASE request of fabric holding privilege on the Groupcast server.
func as(fabric uint8, privilege uint8) context.Context {
	ctx := im.WithSubject(im.WithFabricFilter(context.Background(), true, fabric), gcAdminNode, nil)
	return im.WithAuthority(ctx, func(_ uint16, _ uint32, need uint8) im.StatusCode {
		if need <= privilege {
			return im.StatusSuccess
		}
		return im.StatusUnsupportedAccess
	})
}

func statusOf(err error) im.StatusCode {
	if err == nil {
		return im.StatusSuccess
	}
	var sce im.StatusCodeError
	if errors.As(err, &sce) {
		return sce.MatterStatusCode()
	}
	return im.StatusFailure
}

func (f *gcFixture) invoke(ctx context.Context, cmd uint32, fields any) (any, im.StatusCode) {
	resp, err := f.gc.MatterInvoke(ctx, cmd, fields)
	return resp, statusOf(err)
}

var testKey = make([]byte, 16)

func boolp(v bool) *bool    { return &v }
func u8p(v uint8) *uint8    { return &v }
func u16p(v uint16) *uint16 { return &v }

func (f *gcFixture) join(t *testing.T, ctx context.Context, req core.JoinGroupRequest) {
	t.Helper()
	if _, st := f.invoke(ctx, 0x00, req); st != im.StatusSuccess {
		t.Fatalf("JoinGroup %+v: %v", req, st)
	}
}

func (f *gcFixture) membership(t *testing.T) []core.GroupcastMembershipStruct {
	t.Helper()
	v, ok := f.gc.MatterRead(0x0000)
	if !ok {
		t.Fatal("Membership unreadable")
	}
	return v.([]core.GroupcastMembershipStruct)
}

func (f *gcFixture) memberOf(t *testing.T, fabric uint8, gid uint16) (core.GroupcastMembershipStruct, bool) {
	t.Helper()
	for _, m := range f.membership(t) {
		if m.FabricIndex == fabric && m.GroupID == gid {
			return m, true
		}
	}
	return core.GroupcastMembershipStruct{}, false
}

func TestGroupcastJoinGroupParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)

	t.Run("adds a membership entry on the IANA address with its key", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey, McastAddrPolicy: u8p(groups.PolicyIanaAddr)})
		m, ok := f.memberOf(t, 1, 1)
		if !ok || *m.KeySetID != 1 || m.McastAddrPolicy != groups.PolicyIanaAddr || !slices.Equal(m.Endpoints, []uint16{3}) || m.HasAuxiliaryACL {
			t.Fatalf("membership = %+v", m)
		}
		if addr, _ := f.mgr.MulticastAddressFor(context.Background(), 1, 1); !addr.Equal(net.ParseIP("ff05::fa")) {
			t.Errorf("multicast address %s, want ff05::fa", addr)
		}
		ks, err := f.fs.GetGroupKeySet(context.Background(), 1, 1)
		if err != nil || ks.EpochStart0 != 1 || ks.SecurityPolicy != mstore.SecurityPolicyTrustFirst {
			t.Errorf("created key set = %+v, %v; want TrustFirst with EpochStartTime0 1 (createKeySetForGroupcast)", ks, err)
		}
		if ok, _ := f.mgr.HasKeyMapping(context.Background(), 1, 1); !ok {
			t.Error("GroupKeyMap does not bind the group (#setGroupKeyMapping)")
		}
	})

	t.Run("McastAddrPolicy defaults to IanaAddr; PerGroup uses the group address", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})
		f.join(t, admin, core.JoinGroupRequest{GroupID: 2, Endpoints: []uint16{3}, KeySetID: 1, McastAddrPolicy: u8p(groups.PolicyPerGroup)})
		if m, _ := f.memberOf(t, 1, 1); m.McastAddrPolicy != groups.PolicyIanaAddr {
			t.Errorf("omitted policy = %d, want IanaAddr", m.McastAddrPolicy)
		}
		if addr, _ := f.mgr.MulticastAddressFor(context.Background(), 1, 2); !addr.Equal(groups.MulticastAddress(0x0FA0, 2)) {
			t.Errorf("PerGroup address = %s", addr)
		}
		if v, _ := f.gc.MatterRead(0x0003); v != uint16(2) {
			t.Errorf("UsedMcastAddrCount = %v, want 2 (one IANA, one per-group)", v)
		}
	})

	t.Run("rejects invalid group ids, KeySetId 0, a short key and an unknown policy", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		for name, req := range map[string]core.JoinGroupRequest{
			"GroupId 0":      {GroupID: 0, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey},
			"GroupId 0xFFF8": {GroupID: 0xFFF8, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey},
			"KeySetId 0":     {GroupID: 1, Endpoints: []uint16{3}, KeySetID: 0},
			"15-byte key":    {GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: make([]byte, 15)},
			"policy 2":       {GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey, McastAddrPolicy: u8p(2)},
			"no endpoints":   {GroupID: 1, Endpoints: []uint16{}, KeySetID: 1, Key: testKey},
		} {
			if _, st := f.invoke(admin, 0x00, req); st != im.StatusConstraintError {
				t.Errorf("%s: %v, want ConstraintError", name, st)
			}
		}
		for _, ep := range []uint16{0, 0xFFFF} {
			if _, st := f.invoke(admin, 0x00, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{ep}, KeySetID: 1, Key: testKey}); st != im.StatusUnsupportedEndpoint {
				t.Errorf("endpoint %d: %v, want UnsupportedEndpoint", ep, st)
			}
		}
		if len(f.membership(t)) != 0 {
			t.Error("a rejected join left a membership")
		}
	})

	t.Run("KeySetId must exist without a key, must not with one", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		if _, st := f.invoke(admin, 0x00, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 99}); st != im.StatusNotFound {
			t.Errorf("missing key set: %v, want NotFound", st)
		}
		f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})
		if _, st := f.invoke(admin, 0x00, core.JoinGroupRequest{GroupID: 2, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey}); st != im.StatusAlreadyExists {
			t.Errorf("existing key set with a key: %v, want AlreadyExists", st)
		}
	})

	t.Run("requires Admin for a key or UseAuxiliaryAcl", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		manage := as(1, 4)
		if _, st := f.invoke(manage, 0x00, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey}); st != im.StatusUnsupportedAccess {
			t.Errorf("key at Manage: %v, want UnsupportedAccess", st)
		}
		f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})
		if _, st := f.invoke(manage, 0x00, core.JoinGroupRequest{GroupID: 2, Endpoints: []uint16{3}, KeySetID: 1, UseAuxiliaryACL: boolp(false)}); st != im.StatusUnsupportedAccess {
			t.Errorf("UseAuxiliaryAcl at Manage: %v, want UnsupportedAccess", st)
		}
		f.join(t, manage, core.JoinGroupRequest{GroupID: 2, Endpoints: []uint16{3}, KeySetID: 1})
		if _, st := f.invoke(im.WithFabricFilter(context.Background(), true, 1), 0x00, core.JoinGroupRequest{GroupID: 3, Endpoints: []uint16{3}, KeySetID: 2, Key: testKey}); st != im.StatusUnsupportedAccess {
			t.Errorf("no access check at all: %v, want UnsupportedAccess", st)
		}
		if _, st := f.invoke(context.Background(), 0x00, core.JoinGroupRequest{GroupID: 3, Endpoints: []uint16{3}, KeySetID: 1}); st != im.StatusUnsupportedAccess {
			t.Errorf("no accessing fabric: %v, want UnsupportedAccess", st)
		}
	})

	t.Run("merges endpoints, or replaces them with ReplaceEndpoints", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3, 4}, KeySetID: 1, Key: testKey, UseAuxiliaryACL: boolp(true)})
		f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{5}, KeySetID: 1})
		if m, _ := f.memberOf(t, 1, 1); !slices.Equal(m.Endpoints, []uint16{3, 4, 5}) || !m.HasAuxiliaryACL {
			t.Fatalf("merge = %+v (a plain re-join must keep HasAuxiliaryAcl)", m)
		}
		f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{6, 4}, KeySetID: 1, ReplaceEndpoints: boolp(true)})
		m, _ := f.memberOf(t, 1, 1)
		if !slices.Equal(m.Endpoints, []uint16{4, 6}) || !m.HasAuxiliaryACL {
			t.Errorf("replace = %+v, want [4 6] with the flag kept", m)
		}
		f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{7}, KeySetID: 1, ReplaceEndpoints: boolp(true)})
		if m, _ := f.memberOf(t, 1, 1); !slices.Equal(m.Endpoints, []uint16{7}) || !m.HasAuxiliaryACL {
			t.Errorf("a disjoint replace = %+v, want [7] with the flag kept", m)
		}
	})

	t.Run("per-fabric quota, total limit, and no key installed by a refused join", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 3)
		for fabric := uint8(1); fabric <= 2; fabric++ {
			ctx := as(fabric, 5)
			f.join(t, ctx, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})
			for gid := uint16(2); gid <= 22; gid++ {
				f.join(t, ctx, core.JoinGroupRequest{GroupID: gid, Endpoints: []uint16{3}, KeySetID: 1})
			}
		}
		if _, st := f.invoke(as(1, 5), 0x00, core.JoinGroupRequest{GroupID: 23, Endpoints: []uint16{3}, KeySetID: 1}); st != im.StatusResourceExhausted {
			t.Errorf("23rd group of a fabric: %v, want ResourceExhausted", st)
		}
		if _, st := f.invoke(as(3, 5), 0x00, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey}); st != im.StatusResourceExhausted {
			t.Errorf("45th membership: %v, want ResourceExhausted", st)
		}
		if _, err := f.fs.GetGroupKeySet(context.Background(), 3, 1); !errors.Is(err, mstore.ErrGroupKeySetNotFound) {
			t.Error("the refused join installed its key")
		}
		f.join(t, as(1, 5), core.JoinGroupRequest{GroupID: 22, Endpoints: []uint16{4}, KeySetID: 1})
	})

	t.Run("a group id outside the application range fails the GroupKeyMap binding", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		if _, st := f.invoke(admin, 0x00, core.JoinGroupRequest{GroupID: 0xFF00, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey}); st != im.StatusInvalidAction {
			t.Errorf("GroupId 0xFF00: %v, want InvalidAction (#validateGroupKeyMap)", st)
		}
		if len(f.membership(t)) != 0 {
			t.Error("the refused join left a membership behind")
		}
	})
}

func TestGroupcastLeaveGroupParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)
	setup := func(t *testing.T) *gcFixture {
		t.Helper()
		f := newGCFixture(t, 1)
		f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3, 4}, KeySetID: 1, Key: testKey, UseAuxiliaryACL: boolp(true)})
		f.join(t, admin, core.JoinGroupRequest{GroupID: 2, Endpoints: []uint16{3, 5}, KeySetID: 1})
		return f
	}

	t.Run("removes the entry and answers its endpoints", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		resp, st := f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 1})
		r := resp.(core.LeaveGroupResponse)
		if st != im.StatusSuccess || r.GroupID != 1 || !slices.Equal(r.Endpoints, []uint16{3, 4}) {
			t.Fatalf("LeaveGroup = %+v, %v", r, st)
		}
		if _, ok := f.memberOf(t, 1, 1); ok {
			t.Error("still a member")
		}
		if ok, _ := f.mgr.HasKeyMapping(context.Background(), 1, 1); ok {
			t.Error("the GroupKeyMap binding survived (#removeGroupKeyMappings)")
		}
	})

	t.Run("removes only the listed endpoints, and the group with the last", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		resp, _ := f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 1, Endpoints: []uint16{4, 9}, HasEndpoints: true})
		if r := resp.(core.LeaveGroupResponse); !slices.Equal(r.Endpoints, []uint16{4}) {
			t.Errorf("removed %v, want [4]", r.Endpoints)
		}
		if m, _ := f.memberOf(t, 1, 1); !slices.Equal(m.Endpoints, []uint16{3}) {
			t.Errorf("remaining %v", m.Endpoints)
		}
		f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 1, Endpoints: []uint16{3}, HasEndpoints: true})
		if _, ok := f.memberOf(t, 1, 1); ok {
			t.Error("a group without endpoints stayed a member — without the Sender feature it is removed")
		}
	})

	t.Run("GroupID 0 applies its Endpoints to every group of the fabric (0d30528a)", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		resp, st := f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 0, Endpoints: []uint16{3}, HasEndpoints: true})
		if r := resp.(core.LeaveGroupResponse); st != im.StatusSuccess || r.GroupID != 0 || len(r.Endpoints) != 0 {
			t.Fatalf("wildcard response = %+v, %v", r, st)
		}
		m1, _ := f.memberOf(t, 1, 1)
		m2, _ := f.memberOf(t, 1, 2)
		if !slices.Equal(m1.Endpoints, []uint16{4}) || !slices.Equal(m2.Endpoints, []uint16{5}) {
			t.Errorf("after the wildcard leave: %v / %v, want [4] / [5]", m1.Endpoints, m2.Endpoints)
		}
		f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 0})
		if len(f.membership(t)) != 0 {
			t.Error("GroupID 0 without endpoints left groups behind")
		}
		if _, st := f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 0}); st != im.StatusNotFound {
			t.Errorf("wildcard without groups: %v, want NotFound", st)
		}
	})

	t.Run("unknown group, and an empty or long endpoint list", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		if _, st := f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 7}); st != im.StatusNotFound {
			t.Errorf("unknown group: %v", st)
		}
		if _, st := f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 1, HasEndpoints: true, Endpoints: []uint16{}}); st != im.StatusConstraintError {
			t.Errorf("empty Endpoints: %v, want ConstraintError (1 to 20)", st)
		}
		if _, st := f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 1, HasEndpoints: true, Endpoints: make([]uint16, 21)}); st != im.StatusConstraintError {
			t.Errorf("21 Endpoints: %v, want ConstraintError", st)
		}
	})

	t.Run("another fabric's group is not found", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		if _, err := f.fs.AddFabric(context.Background(), mstore.FabricRecord{FabricID: 0xBEEF}); err != nil {
			t.Fatal(err)
		}
		if _, st := f.invoke(as(2, 5), 0x01, core.LeaveGroupRequest{GroupID: 1}); st != im.StatusNotFound {
			t.Errorf("fabric 2 leaving fabric 1's group: %v, want NotFound", st)
		}
	})
}

func TestGroupcastUpdateKeyAndAuxiliaryACLParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)
	f := newGCFixture(t, 1)
	f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})

	if _, st := f.invoke(admin, 0x03, core.UpdateGroupKeyRequest{GroupID: 1, KeySetID: 2, Key: testKey}); st != im.StatusSuccess {
		t.Fatalf("UpdateGroupKey: %v", st)
	}
	if m, _ := f.memberOf(t, 1, 1); *m.KeySetID != 2 {
		t.Errorf("KeySetId = %d, want 2", *m.KeySetID)
	}
	if _, st := f.invoke(admin, 0x03, core.UpdateGroupKeyRequest{GroupID: 9, KeySetID: 1}); st != im.StatusNotFound {
		t.Errorf("unknown group: %v", st)
	}
	if _, st := f.invoke(as(1, 4), 0x03, core.UpdateGroupKeyRequest{GroupID: 1, KeySetID: 3, Key: testKey}); st != im.StatusUnsupportedAccess {
		t.Errorf("key at Manage: %v", st)
	}
	if _, st := f.invoke(admin, 0x03, core.UpdateGroupKeyRequest{GroupID: 1, KeySetID: 0}); st != im.StatusConstraintError {
		t.Errorf("KeySetId 0: %v", st)
	}

	if _, st := f.invoke(admin, 0x04, core.ConfigureAuxiliaryACLRequest{GroupID: 1, UseAuxiliaryACL: true}); st != im.StatusSuccess {
		t.Fatalf("ConfigureAuxiliaryAcl: %v", st)
	}
	if m, _ := f.memberOf(t, 1, 1); !m.HasAuxiliaryACL {
		t.Error("flag not set")
	}
	if _, st := f.invoke(admin, 0x04, core.ConfigureAuxiliaryACLRequest{GroupID: 9}); st != im.StatusNotFound {
		t.Errorf("unknown group: %v", st)
	}

	// A legacy group keeps the PerGroup default when the flag is set
	// ("keeps the feature-aware mcastAddrPolicy default on a legacy-only
	// group").
	if _, err := f.gkm.MatterInvoke(admin, 0x00, core.KeySetWriteRequest{GroupKeySet: core.GroupKeySetStruct{GroupKeySetID: 7, EpochKey0: testKey, EpochStartTime0: 5}}); err != nil {
		t.Fatal(err)
	}
	if err := f.gkm.MatterWrite(admin, 0, []core.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 2}, {GroupID: 0x10, GroupKeySetID: 7}}); err != nil {
		t.Fatal(err)
	}
	if resp, _ := f.ep3.MatterInvoke(admin, 0x00, core.AddGroupRequest{GroupID: 0x10}); resp.(core.AddGroupResponse).Status != im.StatusSuccess {
		t.Fatal("AddGroup failed")
	}
	legacy, ok := f.memberOf(t, 1, 0x10)
	if !ok || legacy.McastAddrPolicy != groups.PolicyPerGroup || legacy.HasAuxiliaryACL || *legacy.KeySetID != 7 {
		t.Fatalf("a Groups-cluster group in Membership = %+v, want PerGroup defaults", legacy)
	}
	f.invoke(admin, 0x04, core.ConfigureAuxiliaryACLRequest{GroupID: 0x10, UseAuxiliaryACL: true})
	if m, _ := f.memberOf(t, 1, 0x10); m.McastAddrPolicy != groups.PolicyPerGroup || !m.HasAuxiliaryACL {
		t.Errorf("legacy group after ConfigureAuxiliaryAcl = %+v", m)
	}
	// A legacy RemoveAllGroups empties the group: it leaves Membership
	// (kDeleteGroupIfEmpty).
	if _, err := f.ep3.MatterInvoke(admin, 0x04, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.membership(t)) != 0 {
		t.Errorf("after RemoveAllGroups: %+v", f.membership(t))
	}
}

func TestGroupcastTestingParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)
	f := newGCFixture(t, 2)
	f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey, McastAddrPolicy: u8p(groups.PolicyPerGroup)})
	var notified int
	unsub := f.gc.OnMatterValueChanged(func() { notified++ })
	defer unsub()

	for name, req := range map[string]core.GroupcastTestingRequest{
		"EnableSenderTesting without SD": {TestOperation: 2},
		"unknown operation":              {TestOperation: 3},
		"9 seconds":                      {TestOperation: 1, DurationSeconds: u16p(9)},
		"1201 seconds":                   {TestOperation: 1, DurationSeconds: u16p(1201)},
	} {
		if _, st := f.invoke(admin, 0x05, req); st != im.StatusConstraintError {
			t.Errorf("%s: %v, want ConstraintError", name, st)
		}
	}

	// Testing ends after 60 s when the request omits the duration (452d6f5c).
	if _, st := f.invoke(admin, 0x05, core.GroupcastTestingRequest{TestOperation: 1}); st != im.StatusSuccess {
		t.Fatal(st)
	}
	if v, _ := f.gc.MatterRead(0x0004); v != uint8(1) || notified == 0 {
		t.Fatalf("FabricUnderTest = %v (notified %d), want 1", v, notified)
	}
	if len(f.timers) != 1 || f.timers[0].d != 60*time.Second {
		t.Fatalf("timers = %+v, want one of 60 s", f.timers)
	}
	f.emitter.take()

	report := func(ev groups.GroupMessageEvent) []core.GroupcastTestingEvent {
		f.mgr.ReportGroupMessage(ev)
		var out []core.GroupcastTestingEvent
		for _, e := range f.emitter.take() {
			if e.cluster == 0x0065 && e.event == 0 {
				out = append(out, e.data.(core.GroupcastTestingEvent))
			}
		}
		return out
	}
	src := net.ParseIP("fe80::1")
	allowed := true
	got := report(groups.GroupMessageEvent{
		Result: groups.TestResultSuccess, FabricIndex: 1, GroupID: 1, HasGroupID: true, SourceIP: src,
		HasPath: true, HasEndpoint: true, EndpointID: 3, ClusterID: 6, ElementID: 2, AccessAllowed: &allowed,
	})
	if len(got) != 1 {
		t.Fatalf("events = %+v", got)
	}
	ev := got[0]
	if !slices.Equal(ev.SourceIPAddress, src.To16()) || !slices.Equal(ev.DestinationIPAddress, groups.MulticastAddress(0x0FA0, 1).To16()) ||
		*ev.GroupID != 1 || *ev.EndpointID != 3 || *ev.ClusterID != 6 || *ev.ElementID != 2 || !*ev.AccessAllowed || ev.FabricIndex != 1 || ev.GroupcastTestResult != 0 {
		t.Errorf("Success event = %+v", ev)
	}
	// Another fabric's message is not reported; an unauthenticated one is,
	// on the fabric under test, from the IANA address for an unknown group.
	if got := report(groups.GroupMessageEvent{Result: groups.TestResultMessageReplay, FabricIndex: 2, GroupID: 1, HasGroupID: true}); len(got) != 0 {
		t.Errorf("another fabric's message reported: %+v", got)
	}
	got = report(groups.GroupMessageEvent{Result: groups.TestResultFailedAuth, HeaderGroupID: 9, HasHeaderGroupID: true, SourceIP: net.IPv4(10, 0, 0, 1)})
	if len(got) != 1 || got[0].GroupID != nil || got[0].SourceIPAddress != nil ||
		!slices.Equal(got[0].DestinationIPAddress, net.ParseIP("ff05::fa").To16()) || got[0].GroupcastTestResult != groups.TestResultFailedAuth || got[0].FabricIndex != 1 {
		t.Errorf("FailedAuth event = %+v", got)
	}

	// The period ends: FabricUnderTest clears and reports stop.
	f.timers[0].fire()
	if v, _ := f.gc.MatterRead(0x0004); v != uint8(0) {
		t.Errorf("FabricUnderTest after the period = %v", v)
	}
	if got := report(groups.GroupMessageEvent{Result: groups.TestResultFailedAuth}); len(got) != 0 {
		t.Errorf("reported after testing ended: %+v", got)
	}

	// Disable stops a running period; a later stale timer changes nothing.
	f.invoke(admin, 0x05, core.GroupcastTestingRequest{TestOperation: 1, DurationSeconds: u16p(10)})
	f.invoke(as(2, 5), 0x05, core.GroupcastTestingRequest{TestOperation: 0})
	if !f.timers[1].stopped {
		t.Error("Disable did not stop the period's timer")
	}
	f.invoke(admin, 0x05, core.GroupcastTestingRequest{TestOperation: 1, DurationSeconds: u16p(1200)})
	f.timers[1].fire() // superseded
	if v, _ := f.gc.MatterRead(0x0004); v != uint8(1) {
		t.Errorf("a superseded timer cleared FabricUnderTest: %v", v)
	}
}

func TestGroupcastAuxiliaryACLParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)
	f := newGCFixture(t, 2)
	ctx := context.Background()

	// Listener requires the Auxiliary feature, which NewGroupcast turned on.
	if v, _ := f.acl.MatterRead(cluster.AttrGlobalFeatureMap); v != uint32(0x5) {
		t.Errorf("AccessControl FeatureMap = %v, want Extension|Auxiliary", v)
	}
	if !slices.Contains(f.acl.MatterAttributes(), 0x0007) || !slices.Contains(f.acl.MatterEvents(), 0x0003) {
		t.Error("AuxiliaryAcl / AuxiliaryAccessUpdated not advertised")
	}
	if f.acl.MinReadPrivilege(0x0007) != 5 {
		t.Error("AuxiliaryAcl is not Administer-read")
	}
	if slices.Contains(f.acl.MatterReportable(), 0x0007) {
		t.Error("AuxiliaryAcl (quality C) is reported on change")
	}

	f.emitter.take()
	f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3, 4, 5, 6, 7}, KeySetID: 1, Key: testKey, UseAuxiliaryACL: boolp(true)})
	evs := f.emitter.take()
	var updates []core.AuxiliaryAccessUpdatedEvent
	for _, e := range evs {
		if e.cluster == 0x001F && e.event == 0x0003 {
			updates = append(updates, e.data.(core.AuxiliaryAccessUpdatedEvent))
		}
	}
	if len(updates) != 1 || updates[0].FabricIndex != 1 || updates[0].AdminNodeID == nil || *updates[0].AdminNodeID != gcAdminNode {
		t.Fatalf("AuxiliaryAccessUpdated = %+v, want one naming node 0x77 for fabric 1", updates)
	}

	// Five endpoints split to the four targets per entry
	// (#auxiliaryAclFor chunking).
	v, _ := f.acl.MatterReadFiltered(im.WithFabricFilter(ctx, true, 1), 0x0007)
	aux := v.([]core.AccessControlAuxiliaryEntryStruct)
	if len(aux) != 2 || len(aux[0].Entry.Targets) != 4 || len(aux[1].Entry.Targets) != 1 {
		t.Fatalf("AuxiliaryAcl = %+v", aux)
	}
	for _, e := range aux {
		if e.Redacted || e.AuxiliaryType != core.AccessControlAuxiliaryTypeGroupcast || e.Entry.Privilege != 3 || e.Entry.AuthMode != 3 ||
			!slices.Equal(e.Entry.Subjects, []uint64{1}) || e.Entry.FabricIndex != 1 {
			t.Errorf("entry = %+v", e)
		}
	}
	// Another fabric's read: filtered sees nothing, unfiltered the
	// entries redacted to FabricIndex.
	if v, _ := f.acl.MatterReadFiltered(im.WithFabricFilter(ctx, true, 2), 0x0007); len(v.([]core.AccessControlAuxiliaryEntryStruct)) != 0 {
		t.Error("fabric 2's filtered read sees fabric 1's entries")
	}
	v, _ = f.acl.MatterReadFiltered(im.WithFabricFilter(ctx, false, 2), 0x0007)
	if r := v.([]core.AccessControlAuxiliaryEntryStruct); len(r) != 2 || !r[0].Redacted {
		t.Errorf("fabric 2's unfiltered read = %+v, want redacted entries", r)
	}
	if v, _ := f.acl.MatterReadFiltered(im.WithFabricFilter(ctx, true, 0), 0x0007); len(v.([]core.AccessControlAuxiliaryEntryStruct)) != 0 {
		t.Error("a read without a fabric sees entries")
	}

	// An unchanged command emits nothing; a LeaveGroup one event.
	f.join(t, admin, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1})
	if evs := f.emitter.take(); len(evs) != 0 {
		t.Errorf("an unchanged aux ACL emitted %+v", evs)
	}
	f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 1})
	if evs := f.emitter.take(); len(evs) != 1 {
		t.Errorf("LeaveGroup emitted %d events, want one AuxiliaryAccessUpdated", len(evs))
	}
	if v, _ := f.acl.MatterRead(0x0007); len(v.([]core.AccessControlAuxiliaryEntryStruct)) != 0 {
		t.Error("entries left after the group was left")
	}

	// A controller may not write an AuxiliaryType: Failure, as CHIP.
	auxType := uint8(1)
	err := f.acl.MatterWrite(admin, 0, []core.AccessControlEntryStruct{{Privilege: 5, AuthMode: 2, Subjects: []uint64{gcAdminNode}, AuxiliaryType: &auxType}})
	if statusOf(err) != im.StatusFailure {
		t.Errorf("ACL write with AuxiliaryType: %v, want Failure", err)
	}
}

func TestGroupcastMembershipReadsAndSurfaceParityMatterJS(t *testing.T) {
	t.Parallel()
	f := newGCFixture(t, 2)
	f.join(t, as(1, 5), core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})
	f.join(t, as(2, 5), core.JoinGroupRequest{GroupID: 2, Endpoints: []uint16{4}, KeySetID: 1, Key: testKey})
	ctx := context.Background()

	read := func(filtered bool, fabric uint8) []core.GroupcastMembershipStruct {
		v, ok := f.gc.MatterReadFiltered(im.WithFabricFilter(ctx, filtered, fabric), 0x0000)
		if !ok {
			t.Fatal("Membership unreadable")
		}
		return v.([]core.GroupcastMembershipStruct)
	}
	if got := read(true, 1); len(got) != 1 || got[0].FabricIndex != 1 || got[0].KeySetID == nil {
		t.Errorf("filtered = %+v", got)
	}
	got := read(false, 1)
	if len(got) != 2 || got[0].KeySetID == nil || got[1].KeySetID != nil || got[1].GroupID != 2 || !slices.Equal(got[1].Endpoints, []uint16{4}) {
		t.Errorf("unfiltered = %+v, want fabric 2's KeySetId (access S) withheld", got)
	}

	// The surface matter.js derives from groupcast.element.ts with
	// Listener and PerGroup.
	for attr, want := range map[uint32]any{
		0x0001: uint16(44), 0x0002: uint16(44), 0x0003: uint16(1),
		cluster.AttrGlobalFeatureMap: uint32(0x5), cluster.AttrGlobalClusterRevision: uint16(1),
	} {
		if v, _ := f.gc.MatterRead(attr); v != want {
			t.Errorf("attribute 0x%04X = %v, want %v", attr, v, want)
		}
	}
	if _, ok := f.gc.MatterRead(0x0005); ok {
		t.Error("an unknown attribute read succeeded")
	}
	if !slices.Equal(f.gc.MatterAttributes(), []uint32{0, 1, 2, 3, 4}) ||
		!slices.Equal(f.gc.MatterAcceptedCommands(), []uint32{0, 1, 3, 4, 5}) ||
		!slices.Equal(f.gc.MatterGeneratedCommands(), []uint32{2}) ||
		!slices.Equal(f.gc.MatterEvents(), []uint32{0}) {
		t.Error("attribute / command / event lists differ from groupcast.element.ts")
	}
	for cmd, want := range map[uint32]uint8{0: 4, 1: 4, 3: 4, 4: 5, 5: 5} {
		if got := f.gc.MinInvokePrivilege(cmd); got != want {
			t.Errorf("command %d privilege %d, want %d", cmd, got, want)
		}
	}
	if err := f.gc.MatterWrite(ctx, 0, nil); err == nil {
		t.Error("a write succeeded")
	}
	if _, st := f.invoke(as(1, 5), 0x09, nil); st != im.StatusUnsupportedCommand {
		t.Errorf("unknown command: %v", st)
	}
	for _, cmd := range []uint32{0, 1, 3, 4, 5} {
		if _, st := f.invoke(as(1, 5), cmd, "wrong"); st != im.StatusInvalidCommand {
			t.Errorf("command %d with a wrong payload: %v, want InvalidCommand", cmd, st)
		}
	}
	// Fabric removal forgets the fabric's memberships.
	if err := f.fs.RemoveFabric(ctx, 2); err != nil {
		t.Fatal(err)
	}
	f.mgr.ForgetFabric(2)
	if got := read(false, 1); len(got) != 1 {
		t.Errorf("after fabric 2's removal: %+v", got)
	}
}

func TestNewGroupcastRequiresItsCollaborators(t *testing.T) {
	t.Parallel()
	fs := newFakeStore()
	mgr, _ := groups.NewManager(fs, nil)
	other, _ := groups.NewManager(fs, nil)
	gkm, _ := core.NewGroupKeyManagement(fs, core.GroupKeyMgmtConfig{Groups: mgr})
	acl, _ := core.NewAccessControl(fs)
	for name, cfg := range map[string]core.GroupcastConfig{
		"no group state":           {GroupKeyManagement: gkm, AccessControl: acl},
		"no GroupKeyManagement":    {Groups: mgr, AccessControl: acl},
		"a different group state":  {Groups: other, GroupKeyManagement: gkm, AccessControl: acl},
		"no AccessControl for AUX": {Groups: mgr, GroupKeyManagement: gkm},
	} {
		if _, err := core.NewGroupcast(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	gc, err := core.NewGroupcast(core.GroupcastConfig{Groups: mgr, GroupKeyManagement: gkm, AccessControl: acl})
	if err != nil {
		t.Fatal(err)
	}
	gc.SetEndpoint(0)
	gc.Close()
	if gc.MatterClusterID() != 0x0065 || gc.MatterDataVersion() == 0 {
		t.Error("identity")
	}
}

// schemaSurface is the attribute, command and event surface parity/schema.json
// (the matter.js HEAD extract) gives a cluster under a feature set.
type schemaSurface struct {
	attrs, accepted, generated, events []uint32
	featureMap                         uint32
}

func surfaceFromSchema(t *testing.T, clusterID uint32, features ...string) schemaSurface {
	t.Helper()
	var doc struct {
		Clusters []struct {
			ID         uint32 `json:"id"`
			Attributes []struct {
				ID          uint32 `json:"id"`
				Conformance string `json:"conformance"`
			} `json:"attributes"`
			Commands []struct {
				ID          uint32 `json:"id"`
				Direction   string `json:"direction"`
				Conformance string `json:"conformance"`
			} `json:"commands"`
			Events []struct {
				ID          uint32 `json:"id"`
				Conformance string `json:"conformance"`
			} `json:"events"`
			Features []struct {
				Name string `json:"name"`
				Bit  uint32 `json:"bit"`
			} `json:"features"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(matterparity.SchemaJSON(), &doc); err != nil {
		t.Fatal(err)
	}
	on := func(conformance string) bool {
		return conformance == "M" || slices.Contains(features, conformance)
	}
	for _, c := range doc.Clusters {
		if c.ID != clusterID {
			continue
		}
		var s schemaSurface
		for _, a := range c.Attributes {
			if a.ID < 0xFFF8 && on(a.Conformance) {
				s.attrs = append(s.attrs, a.ID)
			}
		}
		for _, cmd := range c.Commands {
			if !on(cmd.Conformance) {
				continue
			}
			if cmd.Direction == "request" {
				s.accepted = append(s.accepted, cmd.ID)
			} else {
				s.generated = append(s.generated, cmd.ID)
			}
		}
		for _, e := range c.Events {
			if on(e.Conformance) {
				s.events = append(s.events, e.ID)
			}
		}
		for _, f := range c.Features {
			if slices.Contains(features, f.Name) {
				s.featureMap |= 1 << f.Bit
			}
		}
		return s
	}
	t.Fatalf("cluster 0x%04X not in parity/schema.json", clusterID)
	return schemaSurface{}
}

// TestGroupcastAndAuxiliarySurfaceMatchSchema pins AttributeList,
// AcceptedCommandList, GeneratedCommandList, EventList and FeatureMap of
// Groupcast (Listener + PerGroup) and AccessControl (Extension +
// Auxiliary) to parity/schema.json, the matter.js HEAD pin.
func TestGroupcastAndAuxiliarySurfaceMatchSchema(t *testing.T) {
	t.Parallel()
	f := newGCFixture(t, 1)
	gc := surfaceFromSchema(t, 0x0065, "LN", "PGA")
	fm, _ := f.gc.MatterRead(cluster.AttrGlobalFeatureMap)
	if !slices.Equal(f.gc.MatterAttributes(), gc.attrs) || !slices.Equal(f.gc.MatterAcceptedCommands(), gc.accepted) ||
		!slices.Equal(f.gc.MatterGeneratedCommands(), gc.generated) || !slices.Equal(f.gc.MatterEvents(), gc.events) || fm != gc.featureMap {
		t.Errorf("Groupcast surface (%v %v %v %v %#x) != schema (%v %v %v %v %#x)",
			f.gc.MatterAttributes(), f.gc.MatterAcceptedCommands(), f.gc.MatterGeneratedCommands(), f.gc.MatterEvents(), fm,
			gc.attrs, gc.accepted, gc.generated, gc.events, gc.featureMap)
	}
	ac := surfaceFromSchema(t, 0x001F, "EXTS", "AUX")
	afm, _ := f.acl.MatterRead(cluster.AttrGlobalFeatureMap)
	if !slices.Equal(f.acl.MatterAttributes(), ac.attrs) || !slices.Equal(f.acl.MatterEvents(), ac.events) || afm != ac.featureMap {
		t.Errorf("AccessControl surface (%v %v %#x) != schema (%v %v %#x)",
			f.acl.MatterAttributes(), f.acl.MatterEvents(), afm, ac.attrs, ac.events, ac.featureMap)
	}
	// Without Groupcast the AccessControl surface stays Extension-only.
	plain, err := core.NewAccessControl(newFakeStore())
	if err != nil {
		t.Fatal(err)
	}
	ext := surfaceFromSchema(t, 0x001F, "EXTS")
	pfm, _ := plain.MatterRead(cluster.AttrGlobalFeatureMap)
	if !slices.Equal(plain.MatterAttributes(), ext.attrs) || !slices.Equal(plain.MatterEvents(), ext.events) || pfm != ext.featureMap {
		t.Errorf("AccessControl without AUX: (%v %v %#x) != schema (%v %v %#x)", plain.MatterAttributes(), plain.MatterEvents(), pfm, ext.attrs, ext.events, ext.featureMap)
	}
	if _, ok := plain.MatterRead(0x0007); ok {
		t.Error("AuxiliaryAcl readable without the Auxiliary feature")
	}
}

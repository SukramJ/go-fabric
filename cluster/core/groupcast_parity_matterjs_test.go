// SPDX-License-Identifier: Apache-2.0
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
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
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

func (r *gcRecordingEmitter) MatterEmitEvent(_ uint16, clusterID, event uint32, data any, _ contract.EventPriority) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, gcEvent{clusterID, event, data})
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
	return newGCFixtureWith(t, fabrics, nil)
}

// newGCFixtureWith is newGCFixture with edit applied to the Groupcast
// configuration — matter.js's MockServerNode options for the
// GroupcastServer state.
func newGCFixtureWith(t *testing.T, fabrics int, edit func(*core.GroupcastConfig)) *gcFixture {
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
	cfg := core.GroupcastConfig{
		Groups: f.mgr, GroupKeyManagement: f.gkm, AccessControl: f.acl,
		TestingTimer: func(d time.Duration, fire func()) func() {
			tm := &testingTimer{d: d, fire: fire}
			f.timers = append(f.timers, tm)
			return func() { tm.stopped = true }
		},
	}
	if edit != nil {
		edit(&cfg)
	}
	if f.gc, err = core.NewGroupcast(cfg); err != nil {
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
func as(fabric, privilege uint8) context.Context {
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

func (f *gcFixture) join(ctx context.Context, t *testing.T, req core.JoinGroupRequest) {
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
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey, McastAddrPolicy: u8p(groups.PolicyIanaAddr)})
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
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})
		f.join(admin, t, core.JoinGroupRequest{GroupID: 2, Endpoints: []uint16{3}, KeySetID: 1, McastAddrPolicy: u8p(groups.PolicyPerGroup)})
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
			// 21 endpoints with an unknown key set: the "desc" constraint
			// (chip kMaxCommandEndpoints, TC-GC-2.2 step 12) answers before
			// the key set is looked up.
			"21 endpoints": {GroupID: 1, Endpoints: twentyOneEndpoints(), KeySetID: 9},
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

	// With the Sender feature an empty endpoint list is legal
	// (GroupcastServer.ts:206-209 refuses it only without Sender): the
	// group joins sender-only — a Membership entry from groupProperties
	// alone, no group table entry, no multicast membership to receive on
	// (ServerGroupNetworking joins groups with endpoints) and no
	// auxiliary ACL entry (#emitAuxAcl, core§11.27.10).
	t.Run("an empty endpoint list joins sender-only", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		ctx := context.Background()
		if _, st := f.invoke(admin, 0x00, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{}, KeySetID: 1, Key: testKey, UseAuxiliaryACL: boolp(true)}); st != im.StatusSuccess {
			t.Fatalf("empty endpoint list: %v, want Success", st)
		}
		m, ok := f.memberOf(t, 1, 1)
		if !ok || len(m.Endpoints) != 0 || *m.KeySetID != 1 || m.McastAddrPolicy != groups.PolicyIanaAddr || !m.HasAuxiliaryACL {
			t.Fatalf("sender-only membership = %+v (%v)", m, ok)
		}
		if table, _ := f.mgr.GroupTable(ctx, 1); len(table) != 0 {
			t.Errorf("group table = %+v, want no entry", table)
		}
		if ms, _ := f.mgr.Memberships(ctx); len(ms) != 0 {
			t.Errorf("multicast memberships = %+v, want none to receive on", ms)
		}
		if acl, _ := f.mgr.AuxiliaryACL(ctx, 0); len(acl) != 0 {
			t.Errorf("auxiliary ACL = %+v, want none for a sender-only group", acl)
		}
		if ok, _ := f.mgr.HasKeyMapping(ctx, 1, 1); !ok {
			t.Error("the GroupKeyMap binding is missing")
		}
		if v, _ := f.gc.MatterRead(0x0003); v != uint16(1) {
			t.Errorf("UsedMcastAddrCount = %v, want 1", v)
		}
		// A later join with endpoints adds them to the same membership.
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1})
		if m, _ := f.memberOf(t, 1, 1); !slices.Equal(m.Endpoints, []uint16{3}) {
			t.Errorf("after joining endpoint 3: %v", m.Endpoints)
		}
	})

	// ReplaceEndpoints with an empty list empties the group and keeps it
	// sender-only (GroupcastServer.ts:268-269, :290-292 retainedSenderOnly).
	t.Run("replacing the endpoints with none keeps the group sender-only", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3, 4}, KeySetID: 1, Key: testKey, McastAddrPolicy: u8p(groups.PolicyPerGroup)})
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{}, KeySetID: 1, ReplaceEndpoints: boolp(true), McastAddrPolicy: u8p(groups.PolicyPerGroup)})
		m, ok := f.memberOf(t, 1, 1)
		if !ok || len(m.Endpoints) != 0 || m.McastAddrPolicy != groups.PolicyPerGroup {
			t.Fatalf("after replacing with none: %+v (%v), want a sender-only member", m, ok)
		}
		if table, _ := f.mgr.GroupTable(context.Background(), 1); len(table) != 0 {
			t.Errorf("group table = %+v", table)
		}
	})

	t.Run("KeySetId must exist without a key, must not with one", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		if _, st := f.invoke(admin, 0x00, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 99}); st != im.StatusNotFound {
			t.Errorf("missing key set: %v, want NotFound", st)
		}
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})
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
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})
		if _, st := f.invoke(manage, 0x00, core.JoinGroupRequest{GroupID: 2, Endpoints: []uint16{3}, KeySetID: 1, UseAuxiliaryACL: boolp(false)}); st != im.StatusUnsupportedAccess {
			t.Errorf("UseAuxiliaryAcl at Manage: %v, want UnsupportedAccess", st)
		}
		f.join(manage, t, core.JoinGroupRequest{GroupID: 2, Endpoints: []uint16{3}, KeySetID: 1})
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
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3, 4}, KeySetID: 1, Key: testKey, UseAuxiliaryACL: boolp(true)})
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{5}, KeySetID: 1})
		if m, _ := f.memberOf(t, 1, 1); !slices.Equal(m.Endpoints, []uint16{3, 4, 5}) || !m.HasAuxiliaryACL {
			t.Fatalf("merge = %+v (a plain re-join must keep HasAuxiliaryAcl)", m)
		}
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{6, 4}, KeySetID: 1, ReplaceEndpoints: boolp(true)})
		m, _ := f.memberOf(t, 1, 1)
		if !slices.Equal(m.Endpoints, []uint16{4, 6}) || !m.HasAuxiliaryACL {
			t.Errorf("replace = %+v, want [4 6] with the flag kept", m)
		}
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{7}, KeySetID: 1, ReplaceEndpoints: boolp(true)})
		if m, _ := f.memberOf(t, 1, 1); !slices.Equal(m.Endpoints, []uint16{7}) || !m.HasAuxiliaryACL {
			t.Errorf("a disjoint replace = %+v, want [7] with the flag kept", m)
		}
	})

	t.Run("per-fabric quota, total limit, and no key installed by a refused join", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 3)
		for fabric := uint8(1); fabric <= 2; fabric++ {
			ctx := as(fabric, 5)
			f.join(ctx, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})
			for gid := uint16(2); gid <= 22; gid++ {
				f.join(ctx, t, core.JoinGroupRequest{GroupID: gid, Endpoints: []uint16{3}, KeySetID: 1})
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
		f.join(as(1, 5), t, core.JoinGroupRequest{GroupID: 22, Endpoints: []uint16{4}, KeySetID: 1})
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
		f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3, 4}, KeySetID: 1, Key: testKey, UseAuxiliaryACL: boolp(true)})
		f.join(admin, t, core.JoinGroupRequest{GroupID: 2, Endpoints: []uint16{3, 5}, KeySetID: 1})
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

	// With the Sender feature the group losing its last listed endpoint
	// stays a sender-only member, binding and properties included
	// (GroupcastServer.ts:344-358, spec kKeepGroupIfEmpty); a LeaveGroup
	// without endpoints then removes it (:340-342, :363-366).
	t.Run("removes only the listed endpoints, and keeps the group sender-only with the last", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		ctx := context.Background()
		resp, _ := f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 1, Endpoints: []uint16{4, 9}, HasEndpoints: true})
		if r := resp.(core.LeaveGroupResponse); !slices.Equal(r.Endpoints, []uint16{4}) {
			t.Errorf("removed %v, want [4]", r.Endpoints)
		}
		if m, _ := f.memberOf(t, 1, 1); !slices.Equal(m.Endpoints, []uint16{3}) {
			t.Errorf("remaining %v", m.Endpoints)
		}
		resp, _ = f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 1, Endpoints: []uint16{3}, HasEndpoints: true})
		if r := resp.(core.LeaveGroupResponse); !slices.Equal(r.Endpoints, []uint16{3}) {
			t.Errorf("removed %v, want [3]", r.Endpoints)
		}
		m, ok := f.memberOf(t, 1, 1)
		if !ok || len(m.Endpoints) != 0 || m.McastAddrPolicy != groups.PolicyIanaAddr || *m.KeySetID != 1 {
			t.Fatalf("after the last endpoint left: %+v (%v), want a sender-only member", m, ok)
		}
		if ok, _ := f.mgr.HasKeyMapping(ctx, 1, 1); !ok {
			t.Error("the GroupKeyMap binding of a sender-only group was removed")
		}
		if acl, _ := f.mgr.AuxiliaryACL(ctx, 1); slices.ContainsFunc(acl, func(e mstore.ACLEntry) bool { return slices.Contains(e.Subjects, 1) }) {
			t.Errorf("a sender-only group kept its auxiliary ACL entry: %+v", acl)
		}
		// Listing endpoints it no longer has removes nothing and keeps it.
		resp, _ = f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 1, Endpoints: []uint16{3}, HasEndpoints: true})
		if r := resp.(core.LeaveGroupResponse); len(r.Endpoints) != 0 {
			t.Errorf("removed %v from a sender-only group", r.Endpoints)
		}
		if _, ok := f.memberOf(t, 1, 1); !ok {
			t.Error("a sender-only group left on an empty removal")
		}
		f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 1})
		if _, ok := f.memberOf(t, 1, 1); ok {
			t.Error("LeaveGroup without endpoints kept the group")
		}
		if ok, _ := f.mgr.HasKeyMapping(ctx, 1, 1); ok {
			t.Error("the binding survived the group")
		}
	})

	// A group the Groups cluster created keeps its policy when LeaveGroup
	// keeps it sender-only (GroupcastServer.ts:354-358, "groupProperties
	// keeps it a member" with entry.mcastAddrPolicy).
	t.Run("a Groups-cluster group kept sender-only keeps its policy", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		if _, err := f.gkm.MatterInvoke(admin, 0x00, core.KeySetWriteRequest{GroupKeySet: core.GroupKeySetStruct{GroupKeySetID: 7, EpochKey0: testKey, EpochStartTime0: 5}}); err != nil {
			t.Fatal(err)
		}
		if err := f.gkm.MatterWrite(admin, 0, []core.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 1}, {GroupID: 2, GroupKeySetID: 1}, {GroupID: 0x10, GroupKeySetID: 7}}); err != nil {
			t.Fatal(err)
		}
		if resp, _ := f.ep3.MatterInvoke(admin, 0x00, core.AddGroupRequest{GroupID: 0x10}); resp.(core.AddGroupResponse).Status != im.StatusSuccess {
			t.Fatal("AddGroup failed")
		}
		f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 0x10, Endpoints: []uint16{3}, HasEndpoints: true})
		m, ok := f.memberOf(t, 1, 0x10)
		if !ok || len(m.Endpoints) != 0 || m.McastAddrPolicy != groups.PolicyPerGroup {
			t.Fatalf("legacy group after LeaveGroup = %+v (%v), want sender-only on PerGroup", m, ok)
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
	f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})

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
	f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey, McastAddrPolicy: u8p(groups.PolicyPerGroup)})
	var notified int
	unsub := f.gc.OnMatterValueChanged(func() { notified++ })
	defer unsub()

	for name, req := range map[string]core.GroupcastTestingRequest{
		"unknown operation": {TestOperation: 3},
		"9 seconds":         {TestOperation: 1, DurationSeconds: u16p(9)},
		"1201 seconds":      {TestOperation: 1, DurationSeconds: u16p(1201)},
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

	// EnableSenderTesting (conformance SD) is accepted like
	// EnableListenerTesting: FabricUnderTest is the accessing fabric for
	// 60 s by default (GroupcastServer.ts:405-431).
	if _, st := f.invoke(as(2, 5), 0x05, core.GroupcastTestingRequest{TestOperation: 2}); st != im.StatusSuccess {
		t.Fatalf("EnableSenderTesting: %v", st)
	}
	last := f.timers[len(f.timers)-1]
	if v, _ := f.gc.MatterRead(0x0004); v != uint8(2) || last.d != 60*time.Second || !f.timers[2].stopped {
		t.Fatalf("EnableSenderTesting: FabricUnderTest %v, timer %v, previous stopped %v", v, last.d, f.timers[2].stopped)
	}
	last.fire()
	if v, _ := f.gc.MatterRead(0x0004); v != uint8(0) {
		t.Errorf("FabricUnderTest after the sender testing period = %v", v)
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
	f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3, 4, 5, 6, 7}, KeySetID: 1, Key: testKey, UseAuxiliaryACL: boolp(true)})
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
	f.join(admin, t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1})
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
	f.join(as(1, 5), t, core.JoinGroupRequest{GroupID: 1, Endpoints: []uint16{3}, KeySetID: 1, Key: testKey})
	f.join(as(2, 5), t, core.JoinGroupRequest{GroupID: 2, Endpoints: []uint16{4}, KeySetID: 1, Key: testKey})
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
	// Listener, Sender and PerGroup.
	for attr, want := range map[uint32]any{
		0x0001: uint16(44), 0x0002: uint16(44), 0x0003: uint16(1),
		cluster.AttrGlobalFeatureMap: uint32(0x7), cluster.AttrGlobalClusterRevision: uint16(1),
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
// Groupcast (Listener + Sender + PerGroup, GroupcastServer.ts:83) and
// AccessControl (Extension + Auxiliary) to parity/schema.json, the
// matter.js HEAD pin.
func TestGroupcastAndAuxiliarySurfaceMatchSchema(t *testing.T) {
	t.Parallel()
	f := newGCFixture(t, 1)
	gc := surfaceFromSchema(t, 0x0065, "LN", "SD", "PGA")
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

// twentyOneEndpoints is a JoinGroup endpoint list one past the limit.
func twentyOneEndpoints() []uint16 {
	out := make([]uint16, 21)
	for i := range out {
		out[i] = uint16(i + 1)
	}
	return out
}

// The cases below port GroupcastServerTest.ts describe by describe, one
// test per describe group with the `it` titles as subtests; each names
// its lines. matter.js's node.addFabric() is a fabric of the fixture,
// node.online with a MockExchange of fabric fi is as(fi, Administer),
// node.stateOf(GroupcastServer).groupProperties is the store's
// matter_groupcast_groups rows (gcProps), a local write of
// GroupKeyManagementServer state.groupKeyMap is a GroupKeyMap write of the
// fabric, state.groupTable = [] is every endpoint removed from the
// fabric's groups (gkm.removeEndpoint, the Groups-cluster path), and a
// status matter.js rejects with is the IM status plus the Go error text.

// gcProps is node.stateOf(GroupcastServer).groupProperties of a fabric.
func (f *gcFixture) gcProps(t *testing.T, fabric uint8) map[uint16]mstore.GroupcastGroup {
	t.Helper()
	rows, err := f.fs.ListGroupcastGroups(context.Background(), fabric)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[uint16]mstore.GroupcastGroup, len(rows))
	for _, r := range rows {
		out[r.GroupID] = r
	}
	return out
}

// keyMap is the fabric's GroupKeyManagement GroupKeyMap.
func (f *gcFixture) keyMap(t *testing.T, fabric uint8) []core.GroupKeyMapStruct {
	t.Helper()
	v, ok := f.gkm.MatterReadFiltered(as(fabric, 5), 0x0000)
	if !ok {
		t.Fatal("GroupKeyMap unreadable")
	}
	var out []core.GroupKeyMapStruct
	for _, e := range v.([]core.GroupKeyMapStruct) {
		if e.FabricIndex == fabric {
			out = append(out, e)
		}
	}
	return out
}

// groupTable is the fabric's GroupKeyManagement GroupTable.
func (f *gcFixture) groupTable(t *testing.T, fabric uint8) []core.GroupInfoMapStruct {
	t.Helper()
	v, ok := f.gkm.MatterReadFiltered(as(fabric, 5), 0x0001)
	if !ok {
		t.Fatal("GroupTable unreadable")
	}
	var out []core.GroupInfoMapStruct
	for _, e := range v.([]core.GroupInfoMapStruct) {
		if e.FabricIndex == fabric {
			out = append(out, e)
		}
	}
	return out
}

// auxACL is the fabric's entries of AccessControl AuxiliaryAcl.
func (f *gcFixture) auxACL(t *testing.T, fabric uint8) []core.AccessControlAuxiliaryEntryStruct {
	t.Helper()
	v, ok := f.acl.MatterRead(0x0007)
	if !ok {
		t.Fatal("AuxiliaryAcl unreadable")
	}
	var out []core.AccessControlAuxiliaryEntryStruct
	for _, e := range v.([]core.AccessControlAuxiliaryEntryStruct) {
		if e.Entry.FabricIndex == fabric {
			out = append(out, e)
		}
	}
	return out
}

// writeKeyMap is a local write of state.groupKeyMap for the fabric.
func (f *gcFixture) writeKeyMap(t *testing.T, fabric uint8, list ...core.GroupKeyMapStruct) {
	t.Helper()
	if list == nil {
		list = []core.GroupKeyMapStruct{}
	}
	if err := f.gkm.MatterWrite(as(fabric, 5), 0x0000, list); err != nil {
		t.Fatalf("GroupKeyMap write: %v", err)
	}
}

// clearGroupTable is state.groupTable = [] for the fabric: every endpoint
// leaves every group, the external removal path.
func (f *gcFixture) clearGroupTable(t *testing.T, fabric uint8) {
	t.Helper()
	ctx := context.Background()
	table, err := f.mgr.GroupTable(ctx, fabric)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range table {
		for _, ep := range e.Endpoints {
			if _, err := f.mgr.RemoveEndpoint(ctx, fabric, ep, e.GroupID, false); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// ianaReq is the file's usual JoinGroup: key set 1, the key when it
// creates the set, the IanaAddr policy.
func ianaReq(groupID uint16, endpoints []uint16, withKey bool) core.JoinGroupRequest {
	req := core.JoinGroupRequest{GroupID: groupID, Endpoints: endpoints, KeySetID: 1, McastAddrPolicy: u8p(groups.PolicyIanaAddr)}
	if withKey {
		req.Key = testKey
	}
	return req
}

// refusal checks a rejected command: the IM status and matter.js's
// message, which the Go error carries.
func refusal(t *testing.T, err error, want im.StatusCode, msg string) {
	t.Helper()
	if st := statusOf(err); st != want || err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(msg)) {
		t.Errorf("refusal %v (%v), want %v %q", err, st, want, msg)
	}
}

// uintOf reads an unsigned attribute value: uintOf(t)(server.MatterRead(id)).
func uintOf(t *testing.T) func(v any, ok bool) int {
	t.Helper()
	return func(v any, ok bool) int {
		t.Helper()
		if !ok {
			t.Fatal("attribute unreadable")
		}
		return int(reflect.ValueOf(v).Uint()) //nolint:gosec // an attribute's small count
	}
}

// TestGroupcastJoinGroupPropertiesParityMatterJS ports the groupProperties
// cases of the describe "joinGroup" (GroupcastServerTest.ts:100-151).
func TestGroupcastJoinGroupPropertiesParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)

	t.Run("records groupProperties flags", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0102, []uint16{1}, true))
		p, ok := f.gcProps(t, 1)[0x0102]
		if !ok || p.FabricIndex != 1 || p.McastAddrPolicy != groups.PolicyIanaAddr || p.HasAuxiliaryACL {
			t.Errorf("groupProperties entry %+v (%v), want fabric 1, IanaAddr, no auxiliary ACL", p, ok)
		}
	})

	t.Run("does not overwrite hasAuxiliaryAcl on a plain re-join", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		req := ianaReq(0x0102, []uint16{1}, true)
		req.UseAuxiliaryACL = boolp(true)
		f.join(admin, t, req)
		f.join(admin, t, ianaReq(0x0102, []uint16{2}, false))
		if p := f.gcProps(t, 1)[0x0102]; !p.HasAuxiliaryACL {
			t.Errorf("a re-join without UseAuxiliaryAcl reset the flag: %+v", p)
		}
	})
}

// TestGroupcastLeaveGroupMirrorParityMatterJS ports the remaining cases of
// the describe "leaveGroup" (GroupcastServerTest.ts:358-458). Of the
// loop-generated pair (:358-404) the Sender case runs; the case without
// the Sender feature is in the register's missing list, the Go server
// always has it.
func TestGroupcastLeaveGroupMirrorParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)

	t.Run("keeps a group a wildcard request empties as sender-only", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0001, []uint16{1}, true))
		f.join(admin, t, ianaReq(0x0002, []uint16{1, 2}, false))
		if _, st := f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 0, Endpoints: []uint16{1}, HasEndpoints: true}); st != im.StatusSuccess {
			t.Fatal(st)
		}
		m1, ok1 := f.memberOf(t, 1, 0x0001)
		m2, _ := f.memberOf(t, 1, 0x0002)
		if !ok1 || len(m1.Endpoints) != 0 || !slices.Equal(m2.Endpoints, []uint16{2}) {
			t.Errorf("after the wildcard leave: group 1 %+v (%v), group 2 %v", m1, ok1, m2.Endpoints)
		}
		if !slices.ContainsFunc(f.keyMap(t, 1), func(e core.GroupKeyMapStruct) bool { return e.GroupID == 0x0001 }) {
			t.Error("the sender-only group lost its GroupKeyMap entry")
		}
	})

	t.Run("removes groupProperties entry", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0102, []uint16{1}, true))
		f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 0x0102})
		if _, ok := f.gcProps(t, 1)[0x0102]; ok {
			t.Error("LeaveGroup kept the groupProperties entry")
		}
	})

	t.Run("leaves all groups of the fabric with the wildcard group id", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0001, []uint16{1}, true))
		f.join(admin, t, ianaReq(0x0002, []uint16{1}, false))
		resp, st := f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 0})
		if r, ok := resp.(core.LeaveGroupResponse); st != im.StatusSuccess || !ok || r.GroupID != 0 {
			t.Fatalf("LeaveGroup(0) = %+v, %v", resp, st)
		}
		if ms, props, km, table := f.membership(t), f.gcProps(t, 1), f.keyMap(t, 1), f.groupTable(t, 1); len(ms) != 0 || len(props) != 0 || len(km) != 0 || len(table) != 0 {
			t.Errorf("after the wildcard leave: membership %v, groupProperties %v, groupKeyIdMap %v, endpoints %v", ms, props, km, table)
		}
	})
}

// TestGroupcastUpdateGroupKeyMirrorParityMatterJS ports "updates keySetId
// for an existing group" (GroupcastServerTest.ts:505-530).
func TestGroupcastUpdateGroupKeyMirrorParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("updates keySetId for an existing group", func(t *testing.T) {
		t.Parallel()
		admin := as(1, 5)
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0001, []uint16{1}, true))
		if _, st := f.invoke(admin, 0x03, core.UpdateGroupKeyRequest{GroupID: 0x0001, KeySetID: 2, Key: testKey}); st != im.StatusSuccess {
			t.Fatal(st)
		}
		if ms := f.membership(t); len(ms) != 1 || ms[0].KeySetID == nil || *ms[0].KeySetID != 2 {
			t.Errorf("membership %+v, want KeySetId 2", ms)
		}
	})
}

// TestGroupcastConfigureAuxiliaryACLMirrorParityMatterJS ports the describe
// "configureAuxiliaryAcl" (GroupcastServerTest.ts:550-625).
func TestGroupcastConfigureAuxiliaryACLMirrorParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)

	t.Run("sets hasAuxiliaryAcl on a membership entry", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0001, []uint16{1}, true))
		if _, st := f.invoke(admin, 0x04, core.ConfigureAuxiliaryACLRequest{GroupID: 0x0001, UseAuxiliaryACL: true}); st != im.StatusSuccess {
			t.Fatal(st)
		}
		if ms := f.membership(t); len(ms) != 1 || !ms[0].HasAuxiliaryACL {
			t.Errorf("membership %+v, want hasAuxiliaryAcl", ms)
		}
		if p := f.gcProps(t, 1)[0x0001]; !p.HasAuxiliaryACL {
			t.Errorf("groupProperties %+v, want hasAuxiliaryAcl", p)
		}
		if aux := f.auxACL(t, 1); len(aux) != 1 {
			t.Errorf("auxiliary ACL of fabric 1: %+v, want one entry", aux)
		}
	})

	t.Run("keeps the feature-aware mcastAddrPolicy default on a legacy-only group", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		if err := f.mgr.AddEndpointForGroup(context.Background(), 1, 0x0400, 1, "Legacy"); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.gcProps(t, 1)[0x0400]; ok {
			t.Fatal("a legacy group has groupProperties")
		}
		if _, st := f.invoke(admin, 0x04, core.ConfigureAuxiliaryACLRequest{GroupID: 0x0400, UseAuxiliaryACL: true}); st != im.StatusSuccess {
			t.Fatal(st)
		}
		if m, _ := f.memberOf(t, 1, 0x0400); m.McastAddrPolicy != groups.PolicyPerGroup {
			t.Errorf("membership policy %d, want PerGroup", m.McastAddrPolicy)
		}
		if p := f.gcProps(t, 1)[0x0400]; p.McastAddrPolicy != groups.PolicyPerGroup {
			t.Errorf("groupProperties policy %d, want PerGroup", p.McastAddrPolicy)
		}
	})

	// matter.js refuses the node at start with an ImplementationError;
	// the Go AccessControl's Auxiliary feature is the provider NewGroupcast
	// registers, so the Go form of "AccessControl lacks it" is none, and
	// the assertion is on the Go error.
	t.Run("rejects the Listener feature when AccessControl lacks the Auxiliary feature", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		_, err := core.NewGroupcast(core.GroupcastConfig{Groups: f.mgr, GroupKeyManagement: f.gkm})
		if err == nil || !strings.Contains(err.Error(), "requires AccessControl with the Auxiliary feature") {
			t.Errorf("NewGroupcast without AccessControl: %v", err)
		}
	})
}

// TestGroupcastGKMMirrorParityMatterJS ports the describe "GKM mirror"
// (GroupcastServerTest.ts:627-883).
func TestGroupcastGKMMirrorParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)
	ctx := context.Background()

	t.Run("propagates Membership keySetId mappings to GKM groupKeyMap", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0001, []uint16{1}, true))
		if got, want := f.keyMap(t, 1), []core.GroupKeyMapStruct{{GroupID: 1, GroupKeySetID: 1, FabricIndex: 1}}; !slices.Equal(got, want) {
			t.Errorf("GroupKeyMap %+v, want %+v", got, want)
		}
		// EpochStartTime0 1 on the wire (core§11.27.7.1.4); matter.js's
		// MATTER_EPOCH_OFFSET_US + 1 is its unix-based internal form.
		ks, err := f.fs.GetGroupKeySet(ctx, 1, 1)
		if err != nil || ks.EpochStart0 != 1 {
			t.Errorf("auto-created key set %+v, %v, want EpochStartTime0 1", ks, err)
		}
	})

	t.Run("preserves legacy group configuration not owned by Groupcast", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.writeKeyMap(t, 1, core.GroupKeyMapStruct{GroupID: 0x0300, GroupKeySetID: 5})
		if err := f.mgr.AddEndpointForGroup(ctx, 1, 0x0300, 1, "Legacy"); err != nil {
			t.Fatal(err)
		}
		f.join(admin, t, ianaReq(0x0001, []uint16{1}, true))
		km := map[uint16]uint16{}
		for _, e := range f.keyMap(t, 1) {
			km[e.GroupID] = e.GroupKeySetID
		}
		if km[0x0300] != 5 || km[0x0001] != 1 {
			t.Errorf("GroupKeyMap %v, want 0x0300→5 and 0x0001→1", km)
		}
		name := func() string {
			for _, e := range f.groupTable(t, 1) {
				if e.GroupID == 0x0300 {
					return e.GroupName
				}
			}
			return "<absent>"
		}
		if got := name(); got != "Legacy" {
			t.Errorf("legacy group name %q", got)
		}
		f.join(admin, t, ianaReq(0x0300, []uint16{2}, false))
		if got := name(); got != "Legacy" {
			t.Errorf("legacy group name after the Groupcast join %q", got)
		}
	})

	t.Run("shrinks Membership and auxiliary ACLs when groups are removed via the GKM group table", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		req := ianaReq(0x0001, []uint16{1}, true)
		req.UseAuxiliaryACL = boolp(true)
		f.join(admin, t, req)
		if aux := f.auxACL(t, 1); len(aux) != 1 {
			t.Fatalf("auxiliary ACL %+v, want one entry", aux)
		}
		f.clearGroupTable(t, 1)
		if ms := f.membership(t); len(ms) != 0 {
			t.Errorf("membership after the external removal %+v", ms)
		}
		if aux := f.auxACL(t, 1); len(aux) != 0 {
			t.Errorf("auxiliary ACL after the external removal %+v", aux)
		}
	})

	t.Run("membership KeySetId follows the GroupKeyMap link", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0001, []uint16{1}, true))
		f.writeKeyMap(t, 1)
		if ms := f.membership(t); len(ms) != 1 || *ms[0].KeySetID != groups.UnmappedKeySetID {
			t.Errorf("membership after clearing GroupKeyMap %+v, want KeySetId 0xFFFF", ms)
		}
		f.writeKeyMap(t, 1, core.GroupKeyMapStruct{GroupID: 1, GroupKeySetID: 1})
		if ms := f.membership(t); len(ms) != 1 || *ms[0].KeySetID != 1 {
			t.Errorf("membership after restoring GroupKeyMap %+v, want KeySetId 1", ms)
		}
	})

	t.Run("keeps unmapped groups out of the operational group key map", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0001, []uint16{1}, true))
		f.join(admin, t, ianaReq(0x0002, []uint16{1}, false))
		f.writeKeyMap(t, 1, core.GroupKeyMapStruct{GroupID: 1, GroupKeySetID: 1})
		if m, _ := f.memberOf(t, 1, 0x0002); *m.KeySetID != groups.UnmappedKeySetID {
			t.Errorf("group 2 KeySetId %d, want 0xFFFF", *m.KeySetID)
		}
		f.join(admin, t, ianaReq(0x0001, []uint16{1, 2}, false))
		if ok, _ := f.mgr.HasKeyMapping(ctx, 1, 0x0001); !ok {
			t.Error("group 1 left the operational key map")
		}
		if ok, _ := f.mgr.HasKeyMapping(ctx, 1, 0x0002); ok {
			t.Error("the unmapped group 2 reappeared in the operational key map")
		}
		if !slices.ContainsFunc(f.groupTable(t, 1), func(e core.GroupInfoMapStruct) bool { return e.GroupID == 0x0002 }) {
			t.Error("the unmapped group lost its endpoints")
		}
	})

	t.Run("Groupcast per-fabric cap aligns with GKM maxGroupsPerFabric", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		gkmCap := uintOf(t)(f.gkm.MatterReadFiltered(admin, 0x0002))
		gcMax := uintOf(t)(f.gc.MatterRead(0x0001))
		if gcMax/2 != gkmCap {
			t.Errorf("floor(MaxMembershipCount %d / 2) != MaxGroupsPerFabric %d", gcMax, gkmCap)
		}
	})

	t.Run("mirror fills GKM up to maxGroupsPerFabric without tripping the validator", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		gkmCap := uintOf(t)(f.gkm.MatterReadFiltered(admin, 0x0002))
		for i := 1; i <= gkmCap; i++ {
			f.join(admin, t, ianaReq(uint16(i), []uint16{1}, i == 1)) //nolint:gosec // i ≤ 22
		}
		if n, km := len(f.membership(t)), len(f.keyMap(t, 1)); n != gkmCap || km != gkmCap {
			t.Errorf("membership %d, GroupKeyMap %d, want %d each", n, km, gkmCap)
		}
		_, err := f.gc.MatterInvoke(admin, 0x00, ianaReq(uint16(gkmCap+1), []uint16{1}, false)) //nolint:gosec // 23
		refusal(t, err, im.StatusResourceExhausted, "Per-fabric membership limit reached")
	})

	t.Run("direct GKM attribute write still enforces maxGroupsPerFabric", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		gkmCap := uintOf(t)(f.gkm.MatterReadFiltered(admin, 0x0002))
		tooMany := make([]core.GroupKeyMapStruct, 0, gkmCap+1)
		for i := range gkmCap + 1 {
			tooMany = append(tooMany, core.GroupKeyMapStruct{GroupID: uint16(i + 1), GroupKeySetID: 1, FabricIndex: 1}) //nolint:gosec // ≤ 23
		}
		refusal(t, f.gkm.MatterWrite(admin, 0x0000, tooMany), im.StatusResourceExhausted, "Too many groups per fabric")
	})
}

// TestGroupcastEndpointMirrorParityMatterJS ports the describe "endpoint
// mirror" (GroupcastServerTest.ts:885-936).
func TestGroupcastEndpointMirrorParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)

	t.Run("propagates Membership endpoints to GKM groupTable and FabricGroups.endpoints", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0103, []uint16{1, 2}, true))
		table := f.groupTable(t, 1)
		if len(table) != 1 || table[0].GroupID != 0x0103 || !slices.Equal(slices.Sorted(slices.Values(table[0].Endpoints)), []uint16{1, 2}) {
			t.Errorf("GroupTable %+v, want group 0x0103 on [1 2]", table)
		}
		entries, err := f.mgr.GroupTable(context.Background(), 1)
		if err != nil || len(entries) != 1 || !slices.Equal(entries[0].Endpoints, []uint16{1, 2}) {
			t.Errorf("FabricGroups endpoints %+v, %v", entries, err)
		}
	})

	t.Run("removes endpoints from GKM groupTable and FabricGroups when group is left", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0103, []uint16{1}, true))
		f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 0x0103})
		if table := f.groupTable(t, 1); len(table) != 0 {
			t.Errorf("GroupTable after the leave %+v", table)
		}
		if entries, _ := f.mgr.GroupTable(context.Background(), 1); len(entries) != 0 {
			t.Errorf("FabricGroups endpoints after the leave %+v", entries)
		}
	})
}

// TestGroupcastLegacyRemovalPruneParityMatterJS ports the server-side cases
// of the describe "legacy removal prune" (GroupcastServerTest.ts:938-1122);
// gkm.removeEndpoint(fabric, 1) — Groups RemoveAllGroups on endpoint 1 —
// is the group state's RemoveEndpoint of endpoint 1 from every group.
func TestGroupcastLegacyRemovalPruneParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)
	ctx := context.Background()
	removeAll := func(t *testing.T, f *gcFixture, ep uint16) {
		t.Helper()
		if _, err := f.mgr.RemoveEndpoint(ctx, 1, ep, 0, true); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("legacy RemoveAllGroups fully removes a groupcast listener group", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		req := ianaReq(0x0103, []uint16{1}, true)
		req.UseAuxiliaryACL = boolp(true)
		f.join(admin, t, req)
		if aux := f.auxACL(t, 1); len(aux) != 1 {
			t.Fatalf("auxiliary ACL %+v", aux)
		}
		removeAll(t, f, 1)
		if _, ok := f.memberOf(t, 1, 0x0103); ok {
			t.Error("the group is still a member")
		}
		if _, ok := f.gcProps(t, 1)[0x0103]; ok {
			t.Error("the group kept its groupProperties")
		}
		for _, e := range f.auxACL(t, 1) {
			if slices.Contains(e.Entry.Subjects, 0x0103) {
				t.Errorf("auxiliary ACL entry of the removed group: %+v", e)
			}
		}
	})

	t.Run("legacy RemoveAllGroups does not touch a sender-only group", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0104, []uint16{}, true))
		f.join(admin, t, ianaReq(0x0103, []uint16{1}, false))
		removeAll(t, f, 1)
		if _, ok := f.memberOf(t, 1, 0x0104); !ok {
			t.Error("the sender-only group left")
		}
		if _, ok := f.memberOf(t, 1, 0x0103); ok {
			t.Error("the listener group stayed")
		}
	})

	t.Run("leaveGroup keeps a Sender-feature group as sender-only after emptying its endpoints", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0106, []uint16{1}, true))
		f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 0x0106, Endpoints: []uint16{1}, HasEndpoints: true})
		if _, ok := f.memberOf(t, 1, 0x0106); !ok {
			t.Error("the group left instead of staying sender-only")
		}
	})

	t.Run("a no-op leaveGroup on an already sender-only group does not leak a stale retained mark", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0107, []uint16{}, true))
		f.invoke(admin, 0x01, core.LeaveGroupRequest{GroupID: 0x0107, Endpoints: []uint16{9}, HasEndpoints: true})
		f.join(admin, t, ianaReq(0x0107, []uint16{1}, false))
		f.clearGroupTable(t, 1)
		if _, ok := f.memberOf(t, 1, 0x0107); ok {
			t.Error("the externally removed group stayed a member")
		}
		if _, ok := f.gcProps(t, 1)[0x0107]; ok {
			t.Error("the externally removed group kept its groupProperties")
		}
	})
}

// TestGroupcastPerFabricQuotaFillParityMatterJS ports "two fabrics each
// fill to quota using full M_max (even alignment, no spillover)"
// (GroupcastServerTest.ts:1320-1364).
func TestGroupcastPerFabricQuotaFillParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("two fabrics each fill to quota using full M_max (even alignment, no spillover)", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 2)
		maxCount := uintOf(t)(f.gc.MatterRead(0x0001))
		quota := maxCount / 2
		if 2*quota != maxCount {
			t.Fatalf("MaxMembershipCount %d is odd", maxCount)
		}
		fill := func(fabric uint8, start int) {
			for i := range quota {
				f.join(as(fabric, 5), t, ianaReq(uint16(start+i), []uint16{1}, i == 0)) //nolint:gosec // ≤ 44
			}
		}
		fill(1, 1)
		fill(2, quota+1)
		if n := len(f.membership(t)); n != maxCount {
			t.Errorf("membership %d, want %d", n, maxCount)
		}
		_, err := f.gc.MatterInvoke(as(1, 5), 0x00, ianaReq(uint16(2*quota+1), []uint16{1}, false)) //nolint:gosec // 45
		refusal(t, err, im.StatusResourceExhausted, "Per-fabric membership limit reached")
	})
}

// TestGroupcastMaxMcastAddrCountParityMatterJS ports the describe
// "MaxMcastAddrCount" (GroupcastServerTest.ts:1542-1589) on a server
// configured with MaxMcastAddrCount 4, as matter.js's
// `groupcast: { maxMcastAddrCount: 4 }`.
func TestGroupcastMaxMcastAddrCountParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)
	nodeWithAddrLimit := func(t *testing.T) (*gcFixture, func(groupID uint16, policy uint8) error) {
		t.Helper()
		f := newGCFixtureWith(t, 1, func(cfg *core.GroupcastConfig) { cfg.MaxMcastAddrCount = 4 })
		join := func(groupID uint16, policy uint8) error {
			req := ianaReq(groupID, []uint16{1}, groupID == 1)
			req.McastAddrPolicy = &policy
			_, err := f.gc.MatterInvoke(admin, 0x00, req)
			return err
		}
		for _, id := range []uint16{1, 2, 3} {
			if err := join(id, groups.PolicyPerGroup); err != nil {
				t.Fatal(err)
			}
		}
		for _, id := range []uint16{4, 5} {
			if err := join(id, groups.PolicyIanaAddr); err != nil {
				t.Fatal(err)
			}
		}
		if used := uintOf(t)(f.gc.MatterRead(0x0003)); used != 4 {
			t.Fatalf("UsedMcastAddrCount %d, want 4", used)
		}
		return f, join
	}

	t.Run("rejects a new per-group address beyond the limit and accepts another IANA group", func(t *testing.T) {
		t.Parallel()
		f, join := nodeWithAddrLimit(t)
		refusal(t, join(6, groups.PolicyPerGroup), im.StatusResourceExhausted, "MaxMcastAddrCount limit reached")
		if err := join(6, groups.PolicyIanaAddr); err != nil {
			t.Fatalf("another IANA group: %v", err)
		}
		if used := uintOf(t)(f.gc.MatterRead(0x0003)); used != 4 {
			t.Errorf("UsedMcastAddrCount %d, want 4", used)
		}
	})

	t.Run("rejects switching an existing group to a per-group address beyond the limit", func(t *testing.T) {
		t.Parallel()
		f, join := nodeWithAddrLimit(t)
		refusal(t, join(5, groups.PolicyPerGroup), im.StatusResourceExhausted, "MaxMcastAddrCount limit reached")
		if used := uintOf(t)(f.gc.MatterRead(0x0003)); used != 4 {
			t.Errorf("UsedMcastAddrCount %d, want 4", used)
		}
	})
}

// TestGroupcastTestingDurationParityMatterJS ports the describe
// "groupcastTesting duration" (GroupcastServerTest.ts:1712-1750) on a fake
// clock, MockTime.
func TestGroupcastTestingDurationParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)
	withClock := func(t *testing.T) (*gcFixture, *clocktest.Fake) {
		t.Helper()
		clk := clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		f := newGCFixtureWith(t, 1, func(cfg *core.GroupcastConfig) { cfg.TestingTimer, cfg.Clock = nil, clk })
		return f, clk
	}
	underTest := func(t *testing.T, f *gcFixture) int { t.Helper(); return uintOf(t)(f.gc.MatterRead(0x0004)) }
	runTesting := func(t *testing.T, f *gcFixture, op uint8, seconds *uint16) {
		t.Helper()
		if _, st := f.invoke(admin, 0x05, core.GroupcastTestingRequest{TestOperation: op, DurationSeconds: seconds}); st != im.StatusSuccess {
			t.Fatal(st)
		}
	}

	t.Run("ends testing after 60 seconds when the request omits the duration", func(t *testing.T) {
		t.Parallel()
		f, clk := withClock(t)
		runTesting(t, f, 1, nil)
		if got := underTest(t, f); got != 1 {
			t.Fatalf("FabricUnderTest %d, want 1", got)
		}
		clk.Advance(59 * time.Second)
		if got := underTest(t, f); got != 1 {
			t.Errorf("FabricUnderTest after 59 s %d, want 1", got)
		}
		clk.Advance(time.Second)
		if got := underTest(t, f); got != 0 {
			t.Errorf("FabricUnderTest after 60 s %d, want none", got)
		}
	})

	t.Run("ends testing after the duration of the latest request once disabled and enabled again", func(t *testing.T) {
		t.Parallel()
		f, clk := withClock(t)
		runTesting(t, f, 1, u16p(10))
		clk.Advance(5 * time.Second)
		runTesting(t, f, 0, nil)
		runTesting(t, f, 1, u16p(100))
		clk.Advance(10 * time.Second)
		if got := underTest(t, f); got != 1 {
			t.Errorf("FabricUnderTest 10 s into the second period %d, want 1", got)
		}
		clk.Advance(90 * time.Second)
		if got := underTest(t, f); got != 0 {
			t.Errorf("FabricUnderTest after the second period %d, want none", got)
		}
	})
}

// TestGroupcastTestingEventsParityMatterJS ports the describe
// "groupcastTesting events" (GroupcastServerTest.ts:1752-1915).
// SessionManager.emitGroupMessage is the group state's ReportGroupMessage;
// its sourceIp "fe80::1%en0" arrives here without the zone, which the
// receive path keeps in the UDP address, not the IP.
func TestGroupcastTestingEventsParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)
	iana := groups.IANAGroupcastAddress.To16()
	enable := func(t *testing.T, f *gcFixture, fabric uint8) {
		t.Helper()
		if _, st := f.invoke(as(fabric, 5), 0x05, core.GroupcastTestingRequest{TestOperation: 1, DurationSeconds: u16p(60)}); st != im.StatusSuccess {
			t.Fatal(st)
		}
		f.emitter.take()
	}
	events := func(f *gcFixture) []core.GroupcastTestingEvent {
		var out []core.GroupcastTestingEvent
		for _, e := range f.emitter.take() {
			if e.cluster == 0x0065 && e.event == 0 {
				out = append(out, e.data.(core.GroupcastTestingEvent))
			}
		}
		return out
	}
	allowed := true

	t.Run("derives multicast destination and source addresses for testing events", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0001, []uint16{1}, true))
		enable(t, f, 1)
		f.mgr.ReportGroupMessage(groups.GroupMessageEvent{
			Result: groups.TestResultSuccess, FabricIndex: 1, GroupID: 1, HasGroupID: true,
			SourceIP: net.ParseIP("fd00::1"), HasEndpoint: true, EndpointID: 1, AccessAllowed: &allowed,
		})
		f.mgr.ReportGroupMessage(groups.GroupMessageEvent{
			Result: groups.TestResultNoAvailableKey, HeaderGroupID: 1, HasHeaderGroupID: true, SourceIP: net.ParseIP("fe80::1"),
		})
		f.mgr.ReportGroupMessage(groups.GroupMessageEvent{Result: groups.TestResultNoAvailableKey, SourceIP: net.ParseIP("fd00::2")})
		evs := events(f)
		if len(evs) != 3 {
			t.Fatalf("events %+v, want 3", evs)
		}
		if e := evs[2]; e.GroupcastTestResult != groups.TestResultNoAvailableKey || e.GroupID != nil || !slices.Equal(e.DestinationIPAddress, iana) {
			t.Errorf("event 2 %+v", e)
		}
		if e := evs[0]; e.GroupcastTestResult != groups.TestResultSuccess || e.GroupID == nil || *e.GroupID != 1 || e.AccessAllowed == nil || !*e.AccessAllowed ||
			!slices.Equal(e.SourceIPAddress, net.ParseIP("fd00::1").To16()) || !slices.Equal(e.DestinationIPAddress, iana) {
			t.Errorf("event 0 %+v", e)
		}
		if e := evs[1]; e.GroupcastTestResult != groups.TestResultNoAvailableKey || e.GroupID != nil ||
			!slices.Equal(e.SourceIPAddress, net.ParseIP("fe80::1").To16()) || !slices.Equal(e.DestinationIPAddress, iana) {
			t.Errorf("event 1 %+v", e)
		}
	})

	t.Run("drops messages that another fabric authenticated", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 2)
		enable(t, f, 1)
		for _, fabric := range []uint8{2, 1} {
			f.mgr.ReportGroupMessage(groups.GroupMessageEvent{
				Result: groups.TestResultNoAvailableKey, FabricIndex: fabric, HeaderGroupID: 1, HasHeaderGroupID: true, SourceIP: net.ParseIP("fd00::1"),
			})
		}
		evs := events(f)
		if len(evs) != 1 || evs[0].FabricIndex != 1 || !slices.Equal(evs[0].DestinationIPAddress, iana) {
			t.Errorf("events %+v, want one of fabric 1 from the IANA address", evs)
		}
	})

	t.Run("derives the destination from each group's policy when groups share a key set", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		f.join(admin, t, ianaReq(0x0001, []uint16{1}, true))
		perGroup := ianaReq(0x0002, []uint16{1}, false)
		perGroup.McastAddrPolicy = u8p(groups.PolicyPerGroup)
		f.join(admin, t, perGroup)
		enable(t, f, 1)
		for _, gid := range []uint16{1, 2} {
			f.mgr.ReportGroupMessage(groups.GroupMessageEvent{
				Result: groups.TestResultSuccess, FabricIndex: 1, GroupID: gid, HasGroupID: true,
				SourceIP: net.ParseIP("fd00::1"), HasEndpoint: true, EndpointID: 1, AccessAllowed: &allowed,
			})
		}
		addr, err := f.mgr.MulticastAddressFor(context.Background(), 1, 0x0002)
		if err != nil || addr.Equal(groups.IANAGroupcastAddress) {
			t.Fatalf("per-group address %v, %v", addr, err)
		}
		evs := events(f)
		if len(evs) != 2 || !slices.Equal(evs[0].DestinationIPAddress, iana) || !slices.Equal(evs[1].DestinationIPAddress, addr.To16()) {
			t.Errorf("destinations %+v, want [IANA, %v]", evs, addr)
		}
	})
}

// TestGroupcastDerivedMembershipParityMatterJS ports the server-side cases
// of the describe "derived membership" (GroupcastServerTest.ts:1918-1941,
// 1976-2000, 2145-2168); the reload cases run in node/.
func TestGroupcastDerivedMembershipParityMatterJS(t *testing.T) {
	t.Parallel()
	admin := as(1, 5)

	t.Run("derives Membership from groupProperties + GKM tables", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		req := ianaReq(0x0102, []uint16{1}, true)
		req.KeySetID, req.UseAuxiliaryACL = 3, boolp(true)
		f.join(admin, t, req)
		m, ok := f.memberOf(t, 1, 0x0102)
		if !ok || !slices.Equal(m.Endpoints, []uint16{1}) || *m.KeySetID != 3 || !m.HasAuxiliaryACL || m.McastAddrPolicy != groups.PolicyIanaAddr {
			t.Errorf("membership %+v (%v)", m, ok)
		}
	})

	t.Run("legacy-only group appears in Membership with default flags", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		if err := f.mgr.AddEndpointForGroup(context.Background(), 1, 0x0201, 1, "Legacy"); err != nil {
			t.Fatal(err)
		}
		f.writeKeyMap(t, 1, core.GroupKeyMapStruct{GroupID: 0x0201, GroupKeySetID: 5})
		if _, ok := f.gcProps(t, 1)[0x0201]; ok {
			t.Fatal("a legacy group has groupProperties")
		}
		m, ok := f.memberOf(t, 1, 0x0201)
		if !ok || m.McastAddrPolicy != groups.PolicyPerGroup || m.HasAuxiliaryACL {
			t.Errorf("membership %+v (%v), want PerGroup without auxiliary ACL", m, ok)
		}
	})

	t.Run("Membership$Changed fires on join", func(t *testing.T) {
		t.Parallel()
		f := newGCFixture(t, 1)
		var fired atomic.Bool
		unsub := f.gc.OnMatterValueChanged(func() { fired.Store(true) })
		defer unsub()
		f.join(admin, t, ianaReq(0x0001, []uint16{1}, true))
		if !fired.Load() {
			t.Error("no change notification on JoinGroup")
		}
	})
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

// Behavioural parity for the Groups cluster against matter.js HEAD
// packages/node/src/behaviors/groups/GroupsServer.ts and the group table
// it keeps in GroupKeyManagementServer (addEndpointForGroup /
// removeEndpoint). Each case names the matter.js function it pins.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// groupsFixture is fabric 1 with a GroupKeyManagement server and the
// Groups servers of endpoints 3 and 4, sharing one group manager.
type groupsFixture struct {
	ctx      context.Context
	store    *fakeStore
	gkm      *core.GroupKeyManagement
	mgr      *groups.Manager
	ep3, ep4 *core.Groups
	identify uint16
}

func newGroupsFixture(t *testing.T, maxGroups uint16) *groupsFixture {
	t.Helper()
	fs := newFakeStore()
	if _, err := fs.AddFabric(context.Background(), mstore.FabricRecord{FabricID: 0x0FAB, CompressedID: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}}); err != nil {
		t.Fatal(err)
	}
	mgr, err := groups.NewManager(fs, nil)
	if err != nil {
		t.Fatal(err)
	}
	gkm, err := core.NewGroupKeyManagement(fs, core.GroupKeyMgmtConfig{MaxGroupsPerFabric: maxGroups, Groups: mgr})
	if err != nil {
		t.Fatal(err)
	}
	f := &groupsFixture{ctx: im.WithFabricFilter(context.Background(), true, 1), store: fs, gkm: gkm, mgr: mgr}
	if f.ep3, err = core.NewGroups(3, mgr, func() uint16 { return f.identify }); err != nil {
		t.Fatal(err)
	}
	if f.ep4, err = core.NewGroups(4, mgr, nil); err != nil {
		t.Fatal(err)
	}
	// Key set 0x10 and a GroupKeyMap binding groups 1..5 to it.
	if _, err := gkm.MatterInvoke(f.ctx, 0x00, core.KeySetWriteRequest{GroupKeySet: core.GroupKeySetStruct{
		GroupKeySetID: 0x10, EpochKey0: make([]byte, 16), EpochStartTime0: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	mapped := uint16(5)
	if maxGroups != 0 && maxGroups < mapped {
		mapped = maxGroups
	}
	f.mapGroups(t, 1, mapped)
	return f
}

// mapGroups writes a GroupKeyMap binding groups first..last to key set 0x10.
func (f *groupsFixture) mapGroups(t *testing.T, first, last uint16) {
	t.Helper()
	var keyMap []core.GroupKeyMapStruct
	for gid := first; gid <= last; gid++ {
		keyMap = append(keyMap, core.GroupKeyMapStruct{GroupID: gid, GroupKeySetID: 0x10})
	}
	if err := f.gkm.MatterWrite(f.ctx, 0x0000, keyMap); err != nil {
		t.Fatal(err)
	}
}

func (f *groupsFixture) invoke(t *testing.T, srv *core.Groups, cmd uint32, fields any) (any, im.StatusCode) {
	t.Helper()
	resp, err := srv.MatterInvoke(f.ctx, cmd, fields)
	if err == nil {
		return resp, im.StatusSuccess
	}
	var sce im.StatusCodeError
	if !errors.As(err, &sce) {
		return nil, im.StatusFailure
	}
	return resp, sce.MatterStatusCode()
}

func (f *groupsFixture) addGroup(t *testing.T, srv *core.Groups, gid uint16, name string) im.StatusCode {
	t.Helper()
	resp, status := f.invoke(t, srv, 0x00, core.AddGroupRequest{GroupID: gid, GroupName: name})
	if status != im.StatusSuccess {
		t.Fatalf("AddGroup(%d) failed: %v", gid, status)
	}
	r := resp.(core.AddGroupResponse)
	if r.GroupID != gid {
		t.Fatalf("AddGroupResponse.GroupID = %d, want %d", r.GroupID, gid)
	}
	return r.Status
}

func TestGroupsParityMatterJS(t *testing.T) {
	t.Parallel()

	t.Run("initialize: NameSupport and FeatureMap advertise GroupNames", func(t *testing.T) {
		t.Parallel()
		f := newGroupsFixture(t, 0)
		if v, _ := f.ep3.MatterRead(0x0000); v != uint8(0x80) {
			t.Errorf("NameSupport = %v, want 0x80", v)
		}
		if v, _ := f.ep3.MatterRead(cluster.AttrGlobalFeatureMap); v != uint32(1) {
			t.Errorf("FeatureMap = %v, want 1", v)
		}
		if v, _ := f.ep3.MatterRead(cluster.AttrGlobalClusterRevision); v != uint16(4) {
			t.Errorf("ClusterRevision = %v, want 4 (groups.element.ts)", v)
		}
	})

	t.Run("addGroup", func(t *testing.T) {
		t.Parallel()
		f := newGroupsFixture(t, 0)
		if got := f.addGroup(t, f.ep3, 0, "x"); got != im.StatusConstraintError {
			t.Errorf("GroupId 0: %v, want ConstraintError", got)
		}
		if got := f.addGroup(t, f.ep3, 1, strings.Repeat("n", 17)); got != im.StatusConstraintError {
			t.Errorf("17-character name: %v, want ConstraintError", got)
		}
		if got := f.addGroup(t, f.ep3, 1, strings.Repeat("ü", 16)); got != im.StatusSuccess {
			t.Errorf("16-character name: %v, want Success (length counts characters, not bytes)", got)
		}
		if got := f.addGroup(t, f.ep3, 0x0700, "unmapped"); got != im.StatusUnsupportedAccess {
			t.Errorf("group without a GroupKeyMap entry: %v, want UnsupportedAccess", got)
		}
	})

	t.Run("addEndpointForGroup: maxGroupsPerFabric", func(t *testing.T) {
		t.Parallel()
		f := newGroupsFixture(t, 2)
		for _, gid := range []uint16{1, 2} {
			if got := f.addGroup(t, f.ep3, gid, ""); got != im.StatusSuccess {
				t.Fatalf("group %d: %v", gid, got)
			}
		}
		// The key map may move on while the table still holds two groups;
		// the table's own cap is what AddGroup then hits.
		f.mapGroups(t, 2, 3)
		if got := f.addGroup(t, f.ep3, 3, ""); got != im.StatusResourceExhausted {
			t.Errorf("third group at cap 2: %v, want ResourceExhausted", got)
		}
		if got := f.addGroup(t, f.ep4, 2, ""); got != im.StatusSuccess {
			t.Errorf("existing group from another endpoint at the cap: %v, want Success", got)
		}
	})

	t.Run("viewGroup", func(t *testing.T) {
		t.Parallel()
		f := newGroupsFixture(t, 0)
		f.addGroup(t, f.ep3, 1, "Kitchen")
		resp, _ := f.invoke(t, f.ep3, 0x01, core.ViewGroupRequest{GroupID: 1})
		if r := resp.(core.ViewGroupResponse); r.Status != im.StatusSuccess || r.GroupName != "Kitchen" {
			t.Errorf("ViewGroup = %+v", r)
		}
		resp, _ = f.invoke(t, f.ep4, 0x01, core.ViewGroupRequest{GroupID: 1})
		if r := resp.(core.ViewGroupResponse); r.Status != im.StatusNotFound || r.GroupName != "" {
			t.Errorf("ViewGroup on a non-member endpoint = %+v, want NotFound", r)
		}
		resp, _ = f.invoke(t, f.ep3, 0x01, core.ViewGroupRequest{GroupID: 0})
		if r := resp.(core.ViewGroupResponse); r.Status != im.StatusConstraintError {
			t.Errorf("ViewGroup(0) = %+v, want ConstraintError", r)
		}
	})

	t.Run("getGroupMembership", func(t *testing.T) {
		t.Parallel()
		f := newGroupsFixture(t, 0)
		f.addGroup(t, f.ep3, 2, "")
		f.addGroup(t, f.ep3, 1, "")
		f.addGroup(t, f.ep4, 3, "")
		resp, _ := f.invoke(t, f.ep3, 0x02, core.GetGroupMembershipRequest{})
		r := resp.(core.GetGroupMembershipResponse)
		if r.Capacity != 0xFE-2 || !slices.Equal(r.GroupList, []uint16{1, 2}) {
			t.Errorf("all groups = %+v, want capacity 0xFC and [1 2]", r)
		}
		resp, _ = f.invoke(t, f.ep3, 0x02, core.GetGroupMembershipRequest{GroupList: []uint16{3, 2, 9}})
		if r := resp.(core.GetGroupMembershipResponse); !slices.Equal(r.GroupList, []uint16{2}) || r.Capacity != 0xFC {
			t.Errorf("filtered = %+v, want [2]", r)
		}
		if _, status := f.invoke(t, f.ep3, 0x02, core.GetGroupMembershipRequest{GroupList: []uint16{0}}); status != im.StatusConstraintError {
			t.Errorf("GroupList entry 0: %v, want ConstraintError (all[min 1])", status)
		}
	})

	t.Run("removeGroup and removeAllGroups", func(t *testing.T) {
		t.Parallel()
		f := newGroupsFixture(t, 0)
		f.addGroup(t, f.ep3, 1, "")
		f.addGroup(t, f.ep3, 2, "")
		f.addGroup(t, f.ep4, 2, "")
		resp, _ := f.invoke(t, f.ep3, 0x03, core.RemoveGroupRequest{GroupID: 1})
		if r := resp.(core.RemoveGroupResponse); r.Status != im.StatusSuccess || r.GroupID != 1 {
			t.Errorf("RemoveGroup = %+v", r)
		}
		resp, _ = f.invoke(t, f.ep3, 0x03, core.RemoveGroupRequest{GroupID: 1})
		if r := resp.(core.RemoveGroupResponse); r.Status != im.StatusNotFound {
			t.Errorf("RemoveGroup again = %+v, want NotFound", r)
		}
		resp, _ = f.invoke(t, f.ep3, 0x03, core.RemoveGroupRequest{GroupID: 0})
		if r := resp.(core.RemoveGroupResponse); r.Status != im.StatusConstraintError {
			t.Errorf("RemoveGroup(0) = %+v, want ConstraintError", r)
		}
		if _, status := f.invoke(t, f.ep3, 0x04, nil); status != im.StatusSuccess {
			t.Fatalf("RemoveAllGroups: %v", status)
		}
		table, _ := f.gkm.MatterReadFiltered(f.ctx, 0x0001)
		got := table.([]core.GroupInfoMapStruct)
		if len(got) != 1 || got[0].GroupID != 2 || !slices.Equal(got[0].Endpoints, []uint16{4}) {
			t.Errorf("GroupTable after RemoveAllGroups on ep 3 = %+v, want only group 2 on ep 4", got)
		}
	})

	t.Run("addGroupIfIdentifying", func(t *testing.T) {
		t.Parallel()
		f := newGroupsFixture(t, 0)
		if _, status := f.invoke(t, f.ep3, 0x05, core.AddGroupIfIdentifyingRequest{GroupID: 1}); status != im.StatusSuccess {
			t.Fatalf("not identifying: %v, want Success", status)
		}
		if table, _ := f.mgr.GroupTable(f.ctx, 1); len(table) != 0 {
			t.Fatalf("a group was added while not identifying: %+v", table)
		}
		f.identify = 10
		if _, status := f.invoke(t, f.ep3, 0x05, core.AddGroupIfIdentifyingRequest{GroupID: 1, GroupName: "id"}); status != im.StatusSuccess {
			t.Fatalf("identifying: %v", status)
		}
		if table, _ := f.mgr.GroupTable(f.ctx, 1); len(table) != 1 || table[0].GroupName != "id" {
			t.Fatalf("group not added while identifying: %+v", table)
		}
		if _, status := f.invoke(t, f.ep3, 0x05, core.AddGroupIfIdentifyingRequest{GroupID: 0x0700}); status != im.StatusUnsupportedAccess {
			t.Errorf("identifying, unmapped group: %v, want the AddGroup status UnsupportedAccess", status)
		}
	})

	t.Run("GroupKeyManagement.GroupTable reflects membership", func(t *testing.T) {
		t.Parallel()
		f := newGroupsFixture(t, 0)
		before := f.gkm.MatterDataVersion()
		f.addGroup(t, f.ep3, 1, "Kitchen")
		f.addGroup(t, f.ep4, 1, "Kitchen")
		if f.gkm.MatterDataVersion() == before {
			t.Error("GroupTable change left the GroupKeyManagement DataVersion unchanged")
		}
		table, _ := f.gkm.MatterReadFiltered(f.ctx, 0x0001)
		got := table.([]core.GroupInfoMapStruct)
		if len(got) != 1 || got[0].GroupName != "Kitchen" || !slices.Equal(got[0].Endpoints, []uint16{3, 4}) || got[0].FabricIndex != 1 {
			t.Errorf("GroupTable = %+v", got)
		}
		// Another fabric sees nothing.
		other := im.WithFabricFilter(context.Background(), true, 2)
		if v, ok := f.gkm.MatterReadFiltered(other, 0x0001); ok && len(v.([]core.GroupInfoMapStruct)) != 0 {
			t.Errorf("fabric 2 sees fabric 1's groups: %+v", v)
		}
	})
}

func TestGroupsCommandsNeedAFabric(t *testing.T) {
	t.Parallel()
	f := newGroupsFixture(t, 0)
	_, err := f.ep3.MatterInvoke(context.Background(), 0x00, core.AddGroupRequest{GroupID: 1})
	var sce im.StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusUnsupportedAccess {
		t.Fatalf("command without a fabric: %v, want UnsupportedAccess", err)
	}
}

func TestGroupsRejectsWrongFieldsAndUnknownCommands(t *testing.T) {
	t.Parallel()
	f := newGroupsFixture(t, 0)
	for _, cmd := range []uint32{0x00, 0x01, 0x02, 0x03, 0x05} {
		if _, status := f.invoke(t, f.ep3, cmd, map[uint8]any{}); status != im.StatusInvalidCommand {
			t.Errorf("command 0x%02X with the wrong fields: %v, want InvalidCommand", cmd, status)
		}
	}
	if _, status := f.invoke(t, f.ep3, 0x09, nil); status != im.StatusUnsupportedCommand {
		t.Errorf("unknown command: %v, want UnsupportedCommand", status)
	}
	if err := f.ep3.MatterWrite(f.ctx, 0x0000, uint8(0)); err == nil {
		t.Error("NameSupport accepted a write")
	}
	if _, err := core.NewGroups(3, nil, nil); err == nil {
		t.Error("NewGroups without a manager succeeded")
	}
}

func TestGroupsServerSurface(t *testing.T) {
	t.Parallel()
	f := newGroupsFixture(t, 0)
	s := f.ep3
	if s.MatterClusterID() != core.GroupsClusterID {
		t.Fatal("cluster id")
	}
	if !slices.Equal(s.MatterAcceptedCommands(), []uint32{0, 1, 2, 3, 4, 5}) || !slices.Equal(s.MatterGeneratedCommands(), []uint32{0, 1, 2, 3}) {
		t.Fatal("command lists")
	}
	for cmd, want := range map[uint32]uint8{0: 4, 1: 3, 2: 3, 3: 4, 4: 4, 5: 4} {
		if got := s.MinInvokePrivilege(cmd); got != want {
			t.Errorf("MinInvokePrivilege(0x%02X) = %d, want %d (groups.element.ts access)", cmd, got, want)
		}
	}
	if !slices.Equal(s.MatterReportable(), []uint32{0}) || !slices.Equal(s.MatterAttributes(), []uint32{0}) {
		t.Fatal("attribute lists")
	}
	for _, attr := range []uint32{cluster.AttrGlobalAcceptedCommandList, cluster.AttrGlobalGeneratedCommandList, cluster.AttrGlobalEventList, cluster.AttrGlobalAttributeList} {
		if _, ok := s.MatterRead(attr); !ok {
			t.Errorf("global 0x%04X not served", attr)
		}
	}
	if _, ok := s.MatterRead(0x0042); ok {
		t.Error("unknown attribute served")
	}
}

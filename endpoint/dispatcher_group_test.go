// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
)

func groupEntry(f uint8, priv store.Privilege, subjects []uint64, targets []store.ACLTarget) store.ACLEntry {
	return store.ACLEntry{FabricIndex: f, Privilege: priv, AuthMode: store.AuthModeGroup, Subjects: subjects, Targets: targets}
}

// TestCheckACLForAGroupMessage pins the Group auth mode of matter.js
// FabricAccessControl: only Group entries apply, the subject is the group
// id (an empty list matches every group), a message without a valid key
// mapping matches nothing, and a Group entry granting Administer denies.
func TestCheckACLForAGroupMessage(t *testing.T) {
	t.Parallel()
	ep3 := uint16(3)
	onOff := uint32(0x0006)
	group := func(id uint16, valid bool) context.Context {
		return im.WithGroupSubject(context.Background(), im.GroupSubject{GroupID: id, HasValidMapping: valid, Endpoints: []uint16{3}})
	}
	cases := []struct {
		name    string
		entries []store.ACLEntry
		ctx     context.Context
		priv    uint8
		want    im.StatusCode
	}{
		{"Group entry for the group grants Operate", []store.ACLEntry{groupEntry(1, store.PrivilegeOperate, []uint64{0x0101}, nil)}, group(0x0101, true), privOperate, im.StatusSuccess},
		{"Group entry for another group", []store.ACLEntry{groupEntry(1, store.PrivilegeOperate, []uint64{0x0202}, nil)}, group(0x0101, true), privOperate, im.StatusUnsupportedAccess},
		{"empty subjects match every group", []store.ACLEntry{groupEntry(1, store.PrivilegeOperate, nil, nil)}, group(0x0101, true), privOperate, im.StatusSuccess},
		{"Operate does not cover Manage", []store.ACLEntry{groupEntry(1, store.PrivilegeOperate, nil, nil)}, group(0x0101, true), privManage, im.StatusUnsupportedAccess},
		{"a CASE entry does not apply to a group message", []store.ACLEntry{caseEntry(1, store.PrivilegeAdminister, nil)}, group(0x0101, true), privView, im.StatusUnsupportedAccess},
		{"no valid key mapping matches nothing", []store.ACLEntry{groupEntry(1, store.PrivilegeOperate, nil, nil)}, group(0x0101, false), privView, im.StatusUnsupportedAccess},
		{"a Group entry may never grant Administer", []store.ACLEntry{groupEntry(1, store.PrivilegeAdminister, nil, nil)}, group(0x0101, true), privView, im.StatusUnsupportedAccess},
		{"targets still apply", []store.ACLEntry{groupEntry(1, store.PrivilegeOperate, nil, []store.ACLTarget{{Endpoint: &ep3, Cluster: &onOff}})}, group(0x0101, true), privOperate, im.StatusSuccess},
		{"a target elsewhere does not", []store.ACLEntry{groupEntry(1, store.PrivilegeOperate, nil, []store.ACLTarget{{Cluster: &onOff}})}, group(0x0101, true), privOperate, im.StatusUnsupportedAccess},
		{"a Group entry does not grant a unicast CASE request", []store.ACLEntry{groupEntry(1, store.PrivilegeOperate, nil, nil)}, context.Background(), privOperate, im.StatusUnsupportedAccess},
	}
	for _, tc := range cases {
		d := &TopologyDispatcher{topology: &Topology{}}
		d.SetACLLister(fakeACLLister{entries: tc.entries})
		cluster := uint32(0x0008)
		if tc.name == "targets still apply" {
			cluster = onOff
		}
		if got := d.CheckACL(tc.ctx, 1, 0x1B669, nil, 3, cluster, tc.priv); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// commandServer is a cluster server accepting a fixed command set.
type commandServer struct {
	fakeServer
	accepted []uint32
	invoked  *[]uint16
	ep       uint16
}

func (c commandServer) MatterAcceptedCommands() []uint32  { return c.accepted }
func (c commandServer) MatterGeneratedCommands() []uint32 { return nil }
func (c commandServer) MatterInvoke(context.Context, uint32, any) (any, error) {
	*c.invoked = append(*c.invoked, c.ep)
	return nil, nil
}

// TestInvokeAuthorizedExpandsToHostingEndpoints pins the wildcard command
// dispatch a group Invoke rides on (matter.js CommandInvokeResponse
// #processWildcard / #wildcardTargetOf): every endpoint hosting the
// cluster and accepting the command, in endpoint order, minus the ones the
// authorizer refuses.
func TestInvokeAuthorizedExpandsToHostingEndpoints(t *testing.T) {
	t.Parallel()
	var invoked []uint16
	mk := func(id uint16, accepted []uint32) *Endpoint {
		srv := commandServer{fakeServer: fakeServer{id: 0x0006}, accepted: accepted, invoked: &invoked, ep: id}
		return &Endpoint{
			ID: id, DeviceType: 0x0100, FriendlyName: "L", SourceKey: StringKey("k"),
			Source: deviceTypeSource{dt: 0x0100, servers: []contract.ClusterServer{srv}},
		}
	}
	topo := &Topology{Endpoints: []*Endpoint{{ID: 0}, {ID: 1}, mk(2, []uint32{2}), mk(3, []uint32{0}), mk(4, []uint32{2}), mk(5, []uint32{2})}}
	d := NewTopologyDispatcher(topo)
	path := im.ConcreteCommandPath{Cluster: 0x0006, Command: 2, HasCluster: true, HasCommand: true}
	res := d.InvokeAuthorized(context.Background(), path, nil, func(ep uint16, _, _ uint32) im.StatusCode {
		if ep == 5 {
			return im.StatusUnsupportedAccess
		}
		return im.StatusSuccess
	})
	if !slices.Equal(invoked, []uint16{2, 4}) || len(res) != 2 || res[0].Path.Endpoint != 2 || !res[0].Path.HasEndpoint {
		t.Fatalf("invoked %v (results %+v), want endpoints 2 and 4", invoked, res)
	}
	// A concrete path goes through Invoke.
	invoked = nil
	concrete := path
	concrete.Endpoint, concrete.HasEndpoint = 4, true
	if res := d.InvokeAuthorized(context.Background(), concrete, nil, nil); len(res) != 1 || !slices.Equal(invoked, []uint16{4}) {
		t.Fatalf("concrete path: %+v / %v", res, invoked)
	}
}

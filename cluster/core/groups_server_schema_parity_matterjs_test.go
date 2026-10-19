// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/groups/GroupsServerSchemaTest.ts
// at 862ad75f: GroupsServer relaxes the AddGroup GroupId "min 1" and
// GroupName "max 16" constraints (GroupsServer.ts:20-48), so an
// out-of-range value reaches the handler, which answers ConstraintError in
// its AddGroupResponse (:94-103) instead of the interaction layer refusing
// the request with a bare status.
//
// matter.js compares GroupsServer.schema with the base Groups.schema and
// invokes AddGroup on endpoint 1 of a MockServerNode (interaction.invoke).
// The base schema is the generated definition (cluster/spec/groups, whose
// AddGroupRequest decoder keeps both constraints); the server's schema is
// the request decoding the node's invoke path applies
// (bridge/fields_reader_groups.go, which accepts any uint16 and any name).
// The port asserts the base decoder's refusal and invokes AddGroup over
// the wire of a fabrictest pair on the endpoint of a bridged OnOffLight.
package core_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	groupsdef "github.com/SukramJ/go-fabric/cluster/spec/groups"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/tlv"
)

// anOverlongName is AN_OVERLONG_NAME: the base GroupName constraint is "max 16".
var anOverlongName = strings.Repeat("X", 17)

// encodeNode encodes v and reads it back as the spec.Node a decoder takes.
func encodeNode(t *testing.T, v spec.Encodable) spec.Node {
	t.Helper()
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	raw, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	dec := tlv.NewDecoder(raw)
	open, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	n, err := spec.ReadContainer(dec, open)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// invokeAddGroup is invokeAddGroup (GroupsServerSchemaTest.ts:37-62): AddGroup
// on the light's endpoint over a commissioned pair; it returns the
// response's Status and GroupId, failing when the device answered with a
// bare status instead of a command response.
func invokeAddGroup(t *testing.T, groupID uint16, groupName string) (status im.StatusCode, respGroupID uint16) {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light"), Name: "light", Clock: site.Clock()})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ep, ok := pair.Device.EndpointOf("light")
	if !ok {
		t.Fatal("no light endpoint")
	}
	res, err := fabrictest.Invoke(context.Background(), pair,
		im.ConcreteCommandPath{Endpoint: ep, Cluster: groupsdef.ClusterID, Command: groupsdef.CmdAddGroup},
		groupsdef.AddGroupRequest{GroupId: groupID, GroupName: groupName})
	if err != nil {
		t.Fatalf("AddGroup answered with a status, not a command response: %v", err)
	}
	if !res.HasFields || res.Command != groupsdef.CmdAddGroupResponse {
		t.Fatalf("AddGroup result %+v, want an AddGroupResponse", res)
	}
	// The response's GroupId is relaxed too (GroupsServer.ts:41), so it is
	// read without the generated decoder's "min 1".
	var st groupsdef.Status
	if err := spec.DecodeFields(res.Fields, "AddGroupResponse",
		spec.Bind(0, "Status", true, &st, spec.DecodeUint[groupsdef.Status](8)),
		spec.Bind(1, "GroupId", true, &respGroupID, spec.DecodeUint[uint16](16)),
	); err != nil {
		t.Fatalf("AddGroupResponse: %v", err)
	}
	return im.StatusCode(st), respGroupID
}

// TestGroupsServerSchemaRelaxation ports the three cases of "GroupsServer
// schema relaxation".
func TestGroupsServerSchemaRelaxation(t *testing.T) {
	t.Parallel()

	// :65-77 — the base AddGroup keeps GroupId min 1 and GroupName max 16;
	// the server's request path does not (the two cases below).
	t.Run("relaxes AddGroup GroupId/GroupName constraints relative to the base cluster", func(t *testing.T) {
		t.Parallel()
		var req groupsdef.AddGroupRequest
		if err := req.DecodeTLV(encodeNode(t, groupsdef.AddGroupRequest{GroupId: 0, GroupName: "grp"})); err == nil {
			t.Error("the base AddGroup accepted GroupId 0, want its min 1")
		}
		if err := req.DecodeTLV(encodeNode(t, groupsdef.AddGroupRequest{GroupId: 1, GroupName: anOverlongName})); err == nil {
			t.Error("the base AddGroup accepted a 17-character GroupName, want its max 16")
		}
	})

	// :79-88
	t.Run("accepts an over-long GroupName and returns a command response from the handler", func(t *testing.T) {
		t.Parallel()
		status, gid := invokeAddGroup(t, 1, anOverlongName)
		if status != im.StatusConstraintError || gid != 1 {
			t.Errorf("AddGroupResponse = %v, %d; want ConstraintError, 1", status, gid)
		}
	})

	// :90-99
	t.Run("accepts an out-of-range GroupId and returns a command response from the handler", func(t *testing.T) {
		t.Parallel()
		status, gid := invokeAddGroup(t, 0, "grp")
		if status != im.StatusConstraintError || gid != 0 {
			t.Errorf("AddGroupResponse = %v, %d; want ConstraintError, 0", status, gid)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"strings"
	"testing"

	gkmdef "thingfabric.dev/go-fabric/cluster/spec/groupkeymanagement"
	groupsdef "thingfabric.dev/go-fabric/cluster/spec/groups"
	"thingfabric.dev/go-fabric/device"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/node"
)

// TestStoredGroupNameOverSixteenBytesIsServed measures what a device does
// with a group name stored while AddGroup's "max 16" counted UTF-16 code
// units (9 × "é": 9 units, 18 bytes) — now over the bound in UTF-8 bytes
// (#4995). Over the wire of a commissioned pair, ViewGroup answers SUCCESS
// with the name as stored and GroupKeyManagement's GroupTable reads it:
// neither the response check nor the read encoder refuses it, so the
// module keeps serving it as stored (pin-lift unit P2b-A, coordinator
// question 3: accepted, this test the proof).
func TestStoredGroupNameOverSixteenBytesIsServed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	light, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("light"), Name: "light"})
	if err != nil {
		t.Fatal(err)
	}
	pair := site.CommissionedPair(fabrictest.PairOptions{Device: fabrictest.Topology{Bridged: []node.Endpoint{light}}})
	ep, ok := pair.Device.EndpointOf("light")
	if !ok {
		t.Fatal("no light endpoint")
	}
	fabrics, err := pair.Device.Node().FabricInfos(ctx)
	if err != nil || len(fabrics) != 1 {
		t.Fatalf("device fabrics %v, %v; want the pair's one", fabrics, err)
	}
	name := strings.Repeat("é", 9)
	if err := pair.Device.Node().Groups().AddEndpointForGroup(ctx, fabrics[0].FabricIndex, 0x0101, ep, name); err != nil {
		t.Fatal(err)
	}

	res, err := fabrictest.Invoke(ctx, pair,
		im.ConcreteCommandPath{Endpoint: ep, Cluster: groupsdef.ClusterID, Command: groupsdef.CmdViewGroup},
		groupsdef.ViewGroupRequest{GroupId: 0x0101})
	if err != nil {
		t.Fatalf("ViewGroup answered with a status: %v", err)
	}
	if !res.HasFields || res.Command != groupsdef.CmdViewGroupResponse {
		t.Fatalf("ViewGroup result %+v, want a ViewGroupResponse", res)
	}
	var status, got string
	for _, ch := range res.Fields.Children {
		switch ch.El.Tag.Number {
		case 0:
			status = im.StatusCode(ch.El.Uint).String()
		case 2:
			got = ch.El.String
		}
	}
	if status != im.StatusSuccess.String() || got != name {
		t.Fatalf("ViewGroupResponse status %s name %q, want SUCCESS %q", status, got, name)
	}

	data, err := fabrictest.Read(ctx, pair, im.ConcreteAttributePath{
		Endpoint: 0, Cluster: gkmdef.ClusterID, Attribute: gkmdef.AttrGroupTable,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	})
	if err != nil {
		t.Fatalf("GroupTable read refused: %v", err)
	}
	found := false
	for _, entry := range data.Value.Children {
		for _, f := range entry.Children {
			if f.El.Tag.Number == 3 && f.El.String == name {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("GroupTable %+v does not serve the stored name", data.Value)
	}
}

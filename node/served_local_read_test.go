// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
)

// TestReadServedFabricScopedListsWhole: the facade's read has no request
// behind it, so it reads as matter.js's local actor reads — every fabric's
// entries of a fabric-scoped list, each whole. On a node commissioned into
// two fabrics, AccessControl's Acl and Extension and GroupKeyManagement's
// GroupKeyMap carry an entry of each fabric with its fabric-sensitive
// fields, through Node.ReadAttribute and Node.ReadCluster alike. Mirrors
// matter.js packages/protocol/src/action/server/AccessControl.ts:250-253
// and :342-345 at db021da0 (mayRead grants a local actor every attribute
// and every fabric-sensitive field) and
// packages/node/src/behavior/state/managed/values/ListManager.ts:609-616
// (createProxy filters a fabric-scoped list for a remote actor only).
func TestReadServedFabricScopedListsWhole(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	pair := site.CommissionedPair(fabrictest.PairOptions{})
	n := pair.Device.Node()
	ctx := observeContext(t)

	// A second fabric through the node's own window.
	if err := n.OpenOwnWindow(ctx, 0); err != nil {
		t.Fatalf("OpenOwnWindow: %v", err)
	}
	if _, err := site.AddController(fabrictest.ControllerOptions{}).Commission(ctx, pair.Device, fabrictest.CommissionOptions{}); err != nil {
		t.Fatalf("the second commissioning: %v", err)
	}
	fabrics, err := n.FabricInfos(ctx)
	if err != nil || len(fabrics) != 2 {
		t.Fatalf("FabricInfos = %+v, %v; want two fabrics", fabrics, err)
	}

	// Each fabric writes an Extension and a GroupKeyMap entry as a
	// controller's request would.
	var acl, gkm contract.ClusterServer
	for _, ep := range n.Bridge().Topology().Endpoints {
		if ep.ID != 0 {
			continue
		}
		for _, srv := range endpoint.ClusterServers(ep) {
			switch srv.MatterClusterID() {
			case 0x001F:
				acl = srv
			case 0x003F:
				gkm = srv
			}
		}
	}
	if acl == nil || gkm == nil {
		t.Fatal("the root serves no AccessControl or GroupKeyManagement")
	}
	for _, f := range fabrics {
		req := im.WithFabricFilter(ctx, false, f.FabricIndex)
		if err := acl.MatterWrite(req, 0x0001, []core.AccessControlExtensionEntry{{Data: []byte{0x17, 0x18}}}); err != nil {
			t.Fatalf("Extension of fabric %d: %v", f.FabricIndex, err)
		}
		if err := gkm.MatterWrite(req, 0x0000, []core.GroupKeyMapStruct{{GroupID: 0x0101, GroupKeySetID: 0x0101}}); err != nil {
			t.Fatalf("GroupKeyMap of fabric %d: %v", f.FabricIndex, err)
		}
	}

	check := func(how string, attr uint32, v any) {
		t.Helper()
		var got []uint8
		switch list := v.(type) {
		case []core.AccessControlEntryStruct:
			for _, e := range list {
				if e.Redacted || e.Privilege != 5 || len(e.Subjects) == 0 { //nolint:staticcheck // SA1019: a local read never sets the deprecated field
					t.Errorf("%s Acl: the entry of fabric %d is not whole: %+v", how, e.FabricIndex, e)
				}
				got = append(got, e.FabricIndex)
			}
		case []core.AccessControlExtensionEntry:
			for _, e := range list {
				if e.Redacted || len(e.Data) == 0 { //nolint:staticcheck // SA1019: a local read never sets the deprecated field
					t.Errorf("%s Extension: the entry of fabric %d is not whole: %+v", how, e.FabricIndex, e)
				}
				got = append(got, e.FabricIndex)
			}
		case []core.GroupKeyMapStruct:
			for _, e := range list {
				got = append(got, e.FabricIndex)
			}
		default:
			t.Fatalf("%s 0x%04X is %T", how, attr, v)
		}
		slices.Sort(got)
		if !slices.Equal(got, []uint8{fabrics[0].FabricIndex, fabrics[1].FabricIndex}) {
			t.Errorf("%s 0x%04X carries the entries of fabrics %v, want one of each of %d and %d", how, attr, got, fabrics[0].FabricIndex, fabrics[1].FabricIndex)
		}
	}
	for _, p := range []struct{ cluster, attr uint32 }{{0x001F, 0x0000}, {0x001F, 0x0001}, {0x003F, 0x0000}} {
		v, err := n.ReadAttribute(ctx, 0, p.cluster, p.attr)
		if err != nil {
			t.Fatalf("ReadAttribute 0x%04X/0x%04X: %v", p.cluster, p.attr, err)
		}
		check("ReadAttribute", p.attr, v.Value)
	}
	values, err := n.ReadCluster(ctx, 0, 0x001F)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range values {
		if a.Attribute <= 0x0001 {
			check("ReadCluster", a.Attribute, a.Value)
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/store"
)

// TestGrantRewritesThePairsFabricOnly (R2-P6-test-2514): a device that
// has the same node id on two fabrics — two controllers that commission
// it with an explicit NodeID — gets the ACL of the granting pair's fabric
// rewritten, and the other fabric's left as it was.
func TestGrantRewritesThePairsFabricOnly(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)
	site := fabrictest.NewSite(t, fabrictest.Options{})
	dev := site.AddDevice(fabrictest.Topology{})
	ca := site.AddController(fabrictest.ControllerOptions{FabricID: 0xA, NodeID: 0xA001, Label: "fabric A"})
	cb := site.AddController(fabrictest.ControllerOptions{FabricID: 0xB, NodeID: 0xB001, Label: "fabric B"})
	a, err := ca.Commission(ctx, dev, fabrictest.CommissionOptions{NodeID: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := dev.OpenWindow(ctx, 0); err != nil {
		t.Fatal(err)
	}
	b, err := cb.Commission(ctx, dev, fabrictest.CommissionOptions{NodeID: 5})
	if err != nil {
		t.Fatal(err)
	}
	fabrics, err := dev.Node().FabricInfos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var idxA uint8
	for _, f := range fabrics {
		if f.CompressedFabricID == a.Controller.Fabric().Identity.CompressedFabricID {
			idxA = f.FabricIndex
		}
	}
	before, err := dev.Node().Store().ListACL(ctx, idxA)
	if err != nil || len(before) == 0 {
		t.Fatalf("fabric A's ACL before: %v, %v", before, err)
	}
	if err := b.Grant(ctx, store.PrivilegeView); err != nil {
		t.Fatal(err)
	}
	after, err := dev.Node().Store().ListACL(ctx, idxA)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(before, after, func(x, y store.ACLEntry) bool {
		return x.Privilege == y.Privilege && slices.Equal(x.Subjects, y.Subjects)
	}) {
		t.Errorf("Grant on fabric B rewrote fabric A's ACL: %+v → %+v", before, after)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"testing"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/fabrictest"
)

func TestDeviceOpenWindow(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	p := site.CommissionedPair(fabrictest.PairOptions{})
	if err := p.Device.OpenWindow(ctx, 0); err != nil {
		t.Fatal(err)
	}
	second := site.AddController(fabrictest.ControllerOptions{FabricID: 2, NodeID: 0x2222})
	p2, err := second.Commission(ctx, p.Device, fabrictest.CommissionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fabrics, err := p.Device.Node().FabricInfos(ctx)
	if err != nil || len(fabrics) != 2 {
		t.Fatalf("fabrics %+v, %v", fabrics, err)
	}
	for _, q := range []*fabrictest.Pair{p, p2} {
		if _, err := fabrictest.Read(ctx, q, rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel)); err != nil {
			t.Fatalf("read on fabric %d: %v", q.Controller.Fabric().FabricID, err)
		}
	}
}

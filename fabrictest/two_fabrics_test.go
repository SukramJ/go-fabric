// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/fabrictest"
)

// twoFabrics commissions one device onto controller A's fabric and then,
// through a window the device opens locally, onto controller B's.
func twoFabrics(t *testing.T, topo fabrictest.Topology) (dev *fabrictest.Device, a, b *fabrictest.Pair) {
	t.Helper()
	ctx := testContext(t)
	site := fabrictest.NewSite(t, fabrictest.Options{})
	dev = site.AddDevice(topo)
	ca := site.AddController(fabrictest.ControllerOptions{FabricID: 0xA, NodeID: 0xA001, Label: "fabric A"})
	cb := site.AddController(fabrictest.ControllerOptions{FabricID: 0xB, NodeID: 0xB001, Label: "fabric B"})
	var err error
	if a, err = ca.Commission(ctx, dev, fabrictest.CommissionOptions{}); err != nil {
		t.Fatalf("commission onto fabric A: %v", err)
	}
	if err = dev.OpenWindow(ctx, 0); err != nil {
		t.Fatalf("open window: %v", err)
	}
	if b, err = cb.Commission(ctx, dev, fabrictest.CommissionOptions{}); err != nil {
		t.Fatalf("commission onto fabric B: %v", err)
	}
	return dev, a, b
}

// pairFabricIndex is the device's index of the pair's fabric.
func pairFabricIndex(t *testing.T, p *fabrictest.Pair) uint8 {
	t.Helper()
	fabrics, err := p.Device.Node().FabricInfos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fabrics {
		if f.NodeID == p.Address.NodeID {
			return f.FabricIndex
		}
	}
	t.Fatalf("the device holds no fabric of the pair (%+v)", fabrics)
	return 0
}

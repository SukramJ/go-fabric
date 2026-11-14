// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"testing"

	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	opcreds "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// TestFailSafeOfARemovedFabricEndsWithIt is R2-P3-admin-06: a fail-safe
// armed by fabric A does not outlive fabric A — once A is removed, by
// another fabric's administrator or by itself, the fail-safe has expired
// and fabric B's administrator arms its own.
func TestFailSafeOfARemovedFabricEndsWithIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		remover func(a, b *fabrictest.Pair) *fabrictest.Pair
	}{
		{"B removes A", func(_, b *fabrictest.Pair) *fabrictest.Pair { return b }},
		{"A removes itself", func(a, _ *fabrictest.Pair) *fabrictest.Pair { return a }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := quietSite(t)
			ctx := parityCtx(t)
			dev := site.AddDevice(fabrictest.Topology{})
			a, err := site.AddController(fabrictest.ControllerOptions{FabricID: 0xA, NodeID: 0xA001, Label: "fabric A"}).Commission(ctx, dev, fabrictest.CommissionOptions{})
			if err != nil {
				t.Fatalf("commission onto fabric A: %v", err)
			}
			if err := dev.OpenWindow(ctx, 0); err != nil {
				t.Fatalf("open window: %v", err)
			}
			b, err := site.AddController(fabrictest.ControllerOptions{FabricID: 0xB, NodeID: 0xB001, Label: "fabric B"}).Commission(ctx, dev, fabrictest.CommissionOptions{})
			if err != nil {
				t.Fatalf("commission onto fabric B: %v", err)
			}
			ai, bi := fabricIndexOf(t, a), fabricIndexOf(t, b)

			arm := func(p *fabrictest.Pair, seconds uint16) {
				t.Helper()
				if _, err := fabrictest.Invoke(ctx, p, cmdPath(0, gencomm.ClusterID, gencomm.CmdArmFailSafe), gencomm.ArmFailSafeRequest{ExpiryLengthSeconds: seconds, Breadcrumb: 1}); err != nil {
					t.Fatalf("ArmFailSafe(%d): %v", seconds, err)
				}
			}
			failSafe := func() (fabric uint8, armed bool) {
				_ = dev.Act(func(n *node.Node) error { fabric, armed = n.Root().GeneralCommissioning.FailSafeFabric(); return nil })
				return fabric, armed
			}
			arm(a, 900)
			if fabric, armed := failSafe(); !armed || fabric != ai {
				t.Fatalf("A's ArmFailSafe(900): fail-safe fabric %d armed %v, want fabric %d", fabric, armed, ai)
			}
			if _, err := fabrictest.Invoke(ctx, tc.remover(a, b), cmdPath(0, opcreds.ClusterID, opcreds.CmdRemoveFabric), opcreds.RemoveFabricRequest{FabricIndex: ai}); err != nil {
				t.Logf("RemoveFabric(A): %v", err) // A removing itself may lose the answer with its session
			}
			if fabrics, _ := dev.Node().FabricInfos(ctx); len(fabrics) != 1 || fabrics[0].FabricIndex != bi {
				t.Fatalf("fabrics after A's removal: %+v", fabrics)
			}
			if fabric, armed := failSafe(); armed {
				t.Fatalf("after fabric A's removal the fail-safe is still armed for fabric %d", fabric)
			}
			arm(b, 60)
			if fabric, armed := failSafe(); !armed || fabric != bi {
				t.Errorf("B's ArmFailSafe after A's removal: fail-safe fabric %d armed %v, want fabric %d", fabric, armed, bi)
			}
		})
	}
}

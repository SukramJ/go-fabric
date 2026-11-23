// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/interaction"
	accesscontroldef "thingfabric.dev/go-fabric/cluster/spec/accesscontrol"
	opcreds "thingfabric.dev/go-fabric/cluster/spec/operationalcredentials"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/node"
)

// TestFabricRemovalReachesAnotherFabricsSubscription holds, on the wire,
// what review II's R4-par-4052 and R4-par-4051 fixed at the server
// (decision 618): fabric B subscribes to the root's OperationalCredentials
// Fabrics and CommissionedFabrics and AccessControl Acl; fabric A is
// removed — by the host (Node.RemoveFabric, 4052's path) or by B's
// RemoveFabric command (the control) — and B's subscription receives one
// report after its priming report, carrying all three attributes.
func TestFabricRemovalReachesAnotherFabricsSubscription(t *testing.T) {
	for _, mode := range []string{"host", "command"} {
		t.Run(mode, func(t *testing.T) {
			site := quietSite(t)
			ctx := parityCtx(t)
			dev := site.AddDevice(fabrictest.Topology{})
			a, err := site.AddController(fabrictest.ControllerOptions{FabricID: 0xA, NodeID: 0xA001, Label: "fabric A"}).Commission(ctx, dev, fabrictest.CommissionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if err := dev.OpenWindow(ctx, 0); err != nil {
				t.Fatal(err)
			}
			b, err := site.AddController(fabrictest.ControllerOptions{FabricID: 0xB, NodeID: 0xB001, Label: "fabric B"}).Commission(ctx, dev, fabrictest.CommissionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			ai := fabricIndexOf(t, a)
			type key struct{ cluster, attr uint32 }
			var mu sync.Mutex
			reports := 0
			seen := map[key]int{}
			sub, err := fabrictest.Subscribe(ctx, b, interaction.SubscribeRequest{
				Attributes: []im.ConcreteAttributePath{
					lifecycleAttr(0, opcreds.ClusterID, opcreds.AttrFabrics),
					lifecycleAttr(0, opcreds.ClusterID, opcreds.AttrCommissionedFabrics),
					lifecycleAttr(0, accesscontroldef.ClusterID, accesscontroldef.AttrAcl),
				},
				MinIntervalFloor: interaction.Interval(time.Second), MaxIntervalCeiling: interaction.Interval(time.Hour),
				Updated: func(r *interaction.Report) error {
					mu.Lock()
					defer mu.Unlock()
					reports++
					for _, x := range r.Attributes {
						seen[key{x.Path.Cluster, x.Path.Attribute}]++
					}
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(sub.Close)
			mu.Lock()
			primed := reports
			before := map[key]int{}
			for k, v := range seen {
				before[k] = v
			}
			mu.Unlock()
			if mode == "host" {
				if err := dev.Act(func(n *node.Node) error { return n.RemoveFabric(ctx, ai) }); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := fabrictest.Invoke(ctx, b, cmdPath(0, opcreds.ClusterID, opcreds.CmdRemoveFabric), opcreds.RemoveFabricRequest{FabricIndex: ai}); err != nil {
					t.Fatal(err)
				}
			}
			if err := site.Eventually(ctx, "the report after the removal", func() bool {
				mu.Lock()
				defer mu.Unlock()
				return reports > primed
			}); err != nil {
				t.Fatal(err)
			}
			if err := site.Step(ctx, 30*time.Second); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if got := reports - primed; got != 1 {
				t.Errorf("reports after the removal = %d, want 1 (primed %d)", got, primed)
			}
			for _, k := range []key{
				{opcreds.ClusterID, opcreds.AttrFabrics},
				{opcreds.ClusterID, opcreds.AttrCommissionedFabrics},
				{accesscontroldef.ClusterID, accesscontroldef.AttrAcl},
			} {
				if seen[k] <= before[k] {
					t.Errorf("attribute 0x%04x/0x%04x not reported after the removal (%d -> %d)", k.cluster, k.attr, before[k], seen[k])
				}
			}
		})
	}
}

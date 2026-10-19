// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behavior/system/commissioning/CommissioningRestartTest.ts
// at 862ad75f: a node taken offline and started again on its store stays
// commissionable when it was not commissioned, and opens an administrator's
// window when it was.
//
// matter.js stops and starts the MockSite device (device.stop(),
// device.start()); the port stops and starts the fabrictest device
// (Device.Stop, Device.Start: a new node on the same store and address).
// commission(controller, device) is Controller.Commission, the on-network
// flow; peer.openEnhancedCommissioningWindow is its revoke-then-open
// (ClientNode.openEnhancedCommissioningWindow's pre-emptive revoke, which
// tolerates WindowNotOpen) over the pair's peer, which reconnects over CASE.
package node_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/node"
)

// stopStart is device.stop() then device.start().
func stopStart(t *testing.T, d *fabrictest.Device) {
	t.Helper()
	if err := d.Stop(parityCtx(t)); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := d.Start(parityCtx(t)); err != nil {
		t.Fatalf("start: %v", err)
	}
}

// TestCommissioningRestart ports the describe "A node taken offline and
// started again" (CommissioningRestartTest.ts:11-42).
func TestCommissioningRestart(t *testing.T) {
	holdSite(t)

	t.Run("can be commissioned if it was not commissioned before", func(t *testing.T) {
		p := quietSite(t).UncommissionedPair(fabrictest.PairOptions{})
		stopStart(t, p.Device)

		if _, err := p.Controller.Commission(parityCtx(t), p.Device, fabrictest.CommissionOptions{}); err != nil {
			t.Fatalf("commission after the restart: %v", err)
		}

		fabrics, err := p.Device.Node().Fabrics(context.Background())
		if err != nil || len(fabrics) != 1 {
			t.Errorf("isCommissioned: fabrics %v, %v; want one", fabrics, err)
		}
	})

	t.Run("opens a commissioning window if it was commissioned before", func(t *testing.T) {
		p := quietSite(t).CommissionedPair(fabrictest.PairOptions{})
		stopStart(t, p.Device)

		revoke := im.ConcreteCommandPath{Endpoint: 0, Cluster: 0x003C, Command: 0x02}
		if _, err := fabrictest.InvokeTimed(parityCtx(t), p, revoke, nil); err != nil {
			var se *interaction.StatusError
			if !errors.As(err, &se) || se.ClusterStatus == nil || *se.ClusterStatus != 0x04 { // WindowNotOpen
				t.Fatalf("the pre-emptive revoke: %v", err)
			}
		}
		if err := openCommissioningWindow(t, p); err != nil {
			t.Fatalf("openEnhancedCommissioningWindow: %v", err)
		}

		var status wire.CommissioningWindowStatus
		if err := p.Device.Act(func(n *node.Node) error {
			status = n.CommissioningWindow().CurrentWindow().Status
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if status != wire.WindowStatusEnhanced {
			t.Errorf("AdministratorCommissioning WindowStatus %v, want EnhancedWindowOpen", status)
		}
	})
}

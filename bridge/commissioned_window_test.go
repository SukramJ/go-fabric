// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// TestCommissioningCompleteEndsTheWindow pins that a successful
// CommissioningComplete closes the commissioning window it came through,
// whichever of AttachRootClusters and AttachCommissioningWindow ran first,
// so the next OpenCommissioningWindow is not answered BUSY. matter.js
// DeviceCommissioner ends commissioning on failsafeContext.commissioned
// (DeviceCommissioner.ts:160); TC-CADMIN-1.3 step 9 opens a window right
// after a commissioning through one.
func TestCommissioningCompleteEndsTheWindow(t *testing.T) {
	t.Parallel()
	for _, windowFirst := range []bool{true, false} {
		b := newStartedBridge(t)
		gc, err := core.NewGeneralCommissioning(core.GeneralCommissioningConfig{
			LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 600,
		})
		if err != nil {
			t.Fatal(err)
		}
		w := NewCommissioningWindow()
		if windowFirst {
			b.AttachCommissioningWindow(w)
			b.AttachRootClusters([]contract.ClusterServer{gc})
		} else {
			b.AttachRootClusters([]contract.ClusterServer{gc})
			b.AttachCommissioningWindow(w)
		}
		ctx := context.Background()
		if err := w.OpenWindow(ctx, wire.OpenWindowParams{CommissioningTimeoutSeconds: 600}); err != nil {
			t.Fatal(err)
		}
		// The commissioner arms the fail-safe over PASE (fabric 0); AddNOC
		// moves it onto the new fabric (RearmFailSafeForFabric), over whose
		// CASE session CommissioningComplete arrives. An arm over CASE
		// while the window is open is BusyWithOtherAdmin (#4602).
		pase := im.WithAuthModePASE(im.WithFabricFilter(ctx, false, 0))
		if r, err := gc.MatterInvoke(pase, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || r.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
			t.Fatalf("ArmFailSafe over PASE: %v %+v", err, r)
		}
		gc.SetCurrentFabric(1)
		fabric := im.WithFabricFilter(ctx, false, 1)
		resp, err := gc.MatterInvoke(fabric, 0x04, nil)
		if err != nil || resp.(core.CommissioningCompleteResponse).ErrorCode != core.CommissioningErrorOK {
			t.Fatalf("CommissioningComplete: %v %+v", err, resp)
		}
		if s := w.CurrentWindow().Status; s != wire.WindowStatusClosed {
			t.Fatalf("window-first=%v: window status %v after CommissioningComplete, want closed", windowFirst, s)
		}
		if err := w.OpenWindow(ctx, wire.OpenWindowParams{CommissioningTimeoutSeconds: 600}); err != nil {
			t.Fatalf("window-first=%v: the next OpenWindow failed: %v", windowFirst, err)
		}
	}
}

// TestWindowTransitionsReachSubscribers pins that a window opening and
// closing marks WindowStatus (and the admin attributes) dirty, so a
// subscription to it reports the change — matter.js sets the
// AdministratorCommissioning state, whose $Changed reports it. TC-CADMIN-1.3
// step 9 waits on its WindowStatus subscription for the window to close.
func TestWindowTransitionsReachSubscribers(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{{
			HasEndpoint: true, HasCluster: true, HasAttribute: true,
			Endpoint: 0, Cluster: administratorCommissioningClusterID, Attribute: 0x0000,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	b.AttachSubscriptionManager(mgr)
	w := NewCommissioningWindow()
	b.AttachCommissioningWindow(w)
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{CommissioningTimeoutSeconds: 600}); err != nil {
		t.Fatal(err)
	}
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if len(spy.calls) == 0 || len(spy.calls[0]) == 0 {
		t.Fatal("a window transition reported nothing to a WindowStatus subscription")
	}
}

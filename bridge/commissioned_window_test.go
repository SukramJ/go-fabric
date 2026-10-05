// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
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
		fabric := im.WithFabricFilter(ctx, false, 1)
		if _, err := gc.MatterInvoke(fabric, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil {
			t.Fatal(err)
		}
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

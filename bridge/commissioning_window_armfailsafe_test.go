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

// TestWireCommissioned_ArmFailSafeOverCASEWhileWindowOpen mirrors matter.js
// GeneralCommissioningServer.#armFailSafe (9397828d, #4602): while a
// commissioning window is open and the fail-safe is not armed, ArmFailSafe
// over CASE answers BusyWithOtherAdmin. wireCommissioned hands the window's
// state to GeneralCommissioning; before, the predicate was never wired and
// the CASE arm went through.
func TestWireCommissioned_ArmFailSafeOverCASEWhileWindowOpen(t *testing.T) {
	t.Parallel()
	gc, err := core.NewGeneralCommissioning(core.GeneralCommissioningConfig{})
	if err != nil {
		t.Fatal(err)
	}
	w := NewCommissioningWindow()
	wireCommissioned([]contract.ClusterServer{gc}, w)
	caseCtx := im.WithFabricFilter(context.Background(), false, 2)
	arm := func() core.ArmFailSafeResponse {
		t.Helper()
		resp, err := gc.MatterInvoke(caseCtx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60})
		if err != nil {
			t.Fatal(err)
		}
		return resp.(core.ArmFailSafeResponse)
	}
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{CommissioningTimeoutSeconds: 600}); err != nil {
		t.Fatal(err)
	}
	if r := arm(); r.ErrorCode != core.CommissioningErrorBusyWithOtherAdmin {
		t.Fatalf("CASE ArmFailSafe while a window is open: %+v, want BusyWithOtherAdmin", r)
	}
	if err := w.RevokeWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := arm(); r.ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("CASE ArmFailSafe with the window closed: %+v, want OK", r)
	}
}

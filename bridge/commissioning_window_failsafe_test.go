// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// windowOverGC is a window wired to a GeneralCommissioning server as the
// node wires them (node/commissioning.go).
func windowOverGC(t *testing.T) (*bridge.CommissioningWindow, *core.GeneralCommissioning) {
	t.Helper()
	gc, err := core.NewGeneralCommissioning(core.GeneralCommissioningConfig{})
	if err != nil {
		t.Fatal(err)
	}
	w := bridge.NewCommissioningWindow()
	w.SetFailSafeArmer(gc) //nolint:staticcheck // the node's wiring at the time of the test
	return w, gc
}

// TestCommissioningWindow_OpenArmsNoFailSafe pins owner decision 47
// (review II, R2-P2-comm-08): opening a commissioning window arms no
// fail-safe — the commissioner's ArmFailSafe, or the 60 s arm of its PASE
// session, does, as matter.js has it (AdministratorCommissioningServer.ts
// openCommissioningWindow starts the window only; GeneralCommissioningServer.ts
// #handleAddedPaseSessions arms 60 s per PASE session at db021da0).
func TestCommissioningWindow_OpenArmsNoFailSafe(t *testing.T) {
	t.Parallel()
	w, gc := windowOverGC(t)
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{CommissioningTimeoutSeconds: 600}); err != nil {
		t.Fatal(err)
	}
	if gc.FailSafeArmed() {
		t.Fatal("OpenCommissioningWindow armed the fail-safe")
	}
}

// TestCommissioningWindow_RevokeExpiresOnlyThePASEFailSafe pins owner
// decision 42: RevokeCommissioning expires the fail-safe armed over the
// PASE session and leaves one an administrator armed over CASE alone.
func TestCommissioningWindow_RevokeExpiresOnlyThePASEFailSafe(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name        string
		ctx         context.Context
		wantExpired bool
	}{
		{"armed over PASE", im.WithAuthModePASE(context.Background()), true},
		{"armed over CASE", im.WithFabricFilter(context.Background(), true, 2), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w, gc := windowOverGC(t)
			if resp, err := gc.MatterInvoke(c.ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
				t.Fatalf("ArmFailSafe: %+v, %v", resp, err)
			}
			_ = w.RevokeWindow(context.Background())
			if expired := !gc.FailSafeArmed(); expired != c.wantExpired {
				t.Fatalf("after RevokeCommissioning the fail-safe expired=%v, want %v", expired, c.wantExpired)
			}
		})
	}
}

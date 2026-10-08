// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// The node's own commissioning window, matter.js DeviceCommissioner.ts
// (9397828d, #4602): a window the node opens itself is open — the ArmFailSafe
// guard and RevokeCommissioning see it — but WindowStatus reads WindowNotOpen;
// an administrator's window replaces it instead of answering Busy;
// CommissioningComplete, RevokeCommissioning and its timer close it.

func TestOwnWindow_OpenReadsNotOpenButIsOpen(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	var transitions atomic.Int32
	w.SetTransitionHook(func() { transitions.Add(1) })
	if err := w.OpenOwnWindow(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if !w.IsOpen() || !w.IsOwnWindow() {
		t.Fatalf("IsOpen=%v IsOwnWindow=%v after OpenOwnWindow", w.IsOpen(), w.IsOwnWindow())
	}
	if snap := w.CurrentWindow(); snap.Status != wire.WindowStatusClosed || !snap.AdminFabricIsNull || !snap.AdminVendorIsNull {
		t.Fatalf("CurrentWindow = %+v; the node's own window reads WindowNotOpen", snap)
	}
	if got := w.RequestedDurationSeconds(); got != 0xFFFF {
		t.Errorf("RequestedDurationSeconds = %d; 48 h clamps to 65535", got)
	}
	// Restarting keeps it open and fires the hook again.
	if err := w.OpenOwnWindow(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if transitions.Load() != 2 {
		t.Errorf("transitions = %d, want 2", transitions.Load())
	}
	w.EndCommissioning()
	if w.IsOpen() || w.IsOwnWindow() {
		t.Fatal("CommissioningComplete must close the node's own window")
	}
	if transitions.Load() != 3 {
		t.Errorf("transitions = %d, want 3", transitions.Load())
	}
}

func TestOwnWindow_DefaultTimeoutFollowsFabricCount(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	w.SetFabricCounter(func(context.Context) (int, error) { return 1, nil })
	if err := w.OpenOwnWindow(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if got := w.RequestedDurationSeconds(); got != 900 {
		t.Errorf("commissioned node: RequestedDurationSeconds = %d, want 900 (15 min)", got)
	}
}

func TestOwnWindow_TimerClosesIt(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	closed := make(chan struct{}, 2)
	w.SetTransitionHook(func() {
		if !w.IsOpen() {
			closed <- struct{}{}
		}
	})
	if err := w.OpenOwnWindow(context.Background(), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the node's own window did not close on its timeout")
	}
	if w.IsOpen() {
		t.Fatal("still open after the timeout")
	}
}

func TestOwnWindow_AdministratorReplacesIt(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	if err := w.OpenOwnWindow(context.Background(), 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{CommissioningTimeoutSeconds: 180}); err != nil {
		t.Fatalf("an administrator's window must replace the node's own, got %v", err)
	}
	if w.IsOwnWindow() || w.CurrentWindow().Status != wire.WindowStatusEnhanced {
		t.Fatalf("after replacement: own=%v status=%v", w.IsOwnWindow(), w.CurrentWindow().Status)
	}
	// The replaced window's timer must not close the administrator's.
	time.Sleep(60 * time.Millisecond)
	if !w.IsOpen() {
		t.Fatal("the replaced own window's timer closed the administrator's window")
	}
	// The node's own window does not replace an administrator's.
	if err := w.OpenOwnWindow(context.Background(), 0); !errors.Is(err, bridge.ErrAdministratorWindowOpen) {
		t.Fatalf("OpenOwnWindow over an administrator's window = %v, want ErrAdministratorWindowOpen", err)
	}
	// A second administrator's window is still Busy.
	if err := w.OpenWindow(context.Background(), wire.OpenWindowParams{CommissioningTimeoutSeconds: 180}); !errors.Is(err, wire.ErrAdmCommBusy) {
		t.Fatalf("second administrator window = %v, want Busy", err)
	}
}

// TestOwnWindow_RevokeCommissioningClosesIt drives the cluster: matter.js
// AdministratorCommissioningServer.revokeCommissioning answers WindowNotOpen
// only when DeviceCommissioner.windowStatus says no window — the node's own
// counts.
func TestOwnWindow_RevokeCommissioningClosesIt(t *testing.T) {
	t.Parallel()
	w := bridge.NewCommissioningWindow()
	ac := wire.NewAdministratorCommissioning()
	ac.SetController(w)
	const revoke = 0x02
	caseCtx := im.WithFabricFilter(context.Background(), true, 1)
	if _, err := ac.MatterInvoke(caseCtx, revoke, nil); !errors.Is(err, wire.ErrAdmCommWindowNotOpen) {
		t.Fatalf("Revoke with no window = %v, want WindowNotOpen", err)
	}
	if err := w.OpenOwnWindow(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.MatterInvoke(caseCtx, revoke, nil); err != nil {
		t.Fatalf("Revoke of the node's own window = %v, want success", err)
	}
	if w.IsOpen() {
		t.Fatal("RevokeCommissioning left the node's own window open")
	}
}

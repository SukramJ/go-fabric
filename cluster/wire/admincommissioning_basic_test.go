// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	admdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// The Basic feature (BC): served only behind the switch of
// wire.AdministratorCommissioningConfig. matter.js serves it only through
// AdministratorCommissioningServer.with("Basic")
// (AdministratorCommissioningServer.ts:55-57 at 862ad75f). With it,
// OpenBasicCommissioningWindow (:105-117) opens the controller's window as
// a basic one with the command's timeout and the admin fabric, and without
// a PAKE verifier (DeviceCommissioner.allowBasicCommissioning builds the
// PASE server from the node's own passcode, DeviceCommissioner.ts:130-137).

func newBasicAdmComm() *wire.AdministratorCommissioning {
	return wire.NewAdministratorCommissioningWith(wire.AdministratorCommissioningConfig{BasicCommissioning: true})
}

func TestAdmComm_Basic_FeatureMapAdvertisesBC(t *testing.T) {
	t.Parallel()
	got, ok := newBasicAdmComm().MatterRead(spec.AttrFeatureMap)
	if !ok || got != uint32(admdef.FeatureBasic) {
		t.Fatalf("FeatureMap = %v, %v; want %d (BC)", got, ok, uint32(admdef.FeatureBasic))
	}
	got, ok = newAdmComm().MatterRead(spec.AttrFeatureMap)
	if !ok || got != uint32(0) {
		t.Fatalf("FeatureMap without the feature = %v, %v; want 0", got, ok)
	}
	if !newBasicAdmComm().BasicCommissioning() || newAdmComm().BasicCommissioning() {
		t.Fatal("BasicCommissioning() does not report the configured feature")
	}
}

func TestAdmComm_Basic_OpenBasicWindow_ForwardsABasicWindow(t *testing.T) {
	t.Parallel()
	ac := newBasicAdmComm()
	ctrl := &fakeWindowController{}
	ac.SetController(ctrl)
	ac.SetVendorIDResolver(func(context.Context, uint8) uint16 { return 0xFFF1 })
	caseCtx := im.WithFabricFilter(context.Background(), true, 3)
	if _, err := ac.MatterInvoke(caseCtx, admCommCmdOpenBasicWindow, admdef.OpenBasicCommissioningWindowRequest{CommissioningTimeout: 180}); err != nil {
		t.Fatalf("OpenBasicCommissioningWindow: %v", err)
	}
	got := ctrl.capturedParams
	if ctrl.openWindowCalls != 1 || !got.IsBasicWindow || got.CommissioningTimeoutSeconds != 180 ||
		got.AdminFabricIndex != 3 || got.AdminVendorID != 0xFFF1 || len(got.PAKEPasscodeVerifier) != 0 {
		t.Fatalf("OpenWindow called %d times with %+v; want one basic window of 180 s for fabric 3, vendor 0xFFF1, no verifier", ctrl.openWindowCalls, got)
	}
}

func TestAdmComm_Basic_OpenBasicWindow_GatesLikeTheEnhancedCommand(t *testing.T) {
	t.Parallel()
	req := admdef.OpenBasicCommissioningWindowRequest{CommissioningTimeout: 180}
	// Over PASE (no fabric): PAKEParameterError, as OpenCommissioningWindow answers.
	ac := newBasicAdmComm()
	ac.SetController(&fakeWindowController{})
	if _, err := ac.MatterInvoke(context.Background(), admCommCmdOpenBasicWindow, req); !errors.Is(err, wire.ErrAdmCommPakeParameter) {
		t.Errorf("over PASE: %v, want ErrAdmCommPakeParameter", err)
	}
	// An administrator's open window: Busy.
	ac = newBasicAdmComm()
	ac.SetController(&fakeWindowController{snapshot: wire.WindowStatusSnapshot{Status: wire.WindowStatusBasic, AdminFabricIndex: 2}})
	if _, err := ac.MatterInvoke(im.WithFabricFilter(context.Background(), true, 1), admCommCmdOpenBasicWindow, req); !errors.Is(err, wire.ErrAdmCommBusy) {
		t.Errorf("window open: %v, want ErrAdmCommBusy", err)
	}
	// An armed fail-safe: Busy.
	ac = newBasicAdmComm()
	ac.SetController(&fakeWindowController{})
	ac.SetIsFailSafeArmed(func() bool { return true })
	if _, err := ac.MatterInvoke(im.WithFabricFilter(context.Background(), true, 1), admCommCmdOpenBasicWindow, req); spec.StatusOf(err) != im.StatusFailure {
		t.Errorf("fail-safe armed: %v, want the Busy failure", err)
	}
	// No controller: Busy.
	if _, err := newBasicAdmComm().MatterInvoke(im.WithFabricFilter(context.Background(), true, 1), admCommCmdOpenBasicWindow, req); spec.StatusOf(err) != im.StatusFailure {
		t.Errorf("no controller: %v, want the Busy failure", err)
	}
	// Malformed fields: InvalidCommand.
	ac = newBasicAdmComm()
	ac.SetController(&fakeWindowController{})
	if _, err := ac.MatterInvoke(im.WithFabricFilter(context.Background(), true, 1), admCommCmdOpenBasicWindow, "not a request"); spec.StatusOf(err) != im.StatusInvalidCommand {
		t.Errorf("malformed fields: %v, want InvalidCommand", err)
	}
	// Without the feature the command stays unsupported.
	if _, err := newAdmComm().MatterInvoke(im.WithFabricFilter(context.Background(), true, 1), admCommCmdOpenBasicWindow, req); spec.StatusOf(err) != im.StatusUnsupportedCommand {
		t.Errorf("without BC: %v, want UnsupportedCommand", err)
	}
}

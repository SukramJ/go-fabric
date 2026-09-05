// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// TestPin_OpenWindow_PaseReject pins that OpenCommissioningWindow refuses a
// caller that reaches the cluster over a PASE session (no operational
// fabric, FabricIndex == 0). Multi-Admin is a CASE-only operation per
// Matter §11.19.8.1; a PASE peer that could open a window would be able to
// invite a further commissioner onto a bridge it has not itself joined.
// Mirrors chip AdministratorCommissioningCluster.cpp OpenCommissioningWindow
// VerifyOrExit(session.IsSecureSession(), ...).
//
// The assertion that matters is the second one: the window controller must
// never be reached. A test that only inspects the returned error would stay
// green if the guard were moved after the OpenWindow call — the window
// would already be open by the time the error surfaced.
//
// The CASE leg is the negative control. It drives byte-identical PAKE
// parameters through the same handler and must open the window, so a
// failure on the PASE leg can only come from the session check.
func TestPin_OpenWindow_PaseReject(t *testing.T) {
	t.Parallel()

	params := func() wire.OpenWindowParams {
		return wire.OpenWindowParams{
			CommissioningTimeoutSeconds: 300,
			Iterations:                  1000,
			Salt:                        make([]byte, 16),
			PAKEPasscodeVerifier:        make([]byte, 97),
		}
	}

	t.Run("PASE session is refused and never reaches the controller", func(t *testing.T) {
		t.Parallel()
		ac := newAdmComm()
		ctrl := &fakeWindowController{}
		ac.SetController(ctrl)

		// A bare context carries no fabric filter, so
		// im.FabricFilterFromContext reports FabricIndex 0 — the PASE case.
		_, err := ac.MatterInvoke(context.Background(), admCommCmdOpenWindow, params())
		if !errors.Is(err, wire.ErrAdmCommBusy) {
			t.Errorf("MatterInvoke(OpenWindow) over PASE: error = %v, want wire.ErrAdmCommBusy", err)
		}
		if ctrl.openWindowCalls != 0 {
			t.Errorf("controller OpenWindow calls = %d, want 0 — a PASE caller opened a commissioning window",
				ctrl.openWindowCalls)
		}
	})

	t.Run("CASE session opens the window", func(t *testing.T) {
		t.Parallel()
		ac := newAdmComm()
		ctrl := &fakeWindowController{}
		ac.SetController(ctrl)

		caseCtx := im.WithFabricFilter(context.Background(), true, 1)
		if _, err := ac.MatterInvoke(caseCtx, admCommCmdOpenWindow, params()); err != nil {
			t.Fatalf("MatterInvoke(OpenWindow) over CASE: %v", err)
		}
		if ctrl.openWindowCalls != 1 {
			t.Errorf("controller OpenWindow calls = %d, want 1", ctrl.openWindowCalls)
		}
		if got := ctrl.capturedParams.AdminFabricIndex; got != 1 {
			t.Errorf("AdminFabricIndex = %d, want 1", got)
		}
	})
}

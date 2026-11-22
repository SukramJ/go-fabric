// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// TestFabricUpdateAbortsPendingHandshake: UpdateNOC's session teardown
// ([Bridge.CloseFabricSessionsAfterUpdate]) ends a CASE handshake on the
// updated fabric that no Sigma3 completed yet — its Sigma3 is then
// acknowledged and not answered, and no session is installed — while a
// handshake on another fabric goes on. connectedhomeip aborts a pending
// establishment on the updated fabric
// (src/protocols/secure_channel/CASESession.h:149-153, CASESession.cpp:423-433
// at 6170af84). Review II, R2-P6-sec-2412.
func TestFabricUpdateAbortsPendingHandshake(t *testing.T) {
	t.Parallel()
	for name, provided := range map[string]bool{"singleton": false, "provider": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			t.Run("the updated fabric", func(t *testing.T) {
				t.Parallel()
				clk := clocktest.New(time.Unix(1_700_000_000, 0))
				c := beginCASE(t, newUnsecuredRigOn(t, clk), clk, provided)
				c.r.b.CloseFabricSessionsAfterUpdate(context.Background(), completedCaseFabricIndex)
				if c.recordHeld() {
					t.Fatal("the aborted handshake's record is held")
				}
				if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter); err != nil {
					t.Fatal(err)
				}
				if n := c.sessions.Load(); n != 0 {
					t.Fatalf("the aborted handshake's Sigma3 installed %d sessions", n)
				}
			})
			t.Run("another fabric", func(t *testing.T) {
				t.Parallel()
				clk := clocktest.New(time.Unix(1_700_000_000, 0))
				c := beginCASE(t, newUnsecuredRigOn(t, clk), clk, provided)
				c.r.b.CloseFabricSessionsAfterUpdate(context.Background(), completedCaseFabricIndex+1)
				if err := c.controller.send(mrp.SCOpcodeSigma3, completedCaseExchange, c.sigma3, &c.sigma2Counter); err != nil {
					t.Fatal(err)
				}
				c.expectReport(t, c.controller, mrp.SCStatusGeneralSuccess, "the report on Sigma3")
				if n := c.sessions.Load(); n != 1 {
					t.Fatalf("the handshake on another fabric installed %d sessions, want 1", n)
				}
			})
		})
	}
}

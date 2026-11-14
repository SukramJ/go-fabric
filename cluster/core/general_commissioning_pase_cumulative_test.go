// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
)

// TestGencomm_PASEAutoArmStartsTheCumulativeLimit is R2-P3-pase-02: the
// fail-safe a PASE session's establishment arms from the disarmed state is
// a new fail-safe context, and its MaxCumulativeFailsafeSeconds runs from
// that arm — no re-arm, a later PASE session's auto-arm or the
// commissioner's ArmFailSafe, keeps it armed past the limit.
// connectedhomeip FailSafeContext::ArmFailSafe starts the max-cumulative
// timer whenever it arms from disarmed (src/app/FailSafeContext.cpp:112-115
// at 6170af84), which the arm on PASE establishment is
// (src/app/server/CommissioningWindowManager.cpp:236); matter.js builds a
// new ServerNodeFailsafeContext with maxCumulativeFailsafe when none is
// armed (GeneralCommissioningServer.ts:100-106 at db021da0), whose timer
// starts in its constructor (packages/protocol/src/common/FailsafeTimer.ts:33-35).
func TestGencomm_PASEAutoArmStartsTheCumulativeLimit(t *testing.T) {
	t.Parallel()
	const limit = 900 * time.Second // GeneralCommissioningConfig's default CumulativeFailSafeMaxSeconds
	t.Run("a chain of PASE auto-arms", func(t *testing.T) {
		t.Parallel()
		clk := clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
		gc := newGencomm(t, core.GeneralCommissioningConfig{Clock: clk})
		ctx := context.Background()
		gc.AutoArmOnPaseEstablished(ctx)
		// A further PASE session every 50 s re-arms to 60 s from then,
		// the last at 850 s.
		const step = 50 * time.Second
		for elapsed := step; elapsed < limit; elapsed += step {
			clk.StepWall(step)
			gc.AutoArmOnPaseEstablished(ctx)
			if _, armed := gc.FailSafeFabric(); !armed {
				t.Fatalf("the auto-arm %v after the first armed nothing", elapsed)
			}
		}
		clk.StepWall(step)
		if _, armed := gc.FailSafeFabric(); armed {
			t.Fatalf("%v after the first PASE session's arm the fail-safe is still armed; the cumulative limit is %v", limit, limit)
		}
	})
	t.Run("the commissioner's ArmFailSafe after the auto-arm", func(t *testing.T) {
		t.Parallel()
		clk := clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
		gc := newGencomm(t, core.GeneralCommissioningConfig{Clock: clk})
		ctx := im.WithAuthModePASE(context.Background())
		gc.AutoArmOnPaseEstablished(ctx)
		clk.StepWall(50 * time.Second)
		resp, err := gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 900})
		if err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
			t.Fatalf("ArmFailSafe(900): %+v, %v", resp, err)
		}
		clk.StepWall(limit - 50*time.Second)
		if _, armed := gc.FailSafeFabric(); armed {
			t.Fatalf("%v after the PASE session's arm the commissioner's re-arm still holds the fail-safe; the cumulative limit is %v", limit, limit)
		}
	})
}

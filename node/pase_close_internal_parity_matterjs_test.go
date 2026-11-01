// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// paseCloseNode is a node with only the session table wired, holding one
// PASE session that AddNOC adopted onto fabric 1.
func paseCloseNode(t *testing.T) (*Node, *operational.Manager, *clocktest.Fake) {
	t.Helper()
	mgr := operational.NewManager(nil)
	e, err := mgr.OpenFromPase(1, 2, 7, []byte("pase-shared-secret"))
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	if err := mgr.AdoptFabricIndex(e.SessionID, 1); err != nil {
		t.Fatalf("AdoptFabricIndex: %v", err)
	}
	clk := clocktest.New(time.Unix(0, 0))
	n := &Node{sec: &security{sessions: mgr}}
	n.cfg.Clock = clk
	return n, mgr, clk
}

func paseCount(mgr *operational.Manager) int {
	count := 0
	for _, s := range mgr.Sessions() {
		if s.IsPASE {
			count++
		}
	}
	return count
}

// TestPASEClosedOnCommissioningCompleteParity pins Matter §11.10.6.6 step
// 4 as matter.js packages/protocol/src/common/FailsafeContext.ts:150-153
// performs it (completeCommission → closePaseSession): the PASE session —
// the one AddNOC adopted onto the fabric included — is cleared after the
// CommissioningComplete response has gone out and the grace has elapsed,
// not before. (The hook's wiring into commissioningCompleted is pinned
// end to end by TestPASEClosedAfterCommissioningParity.)
func TestPASEClosedOnCommissioningCompleteParity(t *testing.T) {
	t.Parallel()
	n, mgr, clk := paseCloseNode(t)
	after := &im.AfterResponse{}
	ctx := im.WithAfterResponse(context.Background(), after)

	n.closePASESessions(ctx, paseCloseGrace)
	if got := paseCount(mgr); got != 1 {
		t.Fatalf("PASE sessions before the response = %d, want 1 (the response travels on it)", got)
	}
	after.Run()
	if got := paseCount(mgr); got != 1 {
		t.Fatalf("PASE sessions right after the response = %d, want 1 until the grace elapses", got)
	}
	clk.Advance(paseCloseGrace)
	if got := paseCount(mgr); got != 0 {
		t.Errorf("PASE sessions after CommissioningComplete = %d, want 0", got)
	}
}

// TestPASEClosedOnFailSafeExpiryParity pins the fail-safe rollback's step
// 1, matter.js packages/protocol/src/common/FailsafeContext.ts:277-279
// (closePaseSession): a timer expiry clears the PASE sessions at once,
// an expiry inside an interaction (ArmFailSafe with expiry 0) after its
// response.
func TestPASEClosedOnFailSafeExpiryParity(t *testing.T) {
	t.Parallel()
	t.Run("timer", func(t *testing.T) {
		t.Parallel()
		n, mgr, _ := paseCloseNode(t)
		n.failSafeExpired(context.Background(), 1)
		if got := paseCount(mgr); got != 0 {
			t.Errorf("PASE sessions after the fail-safe expired = %d, want 0", got)
		}
	})
	t.Run("in an interaction", func(t *testing.T) {
		t.Parallel()
		n, mgr, _ := paseCloseNode(t)
		after := &im.AfterResponse{}
		n.failSafeExpired(im.WithAfterResponse(context.Background(), after), 1)
		if got := paseCount(mgr); got != 1 {
			t.Fatalf("PASE sessions before the response = %d, want 1", got)
		}
		after.Run()
		if got := paseCount(mgr); got != 0 {
			t.Errorf("PASE sessions after the response = %d, want 0", got)
		}
	})
}

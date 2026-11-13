// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// paseCloseNode is a node with only the bridge and its session table
// wired, holding one PASE session that AddNOC adopted onto fabric 1.
func paseCloseNode(t *testing.T) (*Node, *operational.Manager) {
	t.Helper()
	mgr := operational.NewManager(nil)
	e, err := mgr.OpenFromPase(1, 2, 7, []byte("pase-shared-secret"))
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	if err := mgr.AdoptFabricIndex(e.SessionID, 1); err != nil {
		t.Fatalf("AdoptFabricIndex: %v", err)
	}
	br, err := bridge.New(endpointtest.NewEmptySnapshotter(), mdns.NewNoop(), bridge.Config{Listen: ":0"}, nil)
	if err != nil {
		t.Fatalf("bridge.New: %v", err)
	}
	br.AttachSessionRegistry(mgr)
	return &Node{sec: &security{sessions: mgr}, br: br}, mgr
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

// TestPASEClosedOnFailSafeExpiryParity pins the fail-safe rollback's step
// 1, matter.js packages/protocol/src/common/FailsafeContext.ts:277-279
// (closePaseSession), on the facade's hook: it hands the close to the
// bridge, which owns the sessions (bridge.Bridge.FailSafeExpired) — a
// timer expiry clears the PASE sessions at once, an expiry inside an
// interaction (ArmFailSafe with expiry 0) after its response. The close
// after CommissioningComplete is the bridge's own
// (bridge/pase_close_test.go); TestPASEClosedAfterCommissioningParity pins
// it end to end through the facade.
func TestPASEClosedOnFailSafeExpiryParity(t *testing.T) {
	t.Parallel()
	t.Run("timer", func(t *testing.T) {
		t.Parallel()
		n, mgr := paseCloseNode(t)
		n.failSafeExpired(context.Background(), 1)
		if got := paseCount(mgr); got != 0 {
			t.Errorf("PASE sessions after the fail-safe expired = %d, want 0", got)
		}
	})
	t.Run("in an interaction", func(t *testing.T) {
		t.Parallel()
		n, mgr := paseCloseNode(t)
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

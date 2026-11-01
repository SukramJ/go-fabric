// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestPASEClosedAfterCommissioningParity pins Matter §11.10.6.6 step 4 end
// to end: once a commissioning completes, the device holds no PASE
// session — the one AddNOC adopted onto the new fabric included. Mirrors
// matter.js packages/protocol/src/common/FailsafeContext.ts:150-153
// (completeCommission → closePaseSession).
func TestPASEClosedAfterCommissioningParity(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	u := site.UncommissionedPair(fabrictest.PairOptions{})
	if _, err := u.Controller.Commission(parityCtx(t), u.Device, fabrictest.CommissionOptions{}); err != nil {
		t.Fatalf("commission: %v", err)
	}
	// Past the grace the close waits after the response.
	if err := site.Step(parityCtx(t), time.Second); err != nil {
		t.Fatalf("step: %v", err)
	}
	for _, s := range u.Device.Node().Sessions().Sessions() {
		if s.IsPASE {
			t.Errorf("PASE session %d (fabric %d) still open after CommissioningComplete", s.SessionID, s.FabricIndex)
		}
	}
}

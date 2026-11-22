// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"testing"

	"thingfabric.dev/go-fabric/fabrictest"
)

// TestPASEClosedAfterCommissioningParity pins Matter §11.10.6.6 step 4 end
// to end: once a commissioning completes, the device holds no PASE
// session — the one AddNOC adopted onto the new fabric included. Mirrors
// matter.js packages/protocol/src/common/FailsafeContext.ts:156-157 at b25ed960
// (completeCommission → closePaseSession).
func TestPASEClosedAfterCommissioningParity(t *testing.T) {
	t.Parallel()
	site := quietSite(t)
	u := site.UncommissionedPair(fabrictest.PairOptions{})
	if _, err := u.Controller.Commission(parityCtx(t), u.Device, fabrictest.CommissionOptions{}); err != nil {
		t.Fatalf("commission: %v", err)
	}
	// The close happened inside CommissioningComplete; nothing waits.
	for _, s := range u.Device.Node().Sessions().Sessions() {
		if s.IsPASE {
			t.Errorf("PASE session %d (fabric %d) still open after CommissioningComplete", s.SessionID, s.FabricIndex)
		}
	}
}

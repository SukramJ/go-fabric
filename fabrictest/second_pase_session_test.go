// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"testing"

	basicinfo "github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestSecondPASESessionRefusedWhileOneExists: while a commissioner holds a
// PASE session, a second passcode holder gets no PASE session beside it, and
// the commissioner's session goes on. matter.js ignores a new PASE exchange
// while a PASE session exists that is not closing
// (packages/protocol/src/session/pase/PaseServer.ts:80-82 at db021da0);
// connectedhomeip stops listening for PBKDFParamRequest once the PASE
// session is established (src/app/server/CommissioningWindowManager.cpp:211,
// :224 and :560-561 at 6170af84); review II, R2-P3-pase-01.
func TestSecondPASESessionRefusedWhileOneExists(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	ctx := testContext(t)
	u := site.UncommissionedPair(fabrictest.PairOptions{})
	legit, err := u.Controller.PASE(ctx, u.Device)
	if err != nil {
		t.Fatalf("commissioner's PASE: %v", err)
	}
	label := rootAttr(basicinfo.ClusterID, basicinfo.AttrNodeLabel)
	if _, err := fabrictest.Read(ctx, legit, label); err != nil {
		t.Fatalf("read over the commissioner's PASE: %v", err)
	}

	second := site.AddController(fabrictest.ControllerOptions{})
	if _, err := second.PASE(ctx, u.Device); err == nil {
		t.Fatal("a second PASE session was established while the commissioner's exists")
	}
	if _, err := fabrictest.Read(ctx, legit, label); err != nil {
		t.Fatalf("the commissioner's session after the refused second PASE: %v", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// TestPASESessionWhoseFailSafeArmIsRefusedIsClosed is R2-P3-pase-03 at the
// node's wiring: a PASE session whose fail-safe arm is refused — here the
// rollback of a due fail-safe the arm settled, which ends every PASE
// session — does not stay open without a fail-safe: the handshake fails
// and the session is closed (connectedhomeip
// src/app/server/CommissioningWindowManager.cpp:240-242 at 6170af84).
func TestPASESessionWhoseFailSafeArmIsRefusedIsClosed(t *testing.T) {
	t.Parallel()
	const (
		passcode   = uint32(20202021)
		iterations = 1000
	)
	salt := []byte("SPAKE2P Key Salt")
	clk := clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	gc, err := mattercore.NewGeneralCommissioning(mattercore.GeneralCommissioningConfig{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	// The rollback ends the PASE sessions (the bridge wires its PASE
	// close here); this one names none, as a close whose list was taken
	// before the new session existed.
	gc.SetOnFailSafeRolledBack(func(context.Context) {})
	ctx := context.Background()
	if err := gc.ArmFailSafeFor(ctx, 1, 0); err != nil {
		t.Fatal(err)
	}
	clk.StepWall(2 * time.Second) // the fail-safe is due; its watcher has not run

	sessions := operational.NewManager(testStore(t))
	vc, err := spake2.NewVerifierContext(passcode, salt, iterations)
	if err != nil {
		t.Fatal(err)
	}
	pa, err := buildPaseAdapterFromContext(ctx, sessions, &RootClusters{GeneralCommissioning: gc}, vc, salt, iterations, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := pbkdfParamRequest()
	_, resp, err := pa.ProcessPBKDFParamRequest(req)
	if err != nil {
		t.Fatalf("ProcessPBKDFParamRequest: %v", err)
	}
	h := sha256.New()
	h.Write([]byte(spake2.MatterContext))
	h.Write(req)
	h.Write(resp)
	prover, err := spake2.NewProver(passcode, salt, iterations, nil, nil, h.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}
	pA, err := prover.GeneratePake1()
	if err != nil {
		t.Fatal(err)
	}
	_, pake2, err := pa.ProcessPake1(spake2.EncodePake1(pA))
	if err != nil {
		t.Fatalf("ProcessPake1: %v", err)
	}
	pB, cB, err := spake2.DecodePake2(pake2)
	if err != nil {
		t.Fatal(err)
	}
	cA, err := prover.ProcessPake2(pB, cB)
	if err != nil {
		t.Fatal(err)
	}
	_, _, pake3Err := pa.ProcessPake3(spake2.EncodePake3(cA))

	for _, s := range sessions.Sessions() {
		if s.IsPASE {
			t.Fatalf("PASE session %d is open, its fail-safe arm refused (fail-safe armed: %v; Pake3: %v)", s.SessionID, gc.FailSafeArmed(), pake3Err)
		}
	}
	if pake3Err == nil {
		t.Error("Pake3 succeeded for a session the node closed")
	}
}

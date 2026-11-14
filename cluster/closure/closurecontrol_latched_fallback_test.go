// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// TestClosureControlLatchedClosureIsNotDrivenByTheFallback: a MoveTo whose
// Latch and Speed the server ignores falls back to the outstanding target
// — but not while the closure is latched: driving it there would move a
// latched closure without the unlatch chip's latched rule demands of a
// Position change (ClosureControlCluster.cpp:607-620 at 6170af84). The
// command answers SUCCESS and changes nothing, as it does without an
// outstanding target (review II, R2-P1-clu-06).
func TestClosureControlLatchedClosureIsNotDrivenByTheFallback(t *testing.T) {
	t.Parallel()
	m := &reportingMover{}
	srv, err := closure.New(closure.Config{
		FeatureMap: wire.ClosureControlFeaturePositioning | wire.ClosureControlFeatureMotionLatching, Mover: m,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.SetMainState(wire.ClosureMainStateStopped)
	fullyOpen := closuredef.TargetPositionMoveToFullyOpen
	unlatch := false
	if _, err := srv.MatterInvoke(context.Background(), closuredef.CmdMoveTo, closuredef.MoveToRequest{Position: &fullyOpen, Latch: &unlatch}); err != nil {
		t.Fatal(err)
	}
	// The drive stopped short and latched: FullyOpen is still outstanding.
	latched := true
	srv.SetCurrentLatch(&latched)
	srv.SetMainState(wire.ClosureMainStateStopped)
	moves := m.moves
	auto := closuredef.ThreeLevelAutoAuto
	if _, err := srv.MatterInvoke(context.Background(), closuredef.CmdMoveTo, closuredef.MoveToRequest{Speed: &auto}); err != nil {
		t.Fatalf("MoveTo with an ignored Speed: %v, want SUCCESS", err)
	}
	if m.moves != moves {
		t.Errorf("the mover was driven to the outstanding target while latched (%d MoveTo calls, want %d)", m.moves, moves)
	}
	if v, _ := srv.MatterRead(wire.ClosureControlAttrMainState); v != uint8(wire.ClosureMainStateStopped) {
		t.Errorf("MainState %v, want Stopped unchanged", v)
	}
}

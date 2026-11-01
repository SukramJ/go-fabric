// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// TestClosureControlMoveToMainStateChip: MoveTo is accepted in Moving,
// WaitingForMotion and Stopped only; in every other MainState it answers
// INVALID_IN_STATE without calling the mover or changing the state, as
// connectedhomeip's HandleMoveTo does
// (src/app/clusters/closure-control-server/ClosureControlCluster.cpp:599-605
// at 6170af84; W3-club1-01). matter.js has no ClosureControl logic
// (ClosureControlServer.ts:17, the empty subclass).
func TestClosureControlMoveToMainStateChip(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		state clusterwire.ClosureMainState
		ok    bool
	}{
		{clusterwire.ClosureMainStateStopped, true},
		{clusterwire.ClosureMainStateMoving, true},
		{clusterwire.ClosureMainStateWaitingForMotion, true},
		{clusterwire.ClosureMainStateError, false},
		{clusterwire.ClosureMainStateCalibrating, false},
		{clusterwire.ClosureMainStateProtected, false},
		{clusterwire.ClosureMainStateDisengaged, false},
		{clusterwire.ClosureMainStateSetupRequired, false},
	} {
		h := &recordingHandlers{}
		s := closure.NewControlServer(h.config())
		s.SetMainState(c.state)
		target := closuredef.TargetPositionMoveToFullyOpen
		_, err := s.MatterInvoke(context.Background(), clusterwire.ClosureControlCmdMoveTo, closuredef.MoveToRequest{Position: &target})
		state, _ := s.MatterRead(clusterwire.ClosureControlAttrMainState)
		if c.ok {
			if err != nil || len(h.moved) != 1 || state != uint8(clusterwire.ClosureMainStateMoving) {
				t.Errorf("MainState %d: %v, moved %v, MainState after %v; want accepted and Moving", c.state, err, h.moved, state)
			}
			continue
		}
		if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusInvalidInState {
			t.Errorf("MainState %d: %v, want INVALID_IN_STATE", c.state, err)
		}
		if len(h.moved) != 0 || state != uint8(c.state) {
			t.Errorf("MainState %d: moved %v, MainState after %v; want neither changed", c.state, h.moved, state)
		}
	}
}

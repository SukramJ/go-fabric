// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/closure"
	cd "thingfabric.dev/go-fabric/cluster/spec/closuredimension"
	"thingfabric.dev/go-fabric/im"
)

// TestStepFromAnOffGridCurrentPositionAnswersFailureAfterTheHandler
// (R3-doc-3280, run 3's probe): a Resolution that divides 10000 and a
// StepValue on its grid do not keep a Step target on the grid when the
// reported CurrentState Position is off it — the server takes an off-grid
// current Position. Resolution 100, StepValue 1000, current 1234: the
// target 2234 is off the grid, the handler runs, and the command answers
// FAILURE with TargetState unchanged.
func TestStepFromAnOffGridCurrentPositionAnswersFailureAfterTheHandler(t *testing.T) {
	t.Parallel()
	p := &panel{}
	cfg := chipPanel(p) // Resolution 100, StepValue 1000
	cfg.CurrentState = &closure.DimensionState{Position: pos(1234), Latch: latch(false)}
	srv := newDimension(t, cfg)
	before := srv.TargetState()
	_, err := srv.MatterInvoke(context.Background(), cd.CmdStep, cd.StepRequest{Direction: cd.StepDirectionIncrease, NumberOfSteps: 1})
	var sc im.StatusCodeError
	if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusFailure {
		t.Fatalf("Step from 1234 by 1000 at Resolution 100: %v, want FAILURE", err)
	}
	p.mu.Lock()
	moved := len(p.steps)
	p.mu.Unlock()
	if moved != 1 {
		t.Errorf("the handler ran %d times, want once (the device acts before the refusal)", moved)
	}
	if after := srv.TargetState(); after.Position == nil || before.Position == nil || after.Position.Null != before.Position.Null || after.Position.Value != before.Position.Value {
		t.Errorf("TargetState moved from %+v to %+v", before, after)
	}
}

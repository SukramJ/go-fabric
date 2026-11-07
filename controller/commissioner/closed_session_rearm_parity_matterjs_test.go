// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// A port of matter.js packages/node/test/node/CommissioningClosedSessionReArmTest.ts
// at db021da0: the failsafe re-arm between commissioning steps is skipped
// once the commissioning session is closed (ControllerCommissioningFlow.ts:835-849).
//
// matter.js subclasses the flow and keeps GetInitialData and the
// ArmFailsafe step, then appends a step 16.9 that advances the mock clock
// by 40 s and closes the PASE session. The Go flow runs over a scripted
// device on a fake clock (newTestFlow); the steps are the flow's own list.

import (
	"context"
	"slices"
	"testing"
	"time"

	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
)

// closingStep is matter.js's "Leave the commissioning channel": it leaves
// less than half of the 60 s failsafe and closes the session.
func closingStep(f *flow, clock *fakeClock, number, sub int, advance time.Duration) step {
	return step{number, sub, "Leave the commissioning channel", func(context.Context) (stepResult, error) {
		clock.Advance(advance)
		f.interaction().Session().Close()
		return stepSuccess, nil
	}}
}

func TestCommissioningClosedSessionReArmParity(t *testing.T) {
	t.Parallel()
	t.Run("skips the re-arm after a step that closed the commissioning session", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}}
		f, clock := newTestFlow(t, dev.handle)
		f.steps = []step{
			{7, 1, "GeneralCommissioning.ArmFailsafe", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
			closingStep(f, clock, 16, 9, 40*time.Second),
		}
		if err := f.execute(context.Background()); err != nil {
			t.Fatalf("flow error %v, want the flow to complete", err)
		}
		if got := dev.armed(); !slices.Equal(got, []uint16{60}) {
			t.Fatalf("ArmFailSafe expiries %v, want [60] (no re-arm on the closed session)", got)
		}
	})
	// The same rule before a step with reArmFailsafe (:385-393): the
	// network steps re-arm first, not on a closed session.
	t.Run("skips the re-arm before a network step once the session is closed", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}}
		f, clock := newTestFlow(t, dev.handle)
		ran := false
		f.reArmBefore[[2]int{16, 2}] = true
		f.steps = []step{
			{7, 1, "GeneralCommissioning.ArmFailsafe", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
			closingStep(f, clock, 16, 1, time.Second),
			{16, 2, "network", func(context.Context) (stepResult, error) { ran = true; return stepSuccess, nil }},
		}
		if err := f.execute(context.Background()); err != nil {
			t.Fatalf("flow error %v", err)
		}
		if got := dev.armed(); !ran || !slices.Equal(got, []uint16{60}) {
			t.Fatalf("network step ran %v, ArmFailSafe expiries %v, want true [60]", ran, got)
		}
	})
}

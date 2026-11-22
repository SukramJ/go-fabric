// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"testing"

	"thingfabric.dev/go-fabric/cluster/closure"
	"thingfabric.dev/go-fabric/cluster/spec"
	cd "thingfabric.dev/go-fabric/cluster/spec/closuredimension"
)

// latchingPanel records, while it takes a SetTarget or Step, a Latch
// target the device chose on its own through the host's SetTargetState.
type latchingPanel struct {
	panel
	srv *closure.DimensionServer
}

func (p *latchingPanel) SetTarget(ctx context.Context, position *uint16, l *bool, speed *closure.ThreeLevelAuto) error {
	if err := p.srv.SetTargetState(&closure.DimensionState{Latch: latch(true)}); err != nil {
		return err
	}
	return p.panel.SetTarget(ctx, position, l, speed)
}

func (p *latchingPanel) Step(ctx context.Context, d closure.StepDirection, n uint16, speed *closure.ThreeLevelAuto) error {
	if err := p.srv.SetTargetState(&closure.DimensionState{Latch: latch(true)}); err != nil {
		return err
	}
	return p.panel.Step(ctx, d, n, speed)
}

// TestACommandKeepsATargetFieldTheHostSetMeanwhile (R2-P6-conc-1585):
// SetTarget and Step store the fields they set into TargetState as it is
// after the handler, so a field the host set meanwhile and the command
// did not name survives.
func TestACommandKeepsATargetFieldTheHostSetMeanwhile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cmd  uint32
		req  any
	}{
		{"SetTarget", cd.CmdSetTarget, cd.SetTargetRequest{Position: ptrTo(uint16(5000))}},
		{"Step", cd.CmdStep, cd.StepRequest{Direction: cd.StepDirectionIncrease, NumberOfSteps: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &latchingPanel{}
			cfg := chipPanel(&p.panel)
			cfg.Handler = p
			cfg.CurrentState = &closure.DimensionState{Position: pos(4000), Latch: latch(false)}
			srv := newDimension(t, cfg)
			p.srv = srv
			if _, err := srv.MatterInvoke(context.Background(), tc.cmd, tc.req); err != nil {
				t.Fatal(err)
			}
			got := srv.TargetState()
			if got == nil || got.Latch == nil || got.Latch.Null || !got.Latch.Value {
				t.Errorf("TargetState %+v: the Latch the host set while the handler ran was overwritten", got)
			}
			if got == nil || got.Position == nil || got.Position.Null {
				t.Errorf("TargetState %+v: the command's Position is missing", got)
			}
		})
	}
}

// TestDimensionStateIsCopiedDeep (decision 142): a state handed in or out
// shares no field with the one the server holds.
func TestDimensionStateIsCopiedDeep(t *testing.T) {
	t.Parallel()
	srv := newDimension(t, chipPanel(&panel{}))
	in := &closure.DimensionState{Position: pos(3000), Latch: latch(false)}
	if err := srv.SetCurrentState(in); err != nil {
		t.Fatal(err)
	}
	in.Position.Value = 7000
	if got := srv.CurrentState().Position.Value; got != 3000 {
		t.Errorf("CurrentState follows the caller's state after SetCurrentState: %d", got)
	}
	out := srv.CurrentState()
	out.Position.Value = 9000
	if got := srv.CurrentState().Position.Value; got != 3000 {
		t.Errorf("CurrentState follows a state it returned: %d", got)
	}
	tgt := srv.TargetState()
	tgt.Speed = nil
	tgt.Position = &spec.Nullable[uint16]{Value: 1}
	if got := srv.TargetState(); got.Position == nil || !got.Position.Null {
		t.Errorf("TargetState follows a state it returned: %+v", got)
	}
}

func ptrTo[T any](v T) *T { return &v }

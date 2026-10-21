// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fan_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/fan"
	"github.com/SukramJ/go-fabric/cluster/spec"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// TestTypedFanServer drives fan.Server through its typed server: Get reads
// the StateSource, Set refuses (the fan holds no attribute of its own),
// HandleStep replaces the server's Step, OnWrite vetoes a controller write
// before the StateSource sees it, and Emit refuses (FanControl has no
// event).
func TestTypedFanServer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{st: fan.State{FanMode: fan.FanModeLow, PercentSetting: u8(30), PercentCurrent: 30}}
	srv := newFull(t, d)
	s := fandef.Wrap(srv)

	if st := s.Get(); st.FanMode != fandef.FanModeLow || st.PercentSetting == nil || *st.PercentSetting != 30 || st.SpeedMax != 10 {
		t.Errorf("state %+v", st)
	}

	// Set: checked first (a percent above 100 is refused as invalid), then
	// refused as not held.
	mode := fandef.FanModeHigh
	if err := s.Set(fandef.Patch{FanMode: &mode}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set FanMode: %v, want ErrNotHeld", err)
	}
	if err := s.Set(fandef.Patch{PercentSetting: u8(150)}); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("Set PercentSetting 150: %v, want ErrInvalidValue", err)
	}
	if err := s.Set(fandef.Patch{}); err != nil {
		t.Errorf("an empty patch: %v", err)
	}
	unsubscribe := s.OnChanged(func(fandef.State, []uint32) { t.Error("OnChanged fired") })
	unsubscribe()

	// HandleStep replaces the default step: the host's handler gets the
	// generated request and the StateSource is not driven.
	var got []fandef.StepRequest
	if err := s.HandleStep(func(_ context.Context, req fandef.StepRequest) (struct{}, error) {
		got = append(got, req)
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	applied := len(d.applied)
	req := fandef.StepRequest{Direction: fandef.StepDirectionIncrease}
	if _, err := srv.MatterInvoke(ctx, fan.CmdStep, &req); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Direction != fandef.StepDirectionIncrease || len(d.applied) != applied {
		t.Errorf("host Step %v, %d settings applied", got, len(d.applied)-applied)
	}
	// nil hands Step back: the server steps the StateSource itself.
	if err := s.HandleStep(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, fan.CmdStep, req); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(d.applied) != applied+1 {
		t.Errorf("after nil: host Step %d times, %d settings applied", len(got), len(d.applied)-applied)
	}
	if err := srv.Handle(0x7F, spec.HandlerOf(func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil })); !errors.Is(err, spec.ErrNotAccepted) {
		t.Errorf("Handle of an unknown command: %v, want ErrNotAccepted", err)
	}

	// OnWrite vetoes a controller write before the StateSource sees it.
	refusal := spec.Errorf(im.StatusConstraintError, "the host says no")
	var seen []uint32
	if err := s.OnWrite(func(_ context.Context, next fandef.State, attrID uint32) error {
		seen = append(seen, attrID)
		if next.PercentSetting != nil && *next.PercentSetting == 90 {
			return refusal
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	applied = len(d.applied)
	if err := srv.MatterWrite(ctx, fan.AttrPercentSetting, uint64(90)); !errors.Is(err, refusal) {
		t.Errorf("vetoed write: %v", err)
	}
	if len(d.applied) != applied {
		t.Error("a vetoed write reached the StateSource")
	}
	if err := srv.MatterWrite(ctx, fan.AttrPercentSetting, uint64(40)); err != nil || len(d.applied) != applied+1 {
		t.Errorf("an accepted write: %v, %d applied", err, len(d.applied)-applied)
	}
	if len(seen) != 2 || seen[0] != fan.AttrPercentSetting {
		t.Errorf("the veto saw %v", seen)
	}

	if err := s.Underlying().(*fan.Server).Emit(1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit: %v, want ErrNotEmitted", err)
	}
}

// TestStepRequestForms pins the Step payload forms: the generated request
// by value and by pointer (absent LowestOff reads as true,
// fan-control.element.ts:73) and the earlier clusterwire form.
func TestStepRequestForms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := &stepper{plainDevice: plainDevice{d: &device{}}}
	srv, err := fan.NewServer(fan.Config{Source: st, Features: fan.FeatureStep, Sequence: fan.SequenceOffLowMedHigh})
	if err != nil {
		t.Fatal(err)
	}
	wrap := true
	for _, fields := range []any{
		fandef.StepRequest{Direction: fandef.StepDirectionDecrease},
		&fandef.StepRequest{Direction: fandef.StepDirectionIncrease, Wrap: &wrap},
		clusterwire.FanStepRequest{Direction: fan.StepIncrease, LowestOff: false},
	} {
		if _, err := srv.MatterInvoke(ctx, fan.CmdStep, fields); err != nil {
			t.Fatalf("%T: %v", fields, err)
		}
	}
	want := []clusterwire.FanStepRequest{
		{Direction: fan.StepDecrease, LowestOff: true},
		{Direction: fan.StepIncrease, Wrap: true, LowestOff: true},
		{Direction: fan.StepIncrease},
	}
	if len(st.steps) != len(want) {
		t.Fatalf("steps %+v", st.steps)
	}
	for i := range want {
		if st.steps[i] != want[i] {
			t.Errorf("step %d: %+v, want %+v", i, st.steps[i], want[i])
		}
	}
	if _, err := srv.MatterInvoke(ctx, fan.CmdStep, (*fandef.StepRequest)(nil)); statusOf(t, err) != im.StatusInvalidCommand {
		t.Errorf("nil pointer: %v", err)
	}
}

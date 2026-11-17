// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	cd "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// TestClosureControlEventsAndHandlerAfterTheHooks (decision 154): with
// spec.Hooks gone from its path, ControlServer's own Emit and EmitEvent
// reach the endpoint's emitter, and a host handler takes a command.
func TestClosureControlEventsAndHandlerAfterTheHooks(t *testing.T) {
	t.Parallel()
	srv, rec, _ := newEmittingServer(t, wire.ClosureControlFeaturePositioning)
	if err := srv.Emit(3, closuredef.EventSecureStateChanged, closure.SecureStateChangedEvent{SecureValue: true}); err != nil {
		t.Fatal(err)
	}
	if err := srv.EmitEvent(closuredef.EventSecureStateChanged, closure.SecureStateChangedEvent{SecureValue: false}); err != nil {
		t.Fatal(err)
	}
	if got := rec.ids(); len(got) != 2 || got[0] != closuredef.EventSecureStateChanged || rec.events[0].endpoint != 3 {
		t.Errorf("emitted %+v, want two SecureStateChanged, the first on endpoint 3", rec.events)
	}
	called := false
	if err := srv.Handle(closuredef.CmdStop, func(context.Context, any) (any, error) { called = true; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), closuredef.CmdStop, nil); err != nil || !called {
		t.Errorf("Stop with a host handler: %v, handler called %t", err, called)
	}
}

// TestSetTargetRoundedAboveTheTopAnswersFailureAfterTheHandler
// (R2-P6-fsm-1581, decision 150): a Resolution that does not divide 10000
// rounds a Position near the top above it; the handler runs, the target
// is refused and the command answers FAILURE with TargetState unchanged,
// as connectedhomeip's server does.
func TestSetTargetRoundedAboveTheTopAnswersFailureAfterTheHandler(t *testing.T) {
	t.Parallel()
	p := &panel{}
	cfg := chipPanel(p)
	cfg.Resolution, cfg.StepValue = 1500, 1500
	cfg.Features &^= closure.DimensionFeatureLimitation
	cfg.LimitRange = closure.LimitRange{}
	cfg.CurrentState = &closure.DimensionState{Position: pos(9000), Latch: latch(false)}
	srv := newDimension(t, cfg)
	before := srv.TargetState()
	_, err := srv.MatterInvoke(context.Background(), cd.CmdSetTarget, cd.SetTargetRequest{Position: ptrTo(uint16(10000))})
	var sc im.StatusCodeError
	if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusFailure {
		t.Fatalf("SetTarget 10000 at Resolution 1500: %v, want FAILURE", err)
	}
	p.mu.Lock()
	moved := len(p.targets)
	p.mu.Unlock()
	if moved != 1 {
		t.Errorf("the handler ran %d times, want once (the device acts before the refusal)", moved)
	}
	if after := srv.TargetState(); after.Position == nil || before.Position == nil || after.Position.Null != before.Position.Null {
		t.Errorf("TargetState moved from %+v to %+v", before, after)
	}
}

// TestClosureWritesAndTimedCommands: every attribute of both clusters is
// read-only, so a write is refused and changes nothing, through
// MatterWriteChanged too; MoveTo and Calibrate are timed, Stop is not.
func TestClosureWritesAndTimedCommands(t *testing.T) {
	t.Parallel()
	ctrl, _, _ := newEmittingServer(t, wire.ClosureControlFeaturePositioning)
	dim := newDimension(t, chipPanel(&panel{}))
	for _, tc := range []struct {
		name string
		w    interface {
			MatterWriteChanged(context.Context, uint32, any) (bool, error)
		}
		attr uint32
	}{{"ClosureControl", ctrl, wire.ClosureControlAttrMainState}, {"ClosureDimension", dim, cd.AttrCurrentState}} {
		changed, err := tc.w.MatterWriteChanged(context.Background(), tc.attr, uint8(1))
		var sc im.StatusCodeError
		if changed || !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusUnsupportedWrite {
			t.Errorf("%s: write of attribute 0x%04X: changed %t, %v; want UNSUPPORTED_WRITE", tc.name, tc.attr, changed, err)
		}
	}
	for cmd, timed := range map[uint32]bool{closuredef.CmdMoveTo: true, closuredef.CmdCalibrate: true, closuredef.CmdStop: false} {
		if got := ctrl.IsTimedCommand(cmd); got != timed {
			t.Errorf("IsTimedCommand(0x%02X) = %t, want %t", cmd, got, timed)
		}
	}
}

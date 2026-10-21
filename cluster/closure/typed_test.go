// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	"github.com/SukramJ/go-fabric/cluster/spec"
	cd "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/contract"
)

// TestControlChangesOrigin is the origin audit of the ClosureControl
// server: the drive's own report (a typed Set) is OriginHostSet, a MoveTo
// OriginCommand, a controller's write of the read-only MainState nothing.
func TestControlChangesOrigin(t *testing.T) {
	t.Parallel()
	open := closuredef.TargetPositionMoveToFullyOpen
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		s := closure.NewControlServer((&recordingHandlers{}).config())
		s.SetMainState(0) // Stopped: MoveTo is the drive's
		return s
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			moving := closuredef.MainStateMoving
			if err := closuredef.Wrap(srv).Set(closuredef.Patch{MainState: &moving}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: func(tb testing.TB, srv contract.ClusterServer) {
			if _, err := srv.MatterInvoke(context.Background(), closuredef.CmdMoveTo, closuredef.MoveToRequest{Position: &open}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
			if err := srv.MatterWrite(context.Background(), closuredef.AttrMainState, uint8(1)); err == nil {
				tb.Fatal("a write of the read-only MainState succeeded")
			}
		}},
	)
}

// TestControlTyped drives the ClosureControl server through the generated
// typed form: the drive's position and state, an error, the events.
func TestControlTyped(t *testing.T) {
	t.Parallel()
	srv := closure.NewControlServer((&recordingHandlers{}).config())
	rec := &eventLog{}
	srv.SetMatterEventEmitter(rec)
	typed := closuredef.Wrap(srv)
	closed := closuredef.CurrentPositionFullyClosed
	stopped := closuredef.MainStateStopped
	if err := typed.Set(closuredef.Patch{
		MainState:           &stopped,
		OverallCurrentState: &closuredef.OverallCurrentStateStruct{Position: &spec.Nullable[closuredef.CurrentPositionEnum]{Value: closed}},
	}); err != nil {
		t.Fatal(err)
	}
	st := typed.Get()
	if st.MainState != stopped || st.OverallCurrentState.Position == nil || st.OverallCurrentState.Position.Value != closed ||
		st.OverallCurrentState.SecureState.Null || !st.OverallCurrentState.SecureState.Value {
		t.Errorf("state after the Set: %+v", st)
	}
	errState := closuredef.MainStateError
	blocked := []closuredef.ClosureErrorEnum{closuredef.ClosureErrorPhysicallyBlocked}
	if err := typed.Set(closuredef.Patch{MainState: &errState, CurrentErrorList: &blocked}); err != nil {
		t.Fatal(err)
	}
	if st := typed.Get(); st.MainState != errState || len(st.CurrentErrorList) != 1 {
		t.Errorf("state after an error: %+v", st)
	}
	if ids := rec.ids(); len(ids) == 0 || ids[len(ids)-1] != closuredef.EventOperationalError {
		t.Errorf("events %v, want OperationalError last", ids)
	}
	if err := typed.Set(closuredef.Patch{CurrentErrorList: &[]closuredef.ClosureErrorEnum{}}); err != nil {
		t.Errorf("Set of an empty error list: %v", err)
	}
	target := closuredef.OverallTargetStateStruct{}
	if err := typed.Set(closuredef.Patch{OverallTargetState: &target}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set of OverallTargetState: %v, want ErrNotHeld", err)
	}
	if err := typed.EmitMovementCompleted(1, closuredef.MovementCompletedEvent{}); err != nil {
		t.Errorf("EmitMovementCompleted: %v", err)
	}
	if err := typed.HandleStop(func(context.Context, closuredef.StopRequest) (struct{}, error) { return struct{}{}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), closuredef.CmdStop, closuredef.StopRequest{}); err != nil {
		t.Errorf("Stop through the host handler: %v", err)
	}
	if err := typed.OnWrite(func(context.Context, closuredef.State, uint32) error { return nil }); err != nil {
		t.Errorf("OnWrite: %v", err)
	}
}

// TestControlEngage covers the ManuallyOperable closure: a typed Set of
// Disengaged and back moves the engage state and emits EngageStateChanged.
func TestControlEngage(t *testing.T) {
	t.Parallel()
	srv, err := closure.New(closure.Config{FeatureMap: closure.PositioningVentilationFeatureMap | uint32(closuredef.FeatureManuallyOperable)})
	if err != nil {
		t.Fatal(err)
	}
	rec := &eventLog{}
	srv.SetMatterEventEmitter(rec)
	typed := closuredef.Wrap(srv)
	off, moving := closuredef.MainStateDisengaged, closuredef.MainStateMoving
	if err := typed.Set(closuredef.Patch{MainState: &off}); err != nil {
		t.Fatal(err)
	}
	if err := typed.Set(closuredef.Patch{MainState: &moving}); err != nil {
		t.Fatal(err)
	}
	if st := typed.Get(); st.MainState != moving {
		t.Errorf("MainState %v, want Moving", st.MainState)
	}
	if n := len(rec.ids()); n != 2 {
		t.Errorf("events %v, want two EngageStateChanged", rec.ids())
	}
}

// TestDimensionChangesOrigin is the origin audit of the ClosureDimension
// server.
func TestDimensionChangesOrigin(t *testing.T) {
	t.Parallel()
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		s, err := closure.NewDimension(chipPanel(&panel{}))
		if err != nil {
			tb.Fatal(err)
		}
		return s
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Act: func(tb testing.TB, srv contract.ClusterServer) {
			st := cd.DimensionStateStruct{Position: pos(0), Latch: latch(true)}
			if err := cd.Wrap(srv).Set(cd.Patch{CurrentState: &st}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "command", Want: contract.OriginCommand, Act: func(tb testing.TB, srv contract.ClusterServer) {
			if _, err := srv.MatterInvoke(context.Background(), cd.CmdSetTarget, cd.SetTargetRequest{Position: ptr[uint16](5000), Latch: ptr(false)}); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.OriginCase{Name: "controller write", Act: func(tb testing.TB, srv contract.ClusterServer) {
			if err := srv.MatterWrite(context.Background(), cd.AttrResolution, uint16(1)); err == nil {
				tb.Fatal("a write of the read-only Resolution succeeded")
			}
		}},
	)
}

// TestDimensionTyped drives the ClosureDimension server through the
// generated typed form.
func TestDimensionTyped(t *testing.T) {
	t.Parallel()
	srv := newDimension(t, chipPanel(&panel{}))
	typed := cd.Wrap(srv)
	st := cd.DimensionStateStruct{Position: pos(2000), Latch: latch(false)}
	units := cd.UnitRangeStruct{Min: 0, Max: 5000}
	limits := cd.RangePercent100thsStruct{Min: 1000, Max: 9000}
	if err := typed.Set(cd.Patch{TargetState: &st, UnitRange: &units, LimitRange: &limits}); err != nil {
		t.Fatal(err)
	}
	got := typed.Get()
	if got.TargetState.Position.Value != 2000 || got.UnitRange.Max != 5000 || got.LimitRange.Min != 1000 {
		t.Errorf("state after the Set: %+v", got)
	}
	if err := typed.Set(cd.Patch{Null: []uint32{cd.AttrCurrentState}}); err != nil {
		t.Errorf("Set CurrentState null: %v", err)
	}
	bad := cd.RangePercent100thsStruct{Min: 9000, Max: 1000}
	if err := typed.Set(cd.Patch{LimitRange: &bad, TargetState: &st}); err == nil {
		t.Error("a LimitRange with Min above Max was set")
	}
	res := uint16(5)
	if err := typed.Set(cd.Patch{Resolution: &res}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Set of Resolution: %v, want ErrNotHeld", err)
	}
	if err := typed.HandleStep(func(context.Context, cd.StepRequest) (struct{}, error) { return struct{}{}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), cd.CmdStep, cd.StepRequest{NumberOfSteps: 1}); err != nil {
		t.Errorf("Step through the host handler: %v", err)
	}
	if err := typed.OnWrite(func(context.Context, cd.State, uint32) error { return nil }); err != nil {
		t.Errorf("OnWrite: %v", err)
	}
	if err := spec.EmitOn(srv, 1, 0, nil); !errors.Is(err, spec.ErrNotEmitted) {
		t.Errorf("Emit: %v, want ErrNotEmitted", err)
	}
}

// eventLog records the ids of the events emitted.
type eventLog struct{ events []uint32 }

func (l *eventLog) MatterEmitEvent(_ uint16, _, event uint32, _ any, _ contract.EventPriority) {
	l.events = append(l.events, event)
}

func (l *eventLog) ids() []uint32 { return l.events }

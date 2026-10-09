// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// servable lists the optional features the server serves next to
// Positioning (closure-control.element.ts: VT and PD add a position, PT a
// MainState value, MO an event).
var servable = []uint32{
	clusterwire.ClosureControlFeatureVentilation,
	clusterwire.ClosureControlFeaturePedestrian,
	clusterwire.ClosureControlFeatureProtection,
	clusterwire.ClosureControlFeatureManuallyOperable,
}

// TestServerMatchesTheGeneratedDefinition holds every FeatureMap the server
// derives against matter.js closure-control.element.ts
// (spectest.CheckServer): Positioning with each subset of the four optional
// features it serves, and the zero default.
func TestServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	for subset := range 1 << len(servable) {
		fm := clusterwire.ClosureControlFeaturePositioning
		for i, f := range servable {
			if subset&(1<<i) != 0 {
				fm |= f
			}
		}
		srv, err := closure.New(closure.Config{FeatureMap: fm})
		if err != nil {
			t.Fatalf("FeatureMap 0x%X: %v", fm, err)
		}
		if got := closure.DerivedFeatureMap(closure.Config{FeatureMap: fm}); got != fm {
			t.Errorf("DerivedFeatureMap(0x%X) = 0x%X", fm, got)
		}
		spectest.CheckServer(t, srv, closuredef.Definition, fm)
	}
	spectest.CheckServer(t, closure.NewControlServer(closure.Config{}), closuredef.Definition, closure.PositioningVentilationFeatureMap)
}

// TestFeatureMapIsDerivedFromWhatTheServerServes pins the refusal: a
// FeatureMap naming a feature whose elements the server does not serve
// (LT, IS, SP, CL), or leaving out Positioning, fails construction —
// New with ErrFeatureMap, NewControlServer with a panic — instead of
// advertising attributes, commands or events nobody answers.
func TestFeatureMapIsDerivedFromWhatTheServerServes(t *testing.T) {
	t.Parallel()
	for _, fm := range []uint32{
		clusterwire.ClosureControlFeatureMotionLatching,
		clusterwire.ClosureControlFeatureVentilation, // no Positioning
		clusterwire.ClosureControlFeaturePositioning | clusterwire.ClosureControlFeatureInstantaneous,
		clusterwire.ClosureControlFeaturePositioning | clusterwire.ClosureControlFeatureSpeed,
		clusterwire.ClosureControlFeaturePositioning | clusterwire.ClosureControlFeatureCalibration,
		clusterwire.ClosureControlFeaturePositioning | 1<<9, // undefined
	} {
		if _, err := closure.New(closure.Config{FeatureMap: fm}); !errors.Is(err, closure.ErrFeatureMap) {
			t.Errorf("FeatureMap 0x%X: New error %v, want ErrFeatureMap", fm, err)
		}
	}
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewControlServer accepted a FeatureMap New refuses")
		}
	}()
	closure.NewControlServer(closure.Config{FeatureMap: clusterwire.ClosureControlFeatureMotionLatching})
}

// TestMoveToTakesTheGeneratedRequest pins the shape the bridge now hands
// over: MoveTo decodes through the generated definition.
func TestMoveToTakesTheGeneratedRequest(t *testing.T) {
	t.Parallel()
	var got []clusterwire.ClosureTargetPosition
	srv := closure.NewControlServer(closure.Config{Move: func(_ context.Context, p clusterwire.ClosureTargetPosition) error {
		got = append(got, p)
		return nil
	}})
	vent := closuredef.TargetPositionMoveToVentilationPosition
	if _, err := srv.MatterInvoke(context.Background(), closuredef.CmdMoveTo, closuredef.MoveToRequest{Position: &vent}); err != nil {
		t.Fatalf("MoveTo: %v", err)
	}
	if len(got) != 1 || got[0] != clusterwire.ClosureTargetPositionMoveToVentilationPosition {
		t.Errorf("Move handler got %v, want [MoveToVentilationPosition]", got)
	}
	ped := closuredef.TargetPositionMoveToPedestrianPosition
	_, err := srv.MatterInvoke(context.Background(), closuredef.CmdMoveTo, &closuredef.MoveToRequest{Position: &ped})
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusConstraintError {
		t.Errorf("MoveTo Pedestrian without PD: %v, want CONSTRAINT_ERROR", err)
	}
	undefined := closuredef.TargetPositionEnum(9)
	_, err = srv.MatterInvoke(context.Background(), closuredef.CmdMoveTo, closuredef.MoveToRequest{Position: &undefined})
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusConstraintError {
		t.Errorf("MoveTo position 9: %v, want CONSTRAINT_ERROR", err)
	}
	_, err = srv.MatterInvoke(context.Background(), closuredef.CmdMoveTo, closuredef.MoveToRequest{})
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusInvalidCommand {
		t.Errorf("MoveTo with no field: %v, want INVALID_COMMAND", err)
	}
	_, err = srv.MatterInvoke(context.Background(), closuredef.CmdMoveTo, (*closuredef.MoveToRequest)(nil))
	if sce, ok := errors.AsType[im.StatusCodeError](err); !ok || sce.MatterStatusCode() != im.StatusInvalidCommand {
		t.Errorf("MoveTo nil: %v, want INVALID_COMMAND", err)
	}
}

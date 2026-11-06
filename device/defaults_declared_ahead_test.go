// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/closure"
	ccldef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/cluster/valve"
	"github.com/SukramJ/go-fabric/surface"
)

// latchingMover is a Mover with the three capability interfaces.
type latchingMover struct{ fakeMover }

func (latchingMover) LatchControlModes() ccldef.LatchControlModesBitmap {
	return ccldef.LatchControlModesRemoteLatching
}
func (latchingMover) Latch(context.Context, bool) error                         { return nil }
func (latchingMover) SetSpeed(context.Context, ccldef.ThreeLevelAutoEnum) error { return nil }
func (latchingMover) Calibrate(context.Context) error                           { return nil }

// levelValve is a StateSource with a level and a default open level.
type levelValve struct{ fakeValve }

func (levelValve) CurrentLevel() (uint8, bool)                      { return 50, true }
func (levelValve) TargetLevel() (uint8, bool)                       { return 0, false }
func (levelValve) DefaultOpenLevel() uint8                          { return 100 }
func (levelValve) SetDefaultOpenLevel(context.Context, uint8) error { return nil }

// TestDeclaredClosureAndValveAheadFeatures: the kit builds the
// ClosureControl server with LT, SP and CL on a Mover with their
// interfaces and the valve with LVL and TS — behind their "Ahead of
// matter.js" rows, which the device layer refuses until the host enables
// them (ADR 0030 decision 3) — and the valve's AutoCloseTime reads the
// device's clock as the node's Matter time.
func TestDeclaredClosureAndValveAheadFeatures(t *testing.T) {
	t.Parallel()
	ccDecl := func() ccldef.Declare {
		return ccldef.Declare{
			Features: ccldef.FeaturePositioning | ccldef.FeatureMotionLatching | ccldef.FeatureSpeed | ccldef.FeatureCalibration,
			Mover:    latchingMover{},
		}
	}
	step := uint8(10)
	vDecl := func() valvedef.Declare {
		return valvedef.Declare{
			Features: valvedef.FeatureLevel | valvedef.FeatureTimeSync, StateSource: levelValve{},
			Initial: valvedef.Patch{LevelStep: &step, DefaultOpenLevel: new(uint8(100))},
		}
	}
	if _, err := New(ClosureType(), testOptions("cc-off"), ccDecl()); !errors.Is(err, surface.ErrExtensionNotEnabled) {
		t.Errorf("ClosureControl LT|SP|CL without the extension: %v, want ErrExtensionNotEnabled", err)
	}
	if _, err := New(WaterValveType(), testOptions("v-off"), vDecl()); !errors.Is(err, surface.ErrExtensionNotEnabled) {
		t.Errorf("Valve LVL|TS without the extension: %v, want ErrExtensionNotEnabled", err)
	}
	on := func(name string) Options {
		o := testOptions(name)
		o.Surface = surface.Policies{Extensions: surface.NewExtensions(append(closure.Ahead.IDs(), valve.Ahead.IDs()...)...)}
		return o
	}
	cc, err := New(ClosureType(), on("cc"), ccDecl())
	if err != nil {
		t.Fatal(err)
	}
	srv, ok := cc.Server(ccldef.ClusterID).(*closure.ControlServer)
	if !ok || srv.FeatureMap() != uint32(ccDecl().Features) {
		t.Fatalf("ClosureControl server %T, FeatureMap %v", cc.Server(ccldef.ClusterID), srv)
	}
	v, err := New(WaterValveType(), on("v"), vDecl())
	if err != nil {
		t.Fatal(err)
	}
	vs, ok := v.Server(valvedef.ClusterID).(*valve.Server)
	if !ok {
		t.Fatalf("Valve server %T", v.Server(valvedef.ClusterID))
	}
	for _, id := range []uint32{valvedef.AttrAutoCloseTime, valvedef.AttrCurrentLevel, valvedef.AttrDefaultOpenLevel, valvedef.AttrLevelStep} {
		if !slices.Contains(vs.MatterAttributes(), id) {
			t.Errorf("Valve AttributeList %v lacks 0x%X", vs.MatterAttributes(), id)
		}
	}
	if got, _ := vs.MatterRead(valvedef.AttrLevelStep); got != step {
		t.Errorf("LevelStep = %v, want %d from the declaration's Initial", got, step)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/pump"
	"github.com/SukramJ/go-fabric/cluster/spec"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
)

// fakeLevel is a LevelSource that takes every command.
type fakeLevel struct{}

func (fakeLevel) CurrentLevel() (uint8, bool)                            { return 100, true }
func (fakeLevel) Options() lvldef.OptionsBitmap                          { return 0 }
func (fakeLevel) OnLevel() (uint8, bool)                                 { return 0, false }
func (fakeLevel) SetOptions(context.Context, lvldef.OptionsBitmap) error { return nil }
func (fakeLevel) SetOnLevel(context.Context, *uint8) error               { return nil }
func (fakeLevel) MoveToLevel(context.Context, lvldef.MoveToLevelRequest) error {
	return nil
}

func (fakeLevel) MoveToLevelWithOnOff(context.Context, lvldef.MoveToLevelWithOnOffRequest) error {
	return nil
}
func (fakeLevel) Move(context.Context, lvldef.MoveRequest) error { return nil }
func (fakeLevel) MoveWithOnOff(context.Context, lvldef.MoveWithOnOffRequest) error {
	return nil
}
func (fakeLevel) Step(context.Context, lvldef.StepRequest) error { return nil }
func (fakeLevel) StepWithOnOff(context.Context, lvldef.StepWithOnOffRequest) error {
	return nil
}
func (fakeLevel) Stop(context.Context, lvldef.StopRequest) error { return nil }
func (fakeLevel) StopWithOnOff(context.Context, lvldef.StopWithOnOffRequest) error {
	return nil
}

// TestALevelSourceServesTheSelection: a declared LevelSource gets the
// module's LevelControl server with the device type's selection — none
// for a Speaker, matter.js's LevelControlServer.with()
// (LevelControlServer.ts:865) — not the generated server a FeatureMap
// mismatch would leave behind.
func TestALevelSourceServesTheSelection(t *testing.T) {
	d, err := NewSpeaker(testOptions("speaker"), lvldef.Declare{LevelSource: fakeLevel{}})
	if err != nil {
		t.Fatal(err)
	}
	srv, ok := d.Server(lvldef.ClusterID).(*levelcontrol.Server)
	if !ok {
		t.Fatalf("LevelControl is %T, want the port server", d.Server(lvldef.ClusterID))
	}
	if fm, _ := srv.MatterRead(spec.AttrFeatureMap); fm != uint32(0) {
		t.Errorf("FeatureMap %v, want the Speaker's empty selection", fm)
	}
	if v, _ := srv.MatterRead(lvldef.AttrCurrentLevel); v != uint8(100) {
		t.Errorf("CurrentLevel %v, want the source's 100", v)
	}
}

// TestPumpSpeedLimitsWithoutAuto: a constant-speed pump whose Initial sets
// the limits SPD mandates gets the module's server; the limits are SPD's,
// not AUTO's optional group.
func TestPumpSpeedLimitsWithoutAuto(t *testing.T) {
	lo, hi := uint16(500), uint16(3200)
	d, err := New(&PumpType, testOptions("pump"), pumpdef.Declare{
		Features: pumpdef.FeatureConstantSpeed, StateSource: fakePump{},
		Initial: pumpdef.Patch{MinConstSpeed: &lo, MaxConstSpeed: &hi},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv, ok := d.Server(pumpdef.ClusterID).(*pump.Server)
	if !ok {
		t.Fatalf("PumpConfigurationAndControl is %T, want the module's server", d.Server(pumpdef.ClusterID))
	}
	if v, _ := srv.MatterRead(pumpdef.AttrMaxConstSpeed); v != hi {
		t.Errorf("MaxConstSpeed %v, want %d", v, hi)
	}
}

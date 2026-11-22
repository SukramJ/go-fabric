// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/levelcontrol"
	"thingfabric.dev/go-fabric/cluster/wire"
)

// scenesPort is the endpoint's ScenesManagement as the LevelControl
// server's scenes port sees it: it counts the invalidations.
type scenesPort struct {
	mu          sync.Mutex
	invalidated int
}

func (p *scenesPort) MakeAllFabricSceneInfoEntriesInvalid() {
	p.mu.Lock()
	p.invalidated++
	p.mu.Unlock()
}
func (*scenesPort) StoreGlobalScene(context.Context, uint8)  {}
func (*scenesPort) RecallGlobalScene(context.Context, uint8) {}

// take returns the invalidations since the last call.
func (p *scenesPort) take() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.invalidated
	p.invalidated = 0
	return n
}

// TestCommandsInvalidateTheSceneAfterTheGate holds the engine path to
// matter.js packages/node/src/behaviors/level-control/LevelControlServer.ts
// at db021da0: the plain commands invalidate the current scene only past
// the ExecuteIfOff gate (moveToLevel :269-276, move :354-359, step
// :421-426, stop :482-487), the "with On/Off" ones always (:293, :373,
// :442, :498); a command that fails changes nothing, its transaction
// rolled back (packages/protocol/src/action/server/CommandInvokeResponse.ts:509-516).
func TestCommandsInvalidateTheSceneAfterTheGate(t *testing.T) {
	d := newDevice(100, false)
	srv := engineServer(d, nil)
	if srv.MatterInvalidatesScenes() {
		t.Fatal("the server claims the invalidation without a scenes port")
	}
	port := &scenesPort{}
	srv.SetScenes(port)
	if !srv.MatterInvalidatesScenes() {
		t.Fatal("the server leaves the invalidation to the endpoint with a scenes port")
	}
	zero := u16(0)
	plain := []struct {
		cmd    uint32
		fields any
	}{
		{levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{Level: 200, TransitionTime: zero}},
		{levelcontrol.CmdMove, wire.MoveRequest{MoveMode: levelcontrol.MoveModeUp}},
		{levelcontrol.CmdStep, wire.StepRequest{StepMode: levelcontrol.StepModeUp, StepSize: 10, TransitionTime: zero}},
		{levelcontrol.CmdStop, wire.StopRequest{}},
	}
	for _, c := range plain {
		if err := invoke(t, srv, c.cmd, c.fields); err != nil {
			t.Fatalf("command 0x%02X: %v", c.cmd, err)
		}
		if n := port.take(); n != 0 {
			t.Errorf("command 0x%02X the gate refused invalidated the scene %d times", c.cmd, n)
		}
	}
	if l := level(t, srv); l != 100 {
		t.Fatalf("the gated commands moved the level to %d", l)
	}
	withOnOff := []struct {
		cmd    uint32
		fields any
	}{
		{levelcontrol.CmdStopWithOnOff, wire.StopRequest{}},
		{levelcontrol.CmdMoveToLevelWithOnOff, wire.MoveToLevelRequest{Level: 150, TransitionTime: zero}},
		{levelcontrol.CmdMoveWithOnOff, wire.MoveRequest{MoveMode: levelcontrol.MoveModeDown}},
		{levelcontrol.CmdStepWithOnOff, wire.StepRequest{StepMode: levelcontrol.StepModeUp, StepSize: 10, TransitionTime: zero}},
	}
	for _, c := range withOnOff {
		if err := invoke(t, srv, c.cmd, c.fields); err != nil {
			t.Fatalf("command 0x%02X: %v", c.cmd, err)
		}
		if n := port.take(); n != 1 {
			t.Errorf("command 0x%02X invalidated the scene %d times, want 1", c.cmd, n)
		}
	}
	// On, the plain commands pass the gate.
	if err := d.SetOnOff(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	for _, c := range plain {
		if err := invoke(t, srv, c.cmd, c.fields); err != nil {
			t.Fatalf("command 0x%02X: %v", c.cmd, err)
		}
		if n := port.take(); n != 1 {
			t.Errorf("command 0x%02X on a light that is on invalidated the scene %d times, want 1", c.cmd, n)
		}
	}
	// A Step at a null level fails (FAILURE) and leaves the scene.
	d.mu.Lock()
	d.known = false
	d.mu.Unlock()
	if err := invoke(t, srv, levelcontrol.CmdStep, plain[2].fields); err == nil {
		t.Fatal("Step at a null level succeeded")
	}
	if n := port.take(); n != 0 {
		t.Errorf("a failed Step invalidated the scene %d times", n)
	}
}

// TestHandOffCommandsInvalidateTheScene holds the hand-off path to the same
// lines: over a NativeSource, whose gate is the module's, a plain command
// the gate refuses leaves the scene valid; over a host's port, which
// applies its gate where the server cannot see it, every command the port
// accepts ends the scene.
func TestHandOffCommandsInvalidateTheScene(t *testing.T) {
	srv, _ := nativeServer(100, false, true, levelcontrol.NativeConfig{})
	port := &scenesPort{}
	srv.SetScenes(port)
	if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{Level: 200, TransitionTime: u16(0)}); err != nil {
		t.Fatal(err)
	}
	if n := port.take(); n != 0 {
		t.Errorf("a gated MoveToLevel over a NativeSource invalidated the scene %d times", n)
	}
	if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{
		Level: 200, TransitionTime: u16(0),
		OptionsMask: levelcontrol.OptionExecuteIfOff, OptionsOverride: levelcontrol.OptionExecuteIfOff,
	}); err != nil {
		t.Fatal(err)
	}
	if n := port.take(); n != 1 {
		t.Errorf("MoveToLevel with ExecuteIfOff over a NativeSource invalidated the scene %d times, want 1", n)
	}

	host := levelcontrol.NewServer(levelcontrol.Config{Source: newDevice(100, false), Lighting: true})
	host.SetScenes(port)
	if err := invoke(t, host, levelcontrol.CmdStop, wire.StopRequest{}); err != nil {
		t.Fatal(err)
	}
	if n := port.take(); n != 1 {
		t.Errorf("a Stop the host's port accepted invalidated the scene %d times, want 1", n)
	}
}

// TestReplacedCommandIsTheWholeCommand: a command the host replaced
// through Handle is the host's handler alone (spec.CommandRegistrar,
// decision 112): neither an accepted nor a refused one invalidates the
// scene — the server runs nothing of its own for it (R2-P5-apispec-05).
func TestReplacedCommandIsTheWholeCommand(t *testing.T) {
	srv := engineServer(newDevice(100, true), nil)
	port := &scenesPort{}
	srv.SetScenes(port)
	refusal := errors.New("not now")
	var refuse bool
	if err := srv.Handle(levelcontrol.CmdMoveToLevel, func(context.Context, any) (any, error) {
		if refuse {
			return nil, refusal
		}
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{Level: 50, TransitionTime: u16(0)}); err != nil {
		t.Fatal(err)
	}
	if n := port.take(); n != 0 {
		t.Errorf("a MoveToLevel the host's handler accepted invalidated the scene %d times, want 0", n)
	}
	if l := level(t, srv); l != 100 {
		t.Errorf("the server moved the level of a replaced command to %d", l)
	}
	refuse = true
	if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{Level: 60, TransitionTime: u16(0)}); !errors.Is(err, refusal) {
		t.Fatalf("the handler's refusal: %v", err)
	}
	if n := port.take(); n != 0 {
		t.Errorf("a MoveToLevel the host's handler refused invalidated the scene %d times, want 0", n)
	}
}

// ctProbe is a ColorTemperatureCoupling that counts its calls.
type ctProbe struct{ synced int }

func (c *ctProbe) SyncColorTemperatureWithLevel(context.Context, uint8) error {
	c.synced++
	return nil
}

var _ levelcontrol.ColorTemperatureCoupling = (*ctProbe)(nil)

// TestSceneRecallOfTheLevel ports matter.js
// packages/node/src/behaviors/level-control/LevelControlServer.ts:746-758
// at db021da0 (#applySceneValues): a level that is absent, equal to
// CurrentLevel or outside MinLevel..MaxLevel is ignored; any other is
// moved to through moveToLevelLogic with only ExecuteIfOff in effect — it
// applies on a light that is off, does not couple the colour temperature
// (:559-560), and invalidates the scene (:340), which the recall then
// makes current again.
func TestSceneRecallOfTheLevel(t *testing.T) {
	d := newDevice(100, false)
	ct := &ctProbe{}
	srv := engineServer(d, ct)
	port := &scenesPort{}
	srv.SetScenes(port)
	if err := srv.MatterWrite(context.Background(), levelcontrol.AttrOptions, levelcontrol.OptionCoupleColorTempToLevel); err != nil {
		t.Fatal(err)
	}
	for _, values := range []map[uint32]uint64{
		{},
		{levelcontrol.AttrCurrentLevel: 0},   // below MinLevel 1
		{levelcontrol.AttrCurrentLevel: 255}, // above MaxLevel 254
		{levelcontrol.AttrCurrentLevel: 100}, // the current level
	} {
		srv.MatterApplySceneValues(context.Background(), values, 0)
		if l := level(t, srv); l != 100 {
			t.Errorf("recalling %v moved the level to %d", values, l)
		}
	}
	if n := port.take(); n != 0 {
		t.Errorf("an ignored recall invalidated the scene %d times", n)
	}
	srv.MatterApplySceneValues(context.Background(), map[uint32]uint64{levelcontrol.AttrCurrentLevel: 200}, 0)
	if l := level(t, srv); l != 200 {
		t.Errorf("recalling level 200 on a light that is off: level %d", l)
	}
	if ct.synced != 0 {
		t.Errorf("the recall coupled the colour temperature %d times", ct.synced)
	}
	if n := port.take(); n != 1 {
		t.Errorf("the recall invalidated the scene %d times, want 1", n)
	}

	// On the hand-off path the level reaches the port as a MoveToLevel
	// with ExecuteIfOff, over the scene's transition in whole tenths.
	native, dev := nativeServer(100, false, true, levelcontrol.NativeConfig{})
	native.MatterApplySceneValues(context.Background(), map[uint32]uint64{levelcontrol.AttrCurrentLevel: 0}, 1000)
	wantLog(t, dev)
	native.MatterApplySceneValues(context.Background(), map[uint32]uint64{levelcontrol.AttrCurrentLevel: 50}, 1550)
	wantLog(t, dev, "ramp 50 1.5s false")
}

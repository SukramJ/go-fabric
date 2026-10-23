// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// TestNativeDimmerTakesTheHandOffPath: the native dimmer's lamp ramps by
// itself, so its LevelControl hands each command to the lamp as one ramp
// (levelcontrol.NewNativeSource): the level is part-way through a ramp
// after half its duration, RemainingTime reads 0, a "with On/Off" command
// switches the light on before it ramps up and off once a ramp down has
// arrived, and the OnOff server reports both.
func TestNativeDimmerTakesTheHandOffPath(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		lamp := newDemoNativeDimmer("dimmer")
		dev, err := lamp.device(part("native-dimmer"))
		if err != nil {
			t.Fatal(err)
		}
		lvl, ok := dev.Server(levelcontrol.ClusterID).(*levelcontrol.Server)
		if !ok {
			t.Fatal("the dimmer mounts no LevelControl server")
		}
		srv, _ := dev.Server(onoff.ClusterID).(*onoff.Server)
		ctx := context.Background()
		tenths := uint16(20)
		if _, err := lvl.MatterInvoke(ctx, levelcontrol.CmdMoveToLevelWithOnOff,
			wire.MoveToLevelRequest{Level: 227, TransitionTime: &tenths}); err != nil {
			t.Fatal(err)
		}
		if !lamp.OnOff() || !srv.OnOff() {
			t.Fatal("MoveToLevelWithOnOff left the light off")
		}
		if v, _ := lvl.MatterRead(levelcontrol.AttrRemainingTime); v != uint16(0) {
			t.Errorf("RemainingTime = %v, want 0 on the hand-off path", v)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if v, _ := lvl.MatterRead(levelcontrol.AttrCurrentLevel); v.(uint8) < 167 || v.(uint8) > 187 {
			t.Errorf("after 1 s the level is %v, want about 177 (127 → 227 in 2 s)", v)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if level, _ := lamp.Level(); level != 227 {
			t.Errorf("after 2 s the level is %d, want 227", level)
		}

		// MoveWithOnOff down at 254/s: 226 to go, 0.9 s; off on arrival.
		rate := uint8(254)
		if _, err := lvl.MatterInvoke(ctx, levelcontrol.CmdMoveWithOnOff,
			wire.MoveRequest{MoveMode: levelcontrol.MoveModeDown, Rate: &rate}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
		synctest.Wait()
		if !lamp.OnOff() {
			t.Fatal("switched off before the ramp arrived")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if level, _ := lamp.Level(); level != lvldef.LightingLevelMin || lamp.OnOff() || srv.OnOff() {
			t.Errorf("after the ramp down: level %d, on %t (server %t), want 1 and off", level, lamp.OnOff(), srv.OnOff())
		}

		// A plain Step while off is gated out; Stop halts a ramp.
		if _, err := lvl.MatterInvoke(ctx, levelcontrol.CmdStep,
			wire.StepRequest{StepMode: levelcontrol.StepModeUp, StepSize: 50}); err != nil {
			t.Fatal(err)
		}
		if level, _ := lamp.Level(); level != lvldef.LightingLevelMin {
			t.Errorf("a gated Step moved the level to %d", level)
		}
		if err := lvl.MatterWrite(ctx, levelcontrol.AttrOnLevel, uint64(100)); err != nil {
			t.Fatal(err)
		}
		srv.Set(true) // the OnOff cluster switches on: OnLevel applies
		synctest.Wait()
		if level, _ := lamp.Level(); level != 100 {
			t.Errorf("switch-on with OnLevel 100: level %d", level)
		}
		tenths = 100
		if _, err := lvl.MatterInvoke(ctx, levelcontrol.CmdMoveToLevel,
			wire.MoveToLevelRequest{Level: 200, TransitionTime: &tenths}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if _, err := lvl.MatterInvoke(ctx, levelcontrol.CmdStop, wire.StopRequest{}); err != nil {
			t.Fatal(err)
		}
		halted, _ := lamp.Level()
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if level, _ := lamp.Level(); level != halted || level < 140 || level > 160 {
			t.Errorf("Stop half-way: level %d, then %d, want it held about 150", halted, level)
		}
	})
}

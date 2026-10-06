// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// TestCeilingLightRampsThroughTheEngine: the ceiling light cannot ramp, so
// its LevelControl and ColorControl servers run the module's transition
// engine — a level and a colour temperature are readable part-way through
// a transition, RemainingTime is live, and a "with On/Off" command switches
// the dimmer on at once (TC-LVL-2.3 to 6.1, TC-CC-2.2, 6.2, 6.3 run against
// this).
func TestCeilingLightRampsThroughTheEngine(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		lamp := newDemoCeilingLight("ceiling")
		var lvl *levelcontrol.Server
		var cc *light.ColorControlServer
		for _, s := range lamp.MatterClusterServers() {
			switch v := s.(type) {
			case *levelcontrol.Server:
				lvl = v
			case *light.ColorControlServer:
				cc = v
			}
		}
		if lvl == nil || cc == nil {
			t.Fatal("the ceiling light mounts no LevelControl or ColorControl server")
		}
		ctx := context.Background()
		tenths := uint16(20)
		if _, err := lvl.MatterInvoke(ctx, levelcontrol.CmdMoveToLevelWithOnOff, levelcontrol.MoveToLevelRequest{Level: 227, TransitionTime: &tenths}); err != nil {
			t.Fatal(err)
		}
		if !lamp.isOn() {
			t.Fatal("MoveToLevelWithOnOff left the light off")
		}
		if v, _ := lvl.MatterRead(levelcontrol.AttrRemainingTime); v != uint16(20) {
			t.Errorf("RemainingTime = %v, want 20", v)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if level, _ := lamp.CurrentLevel(); level < 167 || level > 187 {
			t.Errorf("after 1 s the level is %d, want about 177 (127 → 227 in 2 s)", level)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if level, _ := lamp.CurrentLevel(); level != 227 {
			t.Errorf("after 2 s the level is %d, want 227", level)
		}

		if _, err := cc.MatterInvoke(ctx, wire.ColorCtrlCmdMoveColorTemperature, map[uint8]any{0: uint64(1), 1: uint64(10)}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if v, _ := cc.MatterRead(wire.ColorCtrlAttrColorTemperatureMireds); v.(uint16) <= 370 || v.(uint16) >= 500 {
			t.Errorf("after 1 s the colour temperature is %v, want on its way from 370 to 500", v)
		}
		cc.MatterQuiesce()
		lvl.MatterQuiesce()
	})
}

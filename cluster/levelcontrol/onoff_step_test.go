// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// laggingOnOff is a device whose OnOff() keeps reading off after
// SetOnOff(true): a hand-off On/Off port that reports late.
type laggingOnOff struct{ *device }

func (laggingOnOff) OnOff() bool { return false }

// TestOffDuringAWithOnOffFadeEndsIt (R2-P6-fsm-1401): a with-On/Off fade
// switches the device on once, at the command; a step does not switch it
// back on, and an Off during the fade ends it — RemainingTime 0, the level
// where it was. Follows connectedhomeip (LevelControlCluster.cpp:342-345,
// :742-826, :829-889, :351 at 6170af84); matter.js couples on every step
// and lets the fade run on (LevelControlServer.ts:545-547, :664 at
// db021da0) — the divergence this test pins. A port whose OnOff() lags its
// SetOnOff(true) keeps its fade.
func TestOffDuringAWithOnOffFadeEndsIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(10, false)
		srv := engineServer(d, nil)
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevelWithOnOff, wire.MoveToLevelRequest{Level: 200, TransitionTime: u16(100)}); err != nil {
			t.Fatal(err)
		}
		sleep(time.Second)
		if lvl, on := d.state(); !on || lvl <= 10 {
			t.Fatalf("level %d on %v one second in, want on and rising", lvl, on)
		}
		// The host switches the device off.
		if err := d.SetOnOff(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		sleep(time.Second)
		lvl, on := d.state()
		if on {
			t.Fatal("a step switched the device back on after the Off")
		}
		if remaining(t, srv) != 0 {
			t.Errorf("RemainingTime %d after the Off, want 0: the fade ended", remaining(t, srv))
		}
		sleep(15 * time.Second)
		if after, _ := d.state(); after != lvl {
			t.Errorf("the level moved %d → %d after the Off ended the fade", lvl, after)
		}
	})
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(10, false)
		srv := levelcontrol.NewServer(levelcontrol.Config{
			Source: d, Lighting: true,
			Transitions: &levelcontrol.Transitions{OnOff: laggingOnOff{d}},
		})
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevelWithOnOff, wire.MoveToLevelRequest{Level: 200, TransitionTime: u16(20)}); err != nil {
			t.Fatal(err)
		}
		sleep(5 * time.Second)
		if lvl, _ := d.state(); lvl != 200 {
			t.Errorf("a port that reads off late lost its fade at %d, want 200", lvl)
		}
	})
}

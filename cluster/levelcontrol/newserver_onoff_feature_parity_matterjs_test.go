// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"testing"
	"testing/synctest"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// TestNewServerCouplesToOnOffUnderTheFeatureOnly holds [levelcontrol.NewServer]
// to the gate [levelcontrol.New] has (review II, W3-clua2-04): a selection
// without OO neither couples a "with On/Off" command to the endpoint's
// OnOff nor asks it whether a plain command may run, whatever
// Transitions.OnOff names. Mirrors matter.js
// packages/node/src/behaviors/level-control/LevelControlServer.ts:558 at
// db021da0 (`const coupleOnOff = this.features.onOff && withOnOff && …`)
// and :729-736 (`#optionsAllowExecution`: `!this.features.onOff ||`).
func TestNewServerCouplesToOnOffUnderTheFeatureOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(50, false)
		srv := levelcontrol.NewServer(levelcontrol.Config{
			Source: d, Features: levelcontrol.FeatureLighting, HasFeatures: true,
			Transitions: &levelcontrol.Transitions{OnOff: d},
		})
		// A plain command executes although the OnOff port reads off:
		// without OO there is no On/Off state to gate on.
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{Level: 120, TransitionTime: u16(0)}); err != nil {
			t.Fatal(err)
		}
		if l := level(t, srv); l != 120 {
			t.Errorf("MoveToLevel without OO, OnOff port off: level %d, want 120", l)
		}
		// A "with On/Off" command moves the level and switches nothing.
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevelWithOnOff, wire.MoveToLevelRequest{Level: 200, TransitionTime: u16(0)}); err != nil {
			t.Fatal(err)
		}
		if l := level(t, srv); l != 200 {
			t.Errorf("MoveToLevelWithOnOff without OO: level %d, want 200", l)
		}
		if _, on := d.state(); on {
			t.Error("MoveToLevelWithOnOff without OO switched the OnOff port on")
		}
		if len(d.switches) != 0 {
			t.Errorf("the OnOff port was switched %v without OO", d.switches)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/level-control/OnOffCouplingTest.ts
// at 862ad75f: the coupling-block case the register listed as missing that
// has a Go form. The four switching cases are ported locally
// (TestOnOffCoupling, TestNativeSourceOnOffCoupling).
//
// matter.js blocks its onOff$Changed reaction (which moves the level to
// OnLevel) while a "with On/Off" command switches the light on, so the
// level is moved once. In Go the switch-on is the host's
// (levelcontrol.OnOff.SetOnOff applies OnLevel, as the lamp of
// parity_matterjs_helpers_test.go does) and the command's transition
// starts from there, so the level the command names is the one the light
// ends at. "lifts the on/off coupling block when its transaction rolls
// back" is the register's missing case: a transaction the caller abandons
// after the command has no Go form — a Go command commits when it returns
// and has no caller transaction to roll back.
package levelcontrol_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
)

func TestParityMatterJS_OnOffCouplingTest(t *testing.T) {
	t.Run("holds the on/off coupling block through a committed level change", func(t *testing.T) {
		// coupledLight() (OnOffCouplingTest.ts:134-145): off, level 1,
		// OnLevel 100.
		l := newNodeLight(t, nodeLightOptions{level: 1, on: false, onLevel: u8(100)})
		l.invoke(t, levelcontrol.CmdMoveToLevelWithOnOff, levelcontrol.MoveToLevelRequest{Level: 50})
		// The level is moved once: not a second time, to OnLevel.
		lv, on := l.lamp.state()
		if !on || lv != 50 {
			t.Fatalf("onOff %t, currentLevel %d; want true, 50", on, lv)
		}
		if !l.lamp.srv.OnOff() {
			t.Error("the OnOff server still reads off")
		}
	})
}

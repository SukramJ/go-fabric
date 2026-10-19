// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/level-control/RemainingTimeTest.ts
// at 862ad75f: the cases the register listed as missing — RemainingTime
// part-way through a managed transition and after it completed, and with an
// application-supplied initial RemainingTime of 0. The two stop cases are
// ported in cluster/transition (TestCancelReportsZero).
//
// The cases run on a MockServerNode in matter.js; the port runs them on a
// fabrictest device (parity_matterjs_helpers_test.go). The Go LevelControl
// server keeps no application-stated RemainingTime or transitionEndTime:
// under the engine RemainingTime is the engine's, on the hand-off path it
// reads 0 (notes/parity/by_design.md BD-Matter-LevelControl-NativeRamp).
// The application-supplied 0 of "reports the time left in a transition" is
// therefore the engine's own starting value; the two cases whose
// precondition is a remaining time the application states are the
// register's missing cases.
package levelcontrol_test

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
)

// transitioningLight is
// packages/node/test/behaviors/level-control/RemainingTimeTest.ts:11-32:
// a light that is on at level 1, moving to 254 over 15 s.
func transitioningLight(t *testing.T) *nodeLight {
	t.Helper()
	l := newNodeLight(t, nodeLightOptions{level: 1, on: true})
	l.invoke(t, levelcontrol.CmdMoveToLevel, levelcontrol.MoveToLevelRequest{Level: 254, TransitionTime: u16(150)})
	return l
}

func TestParityMatterJS_LevelControlRemainingTimeTest(t *testing.T) {
	t.Run("LevelControl RemainingTime", func(t *testing.T) {
		t.Run("reports the time left in a transition the application did not configure", func(t *testing.T) {
			l := transitioningLight(t)
			l.advance(10, 99*time.Millisecond)
			if got := level(t, l.srv); got <= 1 {
				t.Errorf("CurrentLevel %d, want above 1", got)
			}
			if got := remaining(t, l.srv); got == 0 {
				t.Error("RemainingTime 0, want above 0")
			}
		})

		t.Run("reads zero once the transition completes", func(t *testing.T) {
			l := transitioningLight(t)
			l.advance(200, 99*time.Millisecond)
			if got := level(t, l.srv); got != 254 {
				t.Errorf("CurrentLevel %d, want 254", got)
			}
			if got := remaining(t, l.srv); got != 0 {
				t.Errorf("RemainingTime %d, want 0", got)
			}
		})
	})

	t.Run("LevelControl RemainingTime with an application-supplied value", func(t *testing.T) {
		t.Run("reports the time left in a transition", func(t *testing.T) {
			// levelControl: { remainingTime: 0 } is the server's own
			// starting RemainingTime.
			l := transitioningLight(t)
			l.advance(10, 99*time.Millisecond)
			if got := remaining(t, l.srv); got == 0 {
				t.Error("RemainingTime 0, want above 0")
			}
		})
	})
}

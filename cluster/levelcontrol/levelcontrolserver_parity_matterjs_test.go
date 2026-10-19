// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/level-control/LevelControlServerTest.ts
// at 862ad75f: the cases the register listed as missing — the transition
// timers stopping when the node closes, a LevelControl server of an
// anonymous class with downlevel features. The other cases are ported in
// cluster/transition (TestStepCadenceRoundingAndFinish,
// TestRemainingTimeReportsOnCommandChanges) and here (TestExecuteIfOff).
// The case matter.js keeps commented out as non-deterministic ("emits
// CurrentLevel and RemainingTime updates quietly", :24-53) is the
// register's missing case: its vector puts the first CurrentLevel report
// 300 ms after the RemainingTime report, the Go server reports it at the
// first step (100 ms), so every later level of the vector is sampled at
// another phase of the quiet interval.
//
// The cases run on a MockServerNode in matter.js; the port runs them on a
// fabrictest device (parity_matterjs_helpers_test.go). node.close is the
// device's Stop, which quiesces every cluster server (bridge Stop,
// contract.ClusterQuiescer). MockTime.timerCountFor counts the timers of
// one name; the port gives the LevelControl server a fake clock of its own
// and counts that clock's armed timers — the transition's step timer and
// the CurrentLevel quieter's deferred report, matter.js's
// "transition-…-LevelControlBaseServer" and "delayed emit".
package levelcontrol_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// changeLevelRequest is changeLevel's MoveToLevel (LevelControlServerTest.ts:297-310).
var changeLevelRequest = levelcontrol.MoveToLevelRequest{Level: 254, TransitionTime: u16(150)}

func TestParityMatterJS_LevelControlServerTest(t *testing.T) {
	t.Run("stops transition timers when destroyed", testStopsTimersWhenDestroyed)
	t.Run("works with anonymous class and downlevel features", testAnonymousClass)
}

// testStopsTimersWhenDestroyed is LevelControlServerTest.ts:134-163.
func testStopsTimersWhenDestroyed(t *testing.T) {
	clk := clocktest.New(fabrictest.DefaultStart)
	// No timers initially.
	if n := clk.Pending(); n != 0 {
		t.Fatalf("%d timers before the node starts", n)
	}
	l := newNodeLight(t, nodeLightOptions{level: 1, on: true, clock: clk})
	// Partial steps so the transition does not complete: 50 steps leave a
	// deferred CurrentLevel report queued.
	l.invoke(t, levelcontrol.CmdMoveToLevel, changeLevelRequest)
	for range 50 {
		clk.Advance(99 * time.Millisecond)
	}
	// The transition timer and the deferred emit timer (expectTimers(1),
	// :180-183, for each name).
	if armed := clk.Armed(); len(armed) != 2 {
		t.Fatalf("armed timers %v, want the transition's and the deferred report's", armed)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := l.dev.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if armed := clk.Armed(); len(armed) != 0 {
		t.Fatalf("armed timers %v after the node closed, want none", armed)
	}
	// Beyond when a timer would have fired: nothing runs.
	level, _ := l.lamp.state()
	clk.Advance(100 * time.Second)
	if after, _ := l.lamp.state(); after != level {
		t.Fatalf("the level moved from %d to %d after the node closed", level, after)
	}
}

// testAnonymousClass is LevelControlServerTest.ts:165-177: a DimmableLight
// whose LevelControl server is an anonymous subclass with the Lighting
// feature alone. The Go form of an anonymous subclass is an unnamed struct
// type embedding the server; the Go server always serves OnOff (OO), so
// "downlevel" leaves the Lighting feature with OO — the node starts either
// way, which is what the case checks.
func testAnonymousClass(t *testing.T) {
	site := fabrictest.NewSite(t, fabrictest.Options{})
	d := newDevice(1, true)
	srv := struct{ *levelcontrol.Server }{levelcontrol.NewServer(levelcontrol.Config{
		Source: d, Lighting: true, Transitions: &levelcontrol.Transitions{OnOff: d, Clock: site.Clock()},
	})}
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{
		fabrictest.Endpoint("light:1", deviceTypeDimmableLight, []contract.ClusterServer{srv}...),
	}})
	if _, ok := dev.EndpointOf("light:1"); !ok {
		t.Fatal("the light has no endpoint")
	}
}

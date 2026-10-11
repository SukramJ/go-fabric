// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/energy"
	whmdef "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
)

// heaterStep is one expectation of a replayed certification sequence.
type heaterStep struct {
	step    string
	do      func(t *testing.T, w *demoWaterHeater)
	demand  bool // HeatDemand non-zero
	boost   energy.BoostState
	percent int // TankPercentage; -1 skips the check
}

func trigger(code uint64) func(*testing.T, *demoWaterHeater) {
	return func(t *testing.T, w *demoWaterHeater) {
		t.Helper()
		if !w.testEventTrigger(code) {
			t.Fatalf("trigger 0x%016X not handled", code)
		}
	}
}

func invoke(cmd uint32, fields any) func(*testing.T, *demoWaterHeater) {
	return func(t *testing.T, w *demoWaterHeater) {
		t.Helper()
		if _, err := w.whm.MatterInvoke(context.Background(), cmd, fields); err != nil {
			t.Fatalf("invoke 0x%02X: %v", cmd, err)
		}
	}
}

func boostFor(seconds uint32, percent, reheat *uint8) func(*testing.T, *demoWaterHeater) {
	return invoke(whmdef.CmdBoost, whmdef.BoostRequest{BoostInfo: energy.BoostInfo{Duration: seconds, TargetPercentage: percent, TargetReheat: reheat}})
}

func replay(t *testing.T, w *demoWaterHeater, steps []heaterStep) {
	t.Helper()
	for _, s := range steps {
		s.do(t, w)
		st := w.whm.State()
		if (st.HeatDemand != 0) != s.demand || w.whm.BoostState() != s.boost || (s.percent >= 0 && int(st.TankPercentage) != s.percent) {
			t.Errorf("step %s: demand 0x%02X, boost %d, tank %d%%; want demand %v, boost %d, tank %d%%",
				s.step, st.HeatDemand, w.whm.BoostState(), st.TankPercentage, s.demand, s.boost, s.percent)
		}
	}
}

// TestWaterHeaterTankPercentSequence replays TC_EWATERHTR_2_3.py (steps
// 4-13, connectedhomeip at the harness pin) against the daemon's tank
// model: a TargetPercentage boost, draw-offs to 76 % and 57 %, and a
// TargetReheat boost that resumes heating only below its reheat level.
func TestWaterHeaterTankPercentSequence(t *testing.T) {
	t.Parallel()
	w := newDemoWaterHeater("test heater")
	w.build()
	hundred, reheat := uint8(100), uint8(65)
	active, inactive := energy.BoostStateActive, energy.BoostStateInactive
	replay(t, w, []heaterStep{
		{"4", trigger(triggerWaterHeaterInstallation), false, inactive, 0},
		{"5", boostFor(600, &hundred, nil), true, active, 0},
		{"6", trigger(triggerWaterTemperature61C), false, active, 100},
		{"7", trigger(triggerDrawOffHotWater), true, active, 76},
		{"8", invoke(whmdef.CmdCancelBoost, whmdef.CancelBoostRequest{}), false, inactive, 76},
		{"9", boostFor(400, &hundred, &reheat), true, active, 76},
		{"10", trigger(triggerWaterTemperature61C), false, active, 100},
		{"11", trigger(triggerDrawOffHotWater), false, active, 76},
		{"12", trigger(triggerDrawOffHotWater), true, active, 57},
		{"13", invoke(whmdef.CmdCancelBoost, whmdef.CancelBoostRequest{}), false, inactive, 57},
		{"14", trigger(triggerWaterHeaterInstallationClear), false, inactive, 57},
	})
	w.mu.Lock()
	w.stopTimerLocked()
	w.mu.Unlock()
}

// TestWaterHeaterModeAndBoostSequence replays the mode, temperature and
// one-shot steps of TC_EWATERHTR_2_2.py (steps 4-11, 26): Manual mode
// heats until the water reaches 60 °C, Off stops, a one-shot boost ends
// when its duration runs out, and a CancelBoost without a boost changes
// nothing.
func TestWaterHeaterModeAndBoostSequence(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		w := newDemoWaterHeater("test heater")
		w.build()
		yes := true
		active, inactive := energy.BoostStateActive, energy.BoostStateInactive
		replay(t, w, []heaterStep{
			{"4", trigger(triggerWaterHeaterInstallation), false, inactive, -1},
			{"5", trigger(triggerWaterHeaterManual), true, inactive, -1},
			{"6", trigger(triggerWaterTemperature61C), false, inactive, -1},
			{"7", trigger(triggerWaterTemperature20C), true, inactive, -1},
			{"8", trigger(triggerWaterHeaterOff), false, inactive, -1},
			{"9", invoke(whmdef.CmdBoost, whmdef.BoostRequest{BoostInfo: energy.BoostInfo{Duration: 5, OneShot: &yes}}), true, active, -1},
			{"10", func(*testing.T, *demoWaterHeater) { time.Sleep(6 * time.Second); synctest.Wait() }, false, inactive, -1},
			{"11", invoke(whmdef.CmdBoost, whmdef.BoostRequest{BoostInfo: energy.BoostInfo{Duration: 600, OneShot: &yes}}), true, active, -1},
			// The one-shot boost ends when the water reaches its target.
			{"12", trigger(triggerWaterTemperature61C), false, inactive, -1},
			{"26", invoke(whmdef.CmdCancelBoost, whmdef.CancelBoostRequest{}), false, inactive, -1},
		})
		if trigger := uint64(0x0094_0000_0000_00FF); w.testEventTrigger(trigger) {
			t.Error("an unknown water heater trigger was handled")
		}
	})
}

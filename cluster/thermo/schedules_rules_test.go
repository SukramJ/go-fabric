// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// The Schedules write rules no case of matter.js's three Schedules suites
// names, each against the rule it holds the Go server to:
// #validateScheduleWriteRequest and #validateScheduleTransitions in
// matter.js packages/node/src/behaviors/thermostat/ThermostatServer.ts at
// b25ed960 (:2693-2817, :2825-3003), the presets getter a peer reads
// (:3196-3222), and AtomicWriteHandler.ts:433-458 (another peer's staged
// value is never seen). The fixtures are schedule-helpers' (the port in
// schedule_helpers_parity_matterjs_test.go).
package thermo_test

import (
	"testing"

	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
)

// TestScheduleWriteRules holds the rules matter.js applies per schedule
// and after all of them, over the wire inside an atomic write and by the
// host's SetSchedules.
func TestScheduleWriteRules(t *testing.T) {
	sixOnDays := func(day tdef.ScheduleDayOfWeekBitmap) []thermo.ScheduleTransition {
		trs := make([]thermo.ScheduleTransition, 0, 6)
		for i := range 6 {
			trs = append(trs, newScheduleTransition(trDay(day), trTime(uint16(i*60)))) //nolint:gosec // i < 6
		}
		return trs
	}

	t.Run("NumberOfScheduleTransitions bounds each schedule, not the list", func(t *testing.T) {
		// TS:2758-2762: schedule.transitions.length against the limit; two
		// schedules of six transitions each fit a limit of ten.
		p := schedulesThermostatPair(t, schedulesConfig(5, nil))
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{
			newSchedule(scTransitions(sixOnDays(tdef.ScheduleDayOfWeekMonday)...)),
			newSchedule(scTransitions(sixOnDays(tdef.ScheduleDayOfWeekTuesday)...)),
		}), im.StatusSuccess, im.StatusSuccess, im.StatusSuccess)
	})

	t.Run("a schedule type of no schedules exhausts its count", func(t *testing.T) {
		// TS:2750-2755 finds the type; TS:2799-2806 refuses one schedule
		// against its NumberOfSchedules 0 with RESOURCE_EXHAUSTED.
		p := schedulesThermostatPair(t, withScheduleTypes(schedulesConfig(5, nil),
			autoType(0, tdef.ScheduleTypeFeaturesSupportsPresets|tdef.ScheduleTypeFeaturesSupportsSetpoints)))
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule()}), im.StatusSuccess, im.StatusResourceExhausted)
	})

	t.Run("the count limits come after every schedule's own rules", func(t *testing.T) {
		// TS:2793-2806 after the loop: the third schedule's unknown preset
		// answers, not the second schedule's count.
		srv := newSchedulesServer(t, schedulesConfig(1, nil))
		wantStatusCode(t, srv.SetSchedules([]thermo.Schedule{newSchedule(), newSchedule(), newSchedule(scPreset(filled(16, 0xee)))}), im.StatusConstraintError, "names no preset")
	})

	t.Run("the handle rules come before the schedule type", func(t *testing.T) {
		// TS:2717-2748 before TS:2750-2755: an unknown handle on a schedule
		// of a mode no type names is NOT_FOUND.
		p := schedulesThermostatPair(t, schedulesConfig(5, nil))
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule(scHandle([]byte{0xee}), scMode(tdef.SystemModeOff))}),
			im.StatusSuccess, im.StatusNotFound)
	})

	t.Run("a peer's preset references resolve against its own pending Presets", func(t *testing.T) {
		// TS:3196-3222, AtomicWriteHandler.ts:449-452: the Presets another
		// peer staged and has not committed are not the writing peer's.
		presetHandle := []byte{0xaa}
		cfg := schedulesConfig(5, nil)
		cfg.Presets = []thermo.Preset{newPreset(withHandle(presetHandle))}
		p := schedulesThermostatPair(t, cfg)
		second := p.secondPeer(t)
		p.atomic(t, p.pair, beginWrite, presetsAttribute)
		if statuses, err := p.writePresets(parityCtx(t), p.pair, []thermo.Preset{}); err != nil || statuses[0].Status != im.StatusSuccess {
			t.Fatalf("Presets write %+v %v", statuses, err)
		}
		p.atomic(t, second, beginWrite, schedulesAttribute)
		statusesAre(t, p.writeSchedulesUnchecked(t, second, []thermo.Schedule{
			newSchedule(scTransitions(newScheduleTransition(trPreset(presetHandle)))),
		}), im.StatusSuccess, im.StatusSuccess)
	})

	t.Run("a Presets commit reads the stored schedules, not another peer's staged ones", func(t *testing.T) {
		// TS:2048-2065 reads persistedSchedules of the commit's own
		// transaction; another peer's staged Schedules are not in it.
		presetHandle := []byte{0xaa}
		cfg := schedulesConfig(5, []thermo.Schedule{newSchedule(scHandle([]byte{1}), scTransitions(newScheduleTransition(trPreset(presetHandle))))})
		cfg.Presets = []thermo.Preset{newPreset(withHandle(presetHandle))}
		p := schedulesThermostatPair(t, cfg)
		second := p.secondPeer(t)
		p.atomic(t, second, beginWrite, schedulesAttribute)
		statusesAre(t, p.writeSchedulesUnchecked(t, second, []thermo.Schedule{newSchedule(scHandle([]byte{1}))}), im.StatusSuccess, im.StatusSuccess)
		p.atomic(t, p.pair, beginWrite, presetsAttribute)
		if _, err := p.writePresets(parityCtx(t), p.pair, []thermo.Preset{}); err != nil {
			t.Fatal(err)
		}
		wantAtomic(t, p.atomic(t, p.pair, commitWrite, presetsAttribute), tdef.StatusFailure,
			[]tdef.AtomicResponseAttributeStatusEntry{{AttributeId: presetsAttribute, StatusCode: tdef.StatusInvalidInState}}, nil)
	})
}

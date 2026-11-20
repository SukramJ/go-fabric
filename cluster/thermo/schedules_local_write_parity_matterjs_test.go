// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat/SchedulesLocalWriteTest.ts
// at b25ed960: local writes of the schedules — handles, the announcement,
// the write rules, the active schedule, the built-in schedules and the
// presets the schedules reference.
//
// matter.js writes on a MockServerNode (`deviceEp.set({ thermostat:
// { schedules } })`, a local actor); the port writes with the server's
// SetSchedules — the one local write of the Go server, for the Schedules
// attribute and for persistedSchedules alike — and reads the stored
// schedules with Schedules() (`persistedSchedules`) or the attribute with
// MatterRead. recordThermostatChanges is the server's OnAttributeChange;
// a local SetActiveScheduleRequest is the command invoked without a peer,
// a local write of activeScheduleHandle SetActiveScheduleHandle, a local
// write of presets SetPresets.
//
// Not ported, each relying on matter.js's managed state: "keeps a held
// schedule live while the schedules are the configured ones" (a reference
// into the state that a later write refreshes), "refuses to clear the
// stored schedules and keeps them" (the assignment of undefined to
// persistedSchedules; SetSchedules takes a slice) and the three cases of
// an application observer on persistedSchedules$Changing that rewrites
// the schedules in flight ("refuses a schedule an observer stripped the
// handle from, and stays writable", "refuses a schedule an observer marks
// built-in after the thermostat issued its handle", "validates a change
// an observer makes after the thermostat normalized"), a seam the Go
// server does not have. A divergence (by_design BD-Thermostat-HostSchedules,
// owner decision O-8): SetSchedules is the host's authoritative replace,
// held to matter.js's content rules but not to the lifecycle rules of a
// Matter write — "rejects a schedule citing an unknown handle and keeps
// the stored schedules", "refuses to remove the active schedule" (the
// replace clears it) and "refuses to remove a schedule the application
// marked built-in" are not ported; schedules_host_test.go holds the Go
// behaviour. Planned: "keeps the generated handle across a
// restart" — the Go server keeps the schedules in memory or behind the
// host's SchedulePort; persisting them is a unit of its own.
package thermo_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
)

func TestParityMatterJS_SchedulesLocalWriteTest(t *testing.T) {
	t.Parallel()
	thermostat := func(t *testing.T, n uint8, schedules ...thermo.Schedule) *thermo.ThermostatServer {
		t.Helper()
		return newSchedulesServer(t, schedulesConfig(n, schedules))
	}
	// refused checks a refused write's status and that nothing is stored.
	refused := func(t *testing.T, srv *thermo.ThermostatServer, err error, status im.StatusCode) {
		t.Helper()
		wantStatusCode(t, err, status, "")
		if got := srv.Schedules(); len(got) != 0 {
			t.Errorf("stored schedules %+v after a refusal, want none", got)
		}
	}
	withPresets := func(cfg thermo.ThermostatConfig, presets ...thermo.Preset) thermo.ThermostatConfig {
		cfg.Presets = presets
		return cfg
	}
	offCapable := func(t *testing.T) *thermo.ThermostatServer {
		t.Helper()
		return newSchedulesServer(t, withScheduleTypes(schedulesConfig(5, nil),
			autoType(5, tdef.ScheduleTypeFeaturesSupportsPresets|tdef.ScheduleTypeFeaturesSupportsSetpoints|tdef.ScheduleTypeFeaturesSupportsOff)))
	}
	storesOne := func(t *testing.T, srv *thermo.ThermostatServer, schedules ...thermo.Schedule) thermo.Schedule {
		t.Helper()
		if err := srv.SetSchedules(schedules); err != nil {
			t.Fatal(err)
		}
		stored := srv.Schedules()
		if len(stored) != 1 {
			t.Fatalf("stored schedules %+v, want one", stored)
		}
		return stored[0]
	}

	t.Run("stores a locally written schedule and generates a handle", func(t *testing.T) {
		t.Parallel()
		sc := storesOne(t, thermostat(t, 5), newSchedule())
		if sc.ScheduleHandle.Null || len(sc.ScheduleHandle.Value) != 16 || sc.BuiltIn.Null || sc.BuiltIn.Value {
			t.Errorf("stored %+v, want a 16-byte handle, not built in", sc)
		}
	})

	t.Run("stores schedules written directly to persistedSchedules", func(t *testing.T) {
		t.Parallel()
		if sc := storesOne(t, thermostat(t, 5), newSchedule()); len(sc.ScheduleHandle.Value) != 16 {
			t.Errorf("stored %+v, want a 16-byte handle", sc)
		}
	})

	t.Run("serves the stored schedules through the attribute", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		storesOne(t, srv, newSchedule())
		v, _ := srv.MatterRead(schedulesAttribute)
		if l, ok := v.(spec.List[thermo.Schedule]); !ok || len(l) != 1 || len(l[0].ScheduleHandle.Value) != 16 {
			t.Errorf("Schedules reads %+v", v)
		}
	})

	t.Run("announces the attribute as changed", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		announcements := recordThermostatChanges(srv)
		storesOne(t, srv, newSchedule())
		if n := announcements.count(schedulesAttribute); n != 1 {
			t.Errorf("Schedules announced %d times, want 1", n)
		}
	})

	t.Run("announces nothing when the write is refused", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 2)
		announcements := recordThermostatChanges(srv)
		if err := srv.SetSchedules([]thermo.Schedule{newSchedule(), newSchedule(), newSchedule()}); err == nil {
			t.Fatal("three schedules accepted with NumberOfSchedules 2")
		}
		if n := announcements.count(schedulesAttribute); n != 0 {
			t.Errorf("Schedules announced %d times, want 0", n)
		}
	})

	t.Run("edits a stored schedule addressed by its handle", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		handle := storesOne(t, srv, newSchedule()).ScheduleHandle.Value
		sc := storesOne(t, srv, newSchedule(scHandle(handle), scTransitions(newScheduleTransition(trTime(480)))))
		if !bytes.Equal(sc.ScheduleHandle.Value, handle) || sc.Transitions[0].TransitionTime != 480 {
			t.Errorf("stored %+v, want the schedule %x at 480", sc, handle)
		}
	})

	t.Run("rejects more schedules than the thermostat supports", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 2)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(), newSchedule(), newSchedule()}), im.StatusResourceExhausted)
	})

	t.Run("checks every schedule's own rules before the schedule count limits", func(t *testing.T) {
		t.Parallel()
		// matter.js's second schedule carries a handle the device never
		// issued (NOT_FOUND); the host's replace is not held to that
		// lifecycle rule (BD-Thermostat-HostSchedules), so the port names a
		// content rule of the second schedule instead: a preset no preset
		// carries. Its refusal still answers before the count.
		srv := thermostat(t, 1)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(), newSchedule(scPreset(filled(16, 0xee)))}), im.StatusConstraintError)
	})

	t.Run("rejects more schedules than a systemMode scenario supports", func(t *testing.T) {
		t.Parallel()
		srv := newSchedulesServer(t, withScheduleTypes(schedulesConfig(5, nil),
			autoType(1, tdef.ScheduleTypeFeaturesSupportsPresets|tdef.ScheduleTypeFeaturesSupportsSetpoints)))
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(), newSchedule()}), im.StatusResourceExhausted)
	})

	t.Run("rejects a schedule for an unsupported systemMode", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scMode(tdef.SystemModeCool))}), im.StatusConstraintError)
	})

	t.Run("rejects a named schedule when the schedule type does not support names", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scName("Weekdays"))}), im.StatusConstraintError)
	})

	t.Run("accepts an unnamed schedule, which carries no name", func(t *testing.T) {
		t.Parallel()
		if sc := storesOne(t, thermostat(t, 5), newSchedule()); sc.Name != nil {
			t.Errorf("stored name %q, want none", *sc.Name)
		}
	})

	t.Run("rejects a schedule presetHandle when the schedule type does not support presets", func(t *testing.T) {
		t.Parallel()
		srv := newSchedulesServer(t, withScheduleTypes(schedulesConfig(5, nil), autoType(5, tdef.ScheduleTypeFeaturesSupportsSetpoints)))
		wantStatusCode(t, srv.SetSchedules([]thermo.Schedule{newSchedule(scPreset(filled(16, 1)))}), im.StatusConstraintError, "")
	})

	t.Run("rejects a schedule presetHandle that matches no existing preset", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scPreset(filled(16, 1)))}), im.StatusConstraintError)
	})

	t.Run("accepts a schedule presetHandle that matches an existing preset", func(t *testing.T) {
		t.Parallel()
		preset := newPreset(withHandle(filled(16, 2)))
		srv := newSchedulesServer(t, withPresets(schedulesConfig(5, nil), preset))
		if sc := storesOne(t, srv, newSchedule(scPreset(preset.PresetHandle.Value))); sc.PresetHandle == nil || !bytes.Equal(*sc.PresetHandle, preset.PresetHandle.Value) {
			t.Errorf("stored %+v, want the preset handle", sc)
		}
	})

	t.Run("rejects a transition presetHandle that matches no existing preset", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trPreset(filled(16, 3)))))}), im.StatusConstraintError)
	})

	t.Run("rejects a transition combining a presetHandle with a setpoint", func(t *testing.T) {
		t.Parallel()
		preset := newPreset(withHandle(filled(16, 4)))
		srv := newSchedulesServer(t, withPresets(schedulesConfig(5, nil), preset))
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trPreset(preset.PresetHandle.Value), trHeat(2000))))}), im.StatusConstraintError)
	})

	t.Run("rejects a transition system mode Off when the schedule type does not support Off", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trMode(tdef.SystemModeOff))))}), im.StatusConstraintError)
	})

	t.Run("rejects a transition setpoint when the schedule type does not support setpoints", func(t *testing.T) {
		t.Parallel()
		srv := newSchedulesServer(t, withScheduleTypes(schedulesConfig(5, nil), autoType(5, tdef.ScheduleTypeFeaturesSupportsPresets)))
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trHeat(2000), trCool(2600))))}), im.StatusConstraintError)
	})

	t.Run("rejects transitions exceeding NumberOfScheduleTransitionPerDay", func(t *testing.T) {
		t.Parallel()
		cfg := schedulesConfig(5, nil)
		one := uint8(1)
		cfg.NumberOfScheduleTransitionPerDay = &one
		srv := newSchedulesServer(t, cfg)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(
			newScheduleTransition(trDay(tdef.ScheduleDayOfWeekMonday), trTime(360)),
			newScheduleTransition(trDay(tdef.ScheduleDayOfWeekMonday), trTime(480)),
		))}), im.StatusResourceExhausted)
	})

	t.Run("rejects duplicate transitions on the same day and time", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(
			newScheduleTransition(trDay(tdef.ScheduleDayOfWeekMonday|tdef.ScheduleDayOfWeekTuesday), trTime(360)),
			newScheduleTransition(trDay(tdef.ScheduleDayOfWeekTuesday), trTime(360)),
		))}), im.StatusConstraintError)
	})

	t.Run("rejects a transition whose dayOfWeek sets the away/vacation bit", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trDay(tdef.ScheduleDayOfWeekAway))))}), im.StatusConstraintError)
	})

	t.Run("accepts a transition without setpoints that overrides systemMode when the schedule carries a presetHandle", func(t *testing.T) {
		t.Parallel()
		preset := newPreset(withHandle(filled(16, 12)))
		srv := newSchedulesServer(t, withPresets(schedulesConfig(5, nil), preset))
		sc := storesOne(t, srv, newSchedule(scPreset(preset.PresetHandle.Value),
			scTransitions(newScheduleTransition(trMode(tdef.SystemModeHeat), trNoHeat, trNoCool))))
		if sc.Transitions[0].HeatingSetpoint != nil {
			t.Errorf("stored transition %+v, want no heating setpoint", sc.Transitions[0])
		}
	})

	t.Run("rejects a transition without setpoints or presetHandle when the schedule carries no presetHandle", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trNoHeat, trNoCool)))}), im.StatusConstraintError)
	})

	t.Run("rejects a schedule with more transitions than NumberOfScheduleTransitions", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		trs := make([]thermo.ScheduleTransition, 0, 11)
		for i := range 11 {
			trs = append(trs, newScheduleTransition(trTime(uint16(i*10)))) //nolint:gosec // i < 11
		}
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(trs...))}), im.StatusResourceExhausted)
	})

	t.Run("rejects a transition systemMode equal to the schedule's systemMode", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trMode(tdef.SystemModeAuto))))}), im.StatusConstraintError)
	})

	t.Run("rejects a transition systemMode other than Auto, Heat, Cool or Off", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trMode(tdef.SystemModeEmergencyHeat), trNoHeat, trNoCool)))}), im.StatusConstraintError)
	})

	// matter.js refuses these through SystemModeEnum's value conformance
	// (a ConformanceError, CONSTRAINT_ERROR on the wire); the Go server
	// refuses a mode whose feature the thermostat lacks itself.
	t.Run("rejects a transition systemMode Cool without the Cooling feature through SystemModeEnum conformance", func(t *testing.T) {
		t.Parallel()
		srv := newSchedulesServer(t, heatingOnlySchedulesConfig(nil))
		wantStatusCode(t, srv.SetSchedules([]thermo.Schedule{newSchedule(scMode(tdef.SystemModeHeat),
			scTransitions(newScheduleTransition(trMode(tdef.SystemModeCool), trNoHeat)))}), im.StatusConstraintError, "")
	})

	t.Run("rejects a transition systemMode Auto without the AutoMode feature through SystemModeEnum conformance", func(t *testing.T) {
		t.Parallel()
		srv := newSchedulesServer(t, heatingOnlySchedulesConfig(nil))
		wantStatusCode(t, srv.SetSchedules([]thermo.Schedule{newSchedule(scMode(tdef.SystemModeHeat),
			scTransitions(newScheduleTransition(trMode(tdef.SystemModeAuto))))}), im.StatusConstraintError, "")
	})

	t.Run("accepts a transition systemMode Heat on a thermostat that supports heating", func(t *testing.T) {
		t.Parallel()
		sc := storesOne(t, thermostat(t, 5), newSchedule(scTransitions(newScheduleTransition(trMode(tdef.SystemModeHeat), trNoCool))))
		if m := sc.Transitions[0].SystemMode; m == nil || *m != tdef.SystemModeHeat {
			t.Errorf("stored transition %+v, want SystemMode Heat", sc.Transitions[0])
		}
	})

	t.Run("rejects a heatingSetpoint on a transition whose effective systemMode is Cool", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trMode(tdef.SystemModeCool))))}), im.StatusConstraintError)
	})

	t.Run("rejects a coolingSetpoint on a transition whose effective systemMode is Heat", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trMode(tdef.SystemModeHeat))))}), im.StatusConstraintError)
	})

	t.Run("rejects a setpoint on a transition to Off", func(t *testing.T) {
		t.Parallel()
		srv := offCapable(t)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trMode(tdef.SystemModeOff), trNoCool)))}), im.StatusConstraintError)
	})

	t.Run("accepts a transition to Off without setpoints when the schedule type supports Off", func(t *testing.T) {
		t.Parallel()
		sc := storesOne(t, offCapable(t), newSchedule(scTransitions(newScheduleTransition(trMode(tdef.SystemModeOff), trNoHeat, trNoCool))))
		if m := sc.Transitions[0].SystemMode; m == nil || *m != tdef.SystemModeOff {
			t.Errorf("stored transition %+v, want SystemMode Off", sc.Transitions[0])
		}
	})

	t.Run("rejects Auto transition setpoints closer than MinSetpointDeadBand", func(t *testing.T) {
		t.Parallel()
		// MinSetpointDeadBand is 2.5 °C in thermostatConfig, so a 2 °C gap
		// is too narrow.
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trHeat(2400), trCool(2600))))}), im.StatusConstraintError)
	})

	t.Run("accepts Auto transition setpoints exactly MinSetpointDeadBand apart", func(t *testing.T) {
		t.Parallel()
		storesOne(t, thermostat(t, 5), newSchedule(scTransitions(newScheduleTransition(trHeat(2350), trCool(2600)))))
	})

	t.Run("rejects a transition whose dayOfWeek sets no day", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		refused(t, srv, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trDay(0))))}), im.StatusConstraintError)
	})

	t.Run("clears an activeScheduleHandle no schedule carries", func(t *testing.T) {
		t.Parallel()
		cfg := schedulesConfig(5, nil)
		cfg.ActiveScheduleHandle = filled(16, 7)
		if h := newSchedulesServer(t, cfg).ActiveScheduleHandle(); h != nil {
			t.Errorf("ActiveScheduleHandle %x, want null", h)
		}
	})

	t.Run("keeps an activeScheduleHandle a configured schedule carries", func(t *testing.T) {
		t.Parallel()
		cfg := schedulesConfig(5, []thermo.Schedule{newSchedule(scHandle(filled(16, 7)))})
		cfg.ActiveScheduleHandle = filled(16, 7)
		if h := newSchedulesServer(t, cfg).ActiveScheduleHandle(); !bytes.Equal(h, filled(16, 7)) {
			t.Errorf("ActiveScheduleHandle %x, want the configured schedule's", h)
		}
	})

	t.Run("refuses to remove a preset a schedule's presetHandle still references", func(t *testing.T) {
		t.Parallel()
		preset := newPreset(withHandle(filled(16, 8)))
		srv := newSchedulesServer(t, withPresets(schedulesConfig(5, []thermo.Schedule{
			newSchedule(scHandle(filled(16, 9)), scPreset(preset.PresetHandle.Value)),
		}), preset))
		wantStatusCode(t, srv.SetPresets(context.Background(), []thermo.Preset{}), im.StatusInvalidInState, "")
	})

	t.Run("refuses to remove a preset a schedule transition's presetHandle still references", func(t *testing.T) {
		t.Parallel()
		preset := newPreset(withHandle(filled(16, 10)))
		srv := newSchedulesServer(t, withPresets(schedulesConfig(5, []thermo.Schedule{
			newSchedule(scHandle(filled(16, 11)), scTransitions(newScheduleTransition(trPreset(preset.PresetHandle.Value)))),
		}), preset))
		wantStatusCode(t, srv.SetPresets(context.Background(), []thermo.Preset{}), im.StatusInvalidInState, "")
	})

	t.Run("sets the active schedule via SetActiveScheduleRequest", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		handle := storesOne(t, srv, newSchedule()).ScheduleHandle.Value
		if err := invokeSetActiveSchedule(srv, handle); err != nil {
			t.Fatal(err)
		}
		if h := srv.ActiveScheduleHandle(); !bytes.Equal(h, handle) {
			t.Errorf("ActiveScheduleHandle %x, want %x", h, handle)
		}
	})

	t.Run("rejects SetActiveScheduleRequest for an unknown handle", func(t *testing.T) {
		t.Parallel()
		srv := thermostat(t, 5)
		wantStatusCode(t, invokeSetActiveSchedule(srv, filled(16, 1)), im.StatusInvalidCommand, "")
		if h := srv.ActiveScheduleHandle(); h != nil {
			t.Errorf("ActiveScheduleHandle %x, want null", h)
		}
	})
}

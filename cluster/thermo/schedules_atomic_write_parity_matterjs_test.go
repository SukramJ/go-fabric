// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat/SchedulesAtomicWriteTest.ts
// at b25ed960: schedules written through BeginWrite, the write and
// CommitWrite or RollbackWrite, the writes the device declines, the
// active and the built-in schedule, and Presets written in the same
// atomic write.
//
// matter.js commissions a MockSite pair and drives a subscribedPeer
// (`ep1.commandsOf(ThermostatClient).atomicRequest`, `state.schedules =
// …`, writeSchedulesUnchecked); the port commissions a fabrictest pair and
// invokes and writes over the wire. The controller's cache of Schedules is
// the pair's subscription to it; the device's announcements
// (recordThermostatChanges) are the server's OnAttributeChange;
// `deviceEp.state.thermostat.persistedSchedules` is the server's
// Schedules(). A write matter.js rejects ("Not found", "Constraint
// error") is a WriteStatuses entry with that status.
//
// Not ported: "refuses a commit whose settled schedules an observer
// stripped the handle from" — an application observer on
// persistedSchedules$Changing that rewrites the value in flight, a seam
// the Go server does not have.
package thermo_test

import (
	"bytes"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/cluster/thermo"
	"thingfabric.dev/go-fabric/im"
)

func TestParityMatterJS_SchedulesAtomicWriteTest(t *testing.T) {
	schedulesOK := []tdef.AtomicResponseAttributeStatusEntry{{AttributeId: schedulesAttribute, StatusCode: tdef.StatusSuccess}}
	commissioned := func(t *testing.T) *thermostatPair {
		t.Helper()
		return schedulesThermostatPair(t, schedulesConfig(5, nil))
	}
	begin := func(t *testing.T, p *thermostatPair) {
		t.Helper()
		wantAtomic(t, p.atomic(t, p.pair, beginWrite, schedulesAttribute), tdef.StatusSuccess, schedulesOK, timeoutMs(5000))
	}
	noneStored := func(t *testing.T, p *thermostatPair) {
		t.Helper()
		if got := p.srv.Schedules(); len(got) != 0 {
			t.Errorf("stored schedules %+v, want none", got)
		}
	}

	t.Run("encodes a whole-list assignment as replace-all followed by ADD", func(t *testing.T) {
		// As in the Presets port: the device's answer carries the paths of
		// the write, the REPLACE-ALL without a list index, then the append.
		p := commissioned(t)
		ia := p.writeSchedulesPaths(t, []thermo.Schedule{newSchedule()})
		if len(ia) != 2 || ia[0].HasListIndex || ia[0].ListAppend || !ia[1].ListAppend {
			t.Errorf("write paths %+v, want REPLACE-ALL then one append", ia)
		}
	})

	t.Run("applies schedules through BeginWrite, write and CommitWrite", func(t *testing.T) {
		p := commissioned(t)
		ctx := parityCtx(t)
		deviceAnnouncements := recordThermostatChanges(p.srv)
		clientReports := p.subscribeSchedules(ctx, t)

		begin(t, p)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule()}), im.StatusSuccess, im.StatusSuccess)

		// The write is staged only; the device's stored value stays empty
		// until commit.
		noneStored(t, p)

		wantAtomic(t, p.atomic(t, p.pair, commitWrite, schedulesAttribute), tdef.StatusSuccess, schedulesOK, nil)

		persisted := p.srv.Schedules()
		if len(persisted) != 1 || len(persisted[0].ScheduleHandle.Value) != 16 || persisted[0].SystemMode != tdef.SystemModeAuto ||
			persisted[0].BuiltIn.Null || persisted[0].BuiltIn.Value {
			t.Fatalf("persisted schedules %+v, want one Auto schedule with a 16-byte handle, not built in", persisted)
		}
		if n := deviceAnnouncements.count(schedulesAttribute); n != 1 {
			t.Errorf("Schedules announced %d times, want 1", n)
		}

		// The controller learns the handle the device generated, so it can
		// address the schedule in a later write.
		if err := p.site.Eventually(ctx, "a Schedules report with the issued handle", func() bool {
			last, ok := clientReports.last()
			return ok && len(last) == 1 && len(last[0].ScheduleHandle.Value) == 16
		}); err != nil {
			t.Fatal(err)
		}
		if cached := p.readSchedules(t, p.pair); len(cached) != 1 || len(cached[0].ScheduleHandle.Value) != 16 {
			t.Errorf("read schedules %+v, want the one with its 16-byte handle", cached)
		}
	})

	t.Run("declines a write outside an atomic write", func(t *testing.T) {
		p := commissioned(t)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule()}), im.StatusInvalidInState, im.StatusInvalidInState)
		noneStored(t, p)
		if got := p.readSchedules(t, p.pair); len(got) != 0 {
			t.Errorf("read schedules %+v, want none", got)
		}
	})

	t.Run("declines a schedule carrying a handle the device does not know", func(t *testing.T) {
		p := commissioned(t)
		begin(t, p)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule(scHandle([]byte{1, 2, 3, 4}))}), im.StatusSuccess, im.StatusNotFound)
		noneStored(t, p)
	})

	t.Run("declines a schedule for an unsupported systemMode", func(t *testing.T) {
		p := commissioned(t)
		begin(t, p)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule(scMode(tdef.SystemModeCool))}), im.StatusSuccess, im.StatusConstraintError)
	})

	t.Run("declines a schedule with more transitions than NumberOfScheduleTransitions", func(t *testing.T) {
		p := commissioned(t)
		begin(t, p)
		trs := make([]thermo.ScheduleTransition, 0, 11)
		for i := range 11 {
			trs = append(trs, newScheduleTransition(trTime(uint16(i*10)))) //nolint:gosec // i < 11
		}
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule(scTransitions(trs...))}), im.StatusSuccess, im.StatusResourceExhausted)
		noneStored(t, p)
	})

	t.Run("declines a transition systemMode equal to the schedule's systemMode", func(t *testing.T) {
		p := commissioned(t)
		begin(t, p)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trMode(tdef.SystemModeAuto))))}),
			im.StatusSuccess, im.StatusConstraintError)
	})

	t.Run("declines a schedule name longer than 64 characters through the schema constraint", func(t *testing.T) {
		p := schedulesThermostatPair(t, withScheduleTypes(schedulesConfig(5, nil),
			autoType(5, tdef.ScheduleTypeFeaturesSupportsPresets|tdef.ScheduleTypeFeaturesSupportsSetpoints|tdef.ScheduleTypeFeaturesSupportsNames)))
		begin(t, p)
		long := string(bytes.Repeat([]byte("x"), 65))
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule(scName(long))}), im.StatusSuccess, im.StatusConstraintError)
	})

	t.Run("declines a schedule without transitions through the schema constraint", func(t *testing.T) {
		p := commissioned(t)
		begin(t, p)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule(scTransitions())}), im.StatusSuccess, im.StatusConstraintError)
	})

	t.Run("declines a transitionTime past the end of the day through the schema constraint", func(t *testing.T) {
		p := commissioned(t)
		begin(t, p)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trTime(1440))))}),
			im.StatusSuccess, im.StatusConstraintError)
	})

	t.Run("discards a staged write on RollbackWrite", func(t *testing.T) {
		p := commissioned(t)
		begin(t, p)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule()}), im.StatusSuccess, im.StatusSuccess)
		wantAtomic(t, p.atomic(t, p.pair, rollbackWrite, schedulesAttribute), tdef.StatusSuccess, schedulesOK, nil)
		noneStored(t, p)
	})

	t.Run("with a built-in and an active schedule", func(t *testing.T) {
		builtIn, active := []byte{1}, []byte{2}
		thermostatWithActiveSchedule := func(t *testing.T) *thermostatPair {
			t.Helper()
			p := schedulesThermostatPair(t, schedulesConfig(5, []thermo.Schedule{
				newSchedule(scHandle(builtIn), scBuiltIn(true)),
				newSchedule(scHandle(active)),
			}))
			if _, err := invokeAs(t, p, tdef.CmdSetActiveScheduleRequest, tdef.SetActiveScheduleRequestRequest{ScheduleHandle: active}); err != nil {
				t.Fatal(err)
			}
			return p
		}
		failedWith := func(status tdef.Status) []tdef.AtomicResponseAttributeStatusEntry {
			return []tdef.AtomicResponseAttributeStatusEntry{{AttributeId: schedulesAttribute, StatusCode: status}}
		}

		t.Run("sets ActiveScheduleHandle through SetActiveScheduleRequest", func(t *testing.T) {
			p := thermostatWithActiveSchedule(t)
			if h := p.srv.ActiveScheduleHandle(); !bytes.Equal(h, active) {
				t.Errorf("ActiveScheduleHandle %x, want %x", h, active)
			}
		})

		t.Run("declines SetActiveScheduleRequest for an unknown handle and keeps the active schedule", func(t *testing.T) {
			p := thermostatWithActiveSchedule(t)
			_, err := invokeAs(t, p, tdef.CmdSetActiveScheduleRequest, tdef.SetActiveScheduleRequestRequest{ScheduleHandle: []byte{9}})
			wantStatus(t, err, im.StatusInvalidCommand)
			if h := p.srv.ActiveScheduleHandle(); !bytes.Equal(h, active) {
				t.Errorf("ActiveScheduleHandle %x, want %x", h, active)
			}
		})

		t.Run("refuses a CommitWrite that removes the active schedule with INVALID_IN_STATE", func(t *testing.T) {
			p := thermostatWithActiveSchedule(t)
			stored := p.srv.Schedules()
			p.atomic(t, p.pair, beginWrite, schedulesAttribute)
			p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule(scHandle(builtIn), scBuiltIn(true))})
			wantAtomic(t, p.atomic(t, p.pair, commitWrite, schedulesAttribute), tdef.StatusFailure, failedWith(tdef.StatusInvalidInState), nil)
			if got := p.srv.Schedules(); !schedulesEqual(got, stored) {
				t.Errorf("stored schedules %+v, want %+v", got, stored)
			}
		})

		t.Run("refuses a CommitWrite that removes a built-in schedule with CONSTRAINT_ERROR", func(t *testing.T) {
			p := thermostatWithActiveSchedule(t)
			stored := p.srv.Schedules()
			p.atomic(t, p.pair, beginWrite, schedulesAttribute)
			p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule(scHandle(active))})
			wantAtomic(t, p.atomic(t, p.pair, commitWrite, schedulesAttribute), tdef.StatusFailure, failedWith(tdef.StatusConstraintError), nil)
			if got := p.srv.Schedules(); !schedulesEqual(got, stored) {
				t.Errorf("stored schedules %+v, want %+v", got, stored)
			}
		})
	})

	t.Run("on a thermostat without Cooling and AutoMode", func(t *testing.T) {
		presetHandle := []byte{0xaa}
		// A transition that takes its setpoints from a preset carries no
		// setpoint field the thermostat could refuse.
		presetTransition := func(mode *tdef.SystemModeEnum) thermo.ScheduleTransition {
			h := presetHandle
			return thermo.ScheduleTransition{DayOfWeek: tdef.ScheduleDayOfWeekMonday, TransitionTime: 360, PresetHandle: &h, SystemMode: mode}
		}
		write := func(t *testing.T, tr thermo.ScheduleTransition) ([]im.StatusCode, []thermo.Schedule) {
			t.Helper()
			heat := int16(2000)
			p := schedulesThermostatPair(t, heatingOnlySchedulesConfig([]thermo.Preset{{
				PresetHandle: spec.ValueOf(presetHandle), PresetScenario: tdef.PresetScenarioOccupied,
				HeatingSetpoint: &heat, BuiltIn: spec.ValueOf(false),
			}}))
			p.atomic(t, p.pair, beginWrite, schedulesAttribute)
			statuses := p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule(scMode(tdef.SystemModeHeat), scTransitions(tr))})
			return statuses, p.srv.Schedules()
		}

		t.Run("accepts a transition that only references a preset", func(t *testing.T) {
			statuses, _ := write(t, presetTransition(nil))
			statusesAre(t, statuses, im.StatusSuccess, im.StatusSuccess)
		})

		for _, c := range []struct {
			name string
			mode tdef.SystemModeEnum
		}{{"Cool", tdef.SystemModeCool}, {"Auto", tdef.SystemModeAuto}} {
			t.Run("declines the same transition switching to systemMode "+c.name, func(t *testing.T) {
				mode := c.mode
				statuses, stored := write(t, presetTransition(&mode))
				statusesAre(t, statuses, im.StatusSuccess, im.StatusConstraintError)
				if len(stored) != 0 {
					t.Errorf("stored schedules %+v, want none", stored)
				}
			})
		}
	})

	t.Run("with Presets in the same atomic write", func(t *testing.T) {
		presetHandle, scheduleHandle := []byte{0xaa}, []byte{0x01}
		both := []uint32{presetsAttribute, schedulesAttribute}
		usingPreset := func() thermo.Schedule {
			return newSchedule(scHandle(scheduleHandle), scTransitions(newScheduleTransition(trPreset(presetHandle))))
		}
		usingSetpoints := func() thermo.Schedule {
			return newSchedule(scHandle(scheduleHandle), scTransitions(newScheduleTransition()))
		}
		// presetSchedulingEndpoint: the one schedule switches to the one preset.
		presetScheduling := func(t *testing.T) *thermostatPair {
			t.Helper()
			cfg := schedulesConfig(5, []thermo.Schedule{usingPreset()})
			cfg.Presets = []thermo.Preset{newPreset(withHandle(presetHandle))}
			return schedulesThermostatPair(t, cfg)
		}
		entry := func(attr uint32, status tdef.Status) tdef.AtomicResponseAttributeStatusEntry {
			return tdef.AtomicResponseAttributeStatusEntry{AttributeId: attr, StatusCode: status}
		}
		writePresets := func(t *testing.T, p *thermostatPair, ps []thermo.Preset) []im.StatusCode {
			t.Helper()
			statuses, err := p.writePresets(parityCtx(t), p.pair, ps)
			if err != nil {
				t.Fatal(err)
			}
			out := make([]im.StatusCode, len(statuses))
			for i, s := range statuses {
				out[i] = s.Status
			}
			return out
		}

		t.Run("removes a preset together with the schedule transition that referenced it", func(t *testing.T) {
			p := presetScheduling(t)
			p.atomic(t, p.pair, beginWrite, both...)
			writePresets(t, p, []thermo.Preset{})
			p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{usingSetpoints()})
			wantAtomic(t, p.atomic(t, p.pair, commitWrite, both...), tdef.StatusSuccess,
				[]tdef.AtomicResponseAttributeStatusEntry{entry(presetsAttribute, tdef.StatusSuccess), entry(schedulesAttribute, tdef.StatusSuccess)}, nil)
			if got := p.srv.Presets(); len(got) != 0 {
				t.Errorf("presets %+v, want none", got)
			}
			if got := p.srv.Schedules(); len(got) != 1 || got[0].Transitions[0].PresetHandle != nil {
				t.Errorf("schedules %+v, want the transition without a preset", got)
			}
		})

		t.Run("refuses the whole atomic write when Presets staged after Schedules removes a referenced preset", func(t *testing.T) {
			p := presetScheduling(t)
			p.atomic(t, p.pair, beginWrite, both...)
			statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{usingPreset()}), im.StatusSuccess, im.StatusSuccess)
			statusesAre(t, writePresets(t, p, []thermo.Preset{}), im.StatusSuccess)
			wantAtomic(t, p.atomic(t, p.pair, commitWrite, both...), tdef.StatusFailure,
				[]tdef.AtomicResponseAttributeStatusEntry{entry(presetsAttribute, tdef.StatusInvalidInState), entry(schedulesAttribute, tdef.StatusConstraintError)}, nil)
			if got := p.srv.Presets(); len(got) != 1 {
				t.Errorf("presets %+v, want the stored one", got)
			}
			if got := p.srv.Schedules(); len(got) != 1 || got[0].Transitions[0].PresetHandle == nil || !bytes.Equal(*got[0].Transitions[0].PresetHandle, presetHandle) {
				t.Errorf("schedules %+v, want the transition naming the preset", got)
			}
		})

		t.Run("declines a schedule that references a preset the same atomic write removes", func(t *testing.T) {
			p := presetScheduling(t)
			p.atomic(t, p.pair, beginWrite, both...)
			writePresets(t, p, []thermo.Preset{})
			// The schedule is unchanged, but its preset no longer exists once
			// the pending Presets apply.
			statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{usingPreset()}), im.StatusSuccess, im.StatusConstraintError)
			p.atomic(t, p.pair, rollbackWrite, both...)
			if got := p.srv.Presets(); len(got) != 1 {
				t.Errorf("presets %+v, want the stored one", got)
			}
		})

		t.Run("declines a schedule whose own presetHandle names a preset the same atomic write removes", func(t *testing.T) {
			p := presetScheduling(t)
			p.atomic(t, p.pair, beginWrite, both...)
			writePresets(t, p, []thermo.Preset{})
			statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{
				newSchedule(scHandle(scheduleHandle), scPreset(presetHandle), scTransitions(newScheduleTransition())),
			}), im.StatusSuccess, im.StatusConstraintError)
			p.atomic(t, p.pair, rollbackWrite, both...)
		})
	})
}

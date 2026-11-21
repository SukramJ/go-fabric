// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
)

// withPreset2Schedule adds the unoccupied preset 02 and schedule 01, which
// switches to it, to presetsAndSchedulesServer's configuration.
func withPreset2Schedule(cfg *ThermostatConfig) {
	cool, heat := int16(2600), int16(2000)
	cfg.Presets = append(cfg.Presets, Preset{
		PresetHandle: spec.ValueOf([]byte{0x02}), PresetScenario: tdef.PresetScenarioUnoccupied,
		CoolingSetpoint: &cool, HeatingSetpoint: &heat, BuiltIn: spec.ValueOf(false),
	})
	cfg.ScheduleTypes[0].ScheduleTypeFeatures |= tdef.ScheduleTypeFeaturesSupportsPresets
	cfg.Schedules = []Schedule{usesPreset2()}
}

// TestScheduleCommitCheckRunsAfterStaging: the Schedules commit check —
// a built-in or the active schedule dropped — runs where matter.js runs
// it, in schedules$AtomicChanged (ThermostatServer.ts:3009-3014 at
// b25ed960, AtomicWriteHandler.ts:314-320), after the Schedules staged.
// So a Presets write that drops a preset only the dropped schedules named
// reads the unit's staged Schedules and passes, while Schedules answers
// INVALID_IN_STATE for the active schedule it drops.
func TestScheduleCommitCheckRunsAfterStaging(t *testing.T) {
	s := presetsAndSchedulesServer(t, withPreset2Schedule)
	if err := s.SetActiveScheduleHandle([]byte{0x01}); err != nil {
		t.Fatal(err)
	}
	both := []uint32{tdef.AttrPresets, tdef.AttrSchedules}
	atomicAs(t, s, atomicBeginWrite, both)
	stage(t, s, tdef.AttrPresets, s.Presets()[:1])
	stage(t, s, tdef.AttrSchedules, []Schedule{})
	wantStatuses(t, atomicAs(t, s, atomicCommitWrite, both), tdef.StatusFailure,
		tdef.AtomicResponseAttributeStatusEntry{AttributeId: tdef.AttrPresets, StatusCode: tdef.StatusSuccess},
		tdef.AtomicResponseAttributeStatusEntry{AttributeId: tdef.AttrSchedules, StatusCode: tdef.StatusInvalidInState})
}

// activatingSchedulePort sets the active handle from the host while a
// commit runs, between its checks and its store.
type activatingSchedulePort struct {
	s      *ThermostatServer
	handle []byte
}

func (*activatingSchedulePort) SetActiveSchedule(context.Context, []byte) error { return nil }

func (p *activatingSchedulePort) CommitSchedules(context.Context, []Schedule) error {
	if p.handle == nil {
		return nil
	}
	h := p.handle
	p.handle = nil
	return p.s.SetActiveScheduleHandle(h)
}

// TestDanglingActiveHandleDoesNotBlockCommits (F5): a host that activates
// a schedule while a commit dropping it runs leaves a handle that names
// no schedule; the next commit is not refused for it, as matter.js
// refuses only the removal of an active schedule that was stored
// (ThermostatServer.ts:3073-3085, :3077-3080 at b25ed960).
func TestDanglingActiveHandleDoesNotBlockCommits(t *testing.T) {
	port := &activatingSchedulePort{}
	s := presetsAndSchedulesServer(t, func(cfg *ThermostatConfig) {
		cfg.Schedules = []Schedule{scheduleWithHandle1(360)}
		cfg.SchedulePort = port
	})
	port.s, port.handle = s, []byte{0x01}
	schedules := []uint32{tdef.AttrSchedules}
	ok := tdef.AtomicResponseAttributeStatusEntry{AttributeId: tdef.AttrSchedules, StatusCode: tdef.StatusSuccess}

	// The commit drops schedule 01; the port activates it meanwhile.
	atomicAs(t, s, atomicBeginWrite, schedules)
	stage(t, s, tdef.AttrSchedules, []Schedule{scheduleAt(480)})
	wantStatuses(t, atomicAs(t, s, atomicCommitWrite, schedules), tdef.StatusSuccess, ok)
	if h := s.ActiveScheduleHandle(); !bytes.Equal(h, []byte{0x01}) {
		t.Fatalf("ActiveScheduleHandle %x, want the dangling 01 the race left", h)
	}

	// The next commit is judged on the stored list, which 01 is not in.
	atomicAs(t, s, atomicBeginWrite, schedules)
	stage(t, s, tdef.AttrSchedules, []Schedule{scheduleAt(600)})
	wantStatuses(t, atomicAs(t, s, atomicCommitWrite, schedules), tdef.StatusSuccess, ok)
}

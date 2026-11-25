// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/contract"
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

// scheduleEventsA4 records the ActiveScheduleChange events of a server.
type scheduleEventsA4 struct {
	mu     sync.Mutex
	events []tdef.ActiveScheduleChangeEvent
}

func (r *scheduleEventsA4) MatterEmitEvent(_ uint16, _, event uint32, data any, _ contract.EventPriority) {
	if event != tdef.EventActiveScheduleChange {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, data.(tdef.ActiveScheduleChangeEvent))
}

func (r *scheduleEventsA4) take() []tdef.ActiveScheduleChangeEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

// TestCommitClearsAHandleTheHostSetMeanwhile (R4-fsm-4253): a host that
// activates a schedule while a commit dropping it runs — after the
// commit's checks, before its store — does not leave a handle that names
// no schedule: the commit clears it at the store, as [SetSchedules]
// clears an active schedule its list drops, and with TEVT the clear emits
// one ActiveScheduleChange. The next commit is not refused for it.
func TestCommitClearsAHandleTheHostSetMeanwhile(t *testing.T) {
	port := &activatingSchedulePort{}
	s := presetsAndSchedulesServer(t, func(cfg *ThermostatConfig) {
		cfg.Features |= ThermostatFeatureTEVT
		cfg.Schedules = []Schedule{scheduleWithHandle1(360)}
		cfg.SchedulePort = port
	})
	rec := &scheduleEventsA4{}
	s.SetMatterEventEmitter(rec)
	s.SetEndpoint(1)
	port.s, port.handle = s, []byte{0x01}
	schedules := []uint32{tdef.AttrSchedules}
	ok := tdef.AtomicResponseAttributeStatusEntry{AttributeId: tdef.AttrSchedules, StatusCode: tdef.StatusSuccess}

	// The commit drops schedule 01; the port activates it meanwhile.
	atomicAs(t, s, atomicBeginWrite, schedules)
	stage(t, s, tdef.AttrSchedules, []Schedule{scheduleAt(480)})
	_ = rec.take()
	wantStatuses(t, atomicAs(t, s, atomicCommitWrite, schedules), tdef.StatusSuccess, ok)
	if h := s.ActiveScheduleHandle(); h != nil {
		t.Fatalf("ActiveScheduleHandle %x names no stored schedule, want null", h)
	}
	got := rec.take()
	// The host's own set emits null → 01; the commit's clear 01 → null.
	if len(got) != 2 {
		t.Fatalf("ActiveScheduleChange events %+v, want the host's set and one clear", got)
	}
	if e := got[1]; !bytes.Equal(e.PreviousScheduleHandle.Value, []byte{0x01}) || !e.CurrentScheduleHandle.Null {
		t.Errorf("clear event %+v, want 01 to null", e)
	}

	// The next commit is judged on the stored list and passes.
	atomicAs(t, s, atomicBeginWrite, schedules)
	stage(t, s, tdef.AttrSchedules, []Schedule{scheduleAt(600)})
	wantStatuses(t, atomicAs(t, s, atomicCommitWrite, schedules), tdef.StatusSuccess, ok)
}

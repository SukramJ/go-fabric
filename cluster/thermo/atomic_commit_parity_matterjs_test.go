// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of the "CommitWrite of more than one attribute (§7.15.6.4.2)"
// cases of matter.js packages/node/test/behaviors/thermostat/AtomicWriteHandlerTest.ts
// at 862ad75f (:392-516), on a thermostat serving Presets and Schedules
// (createPresetsAndSchedulesNode, :186-214).
//
// matter.js stages each value through AtomicWriteHandler.writeAttribute
// (stageAs, :93-101), which checks a Schedules value no further than its
// schema at commit; the port stages Presets and a valid Schedules list
// through the server's write (MatterWrite, the peer's context) and the
// one value the write would refuse — a TransitionTime of 1440 — on the
// atomic-write state directly, which is why the port runs in the package.
//
// "validates an attribute once the other pending attributes are staged"
// observes, in matter.js, the Schedules a presets$AtomicChanged observer
// reads during the commit; the Go server has no such observer, and the
// commit's reading of the staged Schedules is the presets precommit's
// in-use check (chip ThermostatClusterPresets.cpp:708-724 at 645b267a): a
// preset the write drops that a staged schedule names is refused.
package thermo

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/im"
)

// presetsAndSchedulesServer is createPresetsAndSchedulesNode: HEAT, COOL,
// AUTO, PRES and MSCH; presets of the Occupied (5) and Unoccupied (1)
// types, the one stored preset built in with handle 01; one Heat schedule
// type of two schedules with setpoints, two transitions in all, no
// per-day limit, no schedules. extra edits the configuration.
func presetsAndSchedulesServer(t *testing.T, extra ...func(*ThermostatConfig)) *ThermostatServer {
	t.Helper()
	deadband := int8(25)
	cool, heat := int16(2600), int16(2000)
	cfg := ThermostatConfig{
		Features:                ThermostatFeatureHEAT | ThermostatFeatureCOOL | ThermostatFeatureAUTO | ThermostatFeaturePRES | ThermostatFeatureMSCH,
		AbsMinHeatSetpointLimit: 700, AbsMaxHeatSetpointLimit: 3000,
		AbsMinCoolSetpointLimit: 1600, AbsMaxCoolSetpointLimit: 3200,
		InitialHeatingSetpoint: 2000, InitialCoolingSetpoint: 2600,
		MinSetpointDeadBand: &deadband,
		NumberOfPresets:     5,
		PresetTypes: []PresetType{
			{PresetScenario: tdef.PresetScenarioOccupied, NumberOfPresets: 5},
			{PresetScenario: tdef.PresetScenarioUnoccupied, NumberOfPresets: 1},
		},
		Presets: []Preset{{
			PresetHandle: spec.ValueOf([]byte{0x01}), PresetScenario: tdef.PresetScenarioOccupied,
			CoolingSetpoint: &cool, HeatingSetpoint: &heat, BuiltIn: spec.ValueOf(true),
		}},
		ScheduleTypes: []ScheduleType{{
			SystemMode: tdef.SystemModeHeat, NumberOfSchedules: 2, ScheduleTypeFeatures: tdef.ScheduleTypeFeaturesSupportsSetpoints,
		}},
		NumberOfSchedules: 2, NumberOfScheduleTransitions: 2,
	}
	for _, e := range extra {
		e(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// peerContext is the request context of node 1 on fabric 1 with every
// privilege, invoking on endpoint 1 — matter.js invokeAs (:126-140).
func peerContext() context.Context {
	ctx := im.WithFabricFilter(context.Background(), true, 1)
	ctx = im.WithSubject(ctx, 1, nil)
	ctx = im.WithAuthority(ctx, func(uint16, uint32, uint8) im.StatusCode { return im.StatusSuccess })
	return im.WithCommandPath(ctx, im.ConcreteCommandPath{Endpoint: 1, Cluster: tdef.ClusterID, Command: tdef.CmdAtomicRequest, HasEndpoint: true})
}

func atomicAs(t *testing.T, s *ThermostatServer, requestType uint8, attrs []uint32) tdef.AtomicResponse {
	t.Helper()
	req := tdef.AtomicRequestRequest{RequestType: requestType, AttributeRequests: attrs}
	if requestType == atomicBeginWrite {
		timeout := uint16(5000)
		req.Timeout = &timeout
	}
	v, err := s.MatterInvoke(peerContext(), tdef.CmdAtomicRequest, req)
	if err != nil {
		t.Fatal(err)
	}
	return v.(tdef.AtomicResponse)
}

// scheduleAt is matter.js's schedule(transitionTime) (:103-110).
func scheduleAt(minute uint16) Schedule {
	heat := int16(2000)
	return Schedule{
		ScheduleHandle: spec.NullOf[[]byte](), SystemMode: tdef.SystemModeHeat, BuiltIn: spec.NullOf[bool](),
		Transitions: []ScheduleTransition{{DayOfWeek: tdef.ScheduleDayOfWeekMonday, TransitionTime: minute, HeatingSetpoint: &heat}},
	}
}

// newUnoccupiedPreset is newPreset({presetScenario: Unoccupied}).
func newUnoccupiedPreset() Preset {
	cool, heat := int16(2600), int16(2000)
	return Preset{
		PresetHandle: spec.NullOf[[]byte](), PresetScenario: tdef.PresetScenarioUnoccupied,
		CoolingSetpoint: &cool, HeatingSetpoint: &heat, BuiltIn: spec.ValueOf(false),
	}
}

func stage(t *testing.T, s *ThermostatServer, attr uint32, v any) {
	t.Helper()
	if err := s.MatterWrite(peerContext(), attr, v); err != nil {
		t.Fatalf("staging 0x%04X: %v", attr, err)
	}
}

func wantStatuses(t *testing.T, got tdef.AtomicResponse, command tdef.Status, entries ...tdef.AtomicResponseAttributeStatusEntry) {
	t.Helper()
	if got.StatusCode != command || len(got.AttributeStatus) != len(entries) {
		t.Fatalf("AtomicResponse %+v, want %d with %+v", got, command, entries)
	}
	for i := range entries {
		if got.AttributeStatus[i] != entries[i] {
			t.Errorf("AtomicResponse %+v, want %d with %+v", got, command, entries)
			return
		}
	}
}

func TestParityMatterJS_AtomicWriteHandlerCommitTest(t *testing.T) {
	presetsAttr, schedulesAttr := tdef.AttrPresets, tdef.AttrSchedules
	both := []uint32{presetsAttr, schedulesAttr}
	ok := func(attr uint32) tdef.AtomicResponseAttributeStatusEntry {
		return tdef.AtomicResponseAttributeStatusEntry{AttributeId: attr, StatusCode: tdef.StatusSuccess}
	}

	t.Run("applies every pending write when all of them succeed", func(t *testing.T) {
		s := presetsAndSchedulesServer(t)
		atomicAs(t, s, atomicBeginWrite, both)
		stage(t, s, presetsAttr, append(s.Presets(), newUnoccupiedPreset()))
		stage(t, s, schedulesAttr, []Schedule{scheduleAt(360)})
		wantStatuses(t, atomicAs(t, s, atomicCommitWrite, both), tdef.StatusSuccess, ok(presetsAttr), ok(schedulesAttr))
		presets := s.Presets()
		if len(presets) != 2 || len(presets[1].PresetHandle.Value) != 16 || presets[1].BuiltIn.Value {
			t.Errorf("presets %+v, want the added one with a 16-byte handle, not built in", presets)
		}
		if sc := s.Schedules(); len(sc) != 1 || sc[0].Transitions[0].TransitionTime != 360 {
			t.Errorf("schedules %+v, want one at 360", sc)
		}
	})

	t.Run("reports an attribute the peer did not write as SUCCESS, in the CommitWrite request's order", func(t *testing.T) {
		s := presetsAndSchedulesServer(t)
		atomicAs(t, s, atomicBeginWrite, both)
		stage(t, s, schedulesAttr, []Schedule{scheduleAt(360)})
		wantStatuses(t, atomicAs(t, s, atomicCommitWrite, []uint32{schedulesAttr, presetsAttr}), tdef.StatusSuccess, ok(schedulesAttr), ok(presetsAttr))
		if len(s.Presets()) != 1 || len(s.Schedules()) != 1 {
			t.Errorf("presets %d schedules %d, want 1 and 1", len(s.Presets()), len(s.Schedules()))
		}
	})

	t.Run("validates an attribute once the other pending attributes are staged", func(t *testing.T) {
		withPresetSchedules := func(cfg *ThermostatConfig) {
			cool, heat := int16(2600), int16(2000)
			cfg.Presets = append(cfg.Presets, Preset{
				PresetHandle: spec.ValueOf([]byte{0x02}), PresetScenario: tdef.PresetScenarioUnoccupied,
				CoolingSetpoint: &cool, HeatingSetpoint: &heat, BuiltIn: spec.ValueOf(false),
			})
			cfg.ScheduleTypes[0].ScheduleTypeFeatures |= tdef.ScheduleTypeFeaturesSupportsPresets
		}
		usesPreset := Schedule{
			ScheduleHandle: spec.NullOf[[]byte](), SystemMode: tdef.SystemModeHeat, BuiltIn: spec.NullOf[bool](),
			Transitions: []ScheduleTransition{{DayOfWeek: tdef.ScheduleDayOfWeekMonday, TransitionTime: 360, PresetHandle: &[]byte{0x02}}},
		}
		withoutPreset2 := func(s *ThermostatServer) []Preset { return s.Presets()[:1] }

		s := presetsAndSchedulesServer(t, withPresetSchedules)
		atomicAs(t, s, atomicBeginWrite, both)
		stage(t, s, schedulesAttr, []Schedule{usesPreset})
		stage(t, s, presetsAttr, withoutPreset2(s))
		wantStatuses(t, atomicAs(t, s, atomicCommitWrite, both), tdef.StatusFailure,
			tdef.AtomicResponseAttributeStatusEntry{AttributeId: presetsAttr, StatusCode: tdef.StatusInvalidInState}, ok(schedulesAttr))
		if len(s.Presets()) != 2 || len(s.Schedules()) != 0 {
			t.Errorf("presets %d schedules %d after the refusal, want 2 and 0", len(s.Presets()), len(s.Schedules()))
		}

		// Without a staged schedule naming it, the preset goes.
		s = presetsAndSchedulesServer(t, withPresetSchedules)
		atomicAs(t, s, atomicBeginWrite, both)
		stage(t, s, presetsAttr, withoutPreset2(s))
		wantStatuses(t, atomicAs(t, s, atomicCommitWrite, both), tdef.StatusSuccess, ok(presetsAttr), ok(schedulesAttr))
	})

	t.Run("discards a valid Schedules write when the Presets write fails", func(t *testing.T) {
		s := presetsAndSchedulesServer(t)
		atomicAs(t, s, atomicBeginWrite, both)
		// Removing the built-in preset is refused on CommitWrite, not when staged.
		stage(t, s, presetsAttr, []Preset{})
		stage(t, s, schedulesAttr, []Schedule{scheduleAt(360)})
		wantStatuses(t, atomicAs(t, s, atomicCommitWrite, both), tdef.StatusFailure,
			tdef.AtomicResponseAttributeStatusEntry{AttributeId: presetsAttr, StatusCode: tdef.StatusConstraintError}, ok(schedulesAttr))
		if len(s.Presets()) != 1 || len(s.Schedules()) != 0 {
			t.Errorf("presets %d schedules %d, want 1 and 0", len(s.Presets()), len(s.Schedules()))
		}
	})

	t.Run("discards a valid Presets write when the Schedules write fails", func(t *testing.T) {
		s := presetsAndSchedulesServer(t)
		atomicAs(t, s, atomicBeginWrite, both)
		stage(t, s, presetsAttr, append(s.Presets(), newUnoccupiedPreset()))
		// TransitionTime is limited to 1439, the last minute of the day; the
		// write refuses it, so it is staged on the state as matter.js's
		// stageAs does.
		st := s.atomic.forPeer(s.clk.Now(), atomicPeer{fabricIndex: 1, nodeID: 1})
		st.stage(schedulesAttr, []Schedule{scheduleAt(1440)})
		wantStatuses(t, atomicAs(t, s, atomicCommitWrite, both), tdef.StatusFailure,
			ok(presetsAttr), tdef.AtomicResponseAttributeStatusEntry{AttributeId: schedulesAttr, StatusCode: tdef.StatusConstraintError})
		if len(s.Presets()) != 1 || len(s.Schedules()) != 0 {
			t.Errorf("presets %d schedules %d, want 1 and 0", len(s.Presets()), len(s.Schedules()))
		}
		if !bytes.Equal(s.Presets()[0].PresetHandle.Value, []byte{0x01}) {
			t.Errorf("presets %+v, want the stored one", s.Presets())
		}
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/im"
)

// The pending-schedule validation rule by rule, each case one refusal of
// chip's AppendPendingSchedule / ValidateScheduleTransitions
// (ThermostatClusterSchedules.cpp:795-998, :292-446 at 645b267a), called
// directly so every branch is reached without an atomic write around it.
// The wire-level cases are in schedules_write_chip_test.go.

func testScheduleRules() scheduleRules {
	presetA := []byte{1}
	return scheduleRules{
		features: ThermostatFeatureHEAT | ThermostatFeatureCOOL | ThermostatFeatureAUTO | ThermostatFeaturePRES,
		types: []ScheduleType{
			{SystemMode: tdef.SystemModeHeat, NumberOfSchedules: 2, ScheduleTypeFeatures: tdef.ScheduleTypeFeaturesSupportsSetpoints},
			{SystemMode: tdef.SystemModeAuto, NumberOfSchedules: 1, ScheduleTypeFeatures: tdef.ScheduleTypeFeaturesSupportsPresets | tdef.ScheduleTypeFeaturesSupportsNames | tdef.ScheduleTypeFeaturesSupportsOff},
			{SystemMode: tdef.SystemModeCool, NumberOfSchedules: 0},
		},
		maxSchedules: 3,
		maxTotal:     6,
		perDay:       ptr[uint8](2),
		st: setpointState{
			heat: true, cool: true, auto: true, deadband: 250,
			minHeat: 700, maxHeat: 3000, minCool: 1600, maxCool: 3200,
		},
		presetPresent: func(h []byte) bool { return bytes.Equal(h, presetA) },
	}
}

func ptr[T any](v T) *T { return &v }

// nullHandle is a new schedule's handle: null, not an empty string.
var nullHandle = spec.Nullable[[]byte]{Null: true}

func heatTransition(day tdef.ScheduleDayOfWeekBitmap, at uint16, heat int16) ScheduleTransition {
	return ScheduleTransition{DayOfWeek: day, TransitionTime: at, HeatingSetpoint: ptr(heat)}
}

func heatSchedule(transitions ...ScheduleTransition) Schedule {
	return Schedule{ScheduleHandle: nullHandle, SystemMode: tdef.SystemModeHeat, Transitions: transitions}
}

func TestScheduleRulesAppendPending(t *testing.T) {
	stored := []Schedule{
		{
			ScheduleHandle: spec.ValueOf([]byte{0xAA}), SystemMode: tdef.SystemModeHeat, BuiltIn: spec.ValueOf(true),
			Transitions: []ScheduleTransition{heatTransition(tdef.ScheduleDayOfWeekMonday, 0, 2000)},
		},
		{
			ScheduleHandle: spec.ValueOf([]byte{0xBB}), SystemMode: tdef.SystemModeHeat, BuiltIn: spec.Nullable[bool]{Null: true},
			Transitions: []ScheduleTransition{heatTransition(tdef.ScheduleDayOfWeekMonday, 0, 2000)},
		},
	}
	long := strings.Repeat("x", 17)
	mon := tdef.ScheduleDayOfWeekMonday
	cases := []struct {
		name    string
		pending []Schedule
		sc      Schedule
		status  im.StatusCode
		reason  string
	}{
		{"handle too long", nil, Schedule{ScheduleHandle: spec.ValueOf([]byte(long))}, im.StatusConstraintError, "handle longer"},
		{"name too long", nil, Schedule{ScheduleHandle: nullHandle, Name: ptr(strings.Repeat("n", 65))}, im.StatusConstraintError, "name longer"},
		{"preset handle too long", nil, Schedule{ScheduleHandle: nullHandle, PresetHandle: ptr([]byte(long))}, im.StatusConstraintError, "preset handle longer"},
		{"transition preset handle too long", nil, Schedule{ScheduleHandle: nullHandle, Transitions: []ScheduleTransition{{PresetHandle: ptr([]byte(long))}}}, im.StatusConstraintError, "transition preset handle longer"},
		{"system mode off", nil, Schedule{ScheduleHandle: nullHandle, SystemMode: tdef.SystemModeOff}, im.StatusConstraintError, "not Auto, Cool or Heat"},
		{"new schedule built in", nil, Schedule{ScheduleHandle: nullHandle, SystemMode: tdef.SystemModeHeat, BuiltIn: spec.ValueOf(true)}, im.StatusConstraintError, "cannot be built in"},
		{"unknown handle", nil, Schedule{ScheduleHandle: spec.ValueOf([]byte{0xCC}), SystemMode: tdef.SystemModeHeat}, im.StatusNotFound, "names no schedule"},
		{"handle twice", []Schedule{{ScheduleHandle: spec.ValueOf([]byte{0xAA})}}, Schedule{ScheduleHandle: spec.ValueOf([]byte{0xAA}), SystemMode: tdef.SystemModeHeat}, im.StatusConstraintError, "already in the list"},
		{"stored without BuiltIn", nil, Schedule{ScheduleHandle: spec.ValueOf([]byte{0xBB}), SystemMode: tdef.SystemModeHeat}, im.StatusInvalidInState, "has no BuiltIn"},
		{"BuiltIn differs", nil, Schedule{ScheduleHandle: spec.ValueOf([]byte{0xAA}), SystemMode: tdef.SystemModeHeat, BuiltIn: spec.ValueOf(false)}, im.StatusConstraintError, "BuiltIn differs"},
		{"no type for Cool", nil, Schedule{ScheduleHandle: nullHandle, SystemMode: tdef.SystemModeCool}, im.StatusConstraintError, "no schedule type"},
		{"name unsupported", nil, Schedule{ScheduleHandle: nullHandle, SystemMode: tdef.SystemModeHeat, Name: ptr("x")}, im.StatusConstraintError, "does not support names"},
		{"preset unsupported", nil, Schedule{ScheduleHandle: nullHandle, SystemMode: tdef.SystemModeHeat, PresetHandle: ptr([]byte{1})}, im.StatusConstraintError, "does not support presets"},
		{"preset unknown", nil, Schedule{ScheduleHandle: nullHandle, SystemMode: tdef.SystemModeAuto, PresetHandle: ptr([]byte{9})}, im.StatusConstraintError, "names no preset"},
		{"no transitions", nil, heatSchedule(), im.StatusConstraintError, "no transitions"},
		{"day Away", nil, heatSchedule(heatTransition(tdef.ScheduleDayOfWeekAway, 0, 2000)), im.StatusConstraintError, "DayOfWeek"},
		{"time beyond 1439", nil, heatSchedule(heatTransition(mon, 1440, 2000)), im.StatusConstraintError, "beyond 1439"},
		{"repeated time on a day", nil, heatSchedule(heatTransition(mon, 0, 2000), heatTransition(mon|tdef.ScheduleDayOfWeekTuesday, 0, 2000)), im.StatusConstraintError, "repeats transition"},
		{"transition preset unsupported", nil, heatSchedule(ScheduleTransition{DayOfWeek: mon, PresetHandle: ptr([]byte{1})}), im.StatusConstraintError, "does not support presets"},
		{"transition preset unknown", nil, Schedule{ScheduleHandle: nullHandle, SystemMode: tdef.SystemModeAuto, Transitions: []ScheduleTransition{{DayOfWeek: mon, PresetHandle: ptr([]byte{9})}}}, im.StatusConstraintError, "names no preset"},
		{"preset with setpoints", nil, Schedule{ScheduleHandle: nullHandle, SystemMode: tdef.SystemModeAuto, Transitions: []ScheduleTransition{{DayOfWeek: mon, PresetHandle: ptr([]byte{1}), HeatingSetpoint: ptr[int16](2000)}}}, im.StatusConstraintError, "excludes SystemMode"},
		{"setpoints unsupported", nil, Schedule{ScheduleHandle: nullHandle, SystemMode: tdef.SystemModeAuto, Transitions: []ScheduleTransition{{DayOfWeek: mon, HeatingSetpoint: ptr[int16](2000)}}}, im.StatusConstraintError, "does not support setpoints"},
		{"transition Off unsupported", nil, heatSchedule(ScheduleTransition{DayOfWeek: mon, SystemMode: ptr(tdef.SystemModeOff)}), im.StatusConstraintError, "does not support Off"},
		{"transition mode unknown", nil, heatSchedule(ScheduleTransition{DayOfWeek: mon, SystemMode: ptr(tdef.SystemModeDry)}), im.StatusConstraintError, "not Off, Heat, Cool or Auto"},
		{"missing heating setpoint", nil, heatSchedule(ScheduleTransition{DayOfWeek: mon}), im.StatusConstraintError, "needs a HeatingSetpoint"},
		{"missing cooling setpoint", nil, heatSchedule(ScheduleTransition{DayOfWeek: mon, SystemMode: ptr(tdef.SystemModeCool)}), im.StatusConstraintError, "needs a CoolingSetpoint"},
		{"heating outside limits", nil, heatSchedule(heatTransition(mon, 0, 500)), im.StatusConstraintError, "HeatingSetpoint 500 is outside"},
		{"cooling on a heat transition", nil, heatSchedule(ScheduleTransition{DayOfWeek: mon, HeatingSetpoint: ptr[int16](2000), CoolingSetpoint: ptr[int16](2600)}), im.StatusConstraintError, "CoolingSetpoint 2600 is outside"},
		{"closer than the deadband", nil, heatSchedule(ScheduleTransition{DayOfWeek: mon, SystemMode: ptr(tdef.SystemModeAuto), HeatingSetpoint: ptr[int16](2000), CoolingSetpoint: ptr[int16](2100)}), im.StatusConstraintError, "closer than the deadband"},
		{"too many a day", nil, heatSchedule(heatTransition(mon, 0, 2000), heatTransition(mon, 1, 2000), heatTransition(mon, 2, 2000)), im.StatusResourceExhausted, "NumberOfScheduleTransitionPerDay"},
		{"too many schedules", []Schedule{heatSchedule(), heatSchedule(), heatSchedule()}, heatSchedule(heatTransition(mon, 0, 2000)), im.StatusResourceExhausted, "exceed NumberOfSchedules"},
		{"too many of a type", []Schedule{heatSchedule(), heatSchedule()}, heatSchedule(heatTransition(mon, 0, 2000)), im.StatusResourceExhausted, "exceed its type"},
		{"too many transitions", []Schedule{heatSchedule(heatTransition(mon, 0, 2000), heatTransition(mon, 1, 2000)), {SystemMode: tdef.SystemModeAuto, Transitions: make([]ScheduleTransition, 3)}}, heatSchedule(heatTransition(mon, 0, 2000), heatTransition(mon, 1, 2000)), im.StatusResourceExhausted, "exceed NumberOfScheduleTransitions"},
	}
	r := testScheduleRules()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.appendPending(stored, c.pending, c.sc)
			if err == nil {
				t.Fatalf("appendPending accepted the schedule; want %v (%s)", c.status, c.reason)
			}
			var se *spec.StatusError
			if !errors.As(err, &se) || se.Status != c.status {
				t.Fatalf("appendPending: %v; want status %v", err, c.status)
			}
			if !strings.Contains(err.Error(), c.reason) {
				t.Fatalf("appendPending: %v; want the reason %q", err, c.reason)
			}
		})
	}

	t.Run("accepted with BuiltIn inherited", func(t *testing.T) {
		sc := Schedule{
			ScheduleHandle: spec.ValueOf([]byte{0xAA}), SystemMode: tdef.SystemModeHeat, BuiltIn: spec.Nullable[bool]{Null: true},
			Transitions: []ScheduleTransition{heatTransition(mon, 0, 2000)},
		}
		got, err := r.appendPending(stored, nil, sc)
		if err != nil {
			t.Fatalf("appendPending: %v", err)
		}
		if got.BuiltIn.Null || !got.BuiltIn.Value {
			t.Fatalf("BuiltIn %+v; want true from the stored schedule", got.BuiltIn)
		}
	})
	t.Run("no per-day limit", func(t *testing.T) {
		r := testScheduleRules()
		r.perDay = nil
		sc := heatSchedule(heatTransition(mon, 0, 2000), heatTransition(mon, 1, 2000), heatTransition(mon, 2, 2000))
		if _, err := r.appendPending(stored, nil, sc); err != nil {
			t.Fatalf("appendPending without a per-day limit: %v", err)
		}
	})
}

func TestScheduleHelpersInternal(t *testing.T) {
	// cloneSchedules copies every pointer field and nil stays nil.
	if cloneSchedules(nil) != nil {
		t.Fatal("cloneSchedules(nil) is not nil")
	}
	h := []byte{1}
	in := []Schedule{{ScheduleHandle: spec.ValueOf([]byte{2}), Name: ptr("n"), PresetHandle: &h, Transitions: []ScheduleTransition{
		{PresetHandle: &h, SystemMode: ptr(tdef.SystemModeHeat), CoolingSetpoint: ptr[int16](1), HeatingSetpoint: ptr[int16](2)},
	}}}
	out := cloneSchedules(in)
	if &out[0].Transitions[0] == &in[0].Transitions[0] || out[0].PresetHandle == in[0].PresetHandle ||
		out[0].Transitions[0].PresetHandle == in[0].Transitions[0].PresetHandle || out[0].Name == in[0].Name ||
		out[0].Transitions[0].SystemMode == in[0].Transitions[0].SystemMode ||
		out[0].Transitions[0].CoolingSetpoint == in[0].Transitions[0].CoolingSetpoint ||
		out[0].Transitions[0].HeatingSetpoint == in[0].Transitions[0].HeatingSetpoint {
		t.Fatal("cloneSchedules shares a pointer with its input")
	}
	if !schedulesTLVEqual(in, out) {
		t.Fatal("the clone does not encode alike")
	}

	// schedulesValue and scheduleRequest refuse a foreign type.
	if _, err := schedulesValue(42); err == nil {
		t.Fatal("schedulesValue(42) accepted")
	}
	if _, err := scheduleRequest(42); err == nil {
		t.Fatal("scheduleRequest(42) accepted")
	}
	if _, err := scheduleRequest((*tdef.SetActiveScheduleRequestRequest)(nil)); err == nil {
		t.Fatal("scheduleRequest(nil pointer) accepted")
	}
	if _, err := scheduleRequest(&tdef.SetActiveScheduleRequestRequest{ScheduleHandle: h}); err != nil {
		t.Fatalf("scheduleRequest(pointer): %v", err)
	}

	// atomicAttributeName names a known attribute and hex-prints an unknown.
	if got := atomicAttributeName(tdef.AttrSchedules); got != "Schedules" {
		t.Fatalf("atomicAttributeName(Schedules) = %q", got)
	}
	if got := atomicAttributeName(0xFFF0); got != "0xFFF0" {
		t.Fatalf("atomicAttributeName(unknown) = %q", got)
	}
	// equalDuration over null and values.
	if !equalDuration(nil, nil) || equalDuration(nil, ptr[uint16](1)) || !equalDuration(ptr[uint16](1), ptr[uint16](1)) || equalDuration(ptr[uint16](1), ptr[uint16](2)) {
		t.Fatal("equalDuration")
	}
}

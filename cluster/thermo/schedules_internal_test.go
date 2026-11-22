// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"errors"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/internal/deepcopy"

	"thingfabric.dev/go-fabric/cluster/spec"
	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/im"
)

func ptr[T any](v T) *T { return &v }

// TestCheckScheduleSizes: the schema's field sizes
// (thermostat-cluster.element.ts:634-657 at b25ed960), held by the rules
// for the host's SetSchedules as the schema holds a controller's write.
func TestCheckScheduleSizes(t *testing.T) {
	long := []byte(strings.Repeat("x", 17))
	null := spec.NullOf[[]byte]()
	for _, c := range []struct {
		name   string
		sc     Schedule
		reason string
	}{
		{"handle too long", Schedule{ScheduleHandle: spec.ValueOf(long)}, "handle longer"},
		{"name too long", Schedule{ScheduleHandle: null, Name: ptr(strings.Repeat("n", 65))}, "name longer"},
		{"preset handle too long", Schedule{ScheduleHandle: null, PresetHandle: &long}, "preset handle longer"},
		{"transition preset handle too long", Schedule{ScheduleHandle: null, Transitions: []ScheduleTransition{{PresetHandle: &long}}}, "transition preset handle longer"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := checkScheduleSizes(c.sc)
			var se *spec.StatusError
			if !errors.As(err, &se) || se.Status != im.StatusConstraintError || !strings.Contains(err.Error(), c.reason) {
				t.Fatalf("checkScheduleSizes: %v; want CONSTRAINT_ERROR, %q", err, c.reason)
			}
		})
	}
	if err := checkScheduleSizes(Schedule{ScheduleHandle: null}); err != nil {
		t.Fatalf("checkScheduleSizes of a plain schedule: %v", err)
	}
}

func TestScheduleHelpersInternal(t *testing.T) {
	// deepcopy.Each copies every pointer field and nil stays nil.
	if deepcopy.Each[Schedule](nil) != nil {
		t.Fatal("deepcopy.Each(nil) is not nil")
	}
	h := []byte{1}
	in := []Schedule{{ScheduleHandle: spec.ValueOf([]byte{2}), Name: ptr("n"), PresetHandle: &h, Transitions: []ScheduleTransition{
		{PresetHandle: &h, SystemMode: ptr(tdef.SystemModeHeat), CoolingSetpoint: ptr[int16](1), HeatingSetpoint: ptr[int16](2)},
	}}}
	out := deepcopy.Each(in)
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

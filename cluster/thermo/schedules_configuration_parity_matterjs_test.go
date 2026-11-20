// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/thermostat/SchedulesConfigurationTest.ts
// at b25ed960: the schedules and ScheduleTypes a thermostat is configured
// with, and the first writes of a thermostat configured with none.
//
// matter.js refuses a configuration when the node starts (an
// ImplementationError, "Configured schedules are invalid"); the Go server
// refuses it in thermo.New with thermo.ErrSchedules. A local write is
// SetSchedules, a remote one the write of a fabrictest pair.
//
// Not ported here: "refuses to start" without the thermostat-msch forward
// feature — the Go gate is the extension BD-chip-Thermostat-Schedules,
// which the device layer applies (device/surface_ahead_test.go,
// examples/reference-bridge TestDemoThermostatSchedulesAreGated), another
// mechanism with the same effect. js-runtime-only: "serves schedules a
// behavior type supplies as its own default", "leaves the schedules a
// behavior type supplies untouched" (a behavior type's defaults) and
// "seeds from configuration, not from schedules a device stored before
// the accessor existed" (a migration of matter.js's state field). Planned:
// "starts with stored schedules that no longer meet the configured
// limits" and "starts with more stored schedules and transitions than the
// limits allow and raises the limits" — the Go server keeps the schedules
// in memory or behind the host's SchedulePort; persisting them is a unit
// of its own.
package thermo_test

import (
	"errors"
	"strings"
	"testing"

	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/im"
)

// startupRefusal is SchedulesConfigurationTest.ts:30-43: the reason
// thermo.New refuses cfg, "" when it accepts it.
func startupRefusal(t *testing.T, cfg thermo.ThermostatConfig) string {
	t.Helper()
	_, err := thermo.New(cfg)
	if err == nil {
		return ""
	}
	if !errors.Is(err, thermo.ErrSchedules) {
		t.Fatalf("refusal %v is not ErrSchedules", err)
	}
	return err.Error()
}

func TestParityMatterJS_SchedulesConfigurationTest(t *testing.T) {
	t.Parallel()
	t.Run("refuses to start with a configured schedule the write rules refuse", func(t *testing.T) {
		t.Parallel()
		invalid := newSchedule(scTransitions(newScheduleTransition(trMode(tdef.SystemModeAuto))))
		if got := startupRefusal(t, schedulesConfig(5, []thermo.Schedule{invalid})); !strings.Contains(got, "must be omitted when it equals the schedule's SystemMode") {
			t.Errorf("refusal %q, want the transition SystemMode rule", got)
		}
	})

	t.Run("refuses to start with a configured transition closer than the configured deadband", func(t *testing.T) {
		t.Parallel()
		cfg := schedulesConfig(5, []thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trHeat(2000), trCool(2200))))})
		if got := startupRefusal(t, cfg); !strings.Contains(got, "deadband") {
			t.Errorf("refusal %q, want the deadband rule", got)
		}
	})

	t.Run("refuses to start with ScheduleTypes that", func(t *testing.T) {
		auto := func(feats tdef.ScheduleTypeFeaturesBitmap) thermo.ScheduleType { return autoType(5, feats) }
		for _, c := range []struct {
			name   string
			types  []thermo.ScheduleType
			reason string
		}{
			{"name a systemMode twice", []thermo.ScheduleType{auto(tdef.ScheduleTypeFeaturesSupportsSetpoints), auto(tdef.ScheduleTypeFeaturesSupportsPresets)}, "more than once"},
			{"name a systemMode other than Auto, Heat or Cool", []thermo.ScheduleType{{SystemMode: tdef.SystemModeOff, NumberOfSchedules: 5, ScheduleTypeFeatures: tdef.ScheduleTypeFeaturesSupportsSetpoints}}, "may only name Auto, Heat or Cool"},
			{"support neither presets nor setpoints", []thermo.ScheduleType{auto(tdef.ScheduleTypeFeaturesSupportsNames)}, "supports neither presets nor setpoints"},
		} {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				if got := startupRefusal(t, withScheduleTypes(schedulesConfig(5, nil), c.types...)); !strings.Contains(got, c.reason) {
					t.Errorf("refusal %q, want %q", got, c.reason)
				}
			})
		}
	})

	t.Run("starts with configured schedules the write rules accept", func(t *testing.T) {
		t.Parallel()
		if got := startupRefusal(t, schedulesConfig(5, []thermo.Schedule{newSchedule()})); got != "" {
			t.Errorf("refused: %s", got)
		}
	})

	t.Run("validates a local write when the application configured no schedules", func(t *testing.T) {
		t.Parallel()
		// matter.js first refuses a handle the device never issued
		// (NOT_FOUND), a lifecycle rule the host's replace is not held to
		// (BD-Thermostat-HostSchedules); the port refuses a content rule
		// instead — a transition SystemMode equal to the schedule's.
		srv := newSchedulesServer(t, schedulesConfig(5, nil))
		wantStatusCode(t, srv.SetSchedules([]thermo.Schedule{newSchedule(scTransitions(newScheduleTransition(trMode(tdef.SystemModeAuto))))}), im.StatusConstraintError, "")
		if err := srv.SetSchedules([]thermo.Schedule{newSchedule()}); err != nil {
			t.Fatal(err)
		}
		if got := srv.Schedules(); len(got) != 1 || len(got[0].ScheduleHandle.Value) != 16 {
			t.Errorf("schedules %+v, want one with a 16-byte handle", got)
		}
	})
}

// TestParityMatterJS_SchedulesConfigurationTest_Remote is the case of
// SchedulesConfigurationTest.ts that runs over the wire, on fabrictest
// (one after the other, not in parallel).
func TestParityMatterJS_SchedulesConfigurationTest_Remote(t *testing.T) {
	t.Run("declines a remote write outside an atomic write when the application configured no schedules", func(t *testing.T) {
		p := schedulesThermostatPair(t, schedulesConfig(5, nil))
		// Schedules carries quality T, so a client may only write it inside
		// an atomic write.
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule()}), im.StatusInvalidInState, im.StatusInvalidInState)
		if got := p.srv.Schedules(); len(got) != 0 {
			t.Fatalf("schedules %+v, want none", got)
		}

		// The same write inside an atomic write is accepted, so the refusal
		// above is the atomicity rule rather than the endpoint failing to
		// serve the attribute at all.
		p.atomic(t, p.pair, beginWrite, schedulesAttribute)
		statusesAre(t, p.writeSchedulesUnchecked(t, p.pair, []thermo.Schedule{newSchedule()}), im.StatusSuccess, im.StatusSuccess)
		p.atomic(t, p.pair, commitWrite, schedulesAttribute)
		if got := p.srv.Schedules(); len(got) != 1 || len(got[0].ScheduleHandle.Value) != 16 {
			t.Errorf("schedules %+v, want one with a 16-byte handle", got)
		}
	})
}

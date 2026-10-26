// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"fmt"
	"testing"
)

// clusterThermostat is the Thermostat cluster's id.
const clusterThermostat = 0x0201

// testThermostatHold is the capability leg for the week-program
// attributes the reference thermostat serves (issue #273, brief CO):
// chip-tool reads the hold, its duration and ThermostatRunningState —
// the duration null, as matter.js sets temperatureSetpointHoldDuration
// (ThermostatServer.ts:365-369 at 862ad75f) —
// writes the hold on and reads it back, and writes it off again so the
// legs after this one see the daemon as it started.
func testThermostatHold(t *testing.T, f *fixture) {
	t.Helper()
	ep, ok := f.thermostatEndpoint()
	if !ok {
		t.Fatal("no endpoint of the Thermostat device type serves the Thermostat cluster")
	}
	readInt := func(attr string) int64 {
		t.Helper()
		v := f.read(t, "thermostat", attr, ep)
		n, isInt := asInt(v)
		if !isInt {
			t.Fatalf("thermostat %s on endpoint %d = %v, want a number", attr, ep, v)
		}
		return n
	}
	if got := readInt("temperature-setpoint-hold"); got != 0 {
		t.Errorf("TemperatureSetpointHold = %d, want 0 (SetpointHoldOff)", got)
	}
	if v := f.read(t, "thermostat", "temperature-setpoint-hold-duration", ep); v != nil {
		t.Errorf("TemperatureSetpointHoldDuration = %v, want null", v)
	}
	if got := readInt("thermostat-running-state"); got != 0 {
		t.Errorf("ThermostatRunningState = %d, want 0 (idle)", got)
	}
	for _, hold := range []int64{1, 0} {
		f.mustRun(t, fmt.Sprintf("thermostat write temperature-setpoint-hold %d %s %d", hold, f.node(), ep))
		if got := readInt("temperature-setpoint-hold"); got != hold {
			t.Errorf("TemperatureSetpointHold = %d after writing %d", got, hold)
		}
	}
	testThermostatSchedules(t, f, ep, readInt)
}

// weekdaysHex is the reference thermostat's built-in week program's handle
// ("Weekdays", examples/reference-bridge/fleet_climate.go) as chip-tool
// spells an octet string.
const weekdaysHex = "hex:5765656b64617973"

// testThermostatSchedules is the MatterScheduleConfiguration part of the
// thermostat leg (the daemon runs with --thermostat-schedules): chip-tool
// reads ScheduleTypes, the counts, Schedules and ActiveScheduleHandle,
// switches the active schedule with SetActiveScheduleRequest, and writes
// a second schedule inside an atomic write (AtomicRequest BeginWrite, the
// write, CommitWrite) that the server commits.
func testThermostatSchedules(t *testing.T, f *fixture, ep uint16, readInt func(string) int64) {
	t.Helper()
	list := func(attr string) []any {
		t.Helper()
		v, ok := f.read(t, "thermostat", attr, ep).([]any)
		if !ok {
			t.Fatalf("thermostat %s on endpoint %d is not a list", attr, ep)
		}
		return v
	}
	if got := len(list("schedule-types")); got != 1 {
		t.Errorf("ScheduleTypes has %d entries, want 1", got)
	}
	if got := readInt("number-of-schedules"); got != 2 {
		t.Errorf("NumberOfSchedules = %d, want 2", got)
	}
	if got := len(list("schedules")); got != 1 {
		t.Errorf("Schedules has %d entries, want the built-in week program", got)
	}
	if v := f.read(t, "thermostat", "active-schedule-handle", ep); v == nil {
		t.Error("ActiveScheduleHandle is null, want the week program")
	}
	f.mustRun(t, fmt.Sprintf("thermostat set-active-schedule-request %s %s %d", weekdaysHex, f.node(), ep))

	schedules := `'[{"scheduleHandle":"` + weekdaysHex + `","systemMode":4,"name":"Weekdays",` +
		`"transitions":[{"dayOfWeek":62,"transitionTime":360,"heatingSetpoint":2100},{"dayOfWeek":62,"transitionTime":1320,"heatingSetpoint":1800}],"builtIn":true},` +
		`{"scheduleHandle":null,"systemMode":4,"name":"Weekend",` +
		`"transitions":[{"dayOfWeek":65,"transitionTime":480,"heatingSetpoint":2000}],"builtIn":null}]'`
	f.mustRun(t, fmt.Sprintf("thermostat atomic-request 0 '[81]' %s %d --Timeout 5000", f.node(), ep))
	f.mustRun(t, fmt.Sprintf("thermostat write schedules %s %s %d", schedules, f.node(), ep))
	f.mustRun(t, fmt.Sprintf("thermostat atomic-request 1 '[81]' %s %d", f.node(), ep))
	if got := len(list("schedules")); got != 2 {
		t.Errorf("Schedules has %d entries after the committed atomic write, want 2", got)
	}
}

// thermostatEndpoint is the first endpoint of the Thermostat device type
// that serves the Thermostat cluster.
func (f *fixture) thermostatEndpoint() (uint16, bool) {
	for _, id := range f.sortedEndpoints() {
		ep := f.topology[id]
		if hasDeviceType(ep, dtThermostat) && ep.has(clusterThermostat) {
			return id, true
		}
	}
	return 0, false
}

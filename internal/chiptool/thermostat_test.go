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
// attributes the reference thermostat serves (issue #273, brief CO PR 1):
// chip-tool reads the hold, its duration and ThermostatRunningState —
// the duration null, as matter.js sets temperatureSetpointHoldDuration
// (ThermostatServer.ts:365-369 at 862ad75f) —
// writes the hold on and reads it back, and writes it off again so the
// legs after this one see the daemon as it started.
func testThermostatHold(t *testing.T, f *fixture) {
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

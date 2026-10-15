// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import "testing"

// TestCompanionsMovedNamesTheWrittenSetpoint: the re-run after the Sink
// that moves the written setpoint itself is reported with the companions
// (matter.thermostat.sink_companions_moved), and a companion the host
// echoed is not. The re-run cannot move the written setpoint through the
// public API (TestSinkHearsTheValueTheServerStores says why), so the check
// is held here on the function.
func TestCompanionsMovedNamesTheWrittenSetpoint(t *testing.T) {
	t.Parallel()
	st := setpointState{occupHeat: 2500, occupCool: 2700}
	next := setpointState{occupHeat: 2400, occupCool: 2700}
	saw := []SetpointChange{{AttrID: thermoAttrOccupiedCoolingSetpoint, Value: 2700}}
	got := companionsMoved(thermoAttrOccupiedHeatingSetpoint, st, next, saw, nil)
	if len(got) != 1 || got[0] != (SetpointChange{AttrID: thermoAttrOccupiedHeatingSetpoint, Value: 2400}) {
		t.Errorf("moved %v, want the written heating setpoint at 2400 alone", got)
	}
	if got := companionsMoved(thermoAttrOccupiedHeatingSetpoint, st, st, saw, nil); len(got) != 0 {
		t.Errorf("moved %v on an unchanged state, want none", got)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo

import (
	"context"
	"errors"
	"testing"

	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/im"
)

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

// TestSetpointRaiseLowerDeadbandRefusals: when the companion setpoint
// cannot be clamped into its limits, Heat and Cool answer INVALID_COMMAND
// and store nothing — matter.js ThermostatServer.ts:241-243, :263-265
// (0ceb202d). The AUTO limit invariants make that state unreachable
// through the server's API, so the limits are set here directly.
func TestSetpointRaiseLowerDeadbandRefusals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		mode  uint8
		amt   int8
		spoil func(*ThermostatServer)
	}{
		{"Heat: cooling companion above MaxCool", 0, 50, func(s *ThermostatServer) { s.maxCool = 2600 }},
		{"Cool: heating companion below MinHeat", 1, -20, func(s *ThermostatServer) { s.occupHeat, s.minHeat = 2300, 2300 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := NewThermostatServer(DefaultThermostatConfig())
			c.spoil(s)
			heat, cool := s.occupHeat, s.occupCool
			_, err := s.MatterInvoke(context.Background(), tdef.CmdSetpointRaiseLower,
				tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeEnum(c.mode), Amount: c.amt})
			var sc im.StatusCodeError
			if !errors.As(err, &sc) || sc.MatterStatusCode() != im.StatusInvalidCommand {
				t.Fatalf("err %v, want INVALID_COMMAND", err)
			}
			if s.occupHeat != heat || s.occupCool != cool {
				t.Errorf("setpoints moved to %d / %d", s.occupHeat, s.occupCool)
			}
		})
	}
}

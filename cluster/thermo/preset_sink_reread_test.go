// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	tdef "thingfabric.dev/go-fabric/cluster/spec/thermostat"
	"thingfabric.dev/go-fabric/cluster/thermo"
)

// TestPresetSinkStoresOnTheReReadState (R4-conc-4251): SetActivePresetRequest
// stores on the state as it is once the Sink consented, as a setpoint write
// does. The preset (heat 21.00 °C, cool 26.00 °C) on the 20.00 / 26.00 °C
// thermostat moves only the heating setpoint, and the Sink stores a cooling
// setpoint through the inbound setter while it runs.
//
//   - The host stores cool 29.00 °C: the command did not move the cooling
//     setpoint, so it keeps the host's value; nothing to log.
//   - The host stores cool 22.00 °C: the reconcile on the re-read state
//     pushes it to 23.50 °C under the 2.5 °C deadband — a companion the
//     Sink was not told — and the server logs
//     matter.thermostat.sink_companions_moved.
//
// Before the fix the command stored its pre-Sink snapshot, and the cooling
// setpoint fell back to 26.00 °C in both cases.
//
// Not parallel: it redirects slog.Default.
func TestPresetSinkStoresOnTheReReadState(t *testing.T) {
	for _, tc := range []struct {
		name     string
		hostCool int16
		wantCool int16
		wantLog  bool
	}{
		{"the unmoved setpoint keeps the host's value", 2900, 2900, false},
		{"the reconcile on the re-read state moves it", 2200, 2350, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			defer slog.SetDefault(prev)

			sink := &echoSink{}
			cfg := thermostatConfig(5, []thermo.Preset{setpointPreset(1, 2100, 2600)})
			cfg.Sink = sink
			srv := mustNew(t, cfg)
			var told []thermo.SetpointChange
			sink.setpoint = func(attrID uint32, value int16, companions []thermo.SetpointChange) {
				told = append([]thermo.SetpointChange{{AttrID: attrID, Value: value}}, companions...)
				srv.SetOccupiedCoolingSetpoint(tc.hostCool)
			}
			within(t, "SetActivePresetRequest", func() {
				if err := activate(t, srv, []byte{1}); err != nil {
					t.Errorf("SetActivePresetRequest: %v", err)
				}
			})
			if want := (thermo.SetpointChange{AttrID: tdef.AttrOccupiedHeatingSetpoint, Value: 2100}); len(told) != 1 || told[0] != want {
				t.Errorf("Sink told %v, want only %v", told, want)
			}
			if got := readSetpoint(t, srv, tdef.AttrOccupiedHeatingSetpoint, "heat"); got != 2100 {
				t.Errorf("OccupiedHeatingSetpoint = %d, want 2100", got)
			}
			if got := readSetpoint(t, srv, tdef.AttrOccupiedCoolingSetpoint, "cool"); got != tc.wantCool {
				t.Errorf("OccupiedCoolingSetpoint = %d, want %d", got, tc.wantCool)
			}
			if logged := strings.Contains(buf.String(), "matter.thermostat.sink_companions_moved"); logged != tc.wantLog {
				t.Errorf("sink_companions_moved logged = %t, want %t; log:\n%s", logged, tc.wantLog, buf.String())
			}
		})
	}
}

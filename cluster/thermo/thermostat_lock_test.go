// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
)

// echoSink is a Sink whose calls run the given functions; a nil function
// consents.
type echoSink struct {
	mode     func(mode uint8)
	setpoint func(attrID uint32, value int16, companions []thermo.SetpointChange)
}

func (e *echoSink) SetSystemMode(_ context.Context, mode uint8) error {
	if e.mode != nil {
		e.mode(mode)
	}
	return nil
}

func (e *echoSink) SetSetpoint(_ context.Context, attrID uint32, value int16, companions []thermo.SetpointChange) error {
	if e.setpoint != nil {
		e.setpoint(attrID, value, companions)
	}
	return nil
}

// echoSetpoint stores value through the inbound setter for attrID.
func echoSetpoint(srv *thermo.ThermostatServer, attrID uint32, value int16) {
	if attrID == tdef.AttrOccupiedHeatingSetpoint {
		srv.SetOccupiedHeatingSetpoint(value)
	} else {
		srv.SetOccupiedCoolingSetpoint(value)
	}
}

// lockDeadline bounds every call that would block forever if the Sink ran
// under a lock an inbound setter takes (issue #63).
const lockDeadline = time.Second

// within runs fn on its own goroutine and fails the test when it has not
// returned within lockDeadline. It never sleeps: the deadline is a timer.
func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(lockDeadline):
		t.Fatalf("%s did not return within %v: the Sink runs under a lock the inbound setters take", what, lockDeadline)
	}
}

// TestSinkSynchronousSetpointEcho: a Sink that confirms a setpoint write
// through SetOccupiedHeatingSetpoint before it returns lets the write
// complete; the setpoint and its companion are stored as the Sink saw
// them.
func TestSinkSynchronousSetpointEcho(t *testing.T) {
	t.Parallel()
	sink := &echoSink{}
	srv := newSinkHeatCool(sink)
	sink.setpoint = func(attrID uint32, value int16, companions []thermo.SetpointChange) {
		echoSetpoint(srv, attrID, value)
		for _, c := range companions {
			echoSetpoint(srv, c.AttrID, c.Value)
		}
	}
	within(t, "the setpoint write", func() {
		if err := srv.MatterWrite(context.Background(), tdef.AttrOccupiedHeatingSetpoint, int16(2500)); err != nil {
			t.Errorf("write: %v", err)
		}
	})
	if got := readSetpoint(t, srv, tdef.AttrOccupiedHeatingSetpoint, "heat"); got != 2500 {
		t.Errorf("OccupiedHeatingSetpoint = %d, want 2500", got)
	}
	if got := readSetpoint(t, srv, tdef.AttrOccupiedCoolingSetpoint, "cool"); got != 2700 {
		t.Errorf("OccupiedCoolingSetpoint = %d, want 2700 (the companion)", got)
	}
}

// TestSinkSynchronousSystemModeEcho: a Sink that confirms a SystemMode
// write through SetSystemMode before it returns lets the write complete.
func TestSinkSynchronousSystemModeEcho(t *testing.T) {
	t.Parallel()
	sink := &echoSink{}
	srv := newSinkHeatCool(sink)
	sink.mode = srv.SetSystemMode
	within(t, "the SystemMode write", func() {
		if err := srv.MatterWrite(context.Background(), tdef.AttrSystemMode, uint8(tdef.SystemModeHeat)); err != nil {
			t.Errorf("write: %v", err)
		}
	})
	if v, _ := srv.MatterRead(tdef.AttrSystemMode); v != uint8(tdef.SystemModeHeat) {
		t.Errorf("SystemMode = %v, want Heat", v)
	}
}

// TestSinkSynchronousRaiseLowerEcho: a Sink that confirms what a
// SetpointRaiseLower moves through the inbound setters before it returns
// lets the command complete; both setpoints are stored as the Sink saw
// them.
func TestSinkSynchronousRaiseLowerEcho(t *testing.T) {
	t.Parallel()
	sink := &echoSink{}
	srv := newSinkHeatCool(sink)
	sink.setpoint = func(attrID uint32, value int16, companions []thermo.SetpointChange) {
		echoSetpoint(srv, attrID, value)
		for _, c := range companions {
			echoSetpoint(srv, c.AttrID, c.Value)
		}
	}
	within(t, "SetpointRaiseLower", func() {
		if _, err := srv.MatterInvoke(context.Background(), tdef.CmdSetpointRaiseLower,
			tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeEnum(clusterwire.ThermostatSetpointModeBoth), Amount: 3}); err != nil {
			t.Errorf("invoke: %v", err)
		}
	})
	if got := readSetpoint(t, srv, tdef.AttrOccupiedHeatingSetpoint, "heat"); got != 2030 {
		t.Errorf("OccupiedHeatingSetpoint = %d, want 2030", got)
	}
	if got := readSetpoint(t, srv, tdef.AttrOccupiedCoolingSetpoint, "cool"); got != 2630 {
		t.Errorf("OccupiedCoolingSetpoint = %d, want 2630", got)
	}
}

// TestSinkCompanionsRecomputedOnTheReReadState: the reconcile that gives
// a write its companions runs again on the state as it is once the Sink
// consented. Raising the heating setpoint to 25.00 °C under the 2.0 °C
// deadband tells the Sink about the companion cool 27.00 °C.
//
//   - The host stores cool 29.00 °C while the Sink runs: the deadband
//     holds on the re-read state, cool stays 29.00 °C — not the 27.00 °C
//     the Sink was told — and the server logs
//     matter.thermostat.sink_companions_moved.
//   - The host stores cool 25.50 °C while the Sink runs: the reconcile on
//     the re-read state moves it to 27.00 °C, what the Sink was told; no
//     log.
//
// Not parallel: it redirects slog.Default.
func TestSinkCompanionsRecomputedOnTheReReadState(t *testing.T) {
	for _, tc := range []struct {
		name     string
		hostCool int16
		wantCool int16
		wantLog  bool
	}{
		{"host moved the companion elsewhere", 2900, 2900, true},
		{"reconcile lands where the Sink was told", 2550, 2700, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			defer slog.SetDefault(prev)

			sink := &echoSink{}
			srv := newSinkHeatCool(sink)
			var saw []thermo.SetpointChange
			sink.setpoint = func(_ uint32, _ int16, companions []thermo.SetpointChange) {
				saw = companions
				srv.SetOccupiedCoolingSetpoint(tc.hostCool)
			}
			var changed []uint32
			srv.OnAttributeChange(func(change contract.Change) { ids := change.Attributes; changed = append(changed, ids...) })
			within(t, "the setpoint write", func() {
				if err := srv.MatterWrite(context.Background(), tdef.AttrOccupiedHeatingSetpoint, int16(2500)); err != nil {
					t.Errorf("write: %v", err)
				}
			})
			if want := []thermo.SetpointChange{{AttrID: tdef.AttrOccupiedCoolingSetpoint, Value: 2700}}; len(saw) != 1 || saw[0] != want[0] {
				t.Errorf("Sink saw companions %v, want %v", saw, want)
			}
			if got := readSetpoint(t, srv, tdef.AttrOccupiedHeatingSetpoint, "heat"); got != 2500 {
				t.Errorf("OccupiedHeatingSetpoint = %d, want 2500", got)
			}
			if got := readSetpoint(t, srv, tdef.AttrOccupiedCoolingSetpoint, "cool"); got != tc.wantCool {
				t.Errorf("OccupiedCoolingSetpoint = %d, want %d", got, tc.wantCool)
			}
			if logged := strings.Contains(buf.String(), "matter.thermostat.sink_companions_moved"); logged != tc.wantLog {
				t.Errorf("sink_companions_moved logged = %t, want %t; log:\n%s", logged, tc.wantLog, buf.String())
			}
			// Every move of the cooling setpoint is reported: the host's,
			// and the reconcile's when it moves it back.
			if len(changed) == 0 || changed[0] != tdef.AttrOccupiedCoolingSetpoint {
				t.Errorf("changed %v, want the host's cooling setpoint first", changed)
			}
		})
	}
}

// TestSinkHearsTheValueTheServerStores: the Sink is told the written
// setpoint as the reconcile leaves it — the value the server stores — and
// the companions the reconcile moves. The row is the reconcile case
// "cooling setpoint inside the deadband pushes the heating setpoint"
// (TestParityMatterJS_Thermostat_ReconcileSequences): cool 21.00 °C pushes
// heat to 19.00 °C under the 2.0 °C deadband.
//
// No occupied-setpoint write is pinned to another value of itself: within
// the limits the assert admits, moving it needs v + deadband above
// MaxCoolSetpointLimit (or v − deadband below MinHeatSetpointLimit), which
// the deadband between the user limits rules out, and a reconcile that
// cannot fit refuses the write instead. So value equals the written value
// here, and the stored value equals what the Sink was told.
func TestSinkHearsTheValueTheServerStores(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{}
	srv := newSinkHeatCool(sink)
	if err := srv.MatterWrite(context.Background(), tdef.AttrOccupiedCoolingSetpoint, int16(2100)); err != nil {
		t.Fatalf("write: %v", err)
	}
	calls := sink.recorded()
	if len(calls) != 1 {
		t.Fatalf("Sink calls %+v, want one", calls)
	}
	c := calls[0]
	stored := readSetpoint(t, srv, tdef.AttrOccupiedCoolingSetpoint, "cool")
	if c.attrID != tdef.AttrOccupiedCoolingSetpoint || c.value != stored || stored != 2100 {
		t.Errorf("Sink told 0x%04X=%d, stored %d, want both 2100", c.attrID, c.value, stored)
	}
	want := []thermo.SetpointChange{{AttrID: tdef.AttrOccupiedHeatingSetpoint, Value: 1900}}
	if len(c.companions) != 1 || c.companions[0] != want[0] {
		t.Errorf("companions %v, want %v", c.companions, want)
	}
	if got := readSetpoint(t, srv, tdef.AttrOccupiedHeatingSetpoint, "heat"); got != 1900 {
		t.Errorf("OccupiedHeatingSetpoint = %d, want 1900", got)
	}
}

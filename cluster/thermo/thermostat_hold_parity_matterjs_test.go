// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// TemperatureSetpointHold, TemperatureSetpointHoldDuration and
// ThermostatRunningState, the optional attributes a week-program
// thermostat serves (issue #273, brief CO PR 1). matter.js at 862ad75f
// serves them as plain state: #setupThermostatLogic initialises
// temperatureSetpointHoldDuration to null when temperatureSetpointHold is
// present (packages/node/src/behaviors/thermostat/ThermostatServer.ts:365-369),
// leaves setpointHoldExpiryTimestamp unset (:372-379) and does not react to
// a hold change (:381, the reactor commented out). connectedhomeip gives
// the three no behaviour either (src/app/clusters/thermostat-server/
// ThermostatClusterBase.cpp:357-362 at 6170af84: optional attributes
// only). The hold write reaching the host through the Sink is this
// module's port.
package thermo_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// newHoldThermostat is a HEAT+COOL+AUTO thermostat serving the hold (off)
// and the running state (idle), on sink.
func newHoldThermostat(t *testing.T, sink thermo.Sink) *thermo.ThermostatServer {
	t.Helper()
	cfg := thermo.DefaultThermostatConfig()
	cfg.Sink = sink
	hold, running := uint8(tdef.TemperatureSetpointHoldSetpointHoldOff), uint16(0)
	cfg.TemperatureSetpointHold, cfg.ThermostatRunningState = &hold, &running
	srv, err := thermo.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// holdChangeRecorder collects what the change notifier reports.
type holdChangeRecorder struct {
	mu      sync.Mutex
	changes []contract.Change
}

func recordHoldChanges(t *testing.T, srv *thermo.ThermostatServer) *holdChangeRecorder {
	t.Helper()
	r := &holdChangeRecorder{}
	t.Cleanup(srv.OnAttributeChange(func(c contract.Change) {
		r.mu.Lock()
		r.changes = append(r.changes, c)
		r.mu.Unlock()
	}))
	return r
}

func (r *holdChangeRecorder) attrs(origin contract.Origin) []uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []uint32
	for _, c := range r.changes {
		if c.Origin == origin {
			out = append(out, c.Attributes...)
		}
	}
	return out
}

// TestTemperatureSetpointHoldServedOnRequest: without the configuration
// none of the optional attributes is served; with the hold the server
// serves TemperatureSetpointHold and TemperatureSetpointHoldDuration, null
// (matter.js ThermostatServer.ts:365-369 makes the duration present, and
// ValidatedElements.ts:252 counts a defined state as implemented), and
// not SetpointHoldExpiryTimestamp (left unset, :372-379); with the running
// state ThermostatRunningState.
func TestTemperatureSetpointHoldServedOnRequest(t *testing.T) {
	t.Parallel()
	optional := []uint32{
		tdef.AttrTemperatureSetpointHold, tdef.AttrTemperatureSetpointHoldDuration,
		tdef.AttrThermostatRunningState, tdef.AttrSetpointHoldExpiryTimestamp,
	}

	plain := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
	for _, id := range optional {
		if slices.Contains(plain.MatterAttributes(), id) {
			t.Errorf("attribute 0x%04X served without the configuration", id)
		}
		if _, ok := plain.MatterRead(id); ok {
			t.Errorf("attribute 0x%04X readable without the configuration", id)
		}
	}
	err := plain.MatterWrite(context.Background(), tdef.AttrTemperatureSetpointHold, uint8(1))
	if got := sinkStatus(t, err); got != im.StatusUnsupportedAttribute {
		t.Errorf("hold write without the hold: %s, want UNSUPPORTED_ATTRIBUTE", got)
	}

	srv := newHoldThermostat(t, nil)
	attrs := srv.MatterAttributes()
	for _, id := range optional[:3] {
		if !slices.Contains(attrs, id) {
			t.Errorf("attribute 0x%04X not in AttributeList %v", id, attrs)
		}
	}
	if slices.Contains(attrs, tdef.AttrSetpointHoldExpiryTimestamp) {
		t.Error("SetpointHoldExpiryTimestamp served; matter.js leaves it unset")
	}
	if v, ok := srv.MatterRead(tdef.AttrTemperatureSetpointHold); !ok || v != uint8(0) {
		t.Errorf("TemperatureSetpointHold = %v, %v; want 0 (SetpointHoldOff)", v, ok)
	}
	if v, ok := srv.MatterRead(tdef.AttrTemperatureSetpointHoldDuration); !ok || v != nil {
		t.Errorf("TemperatureSetpointHoldDuration = %v, %v; want null", v, ok)
	}
	if v, ok := srv.MatterRead(tdef.AttrThermostatRunningState); !ok || v != uint16(0) {
		t.Errorf("ThermostatRunningState = %v, %v; want 0", v, ok)
	}
}

// TestNewRefusesAnInitialHoldOutsideItsType: a hold outside
// TemperatureSetpointHoldEnum and a running state with a bit
// RelayStateBitmap does not define are refused at construction.
func TestNewRefusesAnInitialHoldOutsideItsType(t *testing.T) {
	t.Parallel()
	cfg := thermo.DefaultThermostatConfig()
	bad := uint8(2)
	cfg.TemperatureSetpointHold = &bad
	if _, err := thermo.New(cfg); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("hold 2: %v, want spec.ErrInvalidValue", err)
	}
	cfg = thermo.DefaultThermostatConfig()
	reserved := uint16(1 << 7)
	cfg.ThermostatRunningState = &reserved
	if _, err := thermo.New(cfg); !errors.Is(err, spec.ErrInvalidValue) {
		t.Errorf("running state 0x0080: %v, want spec.ErrInvalidValue", err)
	}
}

// TestTemperatureSetpointHoldWriteGoesThroughTheSink: a controller's hold
// write reaches the Sink before it is stored and is reported as the
// controller's write; the same value again is no change and does not call
// the Sink (Datasource.ts:760-762 at 0ceb202d). matter.js stores the hold
// only (ThermostatServer.ts:381) — the Sink call is this module's port.
func TestTemperatureSetpointHoldWriteGoesThroughTheSink(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{}
	srv := newHoldThermostat(t, sink)
	rec := recordHoldChanges(t, srv)
	ctx := context.Background()
	on := uint8(tdef.TemperatureSetpointHoldSetpointHoldOn)
	if err := srv.MatterWrite(ctx, tdef.AttrTemperatureSetpointHold, on); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(ctx, tdef.AttrTemperatureSetpointHold, on); err != nil {
		t.Fatal(err)
	}
	if got, want := sink.recorded(), []sinkCall{{hold: true, holdValue: on}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Sink calls = %+v, want %+v", got, want)
	}
	if v, _ := srv.MatterRead(tdef.AttrTemperatureSetpointHold); v != on {
		t.Errorf("TemperatureSetpointHold = %v, want SetpointHoldOn", v)
	}
	if got, want := rec.attrs(contract.OriginControllerWrite), []uint32{tdef.AttrTemperatureSetpointHold}; !reflect.DeepEqual(got, want) {
		t.Errorf("reported %v, want %v", got, want)
	}
}

// TestTemperatureSetpointHoldRefusal: the host's refusal refuses the write
// as a SystemMode refusal does — a status error passes through, any other
// answers FAILURE — and nothing is stored or reported.
func TestTemperatureSetpointHoldRefusal(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		err  error
		want im.StatusCode
	}{
		{"status passes through", spec.Errorf(im.StatusBusy, "device busy"), im.StatusBusy},
		{"plain error is FAILURE", errors.New("radio timeout"), im.StatusFailure},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sink := &recordingThermoSink{err: c.err}
			srv := newHoldThermostat(t, sink)
			rec := recordHoldChanges(t, srv)
			err := srv.MatterWrite(context.Background(), tdef.AttrTemperatureSetpointHold, uint8(1))
			if got := sinkStatus(t, err); got != c.want {
				t.Errorf("status = %s, want %s", got, c.want)
			}
			if v, _ := srv.MatterRead(tdef.AttrTemperatureSetpointHold); v != uint8(0) {
				t.Errorf("TemperatureSetpointHold = %v after a refusal, want 0", v)
			}
			if got := rec.attrs(contract.OriginControllerWrite); len(got) != 0 {
				t.Errorf("a refused write reported %v", got)
			}
		})
	}
}

// TestTemperatureSetpointHoldWriteChecks: a value outside
// TemperatureSetpointHoldEnum is CONSTRAINT_ERROR before the Sink;
// ThermostatRunningState ("R V") is read-only; without a Sink the hold is
// stored as matter.js stores it.
func TestTemperatureSetpointHoldWriteChecks(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{}
	srv := newHoldThermostat(t, sink)
	ctx := context.Background()
	if got := sinkStatus(t, srv.MatterWrite(ctx, tdef.AttrTemperatureSetpointHold, uint8(2))); got != im.StatusConstraintError {
		t.Errorf("hold 2: %s, want CONSTRAINT_ERROR", got)
	}
	if got := sinkStatus(t, srv.MatterWrite(ctx, tdef.AttrThermostatRunningState, uint16(1))); got != im.StatusUnsupportedWrite {
		t.Errorf("running state write: %s, want UNSUPPORTED_WRITE", got)
	}
	if n := len(sink.recorded()); n != 0 {
		t.Errorf("Sink called %d times for refused writes, want 0", n)
	}

	alone := newHoldThermostat(t, nil)
	if err := alone.MatterWrite(ctx, tdef.AttrTemperatureSetpointHold, uint8(1)); err != nil {
		t.Fatal(err)
	}
	if v, _ := alone.MatterRead(tdef.AttrTemperatureSetpointHold); v != uint8(1) {
		t.Errorf("TemperatureSetpointHold = %v without a Sink, want 1 stored", v)
	}
}

// TestTemperatureSetpointHoldDurationIsPlainState: the duration is stored
// as written, null included, within "max 1440", without the Sink and
// without a hold timer (matter.js #handleTemperatureSetpointHoldChange is
// commented out, ThermostatServer.ts:381-386).
func TestTemperatureSetpointHoldDurationIsPlainState(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{}
	srv := newHoldThermostat(t, sink)
	ctx := context.Background()
	if err := srv.MatterWrite(ctx, tdef.AttrTemperatureSetpointHoldDuration, uint16(90)); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(tdef.AttrTemperatureSetpointHoldDuration); v != uint16(90) {
		t.Errorf("TemperatureSetpointHoldDuration = %v, want 90", v)
	}
	if err := srv.MatterWrite(ctx, tdef.AttrTemperatureSetpointHoldDuration, nil); err != nil {
		t.Fatal(err)
	}
	if v, ok := srv.MatterRead(tdef.AttrTemperatureSetpointHoldDuration); !ok || v != nil {
		t.Errorf("TemperatureSetpointHoldDuration = %v, %v; want null", v, ok)
	}
	if got := sinkStatus(t, srv.MatterWrite(ctx, tdef.AttrTemperatureSetpointHoldDuration, uint16(1441))); got != im.StatusConstraintError {
		t.Errorf("duration 1441: %s, want CONSTRAINT_ERROR", got)
	}
	if n := len(sink.recorded()); n != 0 {
		t.Errorf("Sink called %d times for the duration, want 0", n)
	}
	if v, _ := srv.MatterRead(tdef.AttrTemperatureSetpointHold); v != uint8(0) {
		t.Errorf("TemperatureSetpointHold = %v after duration writes, want 0 (no hold timer)", v)
	}
}

// TestInboundHoldAndRunningState: the device's own hold and relay state
// are stored and reported as the host's change, without the Sink; a value
// outside its type, or an attribute the server does not serve, is logged
// and changes nothing.
func TestInboundHoldAndRunningState(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{err: errors.New("must not be called")}
	srv := newHoldThermostat(t, sink)
	rec := recordHoldChanges(t, srv)

	srv.SetTemperatureSetpointHold(uint8(tdef.TemperatureSetpointHoldSetpointHoldOn))
	srv.SetTemperatureSetpointHold(uint8(tdef.TemperatureSetpointHoldSetpointHoldOn)) // unchanged: not reported
	srv.SetTemperatureSetpointHold(2)                                                 // outside the enum: ignored
	heating := uint16(tdef.RelayStateHeat | tdef.RelayStateHeatStage2)
	srv.SetThermostatRunningState(heating)
	srv.SetThermostatRunningState(1 << 7) // reserved bit: ignored

	if v, _ := srv.MatterRead(tdef.AttrTemperatureSetpointHold); v != uint8(1) {
		t.Errorf("TemperatureSetpointHold = %v, want SetpointHoldOn", v)
	}
	if v, _ := srv.MatterRead(tdef.AttrThermostatRunningState); v != heating {
		t.Errorf("ThermostatRunningState = %v, want 0x%04X", v, heating)
	}
	if n := len(sink.recorded()); n != 0 {
		t.Errorf("Sink called %d times by the inbound setters, want 0", n)
	}
	want := []uint32{tdef.AttrTemperatureSetpointHold, tdef.AttrThermostatRunningState}
	if got := rec.attrs(contract.OriginHostSet); !reflect.DeepEqual(got, want) {
		t.Errorf("reported %v, want %v", got, want)
	}

	plain := thermo.NewThermostatServer(thermo.DefaultThermostatConfig())
	plain.SetTemperatureSetpointHold(1)
	plain.SetThermostatRunningState(1)
	if _, ok := plain.MatterRead(tdef.AttrTemperatureSetpointHold); ok {
		t.Error("the inbound hold served an attribute the configuration left out")
	}
}

// TestTypedSetOfHoldAndRunningState: the typed server's Set stores the
// hold, its duration and the running state as the host's change, without
// the Sink, and Get reads them back.
func TestTypedSetOfHoldAndRunningState(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{err: errors.New("must not be called")}
	typed := tdef.Wrap(newHoldThermostat(t, sink))
	on := tdef.TemperatureSetpointHoldSetpointHoldOn
	minutes := uint16(45)
	cooling := tdef.RelayStateCool | tdef.RelayStateFan
	if err := typed.Set(tdef.Patch{
		TemperatureSetpointHold: &on, TemperatureSetpointHoldDuration: &minutes, ThermostatRunningState: &cooling,
	}); err != nil {
		t.Fatal(err)
	}
	st := typed.Get()
	if st.TemperatureSetpointHold != on || st.ThermostatRunningState != cooling {
		t.Errorf("state hold %v running %v, want %v / %v", st.TemperatureSetpointHold, st.ThermostatRunningState, on, cooling)
	}
	if st.TemperatureSetpointHoldDuration == nil || *st.TemperatureSetpointHoldDuration != 45 {
		t.Errorf("TemperatureSetpointHoldDuration = %v, want 45", st.TemperatureSetpointHoldDuration)
	}
	if n := len(sink.recorded()); n != 0 {
		t.Errorf("Sink called %d times by Set, want 0", n)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// sinkCall is one call a recordingSink saw.
type sinkCall struct {
	systemMode bool
	mode       uint8
	attrID     uint32
	value      int16
	companions []thermo.SetpointChange
}

// recordingThermoSink records every call and refuses each with err.
type recordingThermoSink struct {
	mu    sync.Mutex
	calls []sinkCall
	err   error
}

func (r *recordingThermoSink) SetSystemMode(_ context.Context, mode uint8) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, sinkCall{systemMode: true, mode: mode})
	return r.err
}

func (r *recordingThermoSink) SetSetpoint(_ context.Context, attrID uint32, value int16, companions []thermo.SetpointChange) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, sinkCall{attrID: attrID, value: value, companions: companions})
	return r.err
}

func (r *recordingThermoSink) recorded() []sinkCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]sinkCall(nil), r.calls...)
}

func newSinkHeatCool(sink thermo.Sink) *thermo.ThermostatServer {
	cfg := thermo.DefaultThermostatConfig() // HEAT+COOL+AUTO, heat 2000, cool 2600, deadband 2.0 °C
	cfg.Sink = sink
	return thermo.NewThermostatServer(cfg)
}

func sinkStatus(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var sc im.StatusCodeError
	if !errors.As(err, &sc) {
		t.Fatalf("error %v carries no IM status", err)
	}
	return sc.MatterStatusCode()
}

// TestSinkRefusalKeepsTheStoredSetpoint: a refused occupied-setpoint write
// answers FAILURE and stores nothing — matter.js refuses in
// occupied*Setpoint$Changing, before the state changes
// (ThermostatServer.ts:594-601 at 0ceb202d).
func TestSinkRefusalKeepsTheStoredSetpoint(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{err: errors.New("radio timeout")}
	srv := newSinkHeatCool(sink)
	err := srv.MatterWrite(context.Background(), tdef.AttrOccupiedHeatingSetpoint, int16(2500))
	if err == nil {
		t.Fatal("refused write answered success")
	}
	if got := sinkStatus(t, err); got != im.StatusFailure {
		t.Errorf("status = %s, want FAILURE", got)
	}
	if got := readSetpoint(t, srv, tdef.AttrOccupiedHeatingSetpoint, "heat"); got != 2000 {
		t.Errorf("OccupiedHeatingSetpoint = %d after a refusal, want 2000", got)
	}
	if got := readSetpoint(t, srv, tdef.AttrOccupiedCoolingSetpoint, "cool"); got != 2600 {
		t.Errorf("OccupiedCoolingSetpoint = %d after a refusal, want 2600 (the companion stays)", got)
	}
	if n := len(sink.recorded()); n != 1 {
		t.Errorf("Sink called %d times, want 1", n)
	}
}

// TestSinkStatusErrorPassesThrough: a refusal that carries an IM status
// answers with it.
func TestSinkStatusErrorPassesThrough(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{err: spec.Errorf(im.StatusBusy, "device busy")}
	srv := newSinkHeatCool(sink)
	err := srv.MatterWrite(context.Background(), tdef.AttrSystemMode, uint8(tdef.SystemModeHeat))
	if got := sinkStatus(t, err); got != im.StatusBusy {
		t.Errorf("status = %s, want BUSY", got)
	}
	if v, _ := srv.MatterRead(tdef.AttrSystemMode); v != uint8(tdef.SystemModeAuto) {
		t.Errorf("SystemMode = %v after a refusal, want Auto", v)
	}
}

// TestSinkSeesSystemModeAndTheCompanions: an accepted SystemMode write is
// stored after the Sink saw it; an accepted setpoint write hands the Sink
// what the reconcile moves with it — raising the heating setpoint to
// 25.00 °C under a 2.0 °C deadband moves the cooling setpoint to 27.00 °C
// (matter.js #reconcileSetpoints → #fixSetpointRange) — and both are
// stored.
func TestSinkSeesSystemModeAndTheCompanions(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{}
	srv := newSinkHeatCool(sink)
	ctx := context.Background()
	if err := srv.MatterWrite(ctx, tdef.AttrSystemMode, uint8(tdef.SystemModeCool)); err != nil {
		t.Fatalf("SystemMode: %v", err)
	}
	if v, _ := srv.MatterRead(tdef.AttrSystemMode); v != uint8(tdef.SystemModeCool) {
		t.Errorf("SystemMode = %v, want Cool", v)
	}
	if err := srv.MatterWrite(ctx, tdef.AttrOccupiedHeatingSetpoint, int16(2500)); err != nil {
		t.Fatalf("OccupiedHeatingSetpoint: %v", err)
	}
	want := []sinkCall{
		{systemMode: true, mode: uint8(tdef.SystemModeCool)},
		{
			attrID: tdef.AttrOccupiedHeatingSetpoint, value: 2500,
			companions: []thermo.SetpointChange{{AttrID: tdef.AttrOccupiedCoolingSetpoint, Value: 2700}},
		},
	}
	if got := sink.recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("Sink calls = %+v, want %+v", got, want)
	}
	if got := readSetpoint(t, srv, tdef.AttrOccupiedCoolingSetpoint, "cool"); got != 2700 {
		t.Errorf("OccupiedCoolingSetpoint = %d, want 2700", got)
	}
}

// TestSinkNotCalledForLimitsOrRefusedWrites: limits are configuration and
// do not reach the Sink; a write the server's own rules refuse
// (#assertSetpointWithinLimits) does not either.
func TestSinkNotCalledForLimitsOrRefusedWrites(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{}
	srv := newSinkHeatCool(sink)
	ctx := context.Background()
	if err := srv.MatterWrite(ctx, tdef.AttrMaxHeatSetpointLimit, int16(2800)); err != nil {
		t.Fatalf("MaxHeatSetpointLimit: %v", err)
	}
	err := srv.MatterWrite(ctx, tdef.AttrOccupiedHeatingSetpoint, int16(2900))
	if got := sinkStatus(t, err); got != im.StatusConstraintError {
		t.Errorf("setpoint above its limit: status %s, want CONSTRAINT_ERROR", got)
	}
	if n := len(sink.recorded()); n != 0 {
		t.Errorf("Sink called %d times, want 0", n)
	}
}

// TestSetpointRaiseLowerGoesThroughTheSink: Both on a HEAT+COOL thermostat
// is one Sink call — the cooling setpoint as the primary, the heating
// setpoint its companion (matter.js ThermostatServer.ts:219-220 at
// 0ceb202d assigns cooling first); a single-mode command is one call with
// no companions; a refusal stores neither.
func TestSetpointRaiseLowerGoesThroughTheSink(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{}
	srv := newSinkHeatCool(sink)
	invokeSetpointRaiseLower(t, srv, clusterwire.ThermostatSetpointModeBoth, 3)
	invokeSetpointRaiseLower(t, srv, clusterwire.ThermostatSetpointModeHeat, -2)
	want := []sinkCall{
		{
			attrID: tdef.AttrOccupiedCoolingSetpoint, value: 2630,
			companions: []thermo.SetpointChange{{AttrID: tdef.AttrOccupiedHeatingSetpoint, Value: 2030}},
		},
		{attrID: tdef.AttrOccupiedHeatingSetpoint, value: 2010},
	}
	if got := sink.recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("Sink calls = %+v, want %+v", got, want)
	}
	if got := readSetpoint(t, srv, tdef.AttrOccupiedCoolingSetpoint, "cool"); got != 2630 {
		t.Errorf("OccupiedCoolingSetpoint = %d, want 2630", got)
	}
	if got := readSetpoint(t, srv, tdef.AttrOccupiedHeatingSetpoint, "heat"); got != 2010 {
		t.Errorf("OccupiedHeatingSetpoint = %d, want 2010", got)
	}

	refusing := &recordingThermoSink{err: errors.New("no")}
	srv = newSinkHeatCool(refusing)
	_, err := srv.MatterInvoke(context.Background(), tdef.CmdSetpointRaiseLower,
		clusterwire.SetpointRaiseLowerRequest{Mode: clusterwire.ThermostatSetpointModeBoth, Amount: 3})
	if got := sinkStatus(t, err); got != im.StatusFailure {
		t.Errorf("refused SetpointRaiseLower: status %s, want FAILURE", got)
	}
	if got := readSetpoint(t, srv, tdef.AttrOccupiedCoolingSetpoint, "cool"); got != 2600 {
		t.Errorf("OccupiedCoolingSetpoint = %d after a refusal, want 2600", got)
	}
	if got := readSetpoint(t, srv, tdef.AttrOccupiedHeatingSetpoint, "heat"); got != 2000 {
		t.Errorf("OccupiedHeatingSetpoint = %d after a refusal, want 2000", got)
	}
	if n := len(refusing.recorded()); n != 1 {
		t.Errorf("Sink called %d times, want 1", n)
	}
}

// TestUnchangedValueDoesNotCallTheSink: a write of the stored SystemMode
// or setpoint, and a SetpointRaiseLower that moves nothing, answer SUCCESS
// without the Sink — matter.js Datasource.ts:760-762 skips an equal value
// on commit and :1042-1043 fires $Changing only for changed fields (0ceb202d).
// A cooling-only Both moves one setpoint, which travels alone.
func TestUnchangedValueDoesNotCallTheSink(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{err: errors.New("must not be called")}
	srv := newSinkHeatCool(sink)
	ctx := context.Background()
	if err := srv.MatterWrite(ctx, tdef.AttrOccupiedHeatingSetpoint, int16(2000)); err != nil {
		t.Errorf("unchanged OccupiedHeatingSetpoint: %v", err)
	}
	if err := srv.MatterWrite(ctx, tdef.AttrSystemMode, uint8(tdef.SystemModeAuto)); err != nil {
		t.Errorf("unchanged SystemMode: %v", err)
	}
	invokeSetpointRaiseLower(t, srv, clusterwire.ThermostatSetpointModeBoth, 0)
	if n := len(sink.recorded()); n != 0 {
		t.Errorf("Sink called %d times for unchanged values, want 0", n)
	}

	coolSink := &recordingThermoSink{}
	cfg := thermo.ThermostatConfig{
		Features:                thermo.ThermostatFeatureCOOL,
		AbsMinCoolSetpointLimit: 1600,
		AbsMaxCoolSetpointLimit: 3200,
		InitialCoolingSetpoint:  2600,
		Sink:                    coolSink,
	}
	cool := thermo.NewThermostatServer(cfg)
	invokeSetpointRaiseLower(t, cool, clusterwire.ThermostatSetpointModeBoth, 1)
	want := []sinkCall{{attrID: tdef.AttrOccupiedCoolingSetpoint, value: 2610}}
	if got := coolSink.recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("Sink calls = %+v, want %+v", got, want)
	}
}

// TestInboundSettersDoNotCallTheSink: the device's own SystemMode and
// setpoints are stored without the Sink and reported through the change
// notifier; a setpoint runs the write's reconcile (the companion moves
// and is reported) and is clamped to its limits rather than refused.
func TestInboundSettersDoNotCallTheSink(t *testing.T) {
	t.Parallel()
	sink := &recordingThermoSink{err: errors.New("must not be called")}
	srv := newSinkHeatCool(sink)
	var mu sync.Mutex
	var reported []uint32
	unsub := srv.OnMatterAttributesChanged(func(ids []uint32) {
		mu.Lock()
		reported = append(reported, ids...)
		mu.Unlock()
	})
	defer unsub()

	srv.SetSystemMode(uint8(tdef.SystemModeHeat))
	if v, _ := srv.MatterRead(tdef.AttrSystemMode); v != uint8(tdef.SystemModeHeat) {
		t.Errorf("SystemMode = %v, want Heat", v)
	}
	srv.SetOccupiedHeatingSetpoint(2500)
	if got := readSetpoint(t, srv, tdef.AttrOccupiedCoolingSetpoint, "cool"); got != 2700 {
		t.Errorf("OccupiedCoolingSetpoint = %d, want 2700 (reconciled)", got)
	}
	srv.SetOccupiedCoolingSetpoint(9000) // above MaxCoolSetpointLimit 3200: clamped
	if got := readSetpoint(t, srv, tdef.AttrOccupiedCoolingSetpoint, "cool"); got != 3200 {
		t.Errorf("OccupiedCoolingSetpoint = %d, want 3200 (clamped)", got)
	}
	if n := len(sink.recorded()); n != 0 {
		t.Errorf("Sink called %d times by the inbound setters, want 0", n)
	}
	mu.Lock()
	got := append([]uint32(nil), reported...)
	mu.Unlock()
	want := []uint32{
		tdef.AttrSystemMode,
		tdef.AttrOccupiedHeatingSetpoint, tdef.AttrOccupiedCoolingSetpoint,
		tdef.AttrOccupiedCoolingSetpoint,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reported %v, want %v", got, want)
	}
}

// TestInboundSystemModeTheSelectionCannotServe is logged and ignored: Cool
// on a heating-only thermostat (SystemModeEnum Cool is "[COOL]").
func TestInboundSystemModeTheSelectionCannotServe(t *testing.T) {
	t.Parallel()
	srv := newHeatOnly()
	srv.SetSystemMode(uint8(tdef.SystemModeCool))
	if v, _ := srv.MatterRead(tdef.AttrSystemMode); v != uint8(tdef.SystemModeHeat) {
		t.Errorf("SystemMode = %v, want Heat kept", v)
	}
	srv.SetOccupiedCoolingSetpoint(2000) // no COOL: ignored
	if _, ok := srv.MatterRead(tdef.AttrOccupiedCoolingSetpoint); ok {
		t.Error("OccupiedCoolingSetpoint served without COOL")
	}
}

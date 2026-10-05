// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package pump_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/pump"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// device is a host pump with every setter.
type device struct {
	mu      sync.Mutex
	st      pump.State
	setErr  error
	hours   []*uint32
	energy  []*uint32
	control []pump.ControlMode
}

func (d *device) PumpState() pump.State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.st
}

func (d *device) SetOperationMode(_ context.Context, m pump.OperationMode) error {
	if d.setErr != nil {
		return d.setErr
	}
	d.st.OperationMode = m
	return nil
}

func (d *device) SetControlMode(_ context.Context, m pump.ControlMode) error {
	d.control = append(d.control, m)
	d.st.ControlMode = m
	return nil
}

func (d *device) SetLifetimeRunningHours(_ context.Context, h *uint32) error {
	d.hours = append(d.hours, h)
	d.st.LifetimeRunningHours = h
	return nil
}

func (d *device) SetLifetimeEnergyConsumed(_ context.Context, e *uint32) error {
	d.energy = append(d.energy, e)
	d.st.LifetimeEnergyConsumed = e
	return nil
}

// plain has only the mandatory port.
type plain struct{ d *device }

func (p plain) PumpState() pump.State { return p.d.PumpState() }
func (p plain) SetOperationMode(ctx context.Context, m pump.OperationMode) error {
	return p.d.SetOperationMode(ctx, m)
}

type recorder struct {
	mu     sync.Mutex
	events []struct {
		endpoint uint16
		event    uint32
		data     any
		priority contract.EventPriority
	}
}

func (r *recorder) MatterEmitEvent(endpoint uint16, _, event uint32, data any, priority contract.EventPriority) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, struct {
		endpoint uint16
		event    uint32
		data     any
		priority contract.EventPriority
	}{endpoint, event, data, priority})
}

func i16(v int16) *int16   { return &v }
func u16(v uint16) *uint16 { return &v }
func u32(v uint32) *uint32 { return &v }

const everyOptional = pump.OptionalPumpStatus | pump.OptionalSpeed | pump.OptionalLifetimeRunningHours |
	pump.OptionalPower | pump.OptionalLifetimeEnergyConsumed | pump.OptionalControlMode

func newFull(t *testing.T, d *device) *pump.Server {
	t.Helper()
	srv, err := pump.NewServer(pump.Config{
		Source: d,
		Features: pump.FeatureConstantPressure | pump.FeatureConstantSpeed | pump.FeatureAutomatic |
			pump.FeatureLocalOperation,
		Optional: everyOptional,
		Limits: pump.Limits{
			MaxPressure: i16(1000), MaxSpeed: u16(3000), MaxFlow: nil,
			MinConstPressure: i16(100), MaxConstPressure: i16(900),
			MinConstSpeed: u16(500), MaxConstSpeed: u16(0xFFFF),
		},
		Events: []uint32{pump.EventDryRunning, pump.EventSupplyVoltageLow, pump.EventDryRunning},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func statusOf(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var sce im.StatusCodeError
	if !errors.As(err, &sce) {
		t.Fatalf("error %v carries no IM status", err)
	}
	return sce.MatterStatusCode()
}

func TestNewServerEnforcesConformance(t *testing.T) {
	t.Parallel()
	d := &device{}
	cases := []struct {
		name string
		cfg  pump.Config
		want error
	}{
		{"no source", pump.Config{}, pump.ErrNoSource},
		{"unknown feature", pump.Config{Source: d, Features: 1 << 7}, pump.ErrUnknownFeature},
		{"no control feature", pump.Config{Source: d, Features: pump.FeatureAutomatic}, pump.ErrNoControlMode},
		{"auto limits without AUTO", pump.Config{Source: d, Features: pump.FeatureConstantFlow, Optional: pump.OptionalAutomaticLimits}, pump.ErrAutoLimits},
		{"control mode without setter", pump.Config{Source: plain{d}, Features: pump.FeatureConstantFlow, Optional: pump.OptionalControlMode}, pump.ErrSetterMissing},
		{"hours without setter", pump.Config{Source: plain{d}, Features: pump.FeatureConstantFlow, Optional: pump.OptionalLifetimeRunningHours}, pump.ErrSetterMissing},
		{"energy without setter", pump.Config{Source: plain{d}, Features: pump.FeatureConstantFlow, Optional: pump.OptionalLifetimeEnergyConsumed}, pump.ErrSetterMissing},
		{"unknown event", pump.Config{Source: d, Features: pump.FeatureConstantFlow, Events: []uint32{0x11}}, pump.ErrUnknownEvent},
	}
	for _, tc := range cases {
		if _, err := pump.NewServer(tc.cfg); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestAttributeSurfaceFollowsFeaturesAndOptionals(t *testing.T) {
	t.Parallel()
	d := &device{}
	minimal, err := pump.NewServer(pump.Config{Source: plain{d}, Features: pump.FeatureConstantFlow})
	if err != nil {
		t.Fatal(err)
	}
	if got := minimal.MatterAttributes(); !slices.Equal(got, []uint32{0, 1, 2, 9, 10, 0x11, 0x12, 0x13, 0x20}) {
		t.Errorf("minimal attributes = %v", got)
	}
	if got := minimal.MatterReportable(); !slices.Equal(got, []uint32{0x11, 0x12, 0x13, 0x20}) {
		t.Errorf("minimal reportable = %v", got)
	}
	full := newFull(t, d)
	if got := full.MatterAttributes(); !slices.Equal(got, []uint32{0, 1, 2, 3, 4, 7, 8, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x20, 0x21}) {
		t.Errorf("full attributes = %v", got)
	}
	auto, err := pump.NewServer(pump.Config{Source: d, Features: pump.FeatureConstantFlow | pump.FeatureAutomatic, Optional: pump.OptionalAutomaticLimits})
	if err != nil {
		t.Fatal(err)
	}
	if got := auto.MatterAttributes(); !slices.Equal(got[:13], []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}) {
		t.Errorf("AUTO limits = %v, want every pair", got)
	}
	if len(full.MatterAcceptedCommands()) != 0 || len(full.MatterGeneratedCommands()) != 0 {
		t.Error("the cluster has no commands")
	}
	if _, err := full.MatterInvoke(context.Background(), 0, nil); statusOf(t, err) != im.StatusUnsupportedCommand {
		t.Errorf("invoke: %v", err)
	}
	if got := full.MatterEvents(); !slices.Equal(got, []uint32{pump.EventSupplyVoltageLow, pump.EventDryRunning}) {
		t.Errorf("events = %v, want the declared set sorted and deduplicated", got)
	}
	if full.MatterClusterID() != pump.ClusterID || full.MatterDataVersion() == 0 {
		t.Error("identity")
	}
}

func TestReadsProjectLimitsAndState(t *testing.T) {
	t.Parallel()
	d := &device{st: pump.State{
		PumpStatus: pump.StatusRunning | pump.StatusRemoteFlow | 1<<12, EffectiveOperationMode: pump.OperationMaximum,
		EffectiveControlMode: pump.ControlConstantPressure, Capacity: i16(-32768), Speed: u16(1450),
		LifetimeRunningHours: u32(0xFFFFFFFF), Power: u32(75), LifetimeEnergyConsumed: u32(0xFFFFFFFF),
		OperationMode: pump.OperationNormal, ControlMode: pump.ControlAutomatic,
	}}
	srv := newFull(t, d)
	want := map[uint32]any{
		pump.AttrMaxPressure:              int16(1000),
		pump.AttrMaxSpeed:                 uint16(3000),
		pump.AttrMaxFlow:                  nil,
		pump.AttrMinConstPressure:         int16(100),
		pump.AttrMaxConstPressure:         int16(900),
		pump.AttrMinConstSpeed:            uint16(500),
		pump.AttrMaxConstSpeed:            uint16(0xFFFE),
		pump.AttrPumpStatus:               uint16(pump.StatusRunning | pump.StatusRemoteFlow),
		pump.AttrEffectiveOperationMode:   uint8(2),
		pump.AttrEffectiveControlMode:     uint8(1),
		pump.AttrCapacity:                 int16(-32767),
		pump.AttrSpeed:                    uint16(1450),
		pump.AttrLifetimeRunningHours:     uint32(0xFFFFFE),
		pump.AttrPower:                    uint32(75),
		pump.AttrLifetimeEnergyConsumed:   uint32(0xFFFFFFFE),
		pump.AttrOperationMode:            uint8(0),
		pump.AttrControlMode:              uint8(7),
		cluster.AttrGlobalFeatureMap:      uint32(pump.FeatureConstantPressure | pump.FeatureConstantSpeed | pump.FeatureAutomatic | pump.FeatureLocalOperation),
		cluster.AttrGlobalClusterRevision: pump.Revision(),
	}
	for id, w := range want {
		if got, ok := srv.MatterRead(id); !ok || got != w {
			t.Errorf("MatterRead(0x%04X) = (%v %T, %v), want %v %T", id, got, got, ok, w, w)
		}
	}
	d.st = pump.State{}
	for _, id := range []uint32{pump.AttrCapacity, pump.AttrSpeed, pump.AttrLifetimeRunningHours, pump.AttrPower, pump.AttrLifetimeEnergyConsumed} {
		if got, ok := srv.MatterRead(id); !ok || got != nil {
			t.Errorf("unknown 0x%04X read (%v, %v), want null", id, got, ok)
		}
	}
	if _, ok := srv.MatterRead(pump.AttrMinConstFlow); ok {
		t.Error("MinConstFlow served without FLW")
	}
	temp, err := pump.NewServer(pump.Config{Source: d, Features: pump.FeatureConstantTemperature, Limits: pump.Limits{MinConstTemp: i16(-30000), MaxConstTemp: i16(4000)}})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := temp.MatterRead(pump.AttrMinConstTemp); v != int16(-27315) {
		t.Errorf("MinConstTemp = %v, want the min -27315 floor", v)
	}
	if v, _ := temp.MatterRead(pump.AttrMaxConstTemp); v != int16(4000) {
		t.Errorf("MaxConstTemp = %v", v)
	}
}

func TestOperationAndControlModeWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{}
	srv := newFull(t, d) // PRSCONST, SPD, AUTO, LOCAL
	for _, m := range []pump.OperationMode{pump.OperationNormal, pump.OperationMinimum, pump.OperationMaximum, pump.OperationLocal} {
		if err := srv.MatterWrite(ctx, pump.AttrOperationMode, uint64(m)); err != nil || d.st.OperationMode != m {
			t.Errorf("OperationMode %d: %v", m, err)
		}
	}
	if err := srv.MatterWrite(ctx, pump.AttrOperationMode, uint64(4)); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("OperationMode 4: %v", err)
	}
	for _, m := range []pump.ControlMode{pump.ControlConstantSpeed, pump.ControlConstantPressure, pump.ControlAutomatic} {
		if err := srv.MatterWrite(ctx, pump.AttrControlMode, uint64(m)); err != nil {
			t.Errorf("ControlMode %d: %v", m, err)
		}
	}
	for _, m := range []uint64{uint64(pump.ControlProportionalPressure), uint64(pump.ControlConstantFlow), uint64(pump.ControlConstantTemperature), 4, 6} {
		if err := srv.MatterWrite(ctx, pump.AttrControlMode, m); statusOf(t, err) != im.StatusConstraintError {
			t.Errorf("ControlMode %d without its feature: %v", m, err)
		}
	}
	// Without SPD / LOCAL the mode is refused.
	flow, _ := pump.NewServer(pump.Config{Source: d, Features: pump.FeatureConstantFlow})
	for _, m := range []pump.OperationMode{pump.OperationMinimum, pump.OperationMaximum, pump.OperationLocal} {
		if err := flow.MatterWrite(ctx, pump.AttrOperationMode, uint64(m)); statusOf(t, err) != im.StatusConstraintError {
			t.Errorf("OperationMode %d on FLW-only: %v", m, err)
		}
	}
	if err := flow.MatterWrite(ctx, pump.AttrControlMode, uint64(pump.ControlConstantFlow)); statusOf(t, err) != im.StatusUnsupportedAttribute {
		t.Errorf("ControlMode without the optional attribute: %v", err)
	}
	d.setErr = errors.New("bus")
	if err := srv.MatterWrite(ctx, pump.AttrOperationMode, uint64(0)); !errors.Is(err, d.setErr) {
		t.Errorf("host failure: %v", err)
	}
	if srv.MinWritePrivilege(pump.AttrOperationMode) != 4 || srv.MinWritePrivilege(pump.AttrControlMode) != 4 ||
		srv.MinWritePrivilege(pump.AttrLifetimeRunningHours) != 4 || srv.MinWritePrivilege(pump.AttrLifetimeEnergyConsumed) != 4 ||
		srv.MinWritePrivilege(pump.AttrCapacity) != 3 {
		t.Error("the four writable attributes are written with Manage")
	}
}

func TestLifetimeCounterWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &device{}
	srv := newFull(t, d)
	if err := srv.MatterWrite(ctx, pump.AttrLifetimeRunningHours, uint64(0xFFFFFE)); err != nil || *d.hours[0] != 0xFFFFFE {
		t.Errorf("hours at the uint24 ceiling: %v", err)
	}
	if err := srv.MatterWrite(ctx, pump.AttrLifetimeRunningHours, nil); err != nil || d.hours[1] != nil {
		t.Errorf("hours null: %v", err)
	}
	if err := srv.MatterWrite(ctx, pump.AttrLifetimeRunningHours, uint64(0xFFFFFF)); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("hours = uint24 null sentinel: %v", err)
	}
	if err := srv.MatterWrite(ctx, pump.AttrLifetimeEnergyConsumed, uint64(0)); err != nil || *d.energy[0] != 0 {
		t.Errorf("energy reset: %v", err)
	}
	if err := srv.MatterWrite(ctx, pump.AttrLifetimeEnergyConsumed, uint64(0xFFFFFFFF)); statusOf(t, err) != im.StatusConstraintError {
		t.Errorf("energy = uint32 null sentinel: %v", err)
	}
	if err := srv.MatterWrite(ctx, pump.AttrCapacity, uint64(1)); statusOf(t, err) != im.StatusUnsupportedWrite {
		t.Errorf("Capacity write: %v", err)
	}
	if err := srv.MatterWrite(ctx, 0x30, uint64(1)); statusOf(t, err) != im.StatusUnsupportedAttribute {
		t.Errorf("undefined attribute: %v", err)
	}
	var tracker cluster.DataVersionTracker
	tracked, _ := pump.NewServer(pump.Config{Source: d, Features: pump.FeatureConstantFlow, DataVersion: &tracker})
	before := tracker.Current()
	_ = tracked.MatterWrite(ctx, pump.AttrOperationMode, uint64(0))
	if tracker.Current() == before || tracked.MatterDataVersion() != tracker.Current() {
		t.Error("the host tracker did not move")
	}
}

func TestEmitRaisesDeclaredEventsAtTheirPriority(t *testing.T) {
	t.Parallel()
	srv := newFull(t, &device{})
	if err := srv.Emit(pump.EventDryRunning); err != nil {
		t.Fatalf("Emit before an emitter is wired: %v", err)
	}
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	srv.SetEndpoint(4)
	if err := srv.Emit(pump.EventDryRunning); err != nil {
		t.Fatal(err)
	}
	if err := srv.Emit(pump.EventSupplyVoltageLow); err != nil {
		t.Fatal(err)
	}
	if err := srv.Emit(pump.EventPumpBlocked); !errors.Is(err, pump.ErrEventUndeclared) {
		t.Errorf("undeclared event: %v", err)
	}
	if len(rec.events) != 2 {
		t.Fatalf("emitted %d events, want 2", len(rec.events))
	}
	if e := rec.events[0]; e.endpoint != 4 || e.event != pump.EventDryRunning || e.priority != contract.EventPriorityCritical || e.data != (clusterwire.FieldlessEvent{}) {
		t.Errorf("DryRunning = %+v", e)
	}
	if e := rec.events[1]; e.priority != contract.EventPriorityInfo {
		t.Errorf("SupplyVoltageLow priority = %d, want info", e.priority)
	}
}

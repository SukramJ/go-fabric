// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package boolcfg_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	"github.com/SukramJ/go-fabric/cluster/spec"
	def "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// event is one emitted event.
type event struct {
	endpoint uint16
	id       uint32
	data     any
}

// emitter records the events a server emits.
type emitter struct {
	mu     sync.Mutex
	events []event
}

func (e *emitter) MatterEmitEvent(endpoint uint16, cluster, id uint32, data any, _ contract.EventPriority) {
	if cluster != boolcfg.ClusterID {
		panic("foreign cluster")
	}
	e.mu.Lock()
	e.events = append(e.events, event{endpoint, id, data})
	e.mu.Unlock()
}

func (e *emitter) take() []event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.events
	e.events = nil
	return out
}

// delegate records the delegate calls and refuses when told to.
type delegate struct {
	calls  []string
	refuse bool
}

func (d *delegate) answer(call string) error {
	d.calls = append(d.calls, call)
	if d.refuse {
		return errors.New("refused")
	}
	return nil
}

func (d *delegate) CurrentSensitivityLevelChanged(context.Context, uint8) error {
	return d.answer("level")
}

func (d *delegate) AlarmsEnabledChanged(context.Context, boolcfg.AlarmMode) error {
	return d.answer("enabled")
}

func (d *delegate) AlarmsActiveChanged(context.Context, boolcfg.AlarmMode) error {
	return d.answer("active")
}

func (d *delegate) AlarmsSuppressedChanged(context.Context, boolcfg.AlarmMode) error {
	return d.answer("suppressed")
}

// status returns the IM status err carries, Success for nil.
func status(t *testing.T, err error) im.StatusCode {
	t.Helper()
	if err == nil {
		return im.StatusSuccess
	}
	var se im.StatusCodeError
	if !errors.As(err, &se) {
		t.Fatalf("error without a status: %v", err)
	}
	return se.MatterStatusCode()
}

func newServer(t *testing.T, cfg boolcfg.Config) (*boolcfg.Server, *emitter) {
	t.Helper()
	srv, err := boolcfg.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	em := &emitter{}
	srv.SetMatterEventEmitter(em)
	srv.SetEndpoint(7)
	return srv, em
}

func full(d boolcfg.Delegate) boolcfg.Config {
	return boolcfg.Config{
		Features:                   boolcfg.FeatureVisual | boolcfg.FeatureAudible | boolcfg.FeatureAlarmSuppress | boolcfg.FeatureSensitivityLevel | boolcfg.FeatureFaultEvents,
		Optional:                   boolcfg.OptionalDefaultSensitivityLevel | boolcfg.OptionalAlarmsEnabled | boolcfg.OptionalSensorFault,
		SupportedSensitivityLevels: 3,
		DefaultSensitivityLevel:    1,
		AlarmsSupported:            boolcfg.AlarmVisual | boolcfg.AlarmAudible,
		AlarmsEnabled:              boolcfg.AlarmVisual | boolcfg.AlarmAudible,
		Delegate:                   d,
	}
}

func read(t *testing.T, srv *boolcfg.Server, attr uint32) any {
	t.Helper()
	v, ok := srv.MatterRead(attr)
	if !ok {
		t.Fatalf("attribute 0x%04X not served", attr)
	}
	return v
}

// TestSensitivityLevels holds the start-up clamps (chip .cpp:85-87,
// :97-103) and the CurrentSensitivityLevel write (chip .cpp:321-337).
func TestSensitivityLevels(t *testing.T) {
	t.Parallel()
	cfg := full(nil)
	cfg.SupportedSensitivityLevels = 20
	cfg.DefaultSensitivityLevel = 50
	srv, _ := newServer(t, cfg)
	if v := read(t, srv, def.AttrSupportedSensitivityLevels); v != uint8(10) {
		t.Errorf("SupportedSensitivityLevels = %v, want the clamp 10", v)
	}
	if v := read(t, srv, def.AttrDefaultSensitivityLevel); v != uint8(9) {
		t.Errorf("DefaultSensitivityLevel = %v, want 9", v)
	}
	if v := read(t, srv, def.AttrCurrentSensitivityLevel); v != uint8(9) {
		t.Errorf("CurrentSensitivityLevel = %v, want the default 9", v)
	}

	cfg = full(nil)
	cfg.SupportedSensitivityLevels = 0
	high := uint8(7)
	cfg.CurrentSensitivityLevel = &high
	srv, _ = newServer(t, cfg)
	if v := read(t, srv, def.AttrSupportedSensitivityLevels); v != uint8(2) {
		t.Errorf("SupportedSensitivityLevels = %v, want the clamp 2", v)
	}
	if v := read(t, srv, def.AttrCurrentSensitivityLevel); v != uint8(1) {
		t.Errorf("persisted CurrentSensitivityLevel 7 = %v, want capped 1", v)
	}

	d := &delegate{}
	srv, _ = newServer(t, full(d))
	ctx := context.Background()
	if got := status(t, srv.MatterWrite(ctx, def.AttrCurrentSensitivityLevel, uint64(3))); got != im.StatusConstraintError {
		t.Errorf("write 3 of 3 levels: %v, want CONSTRAINT_ERROR", got)
	}
	if got := status(t, srv.MatterWrite(ctx, def.AttrCurrentSensitivityLevel, uint64(1))); got != im.StatusSuccess || len(d.calls) != 0 {
		t.Errorf("unchanged write: %v, delegate %v; want success without a call", got, d.calls)
	}
	if got := status(t, srv.MatterWrite(ctx, def.AttrCurrentSensitivityLevel, uint64(2))); got != im.StatusSuccess {
		t.Errorf("write 2: %v", got)
	}
	if v := read(t, srv, def.AttrCurrentSensitivityLevel); v != uint8(2) || srv.State().CurrentSensitivityLevel != 2 {
		t.Errorf("CurrentSensitivityLevel = %v", v)
	}
	d.refuse = true
	if got := status(t, srv.MatterWrite(ctx, def.AttrCurrentSensitivityLevel, uint64(0))); got != im.StatusFailure {
		t.Errorf("refused write: %v, want FAILURE", got)
	}
	if v := read(t, srv, def.AttrCurrentSensitivityLevel); v != uint8(2) {
		t.Errorf("refused write changed the level to %v", v)
	}
	if got := status(t, srv.MatterWrite(ctx, def.AttrAlarmsActive, uint64(0))); got != im.StatusUnsupportedWrite {
		t.Errorf("AlarmsActive write: %v", got)
	}

	if err := srv.SetCurrentSensitivityLevel(0); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetCurrentSensitivityLevel(3); !errors.Is(err, boolcfg.ErrInvalidConfig) {
		t.Errorf("device level 3: %v", err)
	}
	plain, _ := newServer(t, boolcfg.Config{Features: boolcfg.FeatureVisual})
	if err := plain.SetCurrentSensitivityLevel(0); !errors.Is(err, boolcfg.ErrInvalidConfig) {
		t.Errorf("level without SENSLVL: %v", err)
	}
}

// TestEnableDisableAlarm follows chip .cpp:148-218.
func TestEnableDisableAlarm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &delegate{}
	cfg := full(d)
	cfg.AlarmsSupported = boolcfg.AlarmVisual
	cfg.AlarmsEnabled = boolcfg.AlarmVisual
	srv, em := newServer(t, cfg)
	invoke := func(alarms boolcfg.AlarmMode) error {
		_, err := srv.MatterInvoke(ctx, def.CmdEnableDisableAlarm, def.EnableDisableAlarmRequest{AlarmsToEnableDisable: alarms})
		return err
	}

	if got := status(t, invoke(boolcfg.AlarmAudible)); got != im.StatusConstraintError {
		t.Errorf("unsupported mode: %v, want CONSTRAINT_ERROR", got)
	}
	if err := srv.SetAlarmsActive(boolcfg.AlarmVisual); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(ctx, def.CmdSuppressAlarm, def.SuppressAlarmRequest{AlarmsToSuppress: boolcfg.AlarmVisual}); err != nil {
		t.Fatal(err)
	}
	em.take()
	d.calls = nil

	// Disabling the visual alarm clears it from active and suppressed, one
	// event.
	if err := invoke(0); err != nil {
		t.Fatal(err)
	}
	st := srv.State()
	if st.AlarmsEnabled != 0 || st.AlarmsActive != 0 || st.AlarmsSuppressed != 0 {
		t.Errorf("state %+v", st)
	}
	if !slices.Equal(d.calls, []string{"enabled", "active", "suppressed"}) {
		t.Errorf("delegate calls %v", d.calls)
	}
	evs := em.take()
	if len(evs) != 1 || evs[0].id != def.EventAlarmsStateChanged || evs[0].endpoint != 7 {
		t.Fatalf("events %+v", evs)
	}
	if e := evs[0].data.(def.AlarmsStateChangedEvent); e.AlarmsActive != 0 || e.AlarmsSuppressed == nil || *e.AlarmsSuppressed != 0 {
		t.Errorf("event %+v", e)
	}
	if v := read(t, srv, def.AttrAlarmsEnabled); v != uint8(0) {
		t.Errorf("AlarmsEnabled = %v", v)
	}

	// Re-enabling changes AlarmsEnabled only: nothing active to clear, no
	// event.
	if err := invoke(boolcfg.AlarmVisual); err != nil {
		t.Fatal(err)
	}
	if evs := em.take(); len(evs) != 0 {
		t.Errorf("events %+v", evs)
	}

	// A refused change answers FAILURE and changes nothing.
	d.refuse = true
	if got := status(t, invoke(0)); got != im.StatusFailure {
		t.Errorf("refused: %v", got)
	}
	if srv.State().AlarmsEnabled != boolcfg.AlarmVisual {
		t.Error("refused change applied")
	}
	if _, err := srv.MatterInvoke(ctx, def.CmdEnableDisableAlarm, "bogus"); status(t, err) != im.StatusInvalidCommand {
		t.Errorf("bogus payload: %v", err)
	}
}

// TestEnableDisableAlarmRefusals covers the delegate refusing the active
// and the suppressed change (chip .cpp:192-195, :204-207).
func TestEnableDisableAlarmRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, refuseAt := range []string{"active", "suppressed"} {
		d := &refuser{at: refuseAt}
		cfg := full(d)
		srv, _ := newServer(t, cfg)
		if err := srv.SetAlarmsActive(boolcfg.AlarmVisual); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.MatterInvoke(ctx, def.CmdSuppressAlarm, def.SuppressAlarmRequest{AlarmsToSuppress: boolcfg.AlarmVisual}); err != nil {
			t.Fatal(err)
		}
		_, err := srv.MatterInvoke(ctx, def.CmdEnableDisableAlarm, def.EnableDisableAlarmRequest{AlarmsToEnableDisable: boolcfg.AlarmAudible})
		if got := status(t, err); got != im.StatusFailure {
			t.Errorf("refuse %s: %v", refuseAt, got)
		}
	}
}

// refuser refuses one kind of delegate call.
type refuser struct{ at string }

func (r *refuser) CurrentSensitivityLevelChanged(context.Context, uint8) error { return nil }
func (r *refuser) AlarmsEnabledChanged(context.Context, boolcfg.AlarmMode) error {
	return nil
}

func (r *refuser) AlarmsActiveChanged(context.Context, boolcfg.AlarmMode) error {
	if r.at == "active" {
		return errors.New("no")
	}
	return nil
}

func (r *refuser) AlarmsSuppressedChanged(_ context.Context, m boolcfg.AlarmMode) error {
	if r.at == "suppressed" && m == 0 {
		return errors.New("no")
	}
	return nil
}

// TestSuppressAlarm follows chip .cpp:403-429.
func TestSuppressAlarm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := &delegate{}
	cfg := full(d)
	cfg.AlarmsSupported = boolcfg.AlarmVisual
	cfg.AlarmsEnabled = boolcfg.AlarmVisual
	srv, em := newServer(t, cfg)
	suppress := func(alarms boolcfg.AlarmMode) error {
		_, err := srv.MatterInvoke(ctx, def.CmdSuppressAlarm, def.SuppressAlarmRequest{AlarmsToSuppress: alarms})
		return err
	}
	if got := status(t, suppress(boolcfg.AlarmAudible)); got != im.StatusConstraintError {
		t.Errorf("unsupported: %v, want CONSTRAINT_ERROR", got)
	}
	if got := status(t, suppress(boolcfg.AlarmVisual)); got != im.StatusInvalidInState {
		t.Errorf("inactive: %v, want INVALID_IN_STATE", got)
	}
	if err := srv.SetAlarmsActive(boolcfg.AlarmVisual); err != nil {
		t.Fatal(err)
	}
	em.take()
	if err := suppress(boolcfg.AlarmVisual); err != nil {
		t.Fatal(err)
	}
	if v := read(t, srv, def.AttrAlarmsSuppressed); v != uint8(boolcfg.AlarmVisual) {
		t.Errorf("AlarmsSuppressed = %v", v)
	}
	if evs := em.take(); len(evs) != 1 {
		t.Errorf("events %+v", evs)
	}
	// Already suppressed: success, nothing else.
	d.calls = nil
	if err := suppress(boolcfg.AlarmVisual); err != nil || len(d.calls) != 0 || len(em.take()) != 0 {
		t.Errorf("no-op suppress: %v, calls %v", err, d.calls)
	}
	srv.ClearAllAlarms()
	if err := srv.SetAlarmsActive(boolcfg.AlarmVisual); err != nil {
		t.Fatal(err)
	}
	d.refuse = true
	if got := status(t, suppress(boolcfg.AlarmVisual)); got != im.StatusFailure {
		t.Errorf("refused: %v", got)
	}
	if _, err := srv.MatterInvoke(ctx, def.CmdSuppressAlarm, 3); status(t, err) != im.StatusInvalidCommand {
		t.Errorf("bogus payload: %v", err)
	}

	// Without SPRS the command is not accepted.
	cfg = full(nil)
	cfg.Features &^= boolcfg.FeatureAlarmSuppress
	plain, _ := newServer(t, cfg)
	if _, err := plain.MatterInvoke(ctx, def.CmdSuppressAlarm, def.SuppressAlarmRequest{}); status(t, err) != im.StatusUnsupportedCommand {
		t.Errorf("SuppressAlarm without SPRS: %v", err)
	}
}

// TestDeviceSideAlarms follows chip SetAlarmsActive (.cpp:339-357),
// SetAllEnabledAlarmsActive (:359-375) and ClearAllAlarms (:377-401).
func TestDeviceSideAlarms(t *testing.T) {
	t.Parallel()
	cfg := full(nil)
	cfg.AlarmsEnabled = boolcfg.AlarmVisual
	srv, em := newServer(t, cfg)
	if err := srv.SetAlarmsActive(boolcfg.AlarmAudible); !errors.Is(err, boolcfg.ErrNotEnabled) {
		t.Errorf("not enabled: %v", err)
	}
	if err := srv.SetAllEnabledAlarmsActive(); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetAlarmsActive(boolcfg.AlarmVisual); err != nil { // unchanged: no event
		t.Fatal(err)
	}
	if evs := em.take(); len(evs) != 1 {
		t.Errorf("events %+v", evs)
	}
	if v := read(t, srv, def.AttrAlarmsActive); v != uint8(boolcfg.AlarmVisual) {
		t.Errorf("AlarmsActive = %v", v)
	}
	srv.ClearAllAlarms()
	srv.ClearAllAlarms() // nothing to clear: no event
	if evs := em.take(); len(evs) != 1 {
		t.Errorf("events %+v", evs)
	}

	noAlarms, _ := newServer(t, boolcfg.Config{Features: boolcfg.FeatureSensitivityLevel})
	if err := noAlarms.SetAlarmsActive(0); !errors.Is(err, boolcfg.ErrNoAlarms) {
		t.Errorf("no VIS/AUD: %v", err)
	}

	// Without SPRS the event carries no AlarmsSuppressed.
	cfg = full(nil)
	cfg.Features &^= boolcfg.FeatureAlarmSuppress
	plain, em2 := newServer(t, cfg)
	if err := plain.SetAlarmsActive(boolcfg.AlarmAudible); err != nil {
		t.Fatal(err)
	}
	evs := em2.take()
	if len(evs) != 1 || evs[0].data.(def.AlarmsStateChangedEvent).AlarmsSuppressed != nil {
		t.Errorf("events %+v", evs)
	}
}

// TestSensorFault follows chip GenerateSensorFault (.cpp:302-319).
func TestSensorFault(t *testing.T) {
	t.Parallel()
	srv, em := newServer(t, full(nil))
	srv.SetSensorFault(boolcfg.SensorFaultGeneral)
	if v := read(t, srv, def.AttrSensorFault); v != uint16(boolcfg.SensorFaultGeneral) {
		t.Errorf("SensorFault = %v", v)
	}
	evs := em.take()
	if len(evs) != 1 || evs[0].id != def.EventSensorFault {
		t.Errorf("events %+v", evs)
	}
	if srv.State().SensorFault != boolcfg.SensorFaultGeneral {
		t.Error("state")
	}
	// Without FAULTEV and without the attribute nothing is emitted or
	// served, but the state still records it.
	quiet, em2 := newServer(t, boolcfg.Config{Features: boolcfg.FeatureVisual})
	quiet.SetSensorFault(boolcfg.SensorFaultGeneral)
	if len(em2.take()) != 0 || quiet.State().SensorFault != boolcfg.SensorFaultGeneral {
		t.Error("FAULTEV off")
	}
}

// TestConfigRefusals covers the configurations the model refuses.
func TestConfigRefusals(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]boolcfg.Config{
		"unknown alarm mode":      {Features: boolcfg.FeatureVisual, AlarmsSupported: 4},
		"enabled not supported":   {Features: boolcfg.FeatureVisual, AlarmsSupported: boolcfg.AlarmVisual, AlarmsEnabled: boolcfg.AlarmAudible},
		"audible without AUD":     {Features: boolcfg.FeatureVisual, AlarmsSupported: boolcfg.AlarmAudible},
		"SPRS without VIS or AUD": {Features: boolcfg.FeatureAlarmSuppress},
	} {
		if _, err := boolcfg.New(cfg); !errors.Is(err, boolcfg.ErrInvalidConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestNotificationsAndVersion holds the change notifications and the
// data version the generated server provides.
func TestNotificationsAndVersion(t *testing.T) {
	t.Parallel()
	srv, _ := newServer(t, full(nil))
	var seen []uint32
	unsubscribe := srv.OnMatterAttributesChanged(func(ids []uint32) { seen = append(seen, ids...) })
	before := srv.MatterDataVersion()
	if err := srv.SetAlarmsActive(boolcfg.AlarmVisual); err != nil {
		t.Fatal(err)
	}
	unsubscribe()
	if !slices.Equal(seen, []uint32{def.AttrAlarmsActive}) || srv.MatterDataVersion() == before {
		t.Errorf("notified %v, version %d → %d", seen, before, srv.MatterDataVersion())
	}
	if _, err := spec.New(def.Definition, spec.Options{}); err != nil {
		t.Fatal(err)
	}
}

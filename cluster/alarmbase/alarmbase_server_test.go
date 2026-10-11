// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package alarmbase_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarmbase"
	dwa "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	rfa "github.com/SukramJ/go-fabric/cluster/spec/refrigeratoralarm"
	tma "github.com/SukramJ/go-fabric/cluster/spec/temperaturealarm"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// emitter records the events a server emits.
type emitter struct{ events []emitted }

type emitted struct {
	endpoint uint16
	cluster  uint32
	event    uint32
	data     any
	priority contract.EventPriority
}

func (e *emitter) MatterEmitEvent(endpoint uint16, cluster, event uint32, data any, priority contract.EventPriority) {
	e.events = append(e.events, emitted{endpoint, cluster, event, data, priority})
}

// delegate records the commands it was asked about; err refuses them.
type delegate struct {
	resets, masks []uint32
	err           error
}

func (d *delegate) ResetAlarms(_ context.Context, alarms uint32) error {
	d.resets = append(d.resets, alarms)
	return d.err
}

func (d *delegate) ModifyEnabledAlarms(_ context.Context, mask uint32) error {
	d.masks = append(d.masks, mask)
	return d.err
}

const (
	inflow = uint32(dwa.AlarmInflowError)
	drain  = uint32(dwa.AlarmDrainError)
	door   = uint32(dwa.AlarmDoorError)
)

func newDishwasher(t *testing.T, cfg alarmbase.Config) (*alarmbase.Server, *emitter) {
	t.Helper()
	srv, err := alarmbase.NewDishwasherAlarm(cfg)
	if err != nil {
		t.Fatal(err)
	}
	em := &emitter{}
	srv.SetMatterEventEmitter(em)
	srv.SetEndpoint(7)
	return srv, em
}

func status(t *testing.T, err error) im.StatusCode {
	t.Helper()
	var sce im.StatusCodeError
	if !errors.As(err, &sce) {
		t.Fatalf("error %v carries no status", err)
	}
	return sce.MatterStatusCode()
}

func TestInitialValues(t *testing.T) {
	t.Parallel()
	srv, _ := newDishwasher(t, alarmbase.Config{Features: alarmbase.FeatureReset, Supported: inflow | drain | door, Latch: drain, Mask: inflow | drain, State: drain})
	for id, want := range map[uint32]uint32{
		alarmbase.AttrSupported: inflow | drain | door,
		alarmbase.AttrLatch:     drain,
		alarmbase.AttrMask:      inflow | drain,
		alarmbase.AttrState:     drain,
	} {
		if v, ok := srv.MatterRead(id); !ok || v != want {
			t.Errorf("attribute %d = %v (%v), want 0x%X", id, v, ok, want)
		}
	}
	// Latch is served with Reset only.
	plain, _ := newDishwasher(t, alarmbase.Config{Supported: inflow, Latch: inflow})
	if _, ok := plain.MatterRead(alarmbase.AttrLatch); ok {
		t.Error("Latch served without Reset")
	}
}

func TestConfigRefused(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]alarmbase.Config{
		"mask outside supported":  {Supported: inflow, Mask: drain},
		"state outside mask":      {Supported: inflow | drain, Mask: inflow, State: drain},
		"undefined alarm bit":     {Supported: 1 << 9},
		"temperature no thresh":   {},
		"temperature adjustable":  {Features: uint32(tma.FeatureUnderTemperature | tma.FeatureUnderCriticalAdjustable)},
		"refrigerator with reset": {Features: alarmbase.FeatureReset},
	} {
		var err error
		switch name {
		case "temperature no thresh":
			_, err = alarmbase.NewTemperatureAlarm(alarmbase.Config{Features: uint32(tma.FeatureOverTemperature)})
		case "temperature adjustable":
			_, err = alarmbase.NewTemperatureAlarm(cfg)
		case "refrigerator with reset":
			_, err = alarmbase.NewRefrigeratorAlarm(cfg)
		default:
			_, err = alarmbase.NewDishwasherAlarm(cfg)
		}
		if err == nil {
			t.Errorf("%s: built", name)
		}
	}
}

// TestSetStateNotify: a State change emits Notify with the bits that
// became active and inactive, the new State and the Mask; an unchanged
// State emits nothing (AlarmBaseCluster.cpp:73-80).
func TestSetStateNotify(t *testing.T) {
	t.Parallel()
	srv, em := newDishwasher(t, alarmbase.Config{Supported: inflow | drain | door, Mask: inflow | drain | door})
	if err := srv.SetState(inflow | door); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetState(inflow | drain); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetState(inflow | drain); err != nil {
		t.Fatal(err)
	}
	want := []dwa.NotifyEvent{
		{Active: dwa.AlarmBitmap(inflow | door), Inactive: 0, State: dwa.AlarmBitmap(inflow | door), Mask: dwa.AlarmBitmap(inflow | drain | door)},
		{Active: dwa.AlarmBitmap(drain), Inactive: dwa.AlarmBitmap(door), State: dwa.AlarmBitmap(inflow | drain), Mask: dwa.AlarmBitmap(inflow | drain | door)},
	}
	if len(em.events) != len(want) {
		t.Fatalf("%d events, want %d: %+v", len(em.events), len(want), em.events)
	}
	for i, e := range em.events {
		if e.endpoint != 7 || e.cluster != alarmbase.ClusterIDDishwasherAlarm || e.event != alarmbase.EventNotify || e.priority != contract.EventPriorityInfo {
			t.Errorf("event %d addressed %+v", i, e)
		}
		if e.data != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, e.data, want[i])
		}
	}
}

func TestSetStateRefused(t *testing.T) {
	t.Parallel()
	srv, em := newDishwasher(t, alarmbase.Config{Supported: inflow | drain, Mask: inflow})
	for _, st := range []uint32{door, drain} { // outside Supported; outside Mask
		if err := srv.SetState(st); !errors.Is(err, alarmbase.ErrNotSupported) {
			t.Errorf("SetState(0x%X) = %v", st, err)
		}
	}
	if srv.State() != 0 || len(em.events) != 0 {
		t.Errorf("state 0x%X, events %v", srv.State(), em.events)
	}
}

// TestLatch: with Reset, an active latched alarm stays active until a
// Reset clears it (AlarmBaseCluster.cpp:67-71, :84-91).
func TestLatch(t *testing.T) {
	t.Parallel()
	d := &delegate{}
	srv, em := newDishwasher(t, alarmbase.Config{Features: alarmbase.FeatureReset, Supported: inflow | drain, Latch: drain, Mask: inflow | drain, Delegate: d})
	if err := srv.SetState(inflow | drain); err != nil {
		t.Fatal(err)
	}
	if err := srv.SetState(0); err != nil {
		t.Fatal(err)
	}
	if srv.State() != drain {
		t.Fatalf("state 0x%X after clearing, want the latched 0x%X", srv.State(), drain)
	}
	if _, err := srv.MatterInvoke(context.Background(), alarmbase.CmdReset, dwa.ResetRequest{Alarms: dwa.AlarmBitmap(drain)}); err != nil {
		t.Fatal(err)
	}
	if srv.State() != 0 || !slices.Equal(d.resets, []uint32{drain}) {
		t.Errorf("state 0x%X, resets %v", srv.State(), d.resets)
	}
	if n := len(em.events); n != 3 {
		t.Errorf("%d events, want 3", n)
	}
}

func TestResetStatuses(t *testing.T) {
	t.Parallel()
	d := &delegate{}
	srv, _ := newDishwasher(t, alarmbase.Config{Features: alarmbase.FeatureReset, Supported: inflow, Latch: inflow, Mask: inflow, State: inflow, Delegate: d})
	ctx := context.Background()
	// Outside Supported: INVALID_COMMAND, the delegate is not asked.
	if _, err := srv.MatterInvoke(ctx, alarmbase.CmdReset, dwa.ResetRequest{Alarms: dwa.AlarmBitmap(drain)}); status(t, err) != im.StatusInvalidCommand {
		t.Errorf("Reset outside Supported: %v", err)
	}
	// The host refuses: FAILURE, State unchanged.
	d.err = errors.New("no")
	if _, err := srv.MatterInvoke(ctx, alarmbase.CmdReset, dwa.ResetRequest{Alarms: dwa.AlarmBitmap(inflow)}); status(t, err) != im.StatusFailure {
		t.Errorf("refused Reset: %v", err)
	}
	if srv.State() != inflow || len(d.resets) != 1 {
		t.Errorf("state 0x%X, resets %v", srv.State(), d.resets)
	}
	// Without Reset the command is not accepted.
	plain, _ := newDishwasher(t, alarmbase.Config{Supported: inflow})
	if _, err := plain.MatterInvoke(ctx, alarmbase.CmdReset, dwa.ResetRequest{}); status(t, err) != im.StatusUnsupportedCommand {
		t.Errorf("Reset without the feature: %v", err)
	}
}

// TestModifyEnabledAlarms: a new mask that disables an active alarm clears
// it from State, latch or not (AlarmBaseCluster.cpp:34-46, :177-190).
func TestModifyEnabledAlarms(t *testing.T) {
	t.Parallel()
	d := &delegate{}
	srv, em := newDishwasher(t, alarmbase.Config{
		Features: alarmbase.FeatureReset, ModifyEnabledAlarms: true, Delegate: d,
		Supported: inflow | drain, Latch: drain, Mask: inflow | drain, State: inflow | drain,
	})
	ctx := context.Background()
	if _, err := srv.MatterInvoke(ctx, alarmbase.CmdModifyEnabledAlarms, dwa.ModifyEnabledAlarmsRequest{Mask: dwa.AlarmBitmap(door)}); status(t, err) != im.StatusInvalidCommand {
		t.Errorf("mask outside Supported: %v", err)
	}
	if _, err := srv.MatterInvoke(ctx, alarmbase.CmdModifyEnabledAlarms, dwa.ModifyEnabledAlarmsRequest{Mask: dwa.AlarmBitmap(inflow)}); err != nil {
		t.Fatal(err)
	}
	if srv.Mask() != inflow || srv.State() != inflow {
		t.Errorf("mask 0x%X state 0x%X", srv.Mask(), srv.State())
	}
	if len(em.events) != 1 || em.events[0].data != (dwa.NotifyEvent{Inactive: dwa.AlarmBitmap(drain), State: dwa.AlarmBitmap(inflow), Mask: dwa.AlarmBitmap(inflow)}) {
		t.Errorf("events %+v", em.events)
	}
	d.err = errors.New("no")
	if _, err := srv.MatterInvoke(ctx, alarmbase.CmdModifyEnabledAlarms, dwa.ModifyEnabledAlarmsRequest{Mask: dwa.AlarmBitmap(drain)}); status(t, err) != im.StatusFailure {
		t.Errorf("refused: %v", err)
	}
	if srv.Mask() != inflow || !slices.Equal(d.masks, []uint32{inflow, drain}) {
		t.Errorf("mask 0x%X, asked %v", srv.Mask(), d.masks)
	}
	if err := srv.SetMask(door); !errors.Is(err, alarmbase.ErrNotSupported) {
		t.Errorf("SetMask outside Supported: %v", err)
	}
}

func TestNilDelegateAccepts(t *testing.T) {
	t.Parallel()
	srv, _ := newDishwasher(t, alarmbase.Config{ModifyEnabledAlarms: true, Supported: inflow | drain, Mask: inflow})
	if _, err := srv.MatterInvoke(context.Background(), alarmbase.CmdModifyEnabledAlarms, dwa.ModifyEnabledAlarmsRequest{Mask: dwa.AlarmBitmap(drain)}); err != nil {
		t.Fatal(err)
	}
	if srv.Mask() != drain {
		t.Errorf("mask 0x%X", srv.Mask())
	}
	if _, err := srv.MatterInvoke(context.Background(), alarmbase.CmdModifyEnabledAlarms, "junk"); status(t, err) != im.StatusInvalidCommand {
		t.Errorf("junk fields: %v", err)
	}
}

func TestRefrigeratorNotify(t *testing.T) {
	t.Parallel()
	open := uint32(rfa.AlarmDoorOpen)
	srv, err := alarmbase.NewRefrigeratorAlarm(alarmbase.Config{Supported: open, Mask: open})
	if err != nil {
		t.Fatal(err)
	}
	em := &emitter{}
	srv.SetMatterEventEmitter(em)
	if err := srv.SetState(open); err != nil {
		t.Fatal(err)
	}
	if len(em.events) != 1 || em.events[0].data != (rfa.NotifyEvent{Active: rfa.AlarmBitmap(open), State: rfa.AlarmBitmap(open), Mask: rfa.AlarmBitmap(open)}) {
		t.Errorf("events %+v", em.events)
	}
	if srv.Supported() != open || srv.Latch() != 0 {
		t.Errorf("supported 0x%X latch 0x%X", srv.Supported(), srv.Latch())
	}
}

func TestTemperatureThresholds(t *testing.T) {
	t.Parallel()
	f := uint32(tma.FeatureOverTemperature | tma.FeatureMajorThreshold)
	srv, err := alarmbase.NewTemperatureAlarm(alarmbase.Config{Features: f, Thresholds: allThresholds})
	if err != nil {
		t.Fatal(err)
	}
	got := srv.Thresholds()
	if len(got) != 2 || got[tma.AttrCriticalOverTemperatureThreshold] != 9000 || got[tma.AttrMajorOverTemperatureThreshold] != 7000 {
		t.Errorf("thresholds %v", got)
	}
	if err := srv.SetThresholds(map[uint32]int16{tma.AttrMajorOverTemperatureThreshold: 6500}); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(tma.AttrMajorOverTemperatureThreshold); v != int16(6500) {
		t.Errorf("MajorOver = %v", v)
	}
	if err := srv.SetThresholds(map[uint32]int16{tma.AttrMinorOverTemperatureThreshold: 1}); !errors.Is(err, alarmbase.ErrThresholds) {
		t.Errorf("unserved threshold: %v", err)
	}
	if err := srv.SetThresholds(map[uint32]int16{alarmbase.AttrMask: 1}); !errors.Is(err, alarmbase.ErrThresholds) {
		t.Errorf("not a threshold: %v", err)
	}
}

func TestReadOnly(t *testing.T) {
	t.Parallel()
	srv, _ := newDishwasher(t, alarmbase.Config{Supported: inflow})
	if err := srv.MatterWrite(context.Background(), alarmbase.AttrMask, uint32(0)); status(t, err) != im.StatusUnsupportedWrite {
		t.Errorf("write Mask: %v", err)
	}
	var seen []uint32
	stop := srv.OnMatterAttributesChanged(func(ids []uint32) { seen = append(seen, ids...) })
	defer stop()
	before := srv.MatterDataVersion()
	if err := srv.SetMask(inflow); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seen, []uint32{alarmbase.AttrMask}) || srv.MatterDataVersion() == before {
		t.Errorf("notified %v, data version %d → %d", seen, before, srv.MatterDataVersion())
	}
}

func TestResetLatchedAlarms(t *testing.T) {
	t.Parallel()
	srv, em := newDishwasher(t, alarmbase.Config{Features: alarmbase.FeatureReset, Supported: inflow | drain, Latch: inflow | drain, Mask: inflow | drain, State: inflow | drain})
	if err := srv.ResetLatchedAlarms(door); !errors.Is(err, alarmbase.ErrNotSupported) {
		t.Errorf("reset outside Supported: %v", err)
	}
	if err := srv.ResetLatchedAlarms(inflow); err != nil {
		t.Fatal(err)
	}
	if srv.State() != drain || len(em.events) != 1 {
		t.Errorf("state 0x%X, events %v", srv.State(), em.events)
	}
	if _, err := srv.MatterInvoke(context.Background(), alarmbase.CmdReset, "junk"); status(t, err) != im.StatusInvalidCommand {
		t.Errorf("junk Reset: %v", err)
	}
	// An unchanged Mask changes nothing, State included.
	if err := srv.SetMask(inflow | drain); err != nil || srv.State() != drain {
		t.Errorf("same mask: %v, state 0x%X", err, srv.State())
	}
	_, err := srv.MatterInvoke(context.Background(), alarmbase.CmdReset, dwa.ResetRequest{Alarms: dwa.AlarmBitmap(door)})
	if err == nil || err.Error() == "" {
		t.Errorf("status error %v", err)
	}
}

func TestTemperatureThresholdSet(t *testing.T) {
	t.Parallel()
	srv, err := alarmbase.NewTemperatureAlarm(alarmbase.Config{Features: uint32(tma.FeatureUnderTemperature), Thresholds: allThresholds})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.SetThresholds(map[uint32]int16{tma.AttrCriticalUnderTemperatureThreshold: -2000}); err != nil {
		t.Error(err)
	}
	if got := srv.Thresholds(); got[tma.AttrCriticalUnderTemperatureThreshold] != -2000 {
		t.Errorf("thresholds %v", got)
	}
}

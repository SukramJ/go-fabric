// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package alarmbase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/alarmbase"
	"github.com/SukramJ/go-fabric/cluster/spec"
	dwa "github.com/SukramJ/go-fabric/cluster/spec/dishwasheralarm"
	tma "github.com/SukramJ/go-fabric/cluster/spec/temperaturealarm"
)

// TestTypedSetRefusals: an attribute the server does not serve, a Latch
// other than the configured one and a Mask outside Supported are refused,
// and a patch that sets only a fixed attribute to its value changes nothing.
func TestTypedSetRefusals(t *testing.T) {
	t.Parallel()
	srv, err := alarmbase.NewDishwasherAlarm(alarmbase.Config{Features: uint32(dwa.FeatureReset), Supported: 0x3, Latch: 0x1})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.SetAttributes(map[uint32]any{0x7F: uint32(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("an unserved attribute: %v", err)
	}
	s := dwa.Wrap(srv)
	if err := s.Set(dwa.Patch{Latch: alarm(dwa.AlarmBitmap(0x2))}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("Latch 0x2: %v", err)
	}
	if err := s.Set(dwa.Patch{Mask: alarm(dwa.AlarmBitmap(0x4))}); !errors.Is(err, alarmbase.ErrNotSupported) {
		t.Errorf("Mask outside Supported: %v", err)
	}
	if err := s.Set(dwa.Patch{Supported: alarm(dwa.AlarmBitmap(0x3)), Latch: alarm(dwa.AlarmBitmap(0x1))}); err != nil {
		t.Errorf("the fixed values themselves: %v", err)
	}
	if st := s.Get(); st.Mask != 0 || st.State != 0 {
		t.Errorf("state %+v", st)
	}
}

// TestTypedSetThresholds: the host's TemperatureAlarm thresholds go through
// SetThresholds, alone or with the Mask.
func TestTypedSetThresholds(t *testing.T) {
	t.Parallel()
	f := uint32(tma.FeatureOverTemperature | tma.FeatureMajorThreshold)
	srv, err := alarmbase.NewTemperatureAlarm(alarmbase.Config{Features: f, Supported: 0x1, Thresholds: allThresholds})
	if err != nil {
		t.Fatal(err)
	}
	s := tma.Wrap(srv)
	if err := s.Set(tma.Patch{MajorOverTemperatureThreshold: alarm(int16(6000))}); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(tma.AttrMajorOverTemperatureThreshold); v != int16(6000) {
		t.Errorf("MajorOver = %v", v)
	}
	if err := s.Set(tma.Patch{CriticalOverTemperatureThreshold: alarm(int16(9500)), Mask: alarm(tma.AlarmBitmap(0x1))}); err != nil {
		t.Fatal(err)
	}
	if v, _ := srv.MatterRead(tma.AttrCriticalOverTemperatureThreshold); v != int16(9500) || srv.Mask() != 0x1 {
		t.Errorf("CriticalOver = %v, Mask 0x%X", v, srv.Mask())
	}
}

// TestGeneratedRequestsByPointer: the TemperatureAlarm commands take their
// generated requests by pointer as well as by value. (RefrigeratorAlarm's
// Reset and ModifyEnabledAlarms are disallowed, "X".)
func TestGeneratedRequestsByPointer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := uint32(tma.FeatureReset | tma.FeatureOverTemperature)
	temp, err := alarmbase.NewTemperatureAlarm(alarmbase.Config{Features: f, Supported: 0x1, Latch: 0x1, ModifyEnabledAlarms: true, Thresholds: allThresholds})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := temp.MatterInvoke(ctx, tma.CmdModifyEnabledAlarms, &tma.ModifyEnabledAlarmsRequest{Mask: 0x1}); err != nil || temp.Mask() != 0x1 {
		t.Fatalf("ModifyEnabledAlarms by pointer: %v, Mask 0x%X", err, temp.Mask())
	}
	if err := temp.SetState(0x1); err != nil {
		t.Fatal(err)
	}
	if err := temp.SetState(0x0); err != nil {
		t.Fatal(err)
	}
	if temp.State() != 0x1 {
		t.Fatalf("the latched alarm left State: 0x%X", temp.State())
	}
	if _, err := temp.MatterInvoke(ctx, tma.CmdReset, &tma.ResetRequest{Alarms: 0x1}); err != nil || temp.State() != 0 {
		t.Errorf("Reset by pointer: %v, State 0x%X", err, temp.State())
	}
}

// TestTemperatureAlarmWithoutADirection: a TemperatureAlarm needs OVER or
// UNDER (conformance O.a+); the definition refuses the empty selection.
func TestTemperatureAlarmWithoutADirection(t *testing.T) {
	t.Parallel()
	if _, err := alarmbase.NewTemperatureAlarm(alarmbase.Config{}); err == nil {
		t.Error("a TemperatureAlarm without OVER or UNDER was built")
	}
}

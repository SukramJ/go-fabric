// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package boolcfg_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	"github.com/SukramJ/go-fabric/cluster/spec"
	def "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
)

func pv[T any](v T) *T { return &v }

// TestTypedBooleanStateConfiguration drives the self-held server through
// its typed server: Set keeps chip's state machine and emits its events,
// the fixed attributes are refused, a host handler replaces
// SuppressAlarm, and OnWrite vetoes a CurrentSensitivityLevel write.
func TestTypedBooleanStateConfiguration(t *testing.T) {
	t.Parallel()
	srv, em := newServer(t, full(nil))
	s := def.Wrap(srv)
	var changed int
	defer s.OnChanged(func(def.State, []uint32) { changed++ })()

	if err := s.Set(def.Patch{CurrentSensitivityLevel: pv(uint8(2)), AlarmsActive: pv(boolcfg.AlarmVisual)}); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); st.CurrentSensitivityLevel != 2 || st.AlarmsActive != boolcfg.AlarmVisual {
		t.Errorf("state %+v", st)
	}
	if changed == 0 || len(em.take()) != 1 {
		t.Errorf("changes %d, want the AlarmsStateChanged event", changed)
	}
	// Disabling the visual alarm clears it from AlarmsActive.
	if err := s.Set(def.Patch{AlarmsEnabled: pv(boolcfg.AlarmAudible)}); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); st.AlarmsActive != 0 || st.AlarmsEnabled != boolcfg.AlarmAudible {
		t.Errorf("state after disabling %+v", st)
	}
	if evs := em.take(); len(evs) != 1 || evs[0].id != def.EventAlarmsStateChanged {
		t.Errorf("disabling an active alarm emitted %+v", evs)
	}
	// All or nothing: an active alarm that is not enabled is refused.
	if err := s.Set(def.Patch{CurrentSensitivityLevel: pv(uint8(0)), AlarmsActive: pv(boolcfg.AlarmVisual)}); !errors.Is(err, boolcfg.ErrNotEnabled) || s.Get().CurrentSensitivityLevel != 2 {
		t.Errorf("a refused patch: %v, level %d", err, s.Get().CurrentSensitivityLevel)
	}
	if err := s.Set(def.Patch{SupportedSensitivityLevels: pv(uint8(4))}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("SupportedSensitivityLevels: %v", err)
	}

	var seen []def.SuppressAlarmRequest
	if err := s.HandleSuppressAlarm(func(_ context.Context, req def.SuppressAlarmRequest) (struct{}, error) {
		seen = append(seen, req)
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), def.CmdSuppressAlarm, &def.SuppressAlarmRequest{AlarmsToSuppress: boolcfg.AlarmAudible}); err != nil || len(seen) != 1 {
		t.Errorf("host SuppressAlarm: %v, %v", err, seen)
	}

	if err := s.EmitSensorFault(7, def.SensorFaultEvent{SensorFault: 1}); err != nil {
		t.Fatal(err)
	}
	if evs := em.take(); len(evs) != 1 || evs[0].id != def.EventSensorFault {
		t.Errorf("events %+v", evs)
	}

	refusal := errors.New("not now")
	if err := s.OnWrite(func(context.Context, def.State, uint32) error { return refusal }); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterWrite(context.Background(), def.AttrCurrentSensitivityLevel, uint64(1)); !errors.Is(err, refusal) || s.Get().CurrentSensitivityLevel != 2 {
		t.Errorf("a vetoed write: %v, level %d", err, s.Get().CurrentSensitivityLevel)
	}
}

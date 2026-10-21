// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package boolcfg_test

import (
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/boolcfg"
	"github.com/SukramJ/go-fabric/cluster/spec"
	def "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
)

// TestTypedSetRules: every rule of the host's Set — the served attributes,
// the sensitivity bound, AlarmsEnabled within AlarmsSupported, AlarmsSuppressed
// within AlarmsActive — refuses before anything is stored; the fixed
// attributes accept their own value; SensorFault goes out as its event.
func TestTypedSetRules(t *testing.T) {
	t.Parallel()
	srv, em := newServer(t, full(nil))
	s := def.Wrap(srv)
	if err := srv.SetAttributes(map[uint32]any{0x7F: uint8(1)}); !errors.Is(err, spec.ErrNotServed) {
		t.Errorf("an unserved attribute: %v", err)
	}
	if err := s.Set(def.Patch{CurrentSensitivityLevel: pv(uint8(3))}); !errors.Is(err, boolcfg.ErrInvalidConfig) {
		t.Errorf("CurrentSensitivityLevel 3 of 3: %v", err)
	}
	if err := s.Set(def.Patch{DefaultSensitivityLevel: pv(uint8(1)), AlarmsSupported: pv(boolcfg.AlarmVisual | boolcfg.AlarmAudible)}); err != nil {
		t.Errorf("the fixed attributes' own values: %v", err)
	}
	if err := s.Set(def.Patch{DefaultSensitivityLevel: pv(uint8(2))}); !errors.Is(err, spec.ErrNotHeld) {
		t.Errorf("DefaultSensitivityLevel 2: %v", err)
	}

	em.take()
	if err := s.Set(def.Patch{AlarmsActive: pv(boolcfg.AlarmVisual), AlarmsSuppressed: pv(boolcfg.AlarmVisual)}); err != nil {
		t.Fatal(err)
	}
	if st := s.Get(); st.AlarmsActive != boolcfg.AlarmVisual || st.AlarmsSuppressed != boolcfg.AlarmVisual {
		t.Errorf("state %+v", st)
	}
	if evs := em.take(); len(evs) != 1 || evs[0].id != def.EventAlarmsStateChanged {
		t.Errorf("events %+v", evs)
	}
	if err := s.Set(def.Patch{AlarmsActive: pv(boolcfg.AlarmAudible)}); !errors.Is(err, boolcfg.ErrInvalidConfig) {
		t.Errorf("AlarmsSuppressed outside AlarmsActive: %v", err)
	}
	if err := s.Set(def.Patch{SensorFault: pv(def.SensorFaultBitmap(1))}); err != nil {
		t.Fatal(err)
	}
	if evs := em.take(); len(evs) != 1 || evs[0].id != def.EventSensorFault {
		t.Errorf("SensorFault events %+v", evs)
	}

	narrow := full(nil)
	narrow.AlarmsSupported, narrow.AlarmsEnabled = boolcfg.AlarmVisual, boolcfg.AlarmVisual
	srv2, _ := newServer(t, narrow)
	if err := def.Wrap(srv2).Set(def.Patch{AlarmsEnabled: pv(boolcfg.AlarmAudible)}); !errors.Is(err, boolcfg.ErrInvalidConfig) {
		t.Errorf("AlarmsEnabled outside AlarmsSupported: %v", err)
	}
}

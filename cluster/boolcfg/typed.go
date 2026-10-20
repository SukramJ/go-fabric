// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package boolcfg

import (
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	def "github.com/SukramJ/go-fabric/cluster/spec/booleanstateconfiguration"
)

// The typed server's optional interfaces: Handle, Emit and SetBeforeWrite
// come from the embedded [spec.SelfHeld]; SetAttributes keeps chip's state
// machine.
var (
	_ spec.AttributeSetter   = (*Server)(nil)
	_ spec.CommandRegistrar  = (*Server)(nil)
	_ spec.EventEmitting     = (*Server)(nil)
	_ spec.BeforeWriteSetter = (*Server)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the host's own change,
// all or nothing — every value is checked against its attribute's type and
// the server's rules before any is applied — and no Delegate is asked.
//   - CurrentSensitivityLevel is [Server.SetCurrentSensitivityLevel]'s:
//     below SupportedSensitivityLevels (chip .cpp:324).
//   - AlarmsEnabled is a subset of AlarmsSupported; an alarm it disables
//     leaves AlarmsActive and AlarmsSuppressed, as EnableDisableAlarm's
//     state change does without its delegate calls (.cpp:155-206).
//   - AlarmsActive is [Server.SetAlarmsActive]'s: enabled alarms only
//     (.cpp:339-357); AlarmsSuppressed is a subset of AlarmsActive, with
//     SPRS.
//   - SensorFault is [Server.SetSensorFault]'s: the SensorFault event goes
//     out with it (.cpp:302-319).
//
// A change of the alarm state emits AlarmsStateChanged. SupportedSensitivityLevels,
// DefaultSensitivityLevel and AlarmsSupported are the configuration's: a
// different value is refused ([spec.ErrNotHeld]).
func (s *Server) SetAttributes(values map[uint32]any) error {
	ids := slices.Sorted(maps.Keys(values))
	checked := make(map[uint32]any, len(ids))
	for _, id := range ids {
		v, err := s.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		checked[id] = v
	}
	st := s.State()
	level, enabled, active, suppressed := st.CurrentSensitivityLevel, st.AlarmsEnabled, st.AlarmsActive, st.AlarmsSuppressed
	var fault *SensorFault
	for _, id := range ids {
		switch v := checked[id]; id {
		case def.AttrSupportedSensitivityLevels, def.AttrDefaultSensitivityLevel, def.AttrAlarmsSupported:
			if fixed, _ := s.srv.Value(id); !sameUint8(v, fixed) {
				return fmt.Errorf("%w: boolcfg: attribute 0x%04X is fixed at %v", spec.ErrNotHeld, id, fixed)
			}
		case def.AttrCurrentSensitivityLevel:
			level, _ = v.(uint8)
			if level >= s.supported {
				return fmt.Errorf("%w: CurrentSensitivityLevel %d ≥ %d", ErrInvalidConfig, level, s.supported)
			}
		case def.AttrAlarmsEnabled:
			enabled = AlarmMode(asUint8(v))
		case def.AttrAlarmsActive:
			active = AlarmMode(asUint8(v))
		case def.AttrAlarmsSuppressed:
			suppressed = AlarmMode(asUint8(v))
		case def.AttrSensorFault:
			n, _ := v.(uint16) // a map16 per CheckSet
			f := SensorFault(n)
			fault = &f
		}
	}
	if _, set := checked[def.AttrAlarmsEnabled]; set {
		if enabled&^s.alarmsSup != 0 {
			return fmt.Errorf("%w: AlarmsEnabled 0x%02X outside AlarmsSupported 0x%02X", ErrInvalidConfig, enabled, s.alarmsSup)
		}
		if _, a := checked[def.AttrAlarmsActive]; !a {
			active &= enabled
		}
		if _, sp := checked[def.AttrAlarmsSuppressed]; !sp {
			suppressed &= enabled
		}
	}
	if active&^enabled != 0 {
		return fmt.Errorf("%w: 0x%02X (enabled 0x%02X)", ErrNotEnabled, active, enabled)
	}
	if suppressed&^active != 0 {
		return fmt.Errorf("%w: AlarmsSuppressed 0x%02X outside AlarmsActive 0x%02X", ErrInvalidConfig, suppressed, active)
	}
	s.mu.Lock()
	alarmsMoved := s.active != active || s.suppressed != suppressed
	s.current, s.enabled, s.active, s.suppressed = level, enabled, active, suppressed
	s.mu.Unlock()
	s.publish(map[uint32]any{
		def.AttrCurrentSensitivityLevel: level,
		def.AttrAlarmsEnabled:           enabled,
		def.AttrAlarmsActive:            active,
		def.AttrAlarmsSuppressed:        suppressed,
	})
	if alarmsMoved && s.hasAlarms() {
		s.alarmsStateChanged()
	}
	if fault != nil {
		s.SetSensorFault(*fault)
	}
	return nil
}

// asUint8 is a checked 8-bit bitmap in its stored form.
func asUint8(v any) uint8 {
	n, _ := v.(uint8)
	return n
}

// sameUint8 compares a checked 8-bit value with a stored one of the
// attribute's Go type (a uint8 or the bitmap type the server stores).
func sameUint8(checked, stored any) bool {
	switch v := stored.(type) {
	case uint8:
		return checked == v
	case AlarmMode:
		return checked == uint8(v)
	default:
		return false
	}
}

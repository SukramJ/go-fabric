// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package appliance

import (
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	ldc "github.com/SukramJ/go-fabric/cluster/spec/laundrydryercontrols"
	lwc "github.com/SukramJ/go-fabric/cluster/spec/laundrywashercontrols"
)

// The typed server's optional interfaces: Handle, Emit and SetBeforeWrite
// come from the embedded [spec.SelfHeld]; SetAttributes holds the
// servers' rules.
var (
	_ spec.AttributeSetter   = (*WasherServer)(nil)
	_ spec.CommandRegistrar  = (*WasherServer)(nil)
	_ spec.EventEmitting     = (*WasherServer)(nil)
	_ spec.BeforeWriteSetter = (*WasherServer)(nil)
	_ spec.AttributeSetter   = (*DryerServer)(nil)
	_ spec.CommandRegistrar  = (*DryerServer)(nil)
	_ spec.EventEmitting     = (*DryerServer)(nil)
	_ spec.BeforeWriteSetter = (*DryerServer)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the host's own change
// of SpinSpeedCurrent and NumberOfRinses, all or nothing, checked as
// [WasherServer.SetSpinSpeedCurrent] and [WasherServer.SetNumberOfRinses]
// check them, stored and reported; the Listener is not called.
// SpinSpeeds and SupportedRinses are the configuration's: a different
// value is refused ([spec.ErrNotHeld]).
func (s *WasherServer) SetAttributes(values map[uint32]any) error {
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		switch id {
		case lwc.AttrSpinSpeeds, lwc.AttrSupportedRinses:
			if fixed, _ := s.srv.Value(id); !reflect.DeepEqual(v, fixed) {
				return fmt.Errorf("%w: laundrywashercontrols: attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
			}
		case lwc.AttrSpinSpeedCurrent:
			var spin *uint8
			if n, ok := v.(uint8); ok {
				spin = &n
			}
			if err := s.checkSpin(spin); err != nil {
				return err
			}
		case lwc.AttrNumberOfRinses:
			n, _ := v.(uint8)
			if err := s.checkRinses(NumberOfRinses(n)); err != nil {
				return err
			}
		}
	}
	return s.srv.SetAttributes(values)
}

// SetAttributes implements [spec.AttributeSetter]: the host's own change
// of SelectedDrynessLevel, checked as [DryerServer.SetSelectedDrynessLevel]
// checks it, stored and reported; the Listener is not called.
// SupportedDrynessLevels is the configuration's: a different value is
// refused ([spec.ErrNotHeld]).
func (s *DryerServer) SetAttributes(values map[uint32]any) error {
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		switch id {
		case ldc.AttrSupportedDrynessLevels:
			if fixed, _ := s.srv.Value(id); !reflect.DeepEqual(v, fixed) {
				return fmt.Errorf("%w: laundrydryercontrols: attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
			}
		case ldc.AttrSelectedDrynessLevel:
			var level *DrynessLevel
			if n, ok := v.(uint8); ok {
				l := DrynessLevel(n)
				level = &l
			}
			if err := s.checkLevel(level); err != nil {
				return err
			}
		}
	}
	return s.srv.SetAttributes(values)
}

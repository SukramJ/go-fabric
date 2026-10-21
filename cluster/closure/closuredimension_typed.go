// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure

import (
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	cd "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
)

// The typed server's optional interfaces: Handle, Emit and SetBeforeWrite
// come from the embedded [spec.Hooks]; SetAttributes is the setters'.
var (
	_ spec.AttributeSetter   = (*DimensionServer)(nil)
	_ spec.CommandRegistrar  = (*DimensionServer)(nil)
	_ spec.EventEmitting     = (*DimensionServer)(nil)
	_ spec.BeforeWriteSetter = (*DimensionServer)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the device's own
// report, all or nothing — every value is checked against the definition
// and the rules of its setter before any is applied: CurrentState
// ([DimensionServer.SetCurrentState], with its quiet reporting),
// TargetState ([DimensionServer.SetTargetState]), UnitRange
// ([DimensionServer.SetUnitRange]) and LimitRange
// ([DimensionServer.SetLimitRange]). The other attributes are the
// configuration's ([spec.ErrNotHeld]).
func (s *DimensionServer) SetAttributes(values map[uint32]any) error {
	var apply []func() error
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		switch id {
		case cd.AttrCurrentState, cd.AttrTargetState:
			st, ok := optionalStruct[DimensionState](v)
			if !ok {
				return fmt.Errorf("%w: closure: attribute 0x%04X %T", spec.ErrInvalidValue, id, v)
			}
			if st != nil {
				if err := s.checkState(*st, id == cd.AttrTargetState); err != nil {
					return err
				}
			}
			if id == cd.AttrCurrentState {
				apply = append(apply, func() error { return s.SetCurrentState(st) })
			} else {
				apply = append(apply, func() error { return s.SetTargetState(st) })
			}
		case cd.AttrUnitRange:
			r, ok := optionalStruct[UnitRange](v)
			if !ok {
				return fmt.Errorf("%w: closure: UnitRange %T", spec.ErrInvalidValue, v)
			}
			if err := s.checkUnitRange(r); err != nil {
				return err
			}
			apply = append(apply, func() error { return s.SetUnitRange(r) })
		case cd.AttrLimitRange:
			r, ok := structValue[LimitRange](v)
			if !ok {
				return fmt.Errorf("%w: closure: LimitRange %T", spec.ErrInvalidValue, v)
			}
			if err := s.checkLimitRange(r); err != nil {
				return err
			}
			apply = append(apply, func() error { return s.SetLimitRange(r) })
		default:
			return fmt.Errorf("%w: closure: attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
		}
	}
	for _, fn := range apply {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

// optionalStruct is a checked nullable struct value: nil for null.
func optionalStruct[T any](v any) (*T, bool) {
	if v == nil {
		return nil, true
	}
	t, ok := structValue[T](v)
	if !ok {
		return nil, false
	}
	return &t, true
}

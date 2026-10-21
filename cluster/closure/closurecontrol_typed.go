// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure

import (
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
)

// The typed server's optional interfaces: Handle, Emit and SetBeforeWrite
// come from the embedded [spec.Hooks]; SetAttributes is the setters'.
var (
	_ spec.AttributeSetter   = (*ControlServer)(nil)
	_ spec.CommandRegistrar  = (*ControlServer)(nil)
	_ spec.EventEmitting     = (*ControlServer)(nil)
	_ spec.BeforeWriteSetter = (*ControlServer)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the drive's own report,
// all or nothing — every value is checked against the definition first.
//   - OverallCurrentState's Position follows
//     [ControlServer.SetCurrentPosition] (null between the stops; the
//     server derives SecureState and emits SecureStateChanged).
//   - MainState Error with a non-empty CurrentErrorList is
//     [ControlServer.ReportError] (the OperationalError event); Disengaged,
//     and leaving it, is [ControlServer.SetEngaged] where the closure is
//     manually operable; any other MainState [ControlServer.SetMainState]
//     (Moving to Stopped emits MovementCompleted).
//   - CurrentErrorList alone is [ControlServer.SetErrorList].
//
// OverallTargetState is the commands' ([spec.ErrNotHeld]).
func (s *ControlServer) SetAttributes(values map[uint32]any) error {
	var (
		pos              *wire.ClosureCurrentPosition
		state            wire.ClosureMainState
		errs             wire.ClosureErrorList
		setPos, setState bool
		setErrs          bool
	)
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.inst.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		switch id {
		case closuredef.AttrMainState:
			n, _ := v.(uint8)
			state, setState = wire.ClosureMainState(n), true
		case closuredef.AttrCurrentErrorList:
			list, ok := v.([]closuredef.ClosureErrorEnum)
			if !ok && v != nil {
				return fmt.Errorf("%w: closure: CurrentErrorList %T", spec.ErrInvalidValue, v)
			}
			errs = make(wire.ClosureErrorList, 0, len(list))
			for _, e := range list {
				errs = append(errs, wire.ClosureError(e))
			}
			setErrs = true
		case closuredef.AttrOverallCurrentState:
			st, ok := structValue[closuredef.OverallCurrentStateStruct](v)
			if !ok {
				return fmt.Errorf("%w: closure: OverallCurrentState %T", spec.ErrInvalidValue, v)
			}
			if p := st.Position; p != nil && !p.Null {
				cp := wire.ClosureCurrentPosition(p.Value)
				pos = &cp
			}
			setPos = true
		default:
			return fmt.Errorf("%w: closure: attribute 0x%04X is the commands'", spec.ErrNotHeld, id)
		}
	}
	if setPos {
		s.SetCurrentPosition(pos)
	}
	switch {
	case setState && state == wire.ClosureMainStateError && len(errs) > 0:
		s.ReportError(errs)
		return nil
	case setState && state == wire.ClosureMainStateDisengaged && s.inst.Emits(closuredef.EventEngageStateChanged):
		s.SetEngaged(false)
	case setState && s.disengaged() && s.inst.Emits(closuredef.EventEngageStateChanged):
		s.SetEngaged(true)
		s.SetMainState(state)
	case setState:
		s.SetMainState(state)
	}
	if setErrs {
		s.SetErrorList(errs)
	}
	return nil
}

// disengaged reports whether MainState is Disengaged.
func (s *ControlServer) disengaged() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mainState == wire.ClosureMainStateDisengaged
}

// structValue is a checked struct value, by value or pointer.
func structValue[T any](v any) (T, bool) {
	switch x := v.(type) {
	case T:
		return x, true
	case *T:
		if x != nil {
			return *x, true
		}
	}
	var zero T
	return zero, false
}

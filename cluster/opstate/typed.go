// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package opstate

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// The typed server's optional interfaces ([spec.AttributeSetter],
// [spec.CommandRegistrar], [spec.EventEmitting], [spec.BeforeWriteSetter]):
// the typed server of each of the three clusters (operationalstatedef,
// rvcoperationalstatedef, ovencavityoperationalstatedef) reaches the
// server's own setters through them.
var (
	_ spec.AttributeSetter   = (*Server)(nil)
	_ spec.CommandRegistrar  = (*Server)(nil)
	_ spec.EventEmitting     = (*Server)(nil)
	_ spec.BeforeWriteSetter = (*Server)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the host's own change,
// all or nothing — every value is checked against the definition and the
// rules of its setter before any is applied. OperationalError follows
// [Server.SetOperationalError] (a value other than NoError moves the state
// to Error and emits the OperationalError event), OperationalState
// [Server.SetOperationalState] (applied after OperationalError, so leaving
// Error clears it), PhaseList and CurrentPhase [Server.SetPhaseList] and
// [Server.SetCurrentPhase], CountdownTime [Server.SetCountdownTime].
// OperationalStateList is the configuration's ([spec.ErrNotHeld]).
func (s *Server) SetAttributes(values map[uint32]any) error {
	var (
		st                                  State
		es                                  ErrorState
		phases                              []string
		cur                                 *uint8
		count                               *uint32
		setState, setErr, setPhases, setCur bool
		setCount                            bool
	)
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.inst.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		switch id {
		case AttrOperationalState:
			n, _ := uintOf(v)
			st, setState = State(n), true //nolint:gosec // the definition checked the enum's width
		case AttrOperationalError:
			e, ok := errorStateOf(v)
			if !ok {
				return fmt.Errorf("%w: opstate: OperationalError %T", spec.ErrInvalidValue, v)
			}
			es, setErr = e, true
		case AttrPhaseList:
			p, ok := stringsOf(v)
			if !ok {
				return fmt.Errorf("%w: opstate: PhaseList %T", spec.ErrInvalidValue, v)
			}
			phases, setPhases = p, true
		case AttrCurrentPhase:
			if n, ok := uintOf(v); ok {
				c := uint8(n) //nolint:gosec // the definition checked uint8
				cur = &c
			}
			setCur = true
		case AttrCountdownTime:
			if n, ok := uintOf(v); ok {
				c := uint32(n) //nolint:gosec // the definition checked elapsed-s
				count = &c
			}
			setCount = true
		default:
			return fmt.Errorf("%w: opstate: attribute 0x%04X is the configuration's", spec.ErrNotHeld, id)
		}
	}
	if err := s.checkSet(st, setState, es, setErr, phases, setPhases, cur, setCur, count, setCount); err != nil {
		return err
	}
	if setPhases {
		if !setCur {
			cur = s.phase()
		}
		if err := s.SetPhaseList(phases, cur); err != nil {
			return err
		}
	} else if setCur {
		if err := s.SetCurrentPhase(cur); err != nil {
			return err
		}
	}
	if setErr {
		if err := s.SetOperationalError(es); err != nil {
			return err
		}
	}
	if setState {
		if err := s.SetOperationalState(st); err != nil {
			return err
		}
	}
	if setCount {
		return s.SetCountdownTime(count)
	}
	return nil
}

// checkSet is SetAttributes' check of every value before any is applied:
// the rules each setter applies.
func (s *Server) checkSet(st State, setState bool, es ErrorState, setErr bool, phases []string, setPhases bool,
	cur *uint8, setCur bool, count *uint32, setCount bool,
) error {
	if setState && !slices.ContainsFunc(s.states, func(e StateEntry) bool { return e.ID == st }) {
		return fmt.Errorf("%w: 0x%02X", ErrStateNotListed, uint8(st))
	}
	if setErr {
		if err := s.checkErrorState(es); err != nil {
			return err
		}
	}
	switch {
	case setPhases:
		if !setCur {
			cur = s.phase()
		}
		if err := checkPhases(phases, cur); err != nil {
			return err
		}
		if s.v.phases != nil {
			for _, p := range phases {
				if !slices.Contains(s.v.phases, p) {
					return fmt.Errorf("%w: %q", ErrPhaseNotAllowed, p)
				}
			}
		}
	case setCur:
		s.mu.Lock()
		n := len(s.phases)
		s.mu.Unlock()
		valid := (n == 0 && cur == nil) || (n > 0 && cur != nil && int(*cur) < n)
		if !valid {
			return fmt.Errorf("%w: %v for %d phases", ErrCurrentPhase, cur, n)
		}
	}
	if setCount && (!s.countOn || (count != nil && *count > CountdownTimeMax)) {
		return ErrCountdownTime
	}
	return nil
}

// phase is the current CurrentPhase.
func (s *Server) phase() *uint8 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentPhase == nil {
		return nil
	}
	c := *s.currentPhase
	return &c
}

// Handle implements [spec.CommandRegistrar]: the host's handler of cmdID
// replaces the server's own handling of it — the state check included —
// as a matter.js host overrides the behavior's command method; nil hands
// it back ([spec.HostHandlers]).
func (s *Server) Handle(cmdID uint32, fn spec.CommandHandler) error {
	return s.hooks.Register(s.inst, cmdID, fn)
}

// Emit implements [spec.EventEmitting]: the host's own event of the
// cluster, on endpoint at the priority the definition gives it. An event
// outside the EventList is [spec.ErrNotEmitted]; an OperationCompletion's
// CompletionErrorCode must be one of the cluster's error states. Without
// an emitter the event is dropped. OperationalError is emitted by
// [Server.SetOperationalError] on its own; emitted here it changes no
// attribute.
func (s *Server) Emit(endpoint uint16, eventID uint32, data any) error {
	if !s.inst.Emits(eventID) {
		return fmt.Errorf("%w: %s event 0x%02X", spec.ErrNotEmitted, s.v.def.Name, eventID)
	}
	if eventID == EventOperationCompletion {
		if code, ok := uintOf(fieldOf(data, "CompletionErrorCode")); ok && !s.v.validError(ErrorID(code)) { //nolint:gosec // the generated field is a uint8 enum
			return fmt.Errorf("%w: 0x%02X", ErrUnknownError, code)
		}
	}
	s.mu.Lock()
	emitter := s.emitter
	s.mu.Unlock()
	if emitter != nil {
		emitter.MatterEmitEvent(endpoint, s.inst.MatterClusterID(), eventID, data, s.inst.EventPriority(eventID))
	}
	return nil
}

// SetBeforeWrite implements [spec.BeforeWriteSetter]. Every attribute of
// the cluster is read-only, so no controller write reaches a veto; it is
// kept for the typed server's one shape.
func (s *Server) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	s.veto.Set(fn)
}

// uintOf is a stored unsigned value (the width type of an enum or
// integer); false for null or another kind.
func uintOf(v any) (uint64, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return 0, false
	}
	switch rv.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint(), true
	case reflect.Pointer:
		if rv.IsNil() {
			return 0, false
		}
		return uintOf(rv.Elem().Interface())
	default:
		return 0, false
	}
}

// fieldOf is the field name of the struct v (or of the struct v points
// to), the name compared without case (the generated types spell "Id",
// cluster/wire "ID"); nil when there is none.
func fieldOf(v any, name string) any {
	rv := reflect.Indirect(reflect.ValueOf(v))
	if rv.Kind() != reflect.Struct {
		return nil
	}
	f := rv.FieldByNameFunc(func(n string) bool { return strings.EqualFold(n, name) })
	if !f.IsValid() {
		return nil
	}
	return f.Interface()
}

// stringOf is a string field's value: the string, or what a *string
// points to ("" for nil).
func stringOf(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case *string:
		if s != nil {
			return *s
		}
	}
	return ""
}

// errorStateOf reads an ErrorStateStruct of any of the three clusters'
// generated types (or the wire one) into an [ErrorState].
func errorStateOf(v any) (ErrorState, bool) {
	id, ok := uintOf(fieldOf(v, "ErrorStateID"))
	if !ok {
		return ErrorState{}, false
	}
	return ErrorState{
		ID:      ErrorID(id), //nolint:gosec // the generated field is a uint8 enum
		Label:   stringOf(fieldOf(v, "ErrorStateLabel")),
		Details: stringOf(fieldOf(v, "ErrorStateDetails")),
	}, true
}

// stringsOf is a stored list of strings; nil (null) is a nil list.
func stringsOf(v any) ([]string, bool) {
	if v == nil {
		return nil, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return nil, false
	}
	out := make([]string, 0, rv.Len())
	for i := range rv.Len() {
		e := rv.Index(i)
		if e.Kind() == reflect.Interface {
			e = e.Elem()
		}
		if e.Kind() != reflect.String {
			return nil, false
		}
		out = append(out, e.String())
	}
	return out, true
}

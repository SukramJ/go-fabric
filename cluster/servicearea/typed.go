// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package servicearea

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	sa "github.com/SukramJ/go-fabric/cluster/spec/servicearea"
)

// The typed server's optional interfaces over [spec.Hooks]; SetAttributes
// holds matter.js's assertions.
var (
	_ spec.AttributeSetter   = (*Server)(nil)
	_ spec.CommandRegistrar  = (*Server)(nil)
	_ spec.EventEmitting     = (*Server)(nil)
	_ spec.BeforeWriteSetter = (*Server)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the device's own
// change, all or nothing. The values are held to matter.js's assertions in
// its initialize order — SupportedMaps, SupportedAreas (against the new
// maps), SelectedAreas, CurrentArea and Progress (against the new areas)
// — before any is applied, and reported as one change; EstimatedEndTime
// follows [Server.SetEstimatedEndTime] (its quiet reporting), and a null
// CurrentArea makes it null too.
func (s *Server) SetAttributes(values map[uint32]any) error {
	var (
		areas    []Area
		mapList  []Map
		selected []uint32
		current  *uint32
		end      *uint32
		progress []Progress
		set      = map[uint32]bool{}
	)
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.CheckSet(id, values[id], nil)
		if err != nil {
			return err
		}
		ok := true
		switch id {
		case sa.AttrSupportedAreas:
			areas, ok = listValue[Area](v)
		case sa.AttrSupportedMaps:
			mapList, ok = listValue[Map](v)
		case sa.AttrSelectedAreas:
			selected, ok = listValue[uint32](v)
		case sa.AttrCurrentArea:
			current = uint32Ptr(v)
		case sa.AttrEstimatedEndTime:
			end = uint32Ptr(v)
		case sa.AttrProgress:
			progress, ok = listValue[Progress](v)
		}
		if !ok {
			return fmt.Errorf("%w: servicearea: attribute 0x%04X %T", spec.ErrInvalidValue, id, v)
		}
		set[id] = true
	}
	if err := s.setState(set, areas, mapList, selected, current, progress); err != nil {
		return err
	}
	switch {
	case set[sa.AttrEstimatedEndTime]:
		return s.SetEstimatedEndTime(end)
	case set[sa.AttrCurrentArea] && current == nil && s.Serves(sa.AttrEstimatedEndTime):
		return s.SetEstimatedEndTime(nil)
	}
	return nil
}

// setState checks and stores the list and CurrentArea values of a Set
// under the lock, restoring the state when one is refused, and reports the
// ones it stored.
func (s *Server) setState(set map[uint32]bool, areas []Area, mapList []Map, selected []uint32, current *uint32, progress []Progress) error {
	s.mu.Lock()
	oldAreas, oldMaps, oldSelected, oldCurrent, oldProgress := s.areas, s.maps, s.selected, s.current, s.progress
	steps := []struct {
		id    uint32
		check func() error
		store func()
	}{
		{sa.AttrSupportedMaps, func() error { return s.checkMaps(mapList) }, func() { s.maps = slices.Clone(mapList) }},
		{sa.AttrSupportedAreas, func() error { return s.checkAreas(areas) }, func() { s.areas = slices.Clone(areas) }},
		{sa.AttrSelectedAreas, func() error { return s.checkSelected(selected) }, func() { s.selected = slices.Clone(selected) }},
		{sa.AttrCurrentArea, func() error { return s.checkCurrent(current) }, func() { s.current = clonePtr(current) }},
		{sa.AttrProgress, func() error { return s.checkProgress(progress) }, func() { s.progress = slices.Clone(progress) }},
	}
	var changed []uint32
	for _, st := range steps {
		if !set[st.id] {
			continue
		}
		if err := st.check(); err != nil {
			s.areas, s.maps, s.selected, s.current, s.progress = oldAreas, oldMaps, oldSelected, oldCurrent, oldProgress
			s.mu.Unlock()
			return err
		}
		st.store()
		changed = append(changed, st.id)
	}
	s.mu.Unlock()
	if len(changed) > 0 {
		slices.Sort(changed)
		s.changed(changed...)
	}
	return nil
}

// listValue is a checked list value — a []T or a list type of it, such
// as a spec.List; nil is an empty list.
func listValue[T any](v any) ([]T, bool) {
	switch x := v.(type) {
	case nil:
		return nil, true
	case []T:
		return x, true
	}
	rv, t := reflect.ValueOf(v), reflect.TypeFor[[]T]()
	if rv.Kind() == reflect.Slice && rv.Type().ConvertibleTo(t) {
		out, _ := rv.Convert(t).Interface().([]T)
		return out, true
	}
	return nil, false
}

// uint32Ptr is a checked nullable uint32: nil for null.
func uint32Ptr(v any) *uint32 {
	if n, ok := v.(uint32); ok {
		return &n
	}
	return nil
}

// Handle implements [spec.CommandRegistrar] ([spec.Hooks]).
func (s *Server) Handle(cmdID uint32, fn spec.CommandHandler) error { return s.hooks.Handle(cmdID, fn) }

// Emit implements [spec.EventEmitting]: ServiceArea defines no event, so
// every one is [spec.ErrNotEmitted].
func (s *Server) Emit(endpoint uint16, eventID uint32, data any) error {
	return s.hooks.Emit(endpoint, eventID, data)
}

// SetBeforeWrite implements [spec.BeforeWriteSetter] ([spec.Hooks]).
func (s *Server) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	s.hooks.SetBeforeWrite(fn)
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
)

// The typed server's optional interfaces ([spec.AttributeSetter],
// [spec.CommandRegistrar], [spec.EventEmitting], [spec.BeforeWriteSetter]).
var (
	_ spec.AttributeSetter   = (*Server)(nil)
	_ spec.CommandRegistrar  = (*Server)(nil)
	_ spec.EventEmitting     = (*Server)(nil)
	_ spec.BeforeWriteSetter = (*Server)(nil)
	_ contract.EventReceiver = (*Server)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the host's own change,
// all or nothing, checked against each attribute's type and constraint and
// reported — no write veto, no port write refusal. An attribute set to the
// value it holds is no change and is not reported; the set advances the
// DataVersion once, or not at all when it changed nothing. CurrentLevel is set
// directly ([New]'s level; a server over a [LevelSource] reads the level
// from the port, so it is not the server's to set: [spec.ErrNotHeld]); a
// running transition is not stopped — its next step overwrites the value,
// as matter.js's next step overwrites a state write
// (packages/node/src/behavior/Transitions.ts:455-534, :537-574); matter.js
// ends the transition only when the written value equals its target
// (:659-693 #instrumentProperty), which the engine does not mirror. OnLevel
// and Options go to the port (the held state of [New]) first — a write the
// port refuses undoes the port's writes before it, so the set stays all or
// nothing —, StartUpCurrentLevel is the server's, and MinLevel and
// MaxLevel accept their fixed values only.
// RemainingTime is derived: [spec.ErrNotHeld].
func (s *Server) SetAttributes(values map[uint32]any) error {
	ids := slices.Sorted(maps.Keys(values))
	checked := make([]attrSet, 0, len(ids))
	for _, id := range ids {
		v, err := s.inst.CheckSet(id, values[id], s.peer)
		if err != nil {
			return err
		}
		switch id {
		case AttrCurrentLevel:
			if s.held == nil {
				return fmt.Errorf("%w: levelcontrol: CurrentLevel is the LevelSource's", spec.ErrNotHeld)
			}
		case AttrRemainingTime:
			return fmt.Errorf("%w: levelcontrol: RemainingTime is derived from the running transition", spec.ErrNotHeld)
		case AttrMinLevel, AttrMaxLevel:
			if fixed, _ := s.MatterRead(id); v != fixed {
				return fmt.Errorf("%w: levelcontrol: attribute 0x%04X is fixed at %v", spec.ErrNotHeld, id, fixed)
			}
			continue
		case AttrOnLevel, AttrOptions:
			if s.src == nil {
				return errNoSource("SetAttributes")
			}
		}
		checked = append(checked, attrSet{id, v})
	}
	// One set is one commit: an attribute counts as changed only when it
	// differs from before, as the controller's write does
	// ([Server.MatterWrite]), and the DataVersion advances once for the
	// set however many attributes it changed, CurrentLevel's included.
	// Mirrors matter.js
	// packages/node/src/behavior/state/managed/Datasource.ts:1456-1457 at
	// db021da0 (the version advances once `if (changes)`) and :1476-1481
	// (#noteIfChanged notes nothing for an equal value).
	changed, err := s.setOnPort(checked)
	if err != nil {
		return err
	}
	level := false
	for _, c := range checked {
		switch c.id {
		case AttrCurrentLevel:
			s.held.setLevel(setUint8(c.v))
			level = true
		case AttrStartUpCurrentLevel:
			next := setUint8(c.v)
			s.mu.Lock()
			differs := !equalLevel(s.startUp, next)
			s.startUp = next
			s.mu.Unlock()
			if differs {
				changed = append(changed, c.id)
			}
		}
	}
	if len(changed) > 0 {
		s.tracker().Bump()
		s.changes.NotifyFrom(contract.OriginHostSet, changed...)
	}
	if level {
		// the one place CurrentLevel is reported; it advances the
		// DataVersion only when the set advanced none
		s.noteLevel(contract.OriginHostSet, len(changed) > 0)
	}
	return nil
}

// attrSet is one checked attribute of a [Server.SetAttributes] set.
type attrSet struct {
	id uint32
	v  any
}

// setOnPort writes the set's Options and OnLevel to the port and returns
// the attributes it changed. The port's writes go first in a set, each
// undone when a later one is refused, so the set is all or nothing over a
// port as well (R2-P6-api-1410). An undo the port refuses as well leaves
// its value changed: the error says so, and the change is reported.
func (s *Server) setOnPort(checked []attrSet) ([]uint32, error) {
	var changed []uint32
	var undo []func() error
	refuse := func(err error) error {
		var kept []uint32
		for i := len(undo) - 1; i >= 0; i-- {
			if uerr := undo[i](); uerr != nil {
				err = errors.Join(err, uerr)
				kept = append(kept, changed[i])
			}
		}
		if len(kept) > 0 {
			slices.Sort(kept)
			s.tracker().Bump()
			s.changes.NotifyFrom(contract.OriginHostSet, kept...)
		}
		return err
	}
	for _, c := range checked {
		switch c.id {
		case AttrOptions:
			options, _ := c.v.(uint8) // a map8 per CheckSet
			before := s.src.Options()
			if err := s.src.SetOptions(context.Background(), options); err != nil {
				return nil, refuse(fmt.Errorf("levelcontrol: set Options: %w", err))
			}
			if s.src.Options() != before {
				changed = append(changed, c.id)
				undo = append(undo, func() error { return s.src.SetOptions(context.Background(), before) })
			}
		case AttrOnLevel:
			before, knownBefore := s.src.OnLevel()
			if err := s.src.SetOnLevel(context.Background(), setUint8(c.v)); err != nil {
				return nil, refuse(fmt.Errorf("levelcontrol: set OnLevel: %w", err))
			}
			if after, known := s.src.OnLevel(); known != knownBefore || (known && after != before) {
				changed = append(changed, c.id)
				var prev *uint8
				if knownBefore {
					prev = &before
				}
				undo = append(undo, func() error { return s.src.SetOnLevel(context.Background(), prev) })
			}
		}
	}
	return changed, nil
}

// setUint8 is a checked nullable uint8 in its stored form: nil for
// null.
func setUint8(v any) *uint8 {
	b, ok := v.(uint8)
	if !ok {
		return nil
	}
	return &b
}

// Handle implements [spec.CommandRegistrar]. A scene recall reaches the
// server's own logic, never fn ([Server.MatterApplySceneValues]); a host
// that keeps the server's gate, transition and scene invalidation
// implements the [LevelSource] port instead.
func (s *Server) Handle(cmdID uint32, fn spec.CommandHandler) error {
	return s.host.Register(s.inst, cmdID, fn)
}

// Emit implements [spec.EventEmitting]. LevelControl defines no event, so
// every event is refused with [spec.ErrNotEmitted].
func (s *Server) Emit(endpoint uint16, eventID uint32, data any) error {
	return s.events.Emit(s.inst, endpoint, eventID, data)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) { s.events.SetEmitter(emitter) }

// SetBeforeWrite implements [spec.BeforeWriteSetter]: fn vetoes a
// controller's write after the definition's checks and before it reaches
// the port, with the value in its stored form.
func (s *Server) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	s.veto.Set(fn)
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol

import (
	"context"
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
// reported — no write veto, no port write refusal. CurrentLevel is set
// directly ([New]'s level; a server over a [LevelSource] reads the level
// from the port, so it is not the server's to set: [spec.ErrNotHeld]); a
// running transition is not stopped — its next step overwrites the value,
// as matter.js's next step overwrites a state write
// (packages/node/src/behavior/Transitions.ts:455-534, :537-574). OnLevel
// and Options go to the port (the held state of [New]), StartUpCurrentLevel
// is the server's, and MinLevel and MaxLevel accept their fixed values only.
// RemainingTime is derived: [spec.ErrNotHeld].
func (s *Server) SetAttributes(values map[uint32]any) error {
	type set struct {
		id uint32
		v  any
	}
	ids := slices.Sorted(maps.Keys(values))
	checked := make([]set, 0, len(ids))
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
		checked = append(checked, set{id, v})
	}
	var changed []uint32
	level := false
	for _, c := range checked {
		switch c.id {
		case AttrCurrentLevel:
			s.held.setLevel(setUint8(c.v))
			level = true
		case AttrOptions:
			options, _ := c.v.(uint8) // a map8 per CheckSet
			if err := s.src.SetOptions(context.Background(), options); err != nil {
				return fmt.Errorf("levelcontrol: set Options: %w", err)
			}
			changed = append(changed, c.id)
		case AttrOnLevel:
			if err := s.src.SetOnLevel(context.Background(), setUint8(c.v)); err != nil {
				return fmt.Errorf("levelcontrol: set OnLevel: %w", err)
			}
			changed = append(changed, c.id)
		case AttrStartUpCurrentLevel:
			s.mu.Lock()
			s.startUp = setUint8(c.v)
			s.mu.Unlock()
			changed = append(changed, c.id)
		}
	}
	if len(changed) > 0 {
		s.tracker().Bump()
		s.changes.Notify(changed...)
	}
	if level {
		s.observeLevel()
	}
	return nil
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

// Handle implements [spec.CommandRegistrar]: fn replaces the server's own
// handling of cmdID — the request's checks, the options gate and the
// transition included — nil hands it back, and a command the server does
// not accept is refused with [spec.ErrNotAccepted]. A matter.js host
// overrides the command's logic method instead (moveToLevelLogic,
// LevelControlServer.ts:256-261), keeping the checks.
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

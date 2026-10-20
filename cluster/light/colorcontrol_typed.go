// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/contract"
)

// The typed server's optional interfaces ([spec.AttributeSetter],
// [spec.CommandRegistrar], [spec.EventEmitting], [spec.BeforeWriteSetter]).
var (
	_ spec.AttributeSetter   = (*ColorControlServer)(nil)
	_ spec.CommandRegistrar  = (*ColorControlServer)(nil)
	_ spec.EventEmitting     = (*ColorControlServer)(nil)
	_ spec.BeforeWriteSetter = (*ColorControlServer)(nil)
	_ contract.EventReceiver = (*ColorControlServer)(nil)
)

// SetAttributes implements [spec.AttributeSetter]: the host's own change
// of the colour state, Options or StartUpColorTemperatureMireds, all or
// nothing, each value checked against its attribute's type and
// constraint and reported as the server reports its own changes — the
// "Q" attributes by their quieter rules. The writers are not called and
// no write veto runs. A colour state set while a transition or colour
// loop runs does not stop it: its next step overwrites the value, as
// matter.js's next step overwrites a state write
// (packages/node/src/behavior/Transitions.ts:455-534, :537-574), and a
// command whose writers are running does not store its own colour over
// the host's (as [ColorControlServer.Set]). ColorMode and
// EnhancedColorMode move together: one set alone sets the other to the
// same mode (EnhancedColorMode's enhanced hue is ColorMode's hue and
// saturation), as matter.js's setColorMode sets both; a mode of a feature
// the server lacks is refused ([spec.ErrInvalidValue]).
//
// RemainingTime is the running transition's and NumberOfPrimaries,
// ColorCapabilities, the physical colour temperature bounds and
// CoupleColorTempToLevelMinMireds are the configuration's: a value other
// than the one they read is refused with [spec.ErrNotHeld].
func (s *ColorControlServer) SetAttributes(values map[uint32]any) error {
	checked, err := s.checkSet(values)
	if err != nil || len(checked) == 0 {
		return err
	}
	s.commit.Lock()
	defer s.commit.Unlock()
	s.mu.Lock()
	prev := s.st
	next := prev
	next.apply(checked)
	var plain []uint32
	if v, ok := checked[ccdef.AttrOptions].(uint8); ok && v != s.options {
		s.options = v
		plain = append(plain, ccdef.AttrOptions)
	}
	if v, ok := checked[ccdef.AttrStartUpColorTemperatureMireds]; ok {
		var m *uint16
		if n, isNum := v.(uint16); isNum {
			m = &n
		}
		if !equalMireds(s.startUpMireds, m) {
			s.startUpMireds = m
			plain = append(plain, ccdef.AttrStartUpColorTemperatureMireds)
		}
	}
	if next != prev {
		s.st = next
		// A command whose writers run now does not store its colour over
		// the host's ([ColorControlServer.update]).
		s.setGen++
	}
	s.mu.Unlock()
	if next != prev {
		s.report(prev, next)
	}
	if len(plain) > 0 {
		s.changes.Notify(plain...)
	}
	return nil
}

// checkSet checks every value of a [ColorControlServer.SetAttributes] and
// returns the held ones in their stored form.
func (s *ColorControlServer) checkSet(values map[uint32]any) (map[uint32]any, error) {
	checked := make(map[uint32]any, len(values))
	for _, id := range slices.Sorted(maps.Keys(values)) {
		v, err := s.inst.CheckSet(id, values[id], s.MatterRead)
		if err != nil {
			return nil, err
		}
		switch id {
		case ccdef.AttrRemainingTime:
			return nil, fmt.Errorf("%w: colorcontrol: RemainingTime is derived from the running transition", spec.ErrNotHeld)
		case ccdef.AttrNumberOfPrimaries, ccdef.AttrColorCapabilities, ccdef.AttrColorTempPhysicalMinMireds,
			ccdef.AttrColorTempPhysicalMaxMireds, ccdef.AttrCoupleColorTempToLevelMinMireds:
			if cur, _ := s.MatterRead(id); v != cur {
				return nil, fmt.Errorf("%w: colorcontrol: attribute 0x%04X is the configuration's (%v)", spec.ErrNotHeld, id, cur)
			}
			continue
		case ccdef.AttrColorMode, ccdef.AttrEnhancedColorMode:
			if m, _ := v.(uint8); !s.supportsColorMode(ColorMode(m)) {
				return nil, fmt.Errorf("%w: colorcontrol: colour mode %d has no feature here", spec.ErrInvalidValue, m)
			}
		}
		checked[id] = v
	}
	return checked, nil
}

// apply sets the colour state attributes checked holds; ColorMode and
// EnhancedColorMode follow each other when only one is given.
func (st *colorState) apply(checked map[uint32]any) {
	for id, dst := range map[uint32]*uint8{
		ccdef.AttrCurrentHue: &st.hue, ccdef.AttrCurrentSaturation: &st.saturation,
		ccdef.AttrColorLoopActive: &st.loopActive, ccdef.AttrColorLoopDirection: &st.loopDirection,
	} {
		if v, ok := checked[id].(uint8); ok {
			*dst = v
		}
	}
	for id, dst := range map[uint32]*uint16{
		ccdef.AttrEnhancedCurrentHue: &st.enhancedHue, ccdef.AttrCurrentX: &st.x, ccdef.AttrCurrentY: &st.y,
		ccdef.AttrColorTemperatureMireds: &st.mireds, ccdef.AttrColorLoopTime: &st.loopTime,
		ccdef.AttrColorLoopStartEnhancedHue: &st.loopStart, ccdef.AttrColorLoopStoredEnhancedHue: &st.loopStored,
	} {
		if v, ok := checked[id].(uint16); ok {
			*dst = v
		}
	}
	mode, hasMode := checked[ccdef.AttrColorMode].(uint8)
	enhanced, hasEnhanced := checked[ccdef.AttrEnhancedColorMode].(uint8)
	switch {
	case hasMode && hasEnhanced:
		st.mode, st.enhancedMode = ColorMode(mode), ColorMode(enhanced)
	case hasMode:
		st.mode, st.enhancedMode = ColorMode(mode), ColorMode(mode)
	case hasEnhanced:
		st.mode, st.enhancedMode = colorModeOf(ColorMode(enhanced)), ColorMode(enhanced)
	}
}

func equalMireds(a, b *uint16) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Handle implements [spec.CommandRegistrar]: fn replaces the server's own
// handling of cmdID — the request's checks, the ExecuteIfOff gate, the
// colour mode switch, the transition and the writers — as a matter.js
// host overrides the ColorControlServer command method; fn runs outside
// the server's command queue. nil hands the command back. A command the
// feature selection does not accept is refused with [spec.ErrNotAccepted].
func (s *ColorControlServer) Handle(cmdID uint32, fn spec.CommandHandler) error {
	return s.host.Register(s.inst, cmdID, fn)
}

// Emit implements [spec.EventEmitting]. ColorControl defines no event, so
// every event is refused with [spec.ErrNotEmitted].
func (s *ColorControlServer) Emit(endpoint uint16, eventID uint32, data any) error {
	return s.events.Emit(s.inst, endpoint, eventID, data)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *ColorControlServer) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.events.SetEmitter(emitter)
}

// SetBeforeWrite implements [spec.BeforeWriteSetter]: fn vetoes a
// controller's write of Options or StartUpColorTemperatureMireds after the
// definition's checks and before it is stored, with the value in its
// stored form.
func (s *ColorControlServer) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	s.veto.Set(fn)
}

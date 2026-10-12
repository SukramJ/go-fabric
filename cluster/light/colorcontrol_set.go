// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package light

import "log/slog"

// Set records a colour the device took on itself — a program on the
// device, a wall transmitter, or the device confirming a different value
// than the one written: the twin of onoff.Server.Set. c.Mode is the
// EnhancedColorMode the device reports; ColorMode follows from it. The
// mode's own fields of c become the current colour (hue and saturation,
// enhanced hue and saturation, x and y, or the colour temperature cropped
// to the physical range as every colour temperature the server stores
// is); on a mode change the other modes' fields are first converted as a
// command's mode switch converts them (switchColorMode). Every attribute
// that changed is reported through the change notifier, the "Q" ones by
// their quieter rules. The writers are not called.
//
// A device-side change ends what the controller asked for, so the colour
// movement stops and RemainingTime reports 0 as StopMoveStep does — except
// a running colour loop, which only ColorLoopSet ends. The stop runs on
// the command queue after Set returns, and no transition step changes the
// colour while it is pending.
//
// Set takes only the server's state lock, never the command queue or the
// writers' serialisation: it may be called from inside a writer, on the
// writer's goroutine, and from any goroutine while a writer runs, without
// blocking. A colour equal to the current one, or to the one a writer is
// being given, is no change and stops nothing. A colour of a mode the
// server has no feature for is logged and ignored.
//
// matter.js has no device-originated path: a host writes the state
// attributes, and no observer of ColorControlServer stops a transition on
// such a write (packages/node/src/behavior/Transitions.ts:659-693
// #instrumentProperty finishes a transition only when the written value is
// its target).
func (s *ColorControlServer) Set(c Color) {
	if !s.supportsColorMode(c.Mode) {
		slog.Default().Warn("matter.colorcontrol.inbound_color_mode_ignored",
			slog.Int("color_mode", int(c.Mode)),
			slog.String("reason", "the server has no feature for this colour mode"))
		return
	}
	s.mu.Lock()
	prev := s.st
	next := s.deviceColor(prev, c)
	// An unchanged value is no change (matter.js
	// packages/node/src/behavior/state/managed/Datasource.ts:673-675
	// skips a deep-equal value on commit).
	if next == prev || (s.inflight != nil && s.deviceColor(*s.inflight, c) == *s.inflight) {
		s.mu.Unlock()
		return
	}
	s.st = next
	s.setGen++
	spawn := !s.haltPending
	s.haltPending = true
	s.mu.Unlock()
	s.report(prev, next)
	if spawn {
		go s.haltAfterSet()
	}
}

// deviceColor is st with the device's colour c: the mode switch's
// conversion, then the mode's own fields of c, each cropped to its
// attribute's range. The caller holds mu.
func (s *ColorControlServer) deviceColor(st colorState, c Color) colorState {
	mode := colorModeOf(c.Mode)
	s.switchColorMode(&st, st.mode, mode)
	st.mode, st.enhancedMode = mode, c.Mode
	switch c.Mode {
	case ColorModeHueSaturation:
		st.hue = min(c.Hue, maxHueValue)
		st.saturation = min(c.Saturation, maxSaturationValue)
	case ColorModeEnhancedHueSaturation:
		st.enhancedHue = c.EnhancedHue
		st.saturation = min(c.Saturation, maxSaturationValue)
	case ColorModeXY:
		st.x, st.y = min(c.X, maxCIEXYValue), min(c.Y, maxCIEXYValue)
	case ColorModeColorTemperature:
		s.setMireds(&st, float64(c.ColorTemperatureMireds))
	}
	return st
}

// lockCommands takes the command queue and first runs the stop a Set
// left pending, so a command never starts behind a movement the device
// has ended.
func (s *ColorControlServer) lockCommands() {
	s.cmd.Lock()
	s.haltIfPending()
}

// haltAfterSet runs the stop a Set asked for on the command queue.
func (s *ColorControlServer) haltAfterSet() {
	s.cmd.Lock()
	defer s.cmd.Unlock()
	s.haltIfPending()
}

// haltIfPending stops the colour movement a Set ended. The caller holds
// cmd. The flag clears only after the stop, so no step lands in between.
func (s *ColorControlServer) haltIfPending() {
	s.mu.Lock()
	pending := s.haltPending
	s.mu.Unlock()
	if !pending {
		return
	}
	s.stopMoveStepLogic()
	s.mu.Lock()
	s.haltPending = false
	s.mu.Unlock()
}

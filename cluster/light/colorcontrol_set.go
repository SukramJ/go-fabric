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
// their quieter rules, and a Read returns the new colour at once. The
// writers are not called. A colour equal to the current one is no change,
// and so is the device confirming the colour a writer is being given —
// compared in the device's own colour mode, the in-flight colour
// converted into it as switchColorMode converts, within one attribute
// unit (the quantisation of the conversions, issue #79): a device that
// takes hue and saturation echoes an x/y write as hue and saturation. A
// colour of a mode the server has no feature for is logged and ignored.
//
// Set does not stop a running transition or colour loop and leaves
// RemainingTime alone: the movement's next step overwrites the device's
// value, as matter.js's next step overwrites an external state write
// (packages/node/src/behavior/Transitions.ts:455-534 step computes from
// the transition's own currentValue, :537-574 determineUpdates /
// applyUpdates write it). A late echo of a value written earlier is
// therefore stored and then overwritten by the next step. matter.js also
// finishes a transition when an external write equals its target
// (Transitions.ts:659-693 #instrumentProperty); that is not mirrored,
// because finishing needs the engine lock the writers run under, and Set
// may be called from inside a writer.
//
// Set takes only the server's state lock, never the command queue or the
// writers' serialisation: it may be called from inside a writer, on the
// writer's goroutine, and from any goroutine while a writer runs, without
// blocking. When a writer calls Set with a colour other than the one it
// is given, the device's colour stays stored and the command answers
// SUCCESS: the device accepted the command and reported what it did.
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
	if s.inflight != nil {
		if s.confirms(*s.inflight, c) {
			s.mu.Unlock()
			return
		}
		// The device contradicts the colour being written: its colour
		// stands, whether or not it is the one stored before the write.
		s.setGen++
	}
	// An unchanged value is no change (matter.js
	// packages/node/src/behavior/state/managed/Datasource.ts:759-761
	// skips a deep-equal value on commit).
	if next == prev {
		s.mu.Unlock()
		return
	}
	s.st = next
	s.setGen++
	s.mu.Unlock()
	s.report(prev, next)
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

// confirms reports whether the device's colour c is the in-flight colour
// st seen in c's colour mode: st converted into that mode
// (switchColorMode), the mode's own fields compared within one attribute
// unit, hue around the wrap. The caller holds mu.
func (s *ColorControlServer) confirms(st colorState, c Color) bool {
	mode := colorModeOf(c.Mode)
	s.switchColorMode(&st, st.mode, mode)
	near := func(a, b uint16) bool { return a == b || a+1 == b || b+1 == a }
	switch c.Mode {
	case ColorModeHueSaturation:
		dh := absDiff(uint16(st.hue), uint16(min(c.Hue, maxHueValue)))
		return (dh <= 1 || dh >= maxHueValue-1) && near(uint16(st.saturation), uint16(min(c.Saturation, maxSaturationValue)))
	case ColorModeEnhancedHueSaturation:
		return near(st.enhancedHue, c.EnhancedHue) && near(uint16(st.saturation), uint16(min(c.Saturation, maxSaturationValue)))
	case ColorModeXY:
		return near(st.x, min(c.X, maxCIEXYValue)) && near(st.y, min(c.Y, maxCIEXYValue))
	case ColorModeColorTemperature:
		return near(st.mireds, uint16(s.cropMireds(float64(c.ColorTemperatureMireds))))
	}
	return false
}

func absDiff(a, b uint16) uint16 {
	if a > b {
		return a - b
	}
	return b - a
}

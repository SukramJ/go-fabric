// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light

import (
	"context"
	"math"

	"github.com/SukramJ/go-fabric/contract"
)

// setColorMode is matter.js setColorMode (ColorControlServer.ts:1411-1420
// at 0ceb202d): a command of another colour mode first switches to it —
// every colour movement stops and the current colour is converted into
// the new mode's attributes (switchColorMode, :1454-1540) — and ColorMode
// and EnhancedColorMode both take the new mode. The switch changes state
// only, as matter.js's does: the device shows the same colour before and
// after, so the writers are not driven with the converted colour; the
// command's own movement writes the target (issue #79). A command whose
// movement then fails rolls the switch back ([ColorControlServer.MatterInvoke]).
func (s *ColorControlServer) setColorMode(ctx context.Context, mode ColorMode) error {
	if s.state().mode == mode {
		return nil
	}
	s.stopAllColorMovement()
	s.convert(contract.OriginFrom(ctx), func(st *colorState) {
		s.switchColorMode(st, st.mode, mode)
		st.mode = mode
		st.enhancedMode = mode
	})
	return nil
}

// convert changes the colour state without the writers: the attributes
// take the new values and what changed is reported, from origin.
func (s *ColorControlServer) convert(origin contract.Origin, change func(st *colorState)) {
	s.commit.Lock()
	defer s.commit.Unlock()
	s.mu.Lock()
	prev := s.st
	next := prev
	change(&next)
	if next == prev {
		s.mu.Unlock()
		return
	}
	s.st = next
	s.mu.Unlock()
	s.report(origin, prev, next)
}

// setEnhancedColorMode is matter.js setEnhancedColorMode: the enhanced hue
// mode switches ColorMode to hue and saturation, then EnhancedColorMode to
// the enhanced one. As in matter.js, a light already in the hue and
// saturation mode switches without a conversion, and a hue command in the
// enhanced mode leaves EnhancedColorMode enhanced (setColorMode finds
// ColorMode unchanged).
func (s *ColorControlServer) setEnhancedColorMode(ctx context.Context, mode ColorMode) error {
	if s.state().enhancedMode == mode {
		return nil
	}
	if err := s.setColorMode(ctx, colorModeOf(mode)); err != nil {
		return err
	}
	s.convert(contract.OriginFrom(ctx), func(st *colorState) { st.enhancedMode = mode })
	return nil
}

// switchColorMode is matter.js switchColorMode: the colour of the old mode
// converted into the attributes of the new one, "as close as possible", by
// the conversions of ColorConversionUtils.ts (colorconversion.go). A
// colour temperature outside 1000 K to 20009 K has no x/y and converts to
// nothing; so does a value the arithmetic cannot carry.
//
// The XY → hue and saturation branch converts with xyToHsv. matter.js
// calls hsvToXy with the x and y there (ColorControlServer.ts:1493 at the
// schema pin, unchanged at HEAD), a transcription error that reads x as
// degrees and y as a saturation; the module does not carry it over
// (notes/parity/by_design.md BD-Matter-ColorControl-XYToHS).
func (s *ColorControlServer) switchColorMode(st *colorState, oldMode, newMode ColorMode) {
	if oldMode == newMode {
		return
	}
	hue := float64(st.hue) * 360 / 254
	saturation := float64(st.saturation) / 254
	x, y := float64(st.x)/65536, float64(st.y)/65536
	switch oldMode {
	case ColorModeHueSaturation:
		switch newMode {
		case ColorModeXY:
			st.x, st.y = xyAttribute(HSVToXY(hue, saturation))
		case ColorModeColorTemperature:
			s.setMireds(st, HSVToMireds(hue, saturation))
		default: // nothing to convert to
		}
	case ColorModeXY:
		switch newMode {
		case ColorModeHueSaturation:
			h, sat := XYToHSV(x, y) // matter.js: hsvToXy, see above
			st.hue, st.saturation = hueAttribute(h), saturationAttribute(sat)
		case ColorModeColorTemperature:
			s.setMireds(st, XYToMireds(x, y))
		default: // nothing to convert to
		}
	case ColorModeColorTemperature:
		mireds := float64(st.mireds)
		switch newMode {
		case ColorModeHueSaturation:
			if h, sat, ok := MiredsToHSV(mireds); ok {
				st.hue, st.saturation = hueAttribute(h), saturationAttribute(sat)
			}
		case ColorModeXY:
			if cx, cy, ok := MiredsToXY(mireds); ok {
				st.x, st.y = xyAttribute(cx, cy)
			}
		default: // nothing to convert to
		}
	default: // ColorMode never holds the enhanced hue mode
	}
}

// setMireds is matter.js's mireds setter: the value cropped to the
// physical range.
func (s *ColorControlServer) setMireds(st *colorState, mireds float64) {
	if math.IsNaN(mireds) {
		return
	}
	st.mireds = uint16(s.cropMireds(mireds))
}

// xyAttribute is matter.js #setFromXyValue for x and y: the CIE value times
// 65536, rounded and cropped to 0..0xFEFF.
func xyAttribute(x, y float64) (ax, ay uint16) {
	conv := func(v float64) uint16 {
		if math.IsNaN(v) {
			return 0
		}
		return uint16(cropValue(jsRound(v*65536), minCIEXYValue, maxCIEXYValue))
	}
	return conv(x), conv(y)
}

// hueAttribute is matter.js's hue setter: degrees to 0..254.
func hueAttribute(deg float64) uint8 {
	if math.IsNaN(deg) {
		return 0
	}
	return uint8(cropValue(jsRound(deg*254/360), minHueValue, maxHueValue))
}

// saturationAttribute is matter.js's saturation setter: 0..1 to 0..254.
func saturationAttribute(v float64) uint8 {
	if math.IsNaN(v) {
		return 0
	}
	return uint8(cropValue(jsRound(v*254), minSaturationValue, maxSaturationValue))
}

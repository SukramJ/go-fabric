// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package colorcontrol_test

import (
	"math"
	"testing"

	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
)

// TestColorConversionBranches walks the branches the matter.js vectors of
// colorconversion_parity_matterjs_test.go leave out: every hue sector of
// HSVToRGB (and back through RGBToHSV), black and the linear gamma segment
// of RGBToXY, y = 0 and a non-number in XYToRGB, and a mireds value inside
// the lookup table for MiredsToHSV.
func TestColorConversionBranches(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		hue     float64
		r, g, b float64
	}{
		{0, 1, 0, 0}, {60, 1, 1, 0}, {120, 0, 1, 0}, {180, 0, 1, 1}, {240, 0, 0, 1}, {300, 1, 0, 1},
	} {
		r, g, b := ccdef.HSVToRGB(c.hue, 1, 1)
		if r != c.r || g != c.g || b != c.b {
			t.Errorf("HSVToRGB(%v, 1, 1) = %v %v %v, want %v %v %v", c.hue, r, g, b, c.r, c.g, c.b)
		}
		if h, s, v := ccdef.RGBToHSV(r, g, b); math.Abs(h-c.hue) > 1e-9 || s != 1 || v != 1 {
			t.Errorf("RGBToHSV(%v %v %v) = %v %v %v, want %v 1 1", r, g, b, h, s, v, c.hue)
		}
	}
	if x, y := ccdef.RGBToXY(0, 0, 0); x != 0 || y != 0 {
		t.Errorf("RGBToXY(black) = %v %v, want 0 0", x, y)
	}
	if x, y := ccdef.RGBToXY(0.01, 0.01, 0.01); x <= 0 || y <= 0 {
		t.Errorf("RGBToXY on the linear segment = %v %v, want a point", x, y)
	}
	if r, g, b := ccdef.XYToRGB(0.3, 0); math.IsNaN(r+g+b) || r < 0 || g < 0 || b < 0 || max(r, g, b) > 1 {
		t.Errorf("XYToRGB(0.3, 0) = %v %v %v, want components in 0..1", r, g, b)
	}
	if r, g, b := ccdef.XYToRGB(math.NaN(), 0.3); r != 0 || g != 0 || b != 0 {
		t.Errorf("XYToRGB(NaN, 0.3) = %v %v %v, want 0 0 0", r, g, b)
	}
	if h, s, ok := ccdef.MiredsToHSV(250); !ok || h < 0 || h > 360 || s <= 0 || s > 1 {
		t.Errorf("MiredsToHSV(250) = %v %v %v, want a colour", h, s, ok)
	}
}

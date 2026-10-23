// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package colorcontrol_test

import (
	"math"
	"testing"

	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
)

// TestSaturationScalers holds the saturation scalers to matter.js
// ColorControlServer.ts:198-200 (getter: saturation / 254) and :205-211
// (setter: Math.round(v * 254) cropped to 0..0xFE), with percent / 100 as
// the setter's input.
func TestSaturationScalers(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		pct  float64
		attr uint8
	}{
		{0, 0}, {50, 127}, {100, 254}, {120, 254}, {-5, 0}, {0.2, 1}, {0.1, 0}, {math.NaN(), 0},
	} {
		if got := ccdef.SaturationPercentToAttr(c.pct); got != c.attr {
			t.Errorf("SaturationPercentToAttr(%v) = %d, want %d", c.pct, got, c.attr)
		}
	}
	if got := ccdef.AttrToSaturationPercent(127); got != 50 {
		t.Errorf("AttrToSaturationPercent(127) = %v, want 50", got)
	}
	if got := ccdef.AttrToSaturationPercent(254); got != 100 {
		t.Errorf("AttrToSaturationPercent(254) = %v, want 100", got)
	}
	for a := range uint8(255) {
		if got := ccdef.SaturationPercentToAttr(ccdef.AttrToSaturationPercent(a)); got != a {
			t.Errorf("saturation round trip %d -> %d", a, got)
		}
	}
}

// TestCropMireds holds CropMireds to matter.js ColorControlServer.ts
// :1790-1796 #cropColorTemperature, whose bounds fall back to 0 and 0xFEFF
// when the physical ones are 0 (:239-253).
func TestCropMireds(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		mireds, lo, hi, want uint16
	}{
		{250, 153, 500, 250},
		{100, 153, 500, 153},
		{600, 153, 500, 500},
		{0xFFFF, 0, 0, 0xFEFF}, // both unset: MAX_TEMPERATURE_VALUE
		{10, 0, 500, 10},       // unset minimum: 0
		{0xFF00, 153, 0, 0xFEFF},
	} {
		if got := ccdef.CropMireds(c.mireds, c.lo, c.hi); got != c.want {
			t.Errorf("CropMireds(%d, %d, %d) = %d, want %d", c.mireds, c.lo, c.hi, got, c.want)
		}
	}
}

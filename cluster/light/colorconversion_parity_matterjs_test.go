// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"math"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/light"
)

func closeTo(t *testing.T, what string, got, want, delta float64) {
	t.Helper()
	if math.Abs(got-want) > delta {
		t.Errorf("%s = %v, want %v ± %v", what, got, want, delta)
	}
}

// TestColorConversionHSVToRGBAndBack ports matter.js
// packages/node/test/behaviors/color-control/ColorConversionUtilsTest.ts
// "converts hsv to rgb and back", vector for vector.
func TestColorConversionHSVToRGBAndBack(t *testing.T) {
	t.Parallel()
	for _, v := range []struct {
		h, s          float64
		r, g, b       float64
		backH, backS  float64
		hDelta, sDiff float64
	}{
		{0, 1, 255, 0, 0, 0, 1, 0.5, 0.05},
		{120, 1, 0, 255, 0, 120, 1, 0.5, 0.05},
		{240, 1, 0, 0, 255, 240, 1, 0.5, 0.05},
		{317, 1, 255, 0, 183, 317, 1, 0.5, 0.05},
		{152, 1, 0, 255, 136, 152, 1, 0.5, 0.05},
		{123, 0.5, 128, 255, 134, 123, 0.5, 0.5, 0.05},
		{123, 0, 255, 255, 255, 0, 0, 0, 0.05}, // "saturation of 0 means no color"
		{42, 1, 255, 179, 0, 42, 1, 0.5, 0.05},
	} {
		r, g, b := light.HSVToRGB(v.h, v.s, 1)
		closeTo(t, "r", r*255, v.r, 0.5)
		closeTo(t, "g", g*255, v.g, 0.5)
		closeTo(t, "b", b*255, v.b, 0.5)
		h, s, _ := light.RGBToHSV(r, g, b)
		closeTo(t, "h", h, v.backH, v.hDelta)
		closeTo(t, "s", s, v.backS, v.sDiff)
	}
}

// TestColorConversionXYToRGBAndBack ports "converts xy to rgb and back".
func TestColorConversionXYToRGBAndBack(t *testing.T) {
	t.Parallel()
	for _, v := range []struct{ x, y, r, g, b float64 }{
		{0.5, 0.4, 255, 184, 98},
		{0.421, 0.381, 255, 213, 159},
		{0.217, 0.077, 131, 0, 255},
		{0.5621, 0.4166, 255, 166, 0},
		{0.33, 0.33, 255, 249, 248},
	} {
		r, g, b := light.XYToRGB(v.x, v.y)
		closeTo(t, "r", r*255, v.r, 0.5)
		closeTo(t, "g", g*255, v.g, 0.5)
		closeTo(t, "b", b*255, v.b, 0.5)
		x, y := light.RGBToXY(r, g, b)
		closeTo(t, "x", x, v.x, 0.01)
		closeTo(t, "y", y, v.y, 0.01)
	}
}

// TestColorConversionMiredsToXYAndBack ports "converts mireds to xy and
// back": McCamy's approximation and matter.js's Kelvin table.
func TestColorConversionMiredsToXYAndBack(t *testing.T) {
	t.Parallel()
	for _, v := range []struct{ x, y, kelvin float64 }{
		{0.32208, 0.33175, 6000},
		{0.28692, 0.29558, 9000},
	} {
		mireds := light.XYToMireds(v.x, v.y)
		closeTo(t, "kelvin", 1_000_000/mireds, v.kelvin, 30)
		x, y, ok := light.MiredsToXY(mireds)
		if !ok {
			t.Fatalf("MiredsToXY(%v) has no value", mireds)
		}
		closeTo(t, "x", x, v.x, 0.01)
		closeTo(t, "y", y, v.y, 0.01)
	}
}

// TestKelvinTableReach pins the transcription of matter.js kelvinToXy.ts:
// 1000 K to 20000 K every 10 K, each entry shared by the nine Kelvin above
// it (expandLookupTable), nothing outside.
func TestKelvinTableReach(t *testing.T) {
	t.Parallel()
	for _, v := range []struct {
		mireds float64
		x, y   float64
		ok     bool
	}{
		{1000, 0.652750055750174, 0.34446222719737, true}, // 1000 K, the first entry
		{50, 0.256456439408808, 0.257628552153879, true},  // 20000 K, the last
		{250, 0.380438429420364, 0.376746069841299, true}, // 4000 K
		{1001, 0, 0, false}, // 999 K
		{49.9, 0, 0, false}, // 20040 K
		{0, 0, 0, false},    // infinite Kelvin
		{1_000_000.0 / 20009, 0.256456439408808, 0.257628552153879, true}, // 20009 K shares 20000 K's entry
	} {
		x, y, ok := light.MiredsToXY(v.mireds)
		if ok != v.ok || x != v.x || y != v.y {
			t.Errorf("MiredsToXY(%v) = %v, %v, %v; want %v, %v, %v", v.mireds, x, y, ok, v.x, v.y, v.ok)
		}
	}
	if _, _, ok := light.MiredsToHSV(5000); ok {
		t.Error("MiredsToHSV(200 K) has a value")
	}
	if light.MiredsToKelvin(250) != 4000 || light.KelvinToMireds(2700) != 370 {
		t.Error("Kelvin / mireds are 1e6 / value, rounded half up")
	}
}

// TestHSVToXYCrops: hsvToXy crops x and y to 0..1, and the round trip
// through xyToHsv returns the hue and saturation it started from closely.
func TestHSVToXYCrops(t *testing.T) {
	t.Parallel()
	x, y := light.HSVToXY(240, 1)
	if x < 0 || x > 1 || y < 0 || y > 1 {
		t.Fatalf("HSVToXY(240, 1) = %v, %v outside 0..1", x, y)
	}
	h, s := light.XYToHSV(x, y)
	closeTo(t, "hue", h, 240, 1)
	closeTo(t, "saturation", s, 1, 0.01)
	if m := light.HSVToMireds(30, 0.2); m <= 0 || math.IsNaN(m) {
		t.Errorf("HSVToMireds(30, 0.2) = %v", m)
	}
}

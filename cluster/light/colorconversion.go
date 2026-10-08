// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package light

import (
	"math"
)

// The colour-space conversions matter.js's ColorControlServer performs
// itself when a command switches the colour mode — ported from
// packages/node/src/behaviors/color-control/ColorConversionUtils.ts at the
// schema pin, function for function, with JavaScript's arithmetic (its
// Math.round rounds half up, its Number.toFixed is applied where matter.js
// applies it). Hue is in degrees (0..360), saturation and the CIE x/y in
// 0..1, colour temperature in mireds; the server maps them to and from the
// attributes' Matter ranges.

// HSVToXY is matter.js hsvToXy: hue (0..360) and saturation (0..1) at full
// value to CIE x/y, each cropped to 0..1.
func HSVToXY(hue, saturation float64) (x, y float64) {
	r, g, b := HSVToRGB(hue, saturation, 1)
	x, y = RGBToXY(r, g, b)
	return cropValue(x, 0, 1), cropValue(y, 0, 1)
}

// XYToHSV is matter.js xyToHsv: CIE x/y to hue (0..360) and saturation
// (0..1).
func XYToHSV(x, y float64) (hue, saturation float64) {
	r, g, b := XYToRGB(x, y)
	hue, saturation, _ = RGBToHSV(r, g, b)
	return hue, saturation
}

// HSVToMireds is matter.js hsvToMireds: through x/y, [XYToMireds].
func HSVToMireds(hue, saturation float64) float64 {
	x, y := HSVToXY(hue, saturation)
	return XYToMireds(x, y)
}

// MiredsToHSV is matter.js miredsToHsv: through x/y; ok is false where
// [MiredsToXY] has no value (outside 1000 K to 20009 K).
func MiredsToHSV(mireds float64) (hue, saturation float64, ok bool) {
	x, y, ok := MiredsToXY(mireds)
	if !ok {
		return 0, 0, false
	}
	hue, saturation = XYToHSV(x, y)
	return hue, saturation, true
}

// HSVToRGB is matter.js hsvToRgb: h in 0..360, s and v in 0..1, to r, g, b
// in 0..1.
func HSVToRGB(h, s, v float64) (r, g, b float64) {
	h /= 360
	i := math.Floor(h * 6)
	f := h*6 - i
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)
	switch jsMod(i, 6) {
	case 0:
		return v, t, p
	case 1:
		return q, v, p
	case 2:
		return p, v, t
	case 3:
		return p, q, v
	case 4:
		return t, p, v
	default:
		return v, p, q
	}
}

// RGBToHSV is matter.js rgbToHsv: r, g, b in 0..1 to h in 0..360, s and v
// in 0..1.
func RGBToHSV(r, g, b float64) (h, s, v float64) {
	maxV := max(r, g, b)
	minV := min(r, g, b)
	d := maxV - minV
	if maxV != 0 {
		s = d / maxV
	}
	v = maxV
	// matter.js switches on max with the cases min, r, g, b in that order.
	switch maxV {
	case minV:
		h = 0
	case r:
		h = g - b
		if g < b {
			h += d * 6
		}
		h /= 6 * d
	case g:
		h = (b - r + d*2) / (6 * d)
	default:
		h = (r - g + d*4) / (6 * d)
	}
	return h * 360, s, v
}

// RGBToXY is matter.js rgbToXy: gamma-corrected r, g, b in 0..1 to CIE x/y
// through the Wide RGB D65 matrix; black is (0, 0).
func RGBToXY(r, g, b float64) (x, y float64) {
	gamma := func(c float64) float64 {
		if c > 0.04045 {
			return math.Pow((c+0.055)/(1.0+0.055), 2.4)
		}
		return c / 12.92
	}
	r, g, b = gamma(r), gamma(g), gamma(b)
	bigX := r*0.664511 + g*0.154324 + b*0.162028
	bigY := r*0.283881 + g*0.668433 + b*0.047685
	bigZ := r*0.000088 + g*0.07231 + b*0.986039
	sum := bigX + bigY + bigZ
	if sum == 0 {
		return 0, 0
	}
	return bigX / sum, bigY / sum
}

// XYToRGB is matter.js xyToRgb: CIE x/y at full brightness to r, g, b in
// 0..1 through the Wide RGB D65 matrix, reverse gamma, negatives raised
// to 0 and the components scaled down by the largest above 1.
func XYToRGB(x, y float64) (r, g, b float64) {
	if y == 0 {
		y = 0.00000000001
	}
	z := 1.0 - x - y
	const bigY = 1.0 // matter.js: the brightness 254 over 254, to two decimals
	bigX := (bigY / y) * x
	bigZ := (bigY / y) * z
	rgb := [3]float64{
		bigX*1.656492 - bigY*0.354851 - bigZ*0.255038,
		-bigX*0.707196 + bigY*1.655397 + bigZ*0.036152,
		bigX*0.051713 - bigY*0.121364 + bigZ*1.01153,
	}
	for i, c := range rgb {
		if c <= 0.0031308 {
			c *= 12.92
		} else {
			c = (1.0+0.055)*math.Pow(c, 1.0/2.4) - 0.055
		}
		rgb[i] = max(0, c)
	}
	if m := max(rgb[0], rgb[1], rgb[2]); m > 1 {
		for i := range rgb {
			rgb[i] /= m
		}
	}
	for i, c := range rgb {
		if math.IsNaN(c) || math.IsInf(c, 0) || c < 0 {
			rgb[i] = 0
		}
	}
	return rgb[0], rgb[1], rgb[2]
}

// MiredsToKelvin is matter.js miredsToKelvin: Math.round(1e6 / mireds).
func MiredsToKelvin(mireds float64) float64 { return jsRound(1_000_000 / mireds) }

// KelvinToMireds is matter.js kelvinToMireds: Math.round(1e6 / kelvin).
func KelvinToMireds(kelvin float64) float64 { return jsRound(1_000_000 / kelvin) }

// XYToMireds is matter.js xyToMireds: McCamy's approximation of the
// correlated colour temperature of x/y, in mireds.
func XYToMireds(x, y float64) float64 {
	n := (x - 0.332) / (0.1858 - y)
	kelvin := math.Abs(437*math.Pow(n, 3) + 3601*math.Pow(n, 2) + 6861*n + 5517) //nolint:staticcheck // matter.js Math.pow, kept for its rounding
	return KelvinToMireds(kelvin)
}

// MiredsToXY is matter.js miredsToXy: the x/y of the colour temperature,
// looked up in matter.js's table (kelvinToXyLookup); ok is false outside
// 1000 K to 20009 K, the table's reach.
func MiredsToXY(mireds float64) (x, y float64, ok bool) {
	xy, ok := kelvinToXY(MiredsToKelvin(mireds))
	return xy[0], xy[1], ok
}

// kelvinToXY is matter.js kelvinToXyLookup: the table holds every 10 K,
// and the nine Kelvin above each entry share its value (expandLookupTable).
func kelvinToXY(kelvin float64) ([2]float64, bool) {
	k := jsRound(kelvin)
	if math.IsNaN(k) || k < 1000 || k >= 1000+10*float64(len(kelvinToXYTable)) {
		return [2]float64{}, false
	}
	return kelvinToXYTable[int((k-1000)/10)], true
}

// jsRound is JavaScript's Math.round: half rounds up.
func jsRound(v float64) float64 { return math.Floor(v + 0.5) }

// jsMod is JavaScript's % on a whole number: the sign follows the dividend.
func jsMod(a, b float64) float64 { return math.Mod(a, b) }

// cropValue is matter.js cropValueRange.
func cropValue(v, lo, hi float64) float64 { return min(max(v, lo), hi) }

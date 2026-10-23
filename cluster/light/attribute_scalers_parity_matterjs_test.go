// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light_test

import (
	"math"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/light"
)

// TestHueScalers holds the hue scalers to matter.js ColorControlServer.ts
// :174-176 (getter: hue * 360 / 254) and :181-183 (setter:
// Math.round(deg * 254 / 360) cropped to 0..0xFE).
func TestHueScalers(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		deg  float64
		attr uint8
	}{
		{0, 0},
		{1, 1},     // 0.7056 rounds up
		{0.7, 0},   // 0.4939 rounds down
		{180, 127}, // exactly 127
		{359, 253}, // 253.29
		{360, 254},
		{400, 254}, // cropped to MAX_HUE_VALUE
		{-10, 0},   // cropped to MIN_HUE_VALUE
		{math.NaN(), 0},
	} {
		if got := light.HueDegreesToAttr(c.deg); got != c.attr {
			t.Errorf("HueDegreesToAttr(%v) = %d, want %d", c.deg, got, c.attr)
		}
	}
	for _, c := range []struct {
		attr uint8
		deg  float64
	}{
		{0, 0}, {127, 180}, {254, 360}, {255, 255 * 360.0 / 254},
	} {
		if got := light.AttrToHueDegrees(c.attr); got != c.deg {
			t.Errorf("AttrToHueDegrees(%d) = %v, want %v", c.attr, got, c.deg)
		}
	}
	// The round trip of every attribute value is the identity.
	for a := range uint8(255) {
		if got := light.HueDegreesToAttr(light.AttrToHueDegrees(a)); got != a {
			t.Errorf("hue round trip %d -> %d", a, got)
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"math"
	"testing"

	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
)

// TestUnitScaling holds LevelFromUnit / UnitFromLevel to the CurrentLevel
// range 0..254 (level-control.element.ts:74), rounded half up as matter.js
// Math.round.
func TestUnitScaling(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		u    float64
		want uint8
	}{
		{0, 0}, {1, 254}, {0.5, 127}, {1.5, 254}, {-1, 0}, {math.NaN(), 0}, {1.0 / 254, 1}, {0.5 / 254, 1},
	} {
		if got := lvldef.LevelFromUnit(c.u); got != c.want {
			t.Errorf("LevelFromUnit(%v) = %d, want %d", c.u, got, c.want)
		}
	}
	for l := range uint8(255) {
		if got := lvldef.LevelFromUnit(lvldef.UnitFromLevel(l)); got != l {
			t.Errorf("round trip %d -> %d", l, got)
		}
	}
	if got := lvldef.UnitFromLevel(127); got != 0.5 {
		t.Errorf("UnitFromLevel(127) = %v, want 0.5", got)
	}
}
